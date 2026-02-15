package providers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

const (
	defaultAuthKind            = "oauth2_auth_code"
	defaultTokenRequestTimeout = 30 * time.Second
	maxTokenResponseBodyBytes  = 1 << 20 // 1 MiB
)

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type OAuth2Config struct {
	ID                  string
	AuthURL             string
	TokenURL            string
	ClientID            string
	ClientSecret        string
	ClientSecretInBody  bool
	DefaultScopes       []string
	SupportedScopeTypes []string
	Capabilities        []core.CapabilityDescriptor
	TokenTTL            time.Duration
	TokenRequestTimeout time.Duration
	Now                 func() time.Time
	HTTPClient          HTTPDoer
}

type OAuth2Provider struct {
	cfg        OAuth2Config
	httpClient HTTPDoer
}

type tokenEndpointPayload struct {
	AccessToken      string
	TokenType        string
	RefreshToken     string
	Scope            string
	ExpiresIn        int64
	ErrorCode        string
	ErrorDescription string
}

func NewOAuth2Provider(cfg OAuth2Config) (*OAuth2Provider, error) {
	cfg.ID = strings.TrimSpace(strings.ToLower(cfg.ID))
	if cfg.ID == "" {
		return nil, fmt.Errorf("providers: provider id is required")
	}
	if strings.TrimSpace(cfg.AuthURL) == "" {
		return nil, fmt.Errorf("providers: auth url is required for provider %q", cfg.ID)
	}
	if strings.TrimSpace(cfg.TokenURL) == "" {
		return nil, fmt.Errorf("providers: token url is required for provider %q", cfg.ID)
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, fmt.Errorf("providers: client id is required for provider %q", cfg.ID)
	}

	cfg.AuthURL = strings.TrimSpace(cfg.AuthURL)
	cfg.TokenURL = strings.TrimSpace(cfg.TokenURL)
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.ClientSecret = strings.TrimSpace(cfg.ClientSecret)
	cfg.DefaultScopes = normalizeGrants(cfg.DefaultScopes)
	if len(cfg.SupportedScopeTypes) == 0 {
		cfg.SupportedScopeTypes = []string{"user", "org"}
	}
	cfg.SupportedScopeTypes = normalizeScopeTypes(cfg.SupportedScopeTypes)
	cfg.Capabilities = cloneCapabilities(cfg.Capabilities)
	if cfg.TokenTTL <= 0 {
		cfg.TokenTTL = time.Hour
	}
	if cfg.TokenRequestTimeout <= 0 {
		cfg.TokenRequestTimeout = defaultTokenRequestTimeout
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time {
			return time.Now().UTC()
		}
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.TokenRequestTimeout}
	}

	return &OAuth2Provider{
		cfg:        cfg,
		httpClient: httpClient,
	}, nil
}

func (p *OAuth2Provider) ID() string {
	if p == nil {
		return ""
	}
	return p.cfg.ID
}

func (*OAuth2Provider) AuthKind() string {
	return defaultAuthKind
}

func (p *OAuth2Provider) SupportedScopeTypes() []string {
	if p == nil {
		return []string{}
	}
	return append([]string(nil), p.cfg.SupportedScopeTypes...)
}

func (p *OAuth2Provider) Capabilities() []core.CapabilityDescriptor {
	if p == nil {
		return []core.CapabilityDescriptor{}
	}
	return cloneCapabilities(p.cfg.Capabilities)
}

func (p *OAuth2Provider) BeginAuth(_ context.Context, req core.BeginAuthRequest) (core.BeginAuthResponse, error) {
	if p == nil {
		return core.BeginAuthResponse{}, fmt.Errorf("providers: oauth2 provider is nil")
	}
	if err := req.Scope.Validate(); err != nil {
		return core.BeginAuthResponse{}, err
	}
	state := strings.TrimSpace(req.State)
	if state == "" {
		generated, err := generateOAuthProviderState()
		if err != nil {
			return core.BeginAuthResponse{}, err
		}
		state = generated
	}
	requested := normalizeGrants(req.RequestedGrants)
	if len(requested) == 0 {
		requested = append([]string(nil), p.cfg.DefaultScopes...)
	}

	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", p.cfg.ClientID)
	if strings.TrimSpace(req.RedirectURI) != "" {
		values.Set("redirect_uri", strings.TrimSpace(req.RedirectURI))
	}
	values.Set("scope", strings.Join(requested, " "))
	values.Set("state", state)

	authURL := p.cfg.AuthURL
	if strings.Contains(authURL, "?") {
		authURL += "&" + values.Encode()
	} else {
		authURL += "?" + values.Encode()
	}

	metadata := cloneMetadata(req.Metadata)
	metadata["provider_id"] = p.cfg.ID
	metadata["token_url"] = p.cfg.TokenURL

	return core.BeginAuthResponse{
		URL:             authURL,
		State:           state,
		RequestedGrants: requested,
		Metadata:        metadata,
	}, nil
}

func (p *OAuth2Provider) CompleteAuth(ctx context.Context, req core.CompleteAuthRequest) (core.CompleteAuthResponse, error) {
	if p == nil {
		return core.CompleteAuthResponse{}, fmt.Errorf("providers: oauth2 provider is nil")
	}
	if err := req.Scope.Validate(); err != nil {
		return core.CompleteAuthResponse{}, err
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		return core.CompleteAuthResponse{}, fmt.Errorf("providers: auth code is required")
	}

	requested := normalizeGrants(readStringSlice(req.Metadata, "requested_grants"))
	if len(requested) == 0 {
		requested = append([]string(nil), p.cfg.DefaultScopes...)
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	if redirectURI := strings.TrimSpace(req.RedirectURI); redirectURI != "" {
		form.Set("redirect_uri", redirectURI)
	}

	token, err := p.fetchToken(ctx, form)
	if err != nil {
		return core.CompleteAuthResponse{}, err
	}

	granted := normalizeGrants(parseScopeList(token.Scope))
	if len(granted) == 0 {
		granted = normalizeGrants(readStringSlice(req.Metadata, "granted_grants"))
	}
	if len(granted) == 0 {
		granted = append([]string(nil), requested...)
	}

	externalAccountID := strings.TrimSpace(readString(req.Metadata, "external_account_id"))
	if externalAccountID == "" {
		externalAccountID = fmt.Sprintf("%s:%s:%s", p.cfg.ID, req.Scope.Type, req.Scope.ID)
	}

	now := p.cfg.Now().UTC()
	expiresAt := p.resolveExpiresAt(now, token.ExpiresIn)
	refreshToken := strings.TrimSpace(token.RefreshToken)
	tokenType := normalizeTokenType(token.TokenType)
	credential := core.ActiveCredential{
		TokenType:       tokenType,
		AccessToken:     strings.TrimSpace(token.AccessToken),
		RefreshToken:    refreshToken,
		RequestedScopes: append([]string(nil), requested...),
		GrantedScopes:   append([]string(nil), granted...),
		ExpiresAt:       expiresAt,
		Refreshable:     refreshToken != "",
		Metadata: map[string]any{
			"provider_id": p.cfg.ID,
			"token_url":   p.cfg.TokenURL,
		},
	}

	return core.CompleteAuthResponse{
		ExternalAccountID: externalAccountID,
		Credential:        credential,
		RequestedGrants:   append([]string(nil), requested...),
		GrantedGrants:     append([]string(nil), granted...),
		Metadata: map[string]any{
			"provider_id": p.cfg.ID,
			"token_url":   p.cfg.TokenURL,
		},
	}, nil
}

func (p *OAuth2Provider) Refresh(ctx context.Context, cred core.ActiveCredential) (core.RefreshResult, error) {
	if p == nil {
		return core.RefreshResult{}, fmt.Errorf("providers: oauth2 provider is nil")
	}
	refreshToken := strings.TrimSpace(cred.RefreshToken)
	if refreshToken == "" {
		return core.RefreshResult{}, fmt.Errorf("providers: refresh token is required")
	}

	requested := normalizeGrants(cred.RequestedScopes)
	if len(requested) == 0 {
		requested = append([]string(nil), p.cfg.DefaultScopes...)
	}
	granted := normalizeGrants(cred.GrantedScopes)
	if len(granted) == 0 {
		granted = append([]string(nil), requested...)
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	if len(requested) > 0 {
		form.Set("scope", strings.Join(requested, " "))
	}

	token, err := p.fetchToken(ctx, form)
	if err != nil {
		return core.RefreshResult{}, err
	}
	refreshedScopes := normalizeGrants(parseScopeList(token.Scope))
	if len(refreshedScopes) > 0 {
		granted = refreshedScopes
	}

	now := p.cfg.Now().UTC()
	expiresAt := p.resolveExpiresAt(now, token.ExpiresIn)
	refreshed := cred
	refreshed.TokenType = normalizeTokenType(token.TokenType)
	refreshed.AccessToken = strings.TrimSpace(token.AccessToken)
	if nextRefresh := strings.TrimSpace(token.RefreshToken); nextRefresh != "" {
		refreshed.RefreshToken = nextRefresh
	}
	refreshed.RequestedScopes = append([]string(nil), requested...)
	refreshed.GrantedScopes = append([]string(nil), granted...)
	refreshed.ExpiresAt = expiresAt
	refreshed.Refreshable = strings.TrimSpace(refreshed.RefreshToken) != ""
	refreshed.Metadata = cloneMetadata(refreshed.Metadata)
	refreshed.Metadata["provider_id"] = p.cfg.ID
	refreshed.Metadata["token_url"] = p.cfg.TokenURL

	return core.RefreshResult{
		Credential:    refreshed,
		GrantedGrants: append([]string(nil), granted...),
		Metadata: map[string]any{
			"provider_id": p.cfg.ID,
			"token_url":   p.cfg.TokenURL,
		},
	}, nil
}

func (p *OAuth2Provider) fetchToken(ctx context.Context, form url.Values) (tokenEndpointPayload, error) {
	if p == nil {
		return tokenEndpointPayload{}, fmt.Errorf("providers: oauth2 provider is nil")
	}
	if p.httpClient == nil {
		return tokenEndpointPayload{}, fmt.Errorf("providers: oauth2 http client is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(p.cfg.TokenURL) == "" {
		return tokenEndpointPayload{}, fmt.Errorf("providers: token url is required for provider %q", p.cfg.ID)
	}

	values := url.Values{}
	for key, items := range form {
		if strings.TrimSpace(key) == "" {
			continue
		}
		for _, item := range items {
			values.Add(key, strings.TrimSpace(item))
		}
	}
	values.Set("client_id", p.cfg.ClientID)
	if p.cfg.ClientSecretInBody && p.cfg.ClientSecret != "" {
		values.Set("client_secret", p.cfg.ClientSecret)
	}

	requestCtx := ctx
	cancel := func() {}
	if p.cfg.TokenRequestTimeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, p.cfg.TokenRequestTimeout)
	}
	defer cancel()

	httpReq, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodPost,
		p.cfg.TokenURL,
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		return tokenEndpointPayload{}, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")
	if !p.cfg.ClientSecretInBody && p.cfg.ClientSecret != "" {
		httpReq.SetBasicAuth(p.cfg.ClientID, p.cfg.ClientSecret)
	}

	response, err := p.httpClient.Do(httpReq)
	if err != nil {
		return tokenEndpointPayload{}, fmt.Errorf("providers: token request failed: %w", err)
	}
	defer response.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxTokenResponseBodyBytes+1))
	if readErr != nil {
		return tokenEndpointPayload{}, fmt.Errorf("providers: read token response: %w", readErr)
	}
	if int64(len(body)) > maxTokenResponseBodyBytes {
		return tokenEndpointPayload{}, fmt.Errorf("providers: token response exceeds %d bytes", maxTokenResponseBodyBytes)
	}

	payload, parseErr := parseTokenPayload(body, response.Header.Get("Content-Type"))
	if parseErr != nil {
		return tokenEndpointPayload{}, fmt.Errorf("providers: decode token response: %w", parseErr)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return tokenEndpointPayload{}, fmt.Errorf(
			"providers: token endpoint error (%d): %s",
			response.StatusCode,
			describeTokenError(payload),
		)
	}
	if payload.ErrorCode != "" {
		return tokenEndpointPayload{}, fmt.Errorf("providers: token endpoint error: %s", describeTokenError(payload))
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return tokenEndpointPayload{}, fmt.Errorf("providers: token endpoint response missing access token")
	}
	return payload, nil
}

func describeTokenError(payload tokenEndpointPayload) string {
	if strings.TrimSpace(payload.ErrorDescription) != "" {
		return strings.TrimSpace(payload.ErrorDescription)
	}
	if strings.TrimSpace(payload.ErrorCode) != "" {
		return strings.TrimSpace(payload.ErrorCode)
	}
	return "unknown error"
}

func parseTokenPayload(body []byte, contentType string) (tokenEndpointPayload, error) {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if strings.Contains(contentType, "json") {
		payload, err := parseTokenPayloadJSON(body)
		if err != nil {
			return tokenEndpointPayload{}, err
		}
		return payload, nil
	}
	if strings.Contains(contentType, "x-www-form-urlencoded") || strings.Contains(contentType, "text/plain") {
		payload, err := parseTokenPayloadForm(body)
		if err != nil {
			return tokenEndpointPayload{}, err
		}
		return payload, nil
	}
	if payload, err := parseTokenPayloadJSON(body); err == nil {
		return payload, nil
	}
	return parseTokenPayloadForm(body)
}

func parseTokenPayloadJSON(body []byte) (tokenEndpointPayload, error) {
	if len(bytesTrimSpace(body)) == 0 {
		return tokenEndpointPayload{}, fmt.Errorf("empty payload")
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return tokenEndpointPayload{}, err
	}
	return tokenEndpointPayload{
		AccessToken:      readAnyString(decoded["access_token"]),
		TokenType:        readAnyString(decoded["token_type"]),
		RefreshToken:     readAnyString(decoded["refresh_token"]),
		Scope:            readAnyString(decoded["scope"]),
		ExpiresIn:        readAnyInt64(decoded["expires_in"]),
		ErrorCode:        readAnyString(decoded["error"]),
		ErrorDescription: readAnyString(decoded["error_description"]),
	}, nil
}

func parseTokenPayloadForm(body []byte) (tokenEndpointPayload, error) {
	if len(bytesTrimSpace(body)) == 0 {
		return tokenEndpointPayload{}, fmt.Errorf("empty payload")
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return tokenEndpointPayload{}, err
	}
	expiresIn, _ := strconv.ParseInt(strings.TrimSpace(values.Get("expires_in")), 10, 64)
	return tokenEndpointPayload{
		AccessToken:      strings.TrimSpace(values.Get("access_token")),
		TokenType:        strings.TrimSpace(values.Get("token_type")),
		RefreshToken:     strings.TrimSpace(values.Get("refresh_token")),
		Scope:            strings.TrimSpace(values.Get("scope")),
		ExpiresIn:        expiresIn,
		ErrorCode:        strings.TrimSpace(values.Get("error")),
		ErrorDescription: strings.TrimSpace(values.Get("error_description")),
	}, nil
}

func (p *OAuth2Provider) resolveExpiresAt(now time.Time, expiresIn int64) *time.Time {
	ttl := p.cfg.TokenTTL
	if expiresIn > 0 {
		ttl = time.Duration(expiresIn) * time.Second
	}
	if ttl <= 0 {
		return nil
	}
	expiresAt := now.Add(ttl)
	return &expiresAt
}

func normalizeTokenType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return "bearer"
	}
	return normalized
}

func parseScopeList(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return []string{}
	}
	parts := strings.Fields(strings.ReplaceAll(trimmed, ",", " "))
	return parts
}

func bytesTrimSpace(value []byte) []byte {
	return []byte(strings.TrimSpace(string(value)))
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

func readAnyInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return parsed
		}
		floatParsed, floatErr := typed.Float64()
		if floatErr == nil {
			return int64(floatParsed)
		}
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func cloneCapabilities(input []core.CapabilityDescriptor) []core.CapabilityDescriptor {
	if len(input) == 0 {
		return []core.CapabilityDescriptor{}
	}
	output := make([]core.CapabilityDescriptor, 0, len(input))
	for _, descriptor := range input {
		copyDescriptor := descriptor
		copyDescriptor.RequiredGrants = append([]string(nil), descriptor.RequiredGrants...)
		copyDescriptor.OptionalGrants = append([]string(nil), descriptor.OptionalGrants...)
		output = append(output, copyDescriptor)
	}
	return output
}

func cloneMetadata(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func normalizeScopeTypes(input []string) []string {
	values := make([]string, 0, len(input))
	seen := map[string]struct{}{}
	for _, value := range input {
		normalized := strings.TrimSpace(strings.ToLower(value))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		values = append(values, normalized)
	}
	if len(values) == 0 {
		return []string{"user", "org"}
	}
	sort.Strings(values)
	return values
}

func normalizeGrants(input []string) []string {
	if len(input) == 0 {
		return []string{}
	}
	values := make([]string, 0, len(input))
	seen := map[string]struct{}{}
	for _, value := range input {
		normalized := strings.TrimSpace(strings.ToLower(value))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		values = append(values, normalized)
	}
	sort.Strings(values)
	return values
}

func readString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func readStringSlice(metadata map[string]any, key string) []string {
	if len(metadata) == 0 {
		return []string{}
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return []string{}
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			itemValue := strings.TrimSpace(fmt.Sprint(item))
			if itemValue != "" && itemValue != "<nil>" {
				items = append(items, itemValue)
			}
		}
		return items
	default:
		trimmed := strings.TrimSpace(fmt.Sprint(typed))
		if trimmed == "" || trimmed == "<nil>" {
			return []string{}
		}
		if !strings.Contains(trimmed, ",") {
			return []string{trimmed}
		}
		parts := strings.Split(trimmed, ",")
		items := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				items = append(items, part)
			}
		}
		return items
	}
}

func generateOAuthProviderState() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("providers: generate oauth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

var _ core.Provider = (*OAuth2Provider)(nil)
