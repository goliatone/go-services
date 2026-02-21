package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-services/core"
)

const (
	defaultRequestTimeout   = 10 * time.Second
	maxProfileResponseBytes = 1 << 20 // 1 MiB
	googleIssuer            = "https://accounts.google.com"
	githubIssuer            = "https://github.com"
	googleUserInfoURL       = "https://openidconnect.googleapis.com/v1/userinfo"
	githubUserInfoURL       = "https://api.github.com/user"
)

var ErrProfileNotFound = errors.New("identity: profile not found")

type ProfileNotFoundError struct {
	Cause error
}

func (e *ProfileNotFoundError) Error() string {
	if e == nil || e.Cause == nil {
		return ErrProfileNotFound.Error()
	}
	return ErrProfileNotFound.Error() + ": " + e.Cause.Error()
}

func (e *ProfileNotFoundError) Unwrap() error {
	if e == nil {
		return nil
	}
	if e.Cause == nil {
		return ErrProfileNotFound
	}
	return errors.Join(ErrProfileNotFound, e.Cause)
}

func (e *ProfileNotFoundError) ToServiceError() *goerrors.Error {
	message := ErrProfileNotFound.Error()
	if e != nil && e.Cause != nil {
		message = e.Error()
	}
	return goerrors.New(message, goerrors.CategoryNotFound).
		WithCode(http.StatusNotFound).
		WithTextCode(core.ServiceErrorProfileNotFound)
}

func profileNotFound(cause error) error {
	return &ProfileNotFoundError{Cause: cause}
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type UserProfile struct {
	ProviderID    string
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	GivenName     string
	FamilyName    string
	PictureURL    string
	Locale        string
	Raw           map[string]any
}

func (p UserProfile) ExternalAccountID() string {
	subject := strings.TrimSpace(p.Subject)
	if subject == "" {
		return ""
	}
	issuer := strings.TrimSpace(p.Issuer)
	if issuer == "" {
		return subject
	}
	return issuer + "|" + subject
}

func (p UserProfile) Map() map[string]any {
	metadata := map[string]any{
		"provider_id":    strings.TrimSpace(p.ProviderID),
		"issuer":         strings.TrimSpace(p.Issuer),
		"subject":        strings.TrimSpace(p.Subject),
		"external_id":    strings.TrimSpace(p.ExternalAccountID()),
		"email":          strings.TrimSpace(p.Email),
		"email_verified": p.EmailVerified,
		"name":           strings.TrimSpace(p.Name),
		"given_name":     strings.TrimSpace(p.GivenName),
		"family_name":    strings.TrimSpace(p.FamilyName),
		"picture_url":    strings.TrimSpace(p.PictureURL),
		"locale":         strings.TrimSpace(p.Locale),
	}
	if len(p.Raw) > 0 {
		metadata["raw"] = copyMap(p.Raw)
	}
	return metadata
}

type ProfileResolver interface {
	Resolve(ctx context.Context, providerID string, cred core.ActiveCredential, metadata map[string]any) (UserProfile, error)
}

type ProfileNormalizer func(providerID string, issuer string, payload map[string]any) UserProfile

type IDTokenVerifier func(
	ctx context.Context,
	providerID string,
	idToken string,
	metadata map[string]any,
) (map[string]any, error)

type ProviderUserInfoConfig struct {
	URL        string
	Issuer     string
	Normalizer ProfileNormalizer
}

type Config struct {
	HTTPClient       HTTPDoer
	RequestTimeout   time.Duration
	IDTokenVerifier  IDTokenVerifier
	ProviderUserInfo map[string]ProviderUserInfoConfig
}

type Resolver struct {
	httpClient       HTTPDoer
	requestTimeout   time.Duration
	idTokenVerifier  IDTokenVerifier
	providerUserInfo map[string]ProviderUserInfoConfig
}

func NewResolver(cfg Config) *Resolver {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	requestTimeout := cfg.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout
	}

	providerUserInfo := defaultProviderUserInfoConfigs()
	for key, value := range cfg.ProviderUserInfo {
		normalizedID := normalizeProviderID(key)
		if normalizedID == "" {
			continue
		}
		providerUserInfo[normalizedID] = ProviderUserInfoConfig{
			URL:        strings.TrimSpace(value.URL),
			Issuer:     strings.TrimSpace(value.Issuer),
			Normalizer: value.Normalizer,
		}
	}

	return &Resolver{
		httpClient:       httpClient,
		requestTimeout:   requestTimeout,
		idTokenVerifier:  cfg.IDTokenVerifier,
		providerUserInfo: providerUserInfo,
	}
}

func DefaultResolver() *Resolver {
	return NewResolver(Config{})
}

func (r *Resolver) Resolve(ctx context.Context, providerID string, cred core.ActiveCredential, metadata map[string]any) (UserProfile, error) {
	if r == nil {
		return UserProfile{}, profileNotFound(nil)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	normalizedProviderID := normalizeProviderID(providerID)
	mergedMetadata := mergeMetadata(cred.Metadata, metadata)

	profile, tokenErr := r.profileFromIDToken(ctx, normalizedProviderID, mergedMetadata)
	if tokenErr == nil && strings.TrimSpace(profile.Subject) != "" {
		return profile, nil
	}

	endpointConfig, hasProviderEndpoint := r.providerUserInfo[normalizedProviderID]
	userInfoURL := strings.TrimSpace(readString(mergedMetadata["userinfo_endpoint"]))
	if userInfoURL == "" && hasProviderEndpoint {
		userInfoURL = strings.TrimSpace(endpointConfig.URL)
	}
	if userInfoURL == "" {
		if tokenErr != nil {
			return UserProfile{}, profileNotFound(tokenErr)
		}
		return UserProfile{}, profileNotFound(nil)
	}

	payload, fetchErr := r.fetchUserInfo(ctx, userInfoURL, strings.TrimSpace(cred.AccessToken))
	if fetchErr != nil {
		return UserProfile{}, profileNotFound(fetchErr)
	}

	issuer := strings.TrimSpace(readString(payload["iss"]))
	if issuer == "" {
		issuer = strings.TrimSpace(endpointConfig.Issuer)
	}
	if issuer == "" {
		issuer = defaultIssuerForProvider(normalizedProviderID)
	}
	normalizer := endpointConfig.Normalizer
	if normalizer == nil {
		normalizer = normalizeOIDCProfile
	}
	profile = normalizer(normalizedProviderID, issuer, payload)
	if strings.TrimSpace(profile.Subject) == "" {
		return UserProfile{}, profileNotFound(nil)
	}
	return profile, nil
}

func defaultProviderUserInfoConfigs() map[string]ProviderUserInfoConfig {
	return map[string]ProviderUserInfoConfig{
		"google_calendar": {
			URL:    googleUserInfoURL,
			Issuer: googleIssuer,
		},
		"google_docs": {
			URL:    googleUserInfoURL,
			Issuer: googleIssuer,
		},
		"google_drive": {
			URL:    googleUserInfoURL,
			Issuer: googleIssuer,
		},
		"google_gmail": {
			URL:    googleUserInfoURL,
			Issuer: googleIssuer,
		},
		"github": {
			URL:        githubUserInfoURL,
			Issuer:     githubIssuer,
			Normalizer: normalizeGitHubProfile,
		},
	}
}

func (r *Resolver) fetchUserInfo(ctx context.Context, endpoint string, accessToken string) (map[string]any, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, fmt.Errorf("identity: access token is required")
	}
	requestCtx := ctx
	cancel := func() {}
	if r.requestTimeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, r.requestTimeout)
	}
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	res, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(res.Body, maxProfileResponseBytes+1))
	if readErr != nil {
		return nil, fmt.Errorf("identity: read profile response: %w", readErr)
	}
	if int64(len(body)) > maxProfileResponseBytes {
		return nil, fmt.Errorf("identity: profile response exceeds %d bytes", maxProfileResponseBytes)
	}
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("identity: profile endpoint returned status %d", res.StatusCode)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("identity: decode profile response: %w", err)
	}
	return payload, nil
}

func (r *Resolver) profileFromIDToken(ctx context.Context, providerID string, metadata map[string]any) (UserProfile, error) {
	idToken := strings.TrimSpace(readString(metadata["id_token"]))
	if idToken == "" {
		return UserProfile{}, fmt.Errorf("identity: id_token is required")
	}
	payload, err := r.decodeVerifiedOrRawIDToken(ctx, providerID, idToken, metadata)
	if err != nil {
		return UserProfile{}, err
	}
	issuer := strings.TrimSpace(readString(payload["iss"]))
	if issuer == "" {
		issuer = defaultIssuerForProvider(providerID)
	}
	profile := normalizeOIDCProfile(providerID, issuer, payload)
	if strings.TrimSpace(profile.Subject) == "" {
		return UserProfile{}, fmt.Errorf("identity: id_token is missing subject")
	}
	return profile, nil
}

func (r *Resolver) decodeVerifiedOrRawIDToken(
	ctx context.Context,
	providerID string,
	idToken string,
	metadata map[string]any,
) (map[string]any, error) {
	if r != nil && r.idTokenVerifier != nil {
		claims, err := r.idTokenVerifier(ctx, providerID, idToken, copyMap(metadata))
		if err != nil {
			return nil, fmt.Errorf("identity: verify id_token: %w", err)
		}
		return copyMap(claims), nil
	}
	return decodeJWTPayload(idToken)
}

func decodeJWTPayload(token string) (map[string]any, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("identity: invalid id_token format")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("identity: decode id_token payload: %w", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil, fmt.Errorf("identity: decode id_token claims: %w", err)
	}
	return payload, nil
}

func normalizeOIDCProfile(providerID string, issuer string, payload map[string]any) UserProfile {
	profile := UserProfile{
		ProviderID:    normalizeProviderID(providerID),
		Issuer:        strings.TrimSpace(issuer),
		Subject:       strings.TrimSpace(readString(payload["sub"])),
		Email:         strings.TrimSpace(readString(payload["email"])),
		EmailVerified: readBool(payload["email_verified"]),
		Name:          strings.TrimSpace(readString(payload["name"])),
		GivenName:     strings.TrimSpace(readString(payload["given_name"])),
		FamilyName:    strings.TrimSpace(readString(payload["family_name"])),
		PictureURL:    strings.TrimSpace(readString(payload["picture"])),
		Locale:        strings.TrimSpace(readString(payload["locale"])),
		Raw:           copyMap(payload),
	}
	if strings.TrimSpace(profile.Name) == "" {
		profile.Name = strings.TrimSpace(strings.Join(
			[]string{profile.GivenName, profile.FamilyName},
			" ",
		))
	}
	return profile
}

func normalizeGitHubProfile(providerID string, issuer string, payload map[string]any) UserProfile {
	subject := strings.TrimSpace(readString(payload["id"]))
	if subject == "" {
		subject = strings.TrimSpace(readString(payload["node_id"]))
	}
	if subject == "" {
		subject = strings.TrimSpace(readString(payload["login"]))
	}
	name := strings.TrimSpace(readString(payload["name"]))
	login := strings.TrimSpace(readString(payload["login"]))
	if name == "" {
		name = login
	}
	return UserProfile{
		ProviderID: normalizeProviderID(providerID),
		Issuer:     strings.TrimSpace(issuer),
		Subject:    subject,
		Email:      strings.TrimSpace(readString(payload["email"])),
		Name:       name,
		PictureURL: strings.TrimSpace(readString(payload["avatar_url"])),
		Locale:     strings.TrimSpace(readString(payload["locale"])),
		Raw:        copyMap(payload),
	}
}

func defaultIssuerForProvider(providerID string) string {
	switch normalizeProviderID(providerID) {
	case "google_calendar", "google_docs", "google_drive", "google_gmail":
		return googleIssuer
	case "github":
		return githubIssuer
	default:
		return ""
	}
}

func normalizeProviderID(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

func mergeMetadata(base map[string]any, override map[string]any) map[string]any {
	merged := map[string]any{}
	for key, value := range base {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		merged[trimmed] = value
	}
	for key, value := range override {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		merged[trimmed] = value
	}
	return merged
}

func copyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func readString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	case json.Number:
		return strings.TrimSpace(typed.String())
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	default:
		if value == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func readBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		return err == nil && parsed
	case json.Number:
		parsed, err := typed.Int64()
		return err == nil && parsed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	default:
		return false
	}
}
