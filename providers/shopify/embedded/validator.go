package embedded

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

const defaultClockSkew = 30 * time.Second
const defaultMaxIssuedAtAge = 15 * time.Minute
const defaultShopifyDomainSuffix = ".myshopify.com"

type SessionTokenValidatorConfig struct {
	AppSecret      string
	ClientID       string
	ClockSkew      time.Duration
	MaxIssuedAtAge time.Duration
	Now            func() time.Time
}

type HMACSessionTokenValidator struct {
	config SessionTokenValidatorConfig
}

func NewSessionTokenValidator(cfg SessionTokenValidatorConfig) *HMACSessionTokenValidator {
	clockSkew := cfg.ClockSkew
	if clockSkew <= 0 {
		clockSkew = defaultClockSkew
	}
	maxIssuedAtAge := cfg.MaxIssuedAtAge
	if maxIssuedAtAge <= 0 {
		maxIssuedAtAge = defaultMaxIssuedAtAge
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &HMACSessionTokenValidator{
		config: SessionTokenValidatorConfig{
			AppSecret:      strings.TrimSpace(cfg.AppSecret),
			ClientID:       strings.TrimSpace(cfg.ClientID),
			ClockSkew:      clockSkew,
			MaxIssuedAtAge: maxIssuedAtAge,
			Now:            now,
		},
	}
}

func (v *HMACSessionTokenValidator) ValidateSessionToken(
	_ context.Context,
	req ValidateSessionTokenRequest,
) (core.EmbeddedSessionClaims, error) {
	secret, clientID, parts, err := v.validationInputs(req.SessionToken)
	if err != nil {
		return core.EmbeddedSessionClaims{}, err
	}
	payload, err := validateTokenEnvelope(parts, secret)
	if err != nil {
		return core.EmbeddedSessionClaims{}, err
	}
	identity, err := validateTokenIdentity(payload, clientID, req.ExpectedShopDomain)
	if err != nil {
		return core.EmbeddedSessionClaims{}, err
	}
	times, err := parseTokenTimes(payload)
	if err != nil {
		return core.EmbeddedSessionClaims{}, err
	}
	if err := validateTokenTimes(v.config, times); err != nil {
		return core.EmbeddedSessionClaims{}, err
	}

	return core.EmbeddedSessionClaims{
		Issuer:      identity.issuer,
		Destination: identity.destination,
		Audience:    clientID,
		Subject:     strings.TrimSpace(readAnyString(payload["sub"])),
		JTI:         identity.jti,
		ShopDomain:  identity.shop,
		IssuedAt:    times.issuedAt,
		NotBefore:   times.notBefore,
		ExpiresAt:   times.expiresAt,
		Raw:         copyAnyMap(payload),
	}, nil
}

func validationFailure(code, field string, cause error) error {
	return &ValidationError{Code: code, Field: field, Cause: cause}
}

func (v *HMACSessionTokenValidator) validationInputs(token string) (string, string, []string, error) {
	if v == nil {
		return "", "", nil, validationFailure("validator_not_configured", "validator", ErrInvalidSessionToken)
	}
	secret := strings.TrimSpace(v.config.AppSecret)
	if secret == "" {
		return "", "", nil, validationFailure("app_secret_required", "app_secret", ErrInvalidSessionToken)
	}
	clientID := strings.TrimSpace(v.config.ClientID)
	if clientID == "" {
		return "", "", nil, validationFailure("client_id_required", "client_id", ErrInvalidAudience)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", "", nil, validationFailure("session_token_required", "session_token", ErrInvalidSessionToken)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", nil, validationFailure("token_malformed", "session_token", ErrInvalidSessionToken)
	}
	return secret, clientID, parts, nil
}

func validateTokenEnvelope(parts []string, secret string) (map[string]any, error) {
	header, err := decodeJWTSection(parts[0])
	if err != nil {
		return nil, validationFailure("header_decode_failed", "header", ErrInvalidSessionToken)
	}
	if alg := strings.ToUpper(strings.TrimSpace(readAnyString(header["alg"]))); alg != "HS256" {
		return nil, validationFailure("unsupported_alg", "alg", ErrUnsupportedJWTAlgorithm)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, validationFailure("signature_decode_failed", "signature", ErrInvalidSessionToken)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(mac.Sum(nil), signature) {
		return nil, validationFailure("signature_mismatch", "signature", ErrInvalidSessionToken)
	}
	payload, err := decodeJWTSection(parts[1])
	if err != nil {
		return nil, validationFailure("payload_decode_failed", "payload", ErrInvalidSessionToken)
	}
	return payload, nil
}

type tokenIdentity struct {
	issuer      string
	destination string
	shop        string
	jti         string
}

func validateTokenIdentity(payload map[string]any, clientID, expectedShop string) (tokenIdentity, error) {
	issuer, destination, destinationShop, err := validateTokenDestinations(payload)
	if err != nil {
		return tokenIdentity{}, err
	}
	if !audienceContains(payload["aud"], clientID) {
		return tokenIdentity{}, validationFailure("aud_mismatch", "aud", ErrInvalidAudience)
	}
	jti := strings.TrimSpace(readAnyString(payload["jti"]))
	if jti == "" {
		return tokenIdentity{}, validationFailure("missing_jti", "jti", ErrMissingJTI)
	}
	if err := validateExpectedShop(expectedShop, destinationShop); err != nil {
		return tokenIdentity{}, err
	}
	return tokenIdentity{issuer: issuer, destination: destination, shop: destinationShop, jti: jti}, nil
}

func validateTokenDestinations(payload map[string]any) (string, string, string, error) {
	issuer := strings.TrimSpace(readAnyString(payload["iss"]))
	issuerURL, err := parseHTTPSURL(issuer)
	if err != nil {
		return "", "", "", validationFailure("invalid_iss", "iss", ErrInvalidSessionToken)
	}
	destination := strings.TrimSpace(readAnyString(payload["dest"]))
	destinationURL, err := parseHTTPSURL(destination)
	if err != nil {
		return "", "", "", validationFailure("invalid_dest", "dest", ErrInvalidDestination)
	}
	if !validIssuerPath(issuerURL.Path) {
		return "", "", "", validationFailure("invalid_iss_path", "iss", ErrInvalidSessionToken)
	}
	if !validDestinationPath(destinationURL.Path) {
		return "", "", "", validationFailure("invalid_dest_path", "dest", ErrInvalidDestination)
	}
	issuerShop, err := normalizeShopDomainStrict(issuerURL.Hostname())
	if err != nil {
		return "", "", "", validationFailure("invalid_iss_host", "iss", ErrInvalidSessionToken)
	}
	destinationShop, err := normalizeShopDomainStrict(destinationURL.Hostname())
	if err != nil {
		return "", "", "", validationFailure("invalid_dest_host", "dest", ErrInvalidDestination)
	}
	if !strings.EqualFold(issuerShop, destinationShop) {
		return "", "", "", validationFailure("issuer_destination_mismatch", "iss,dest", ErrInvalidDestination)
	}
	return issuer, destination, destinationShop, nil
}

func validateExpectedShop(expectedShop, destinationShop string) error {
	expectedShop = strings.TrimSpace(expectedShop)
	if expectedShop == "" {
		return nil
	}
	normalized, err := normalizeShopDomain(expectedShop)
	if err != nil {
		return validationFailure("invalid_expected_shop", "expected_shop_domain", ErrInvalidSessionToken)
	}
	if !strings.EqualFold(normalized, destinationShop) {
		return validationFailure("shop_mismatch", "expected_shop_domain", ErrInvalidDestination)
	}
	return nil
}

type tokenTimes struct {
	expiresAt time.Time
	notBefore time.Time
	issuedAt  time.Time
}

func parseTokenTimes(payload map[string]any) (tokenTimes, error) {
	expiresAt, err := parseUnixClaim(payload["exp"])
	if err != nil {
		return tokenTimes{}, validationFailure("invalid_exp", "exp", ErrInvalidSessionToken)
	}
	notBefore, err := parseUnixClaim(payload["nbf"])
	if err != nil {
		return tokenTimes{}, validationFailure("invalid_nbf", "nbf", ErrInvalidSessionToken)
	}
	issuedAt, err := parseUnixClaim(payload["iat"])
	if err != nil {
		return tokenTimes{}, validationFailure("invalid_iat", "iat", ErrInvalidSessionToken)
	}
	return tokenTimes{expiresAt: expiresAt, notBefore: notBefore, issuedAt: issuedAt}, nil
}

func validateTokenTimes(config SessionTokenValidatorConfig, times tokenTimes) error {
	now := config.Now().UTC()
	if now.After(times.expiresAt.Add(config.ClockSkew)) {
		return validationFailure("token_expired", "exp", ErrInvalidSessionToken)
	}
	if now.Add(config.ClockSkew).Before(times.notBefore) {
		return validationFailure("token_not_active", "nbf", ErrInvalidSessionToken)
	}
	if now.Add(config.ClockSkew).Before(times.issuedAt) {
		return validationFailure("issued_in_future", "iat", ErrInvalidSessionToken)
	}
	if times.expiresAt.Add(config.ClockSkew).Before(times.issuedAt) {
		return validationFailure("invalid_time_window", "iat,exp", ErrInvalidSessionToken)
	}
	if config.MaxIssuedAtAge > 0 && now.After(times.issuedAt.Add(config.MaxIssuedAtAge+config.ClockSkew)) {
		return validationFailure("issued_too_old", "iat", ErrInvalidSessionToken)
	}
	return nil
}

func decodeJWTSection(section string) (map[string]any, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(section))
	if err != nil {
		return nil, err
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func parseUnixClaim(value any) (time.Time, error) {
	switch typed := value.(type) {
	case float64:
		return time.Unix(int64(typed), 0).UTC(), nil
	case int64:
		return time.Unix(typed, 0).UTC(), nil
	case int:
		return time.Unix(int64(typed), 0).UTC(), nil
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return time.Time{}, err
		}
		return time.Unix(parsed, 0).UTC(), nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return time.Time{}, err
		}
		return time.Unix(parsed, 0).UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported unix claim type %T", value)
	}
}

func parseHTTPSURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return nil, err
	}
	if parsed == nil {
		return nil, fmt.Errorf("url is nil")
	}
	if !strings.EqualFold(strings.TrimSpace(parsed.Scheme), "https") {
		return nil, fmt.Errorf("https scheme required")
	}
	if strings.TrimSpace(parsed.Hostname()) == "" {
		return nil, fmt.Errorf("host is required")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("userinfo is not allowed")
	}
	if strings.TrimSpace(parsed.Port()) != "" {
		return nil, fmt.Errorf("port is not allowed")
	}
	if strings.TrimSpace(parsed.RawQuery) != "" || strings.TrimSpace(parsed.Fragment) != "" {
		return nil, fmt.Errorf("query and fragment are not allowed")
	}
	return parsed, nil
}

func audienceContains(value any, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return false
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) == expected
	case []any:
		for _, item := range typed {
			if strings.TrimSpace(readAnyString(item)) == expected {
				return true
			}
		}
	case []string:
		for _, item := range typed {
			if strings.TrimSpace(item) == expected {
				return true
			}
		}
	}
	return false
}

func normalizeShopDomain(value string) (string, error) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return "", fmt.Errorf("shop domain is required")
	}
	if strings.Contains(trimmed, "://") {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return "", err
		}
		trimmed = strings.TrimSpace(strings.ToLower(parsed.Hostname()))
	}
	host, _, splitErr := net.SplitHostPort(trimmed)
	if splitErr == nil {
		trimmed = strings.TrimSpace(host)
	}
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" || strings.Contains(trimmed, "/") {
		return "", fmt.Errorf("invalid shop domain")
	}
	if !strings.Contains(trimmed, ".") {
		trimmed += defaultShopifyDomainSuffix
	}
	if !strings.HasSuffix(trimmed, defaultShopifyDomainSuffix) {
		return "", fmt.Errorf("shop domain must end with %q", defaultShopifyDomainSuffix)
	}
	return trimmed, nil
}

func normalizeShopDomainStrict(value string) (string, error) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return "", fmt.Errorf("shop domain is required")
	}
	if strings.Contains(trimmed, "://") {
		return "", fmt.Errorf("shop domain must not include scheme")
	}
	if strings.Contains(trimmed, "/") {
		return "", fmt.Errorf("shop domain must not include path")
	}
	if !strings.Contains(trimmed, ".") {
		return "", fmt.Errorf("shop domain must include a domain suffix")
	}
	if !strings.HasSuffix(trimmed, defaultShopifyDomainSuffix) {
		return "", fmt.Errorf("shop domain must end with %q", defaultShopifyDomainSuffix)
	}
	return trimmed, nil
}

func validIssuerPath(path string) bool {
	trimmed := strings.TrimSpace(path)
	return trimmed == "/admin" || trimmed == "/admin/"
}

func validDestinationPath(path string) bool {
	trimmed := strings.TrimSpace(path)
	return trimmed == "" || trimmed == "/"
}

func readAnyString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		if value == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func copyAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	maps.Copy(out, input)
	return out
}

var _ SessionTokenValidator = (*HMACSessionTokenValidator)(nil)
