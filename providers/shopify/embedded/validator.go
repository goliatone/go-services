package embedded

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	if v == nil {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "validator_not_configured",
			Field: "validator",
			Cause: ErrInvalidSessionToken,
		}
	}
	secret := strings.TrimSpace(v.config.AppSecret)
	if secret == "" {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "app_secret_required",
			Field: "app_secret",
			Cause: ErrInvalidSessionToken,
		}
	}
	clientID := strings.TrimSpace(v.config.ClientID)
	if clientID == "" {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "client_id_required",
			Field: "client_id",
			Cause: ErrInvalidAudience,
		}
	}
	token := strings.TrimSpace(req.SessionToken)
	if token == "" {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "session_token_required",
			Field: "session_token",
			Cause: ErrInvalidSessionToken,
		}
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "token_malformed",
			Field: "session_token",
			Cause: ErrInvalidSessionToken,
		}
	}

	header, err := decodeJWTSection(parts[0])
	if err != nil {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "header_decode_failed",
			Field: "header",
			Cause: ErrInvalidSessionToken,
		}
	}
	alg := strings.ToUpper(strings.TrimSpace(readAnyString(header["alg"])))
	if alg != "HS256" {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "unsupported_alg",
			Field: "alg",
			Cause: ErrUnsupportedJWTAlgorithm,
		}
	}

	signatureRaw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "signature_decode_failed",
			Field: "signature",
			Cause: ErrInvalidSessionToken,
		}
	}
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingInput))
	if !hmac.Equal(mac.Sum(nil), signatureRaw) {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "signature_mismatch",
			Field: "signature",
			Cause: ErrInvalidSessionToken,
		}
	}

	payload, err := decodeJWTSection(parts[1])
	if err != nil {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "payload_decode_failed",
			Field: "payload",
			Cause: ErrInvalidSessionToken,
		}
	}

	issuer := strings.TrimSpace(readAnyString(payload["iss"]))
	issuerURL, err := parseHTTPSURL(issuer)
	if err != nil {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "invalid_iss",
			Field: "iss",
			Cause: ErrInvalidSessionToken,
		}
	}
	destination := strings.TrimSpace(readAnyString(payload["dest"]))
	destinationURL, err := parseHTTPSURL(destination)
	if err != nil {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "invalid_dest",
			Field: "dest",
			Cause: ErrInvalidDestination,
		}
	}
	if !validIssuerPath(issuerURL.Path) {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "invalid_iss_path",
			Field: "iss",
			Cause: ErrInvalidSessionToken,
		}
	}
	if !validDestinationPath(destinationURL.Path) {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "invalid_dest_path",
			Field: "dest",
			Cause: ErrInvalidDestination,
		}
	}

	issuerShop, err := normalizeShopDomainStrict(issuerURL.Hostname())
	if err != nil {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "invalid_iss_host",
			Field: "iss",
			Cause: ErrInvalidSessionToken,
		}
	}
	destinationShop, err := normalizeShopDomainStrict(destinationURL.Hostname())
	if err != nil {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "invalid_dest_host",
			Field: "dest",
			Cause: ErrInvalidDestination,
		}
	}
	if !strings.EqualFold(issuerShop, destinationShop) {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "issuer_destination_mismatch",
			Field: "iss,dest",
			Cause: ErrInvalidDestination,
		}
	}

	if !audienceContains(payload["aud"], clientID) {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "aud_mismatch",
			Field: "aud",
			Cause: ErrInvalidAudience,
		}
	}

	exp, err := parseUnixClaim(payload["exp"])
	if err != nil {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "invalid_exp",
			Field: "exp",
			Cause: ErrInvalidSessionToken,
		}
	}
	nbf, err := parseUnixClaim(payload["nbf"])
	if err != nil {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "invalid_nbf",
			Field: "nbf",
			Cause: ErrInvalidSessionToken,
		}
	}
	iat, err := parseUnixClaim(payload["iat"])
	if err != nil {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "invalid_iat",
			Field: "iat",
			Cause: ErrInvalidSessionToken,
		}
	}

	jti := strings.TrimSpace(readAnyString(payload["jti"]))
	if jti == "" {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "missing_jti",
			Field: "jti",
			Cause: ErrMissingJTI,
		}
	}

	expectedShop := strings.TrimSpace(req.ExpectedShopDomain)
	if expectedShop != "" {
		normalizedExpectedShop, expectedErr := normalizeShopDomain(expectedShop)
		if expectedErr != nil {
			return core.EmbeddedSessionClaims{}, &ValidationError{
				Code:  "invalid_expected_shop",
				Field: "expected_shop_domain",
				Cause: ErrInvalidSessionToken,
			}
		}
		if !strings.EqualFold(normalizedExpectedShop, destinationShop) {
			return core.EmbeddedSessionClaims{}, &ValidationError{
				Code:  "shop_mismatch",
				Field: "expected_shop_domain",
				Cause: ErrInvalidDestination,
			}
		}
	}

	now := v.config.Now().UTC()
	if now.After(exp.Add(v.config.ClockSkew)) {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "token_expired",
			Field: "exp",
			Cause: ErrInvalidSessionToken,
		}
	}
	if now.Add(v.config.ClockSkew).Before(nbf) {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "token_not_active",
			Field: "nbf",
			Cause: ErrInvalidSessionToken,
		}
	}
	if now.Add(v.config.ClockSkew).Before(iat) {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "issued_in_future",
			Field: "iat",
			Cause: ErrInvalidSessionToken,
		}
	}
	if exp.Add(v.config.ClockSkew).Before(iat) {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "invalid_time_window",
			Field: "iat,exp",
			Cause: ErrInvalidSessionToken,
		}
	}
	if v.config.MaxIssuedAtAge > 0 && now.After(iat.Add(v.config.MaxIssuedAtAge+v.config.ClockSkew)) {
		return core.EmbeddedSessionClaims{}, &ValidationError{
			Code:  "issued_too_old",
			Field: "iat",
			Cause: ErrInvalidSessionToken,
		}
	}

	return core.EmbeddedSessionClaims{
		Issuer:      issuer,
		Destination: destination,
		Audience:    clientID,
		Subject:     strings.TrimSpace(readAnyString(payload["sub"])),
		JTI:         jti,
		ShopDomain:  destinationShop,
		IssuedAt:    iat,
		NotBefore:   nbf,
		ExpiresAt:   exp,
		Raw:         copyAnyMap(payload),
	}, nil
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
	for key, value := range input {
		out[key] = value
	}
	return out
}

var _ SessionTokenValidator = (*HMACSessionTokenValidator)(nil)
