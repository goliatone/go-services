package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goliatone/go-services/core"
)

const (
	defaultClientCredentialsTokenRequestTimeout = 30 * time.Second
	maxClientCredentialsTokenResponseBodyBytes  = 1 << 20 // 1 MiB
)

type OAuth2HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type OAuth2ClientCredentialsStrategyConfig struct {
	ClientID            string
	ClientSecret        string
	TokenURL            string
	ClientSecretInBody  bool
	DefaultScopes       []string
	TokenTTL            time.Duration
	RenewBefore         time.Duration
	TokenRequestTimeout time.Duration
	ExternalAccountID   string
	Now                 func() time.Time
	HTTPClient          OAuth2HTTPDoer
}

type cachedClientCredential struct {
	credential core.ActiveCredential
	expiresAt  time.Time
}

type clientCredentialsTokenPayload struct {
	AccessToken      string
	TokenType        string
	Scope            string
	ExpiresIn        int64
	ErrorCode        string
	ErrorDescription string
}

type OAuth2ClientCredentialsStrategy struct {
	config     OAuth2ClientCredentialsStrategyConfig
	httpClient OAuth2HTTPDoer
	mu         sync.Mutex
	cache      map[string]cachedClientCredential
}

func NewOAuth2ClientCredentialsStrategy(cfg OAuth2ClientCredentialsStrategyConfig) *OAuth2ClientCredentialsStrategy {
	tokenTTL := cfg.TokenTTL
	if tokenTTL <= 0 {
		tokenTTL = time.Hour
	}
	renewBefore := cfg.RenewBefore
	if renewBefore <= 0 {
		renewBefore = 2 * time.Minute
	}
	requestTimeout := cfg.TokenRequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = defaultClientCredentialsTokenRequestTimeout
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}

	return &OAuth2ClientCredentialsStrategy{
		config: OAuth2ClientCredentialsStrategyConfig{
			ClientID:            strings.TrimSpace(cfg.ClientID),
			ClientSecret:        strings.TrimSpace(cfg.ClientSecret),
			TokenURL:            strings.TrimSpace(cfg.TokenURL),
			ClientSecretInBody:  cfg.ClientSecretInBody,
			DefaultScopes:       normalizeValues(cfg.DefaultScopes),
			TokenTTL:            tokenTTL,
			RenewBefore:         renewBefore,
			TokenRequestTimeout: requestTimeout,
			ExternalAccountID:   strings.TrimSpace(cfg.ExternalAccountID),
			Now:                 now,
		},
		httpClient: httpClient,
		cache:      map[string]cachedClientCredential{},
	}
}

func (*OAuth2ClientCredentialsStrategy) Type() string {
	return core.AuthKindOAuth2ClientCredential
}

func (s *OAuth2ClientCredentialsStrategy) Begin(_ context.Context, req core.AuthBeginRequest) (core.AuthBeginResponse, error) {
	requested := normalizeValues(req.RequestedRaw)
	if len(requested) == 0 {
		requested = append([]string(nil), s.config.DefaultScopes...)
	}
	return core.AuthBeginResponse{
		State:           strings.TrimSpace(req.State),
		RequestedGrants: requested,
		Metadata: map[string]any{
			"auth_kind": core.AuthKindOAuth2ClientCredential,
			"token_url": s.config.TokenURL,
		},
	}, nil
}

func (s *OAuth2ClientCredentialsStrategy) Complete(ctx context.Context, req core.AuthCompleteRequest) (core.AuthCompleteResponse, error) {
	metadata := cloneMetadata(req.Metadata)
	clientID := firstNonEmpty(
		readString(metadata, "client_id"),
		s.config.ClientID,
	)
	clientSecret := firstNonEmpty(
		readString(metadata, "client_secret"),
		s.config.ClientSecret,
	)
	tokenURL := firstNonEmpty(
		readString(metadata, "token_url"),
		s.config.TokenURL,
	)
	if clientID == "" {
		return core.AuthCompleteResponse{}, fmt.Errorf("auth: oauth2 client credentials client_id is required")
	}
	if clientSecret == "" {
		return core.AuthCompleteResponse{}, fmt.Errorf("auth: oauth2 client credentials client_secret is required")
	}
	if tokenURL == "" {
		return core.AuthCompleteResponse{}, fmt.Errorf("auth: oauth2 client credentials token_url is required")
	}

	requested := readStringSlice(metadata, "requested_grants", "requested_scopes")
	if len(requested) == 0 {
		requested = append([]string(nil), s.config.DefaultScopes...)
	}
	granted := readStringSlice(metadata, "granted_grants", "granted_scopes")
	if len(granted) == 0 {
		granted = append([]string(nil), requested...)
	}

	cacheKey := buildClientCredentialsCacheKey(req.Scope, clientID, requested)
	if cached, ok := s.lookupCachedCredential(cacheKey); ok {
		return s.buildCompleteResponse(req, cached, requested, cached.GrantedScopes), nil
	}

	issued, effectiveGranted, err := s.issueCredential(
		ctx,
		req.Scope,
		clientID,
		clientSecret,
		tokenURL,
		requested,
		granted,
		cacheKey,
	)
	if err != nil {
		return core.AuthCompleteResponse{}, err
	}
	s.storeCachedCredential(cacheKey, issued)

	return s.buildCompleteResponse(req, issued, requested, effectiveGranted), nil
}

func (s *OAuth2ClientCredentialsStrategy) Refresh(ctx context.Context, cred core.ActiveCredential) (core.RefreshResult, error) {
	metadata := cloneMetadata(cred.Metadata)
	clientID := firstNonEmpty(readString(metadata, "client_id"), s.config.ClientID)
	clientSecret := firstNonEmpty(readString(metadata, "client_secret"), s.config.ClientSecret)
	tokenURL := firstNonEmpty(readString(metadata, "token_url"), s.config.TokenURL)
	if clientID == "" || clientSecret == "" {
		return core.RefreshResult{}, fmt.Errorf("auth: oauth2 client credentials refresh requires configured client credentials")
	}
	if tokenURL == "" {
		return core.RefreshResult{}, fmt.Errorf("auth: oauth2 client credentials refresh requires token_url")
	}

	requested := normalizeValues(cred.RequestedScopes)
	if len(requested) == 0 {
		requested = append([]string(nil), s.config.DefaultScopes...)
	}
	granted := normalizeValues(cred.GrantedScopes)
	if len(granted) == 0 {
		granted = append([]string(nil), requested...)
	}
	scope := core.ScopeRef{Type: readString(metadata, "scope_type"), ID: readString(metadata, "scope_id")}
	cacheKey := firstNonEmpty(
		readString(metadata, "cache_key"),
		buildClientCredentialsCacheKey(scope, clientID, requested),
	)

	issued, effectiveGranted, err := s.issueCredential(
		ctx,
		scope,
		clientID,
		clientSecret,
		tokenURL,
		requested,
		granted,
		cacheKey,
	)
	if err != nil {
		return core.RefreshResult{}, err
	}
	s.storeCachedCredential(cacheKey, issued)

	return core.RefreshResult{
		Credential:    issued,
		GrantedGrants: append([]string(nil), effectiveGranted...),
		Metadata: map[string]any{
			"auth_kind": core.AuthKindOAuth2ClientCredential,
			"token_url": tokenURL,
		},
	}, nil
}

func (s *OAuth2ClientCredentialsStrategy) buildCompleteResponse(
	req core.AuthCompleteRequest,
	cred core.ActiveCredential,
	requested []string,
	granted []string,
) core.AuthCompleteResponse {
	externalAccountID := firstNonEmpty(
		readString(req.Metadata, "external_account_id"),
		s.config.ExternalAccountID,
		fmt.Sprintf("%s:%s:%s", core.AuthKindOAuth2ClientCredential, req.Scope.Type, req.Scope.ID),
	)
	return core.AuthCompleteResponse{
		ExternalAccountID: externalAccountID,
		Credential:        cred,
		RequestedGrants:   append([]string(nil), requested...),
		GrantedGrants:     append([]string(nil), granted...),
		Metadata: map[string]any{
			"auth_kind": core.AuthKindOAuth2ClientCredential,
			"token_url": readString(cred.Metadata, "token_url"),
		},
	}
}

func (s *OAuth2ClientCredentialsStrategy) issueCredential(
	ctx context.Context,
	scope core.ScopeRef,
	clientID string,
	clientSecret string,
	tokenURL string,
	requested []string,
	granted []string,
	cacheKey string,
) (core.ActiveCredential, []string, error) {
	payload, err := s.fetchToken(ctx, tokenURL, clientID, clientSecret, requested)
	if err != nil {
		return core.ActiveCredential{}, nil, err
	}

	effectiveGranted := append([]string(nil), granted...)
	if grantedFromToken := parseClientCredentialScopes(payload.Scope); len(grantedFromToken) > 0 {
		effectiveGranted = grantedFromToken
	}
	if len(effectiveGranted) == 0 {
		effectiveGranted = append([]string(nil), requested...)
	}

	now := s.config.Now().UTC()
	expiresAt := resolveClientCredentialsExpiresAt(now, payload.ExpiresIn, s.config.TokenTTL)
	credential := core.ActiveCredential{
		TokenType:       normalizeClientCredentialsTokenType(payload.TokenType),
		AccessToken:     strings.TrimSpace(payload.AccessToken),
		RequestedScopes: append([]string(nil), requested...),
		GrantedScopes:   append([]string(nil), effectiveGranted...),
		ExpiresAt:       expiresAt,
		Refreshable:     true,
		Metadata: map[string]any{
			"auth_kind":  core.AuthKindOAuth2ClientCredential,
			"client_id":  clientID,
			"token_url":  tokenURL,
			"cache_key":  cacheKey,
			"scope_type": strings.TrimSpace(scope.Type),
			"scope_id":   strings.TrimSpace(scope.ID),
		},
	}
	return credential, effectiveGranted, nil
}

func (s *OAuth2ClientCredentialsStrategy) fetchToken(
	ctx context.Context,
	tokenURL string,
	clientID string,
	clientSecret string,
	scopes []string,
) (clientCredentialsTokenPayload, error) {
	if s == nil || s.httpClient == nil {
		return clientCredentialsTokenPayload{}, fmt.Errorf("auth: oauth2 client credentials http client is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tokenURL = strings.TrimSpace(tokenURL)
	if tokenURL == "" {
		return clientCredentialsTokenPayload{}, fmt.Errorf("auth: oauth2 client credentials token_url is required")
	}

	values := url.Values{}
	values.Set("grant_type", "client_credentials")
	if len(scopes) > 0 {
		values.Set("scope", strings.Join(normalizeValues(scopes), " "))
	}
	values.Set("client_id", clientID)
	if s.config.ClientSecretInBody {
		values.Set("client_secret", clientSecret)
	}

	requestCtx := ctx
	cancel := func() {}
	if s.config.TokenRequestTimeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, s.config.TokenRequestTimeout)
	}
	defer cancel()

	httpReq, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodPost,
		tokenURL,
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		return clientCredentialsTokenPayload{}, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")
	if !s.config.ClientSecretInBody {
		httpReq.SetBasicAuth(clientID, clientSecret)
	}

	response, err := s.httpClient.Do(httpReq)
	if err != nil {
		return clientCredentialsTokenPayload{}, fmt.Errorf("auth: oauth2 client credentials token request failed: %w", err)
	}
	defer response.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxClientCredentialsTokenResponseBodyBytes+1))
	if readErr != nil {
		return clientCredentialsTokenPayload{}, fmt.Errorf("auth: oauth2 client credentials read token response: %w", readErr)
	}
	if int64(len(body)) > maxClientCredentialsTokenResponseBodyBytes {
		return clientCredentialsTokenPayload{}, fmt.Errorf(
			"auth: oauth2 client credentials token response exceeds %d bytes",
			maxClientCredentialsTokenResponseBodyBytes,
		)
	}

	payload, parseErr := parseClientCredentialsTokenPayload(body, response.Header.Get("Content-Type"))
	if parseErr != nil {
		return clientCredentialsTokenPayload{}, fmt.Errorf(
			"auth: oauth2 client credentials decode token response: %w",
			parseErr,
		)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return clientCredentialsTokenPayload{}, fmt.Errorf(
			"auth: oauth2 client credentials token endpoint error (%d): %s",
			response.StatusCode,
			describeClientCredentialsError(payload),
		)
	}
	if payload.ErrorCode != "" {
		return clientCredentialsTokenPayload{}, fmt.Errorf(
			"auth: oauth2 client credentials token endpoint error: %s",
			describeClientCredentialsError(payload),
		)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return clientCredentialsTokenPayload{}, fmt.Errorf(
			"auth: oauth2 client credentials token response missing access token",
		)
	}
	return payload, nil
}

func (s *OAuth2ClientCredentialsStrategy) lookupCachedCredential(cacheKey string) (core.ActiveCredential, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cached, ok := s.cache[cacheKey]
	if !ok {
		return core.ActiveCredential{}, false
	}
	now := s.config.Now().UTC()
	if cached.expiresAt.IsZero() || !cached.expiresAt.After(now.Add(s.config.RenewBefore)) {
		delete(s.cache, cacheKey)
		return core.ActiveCredential{}, false
	}
	return cached.credential, true
}

func (s *OAuth2ClientCredentialsStrategy) storeCachedCredential(cacheKey string, cred core.ActiveCredential) {
	s.mu.Lock()
	defer s.mu.Unlock()
	expiresAt := time.Time{}
	if cred.ExpiresAt != nil {
		expiresAt = cred.ExpiresAt.UTC()
	}
	s.cache[cacheKey] = cachedClientCredential{
		credential: cred,
		expiresAt:  expiresAt,
	}
}

func buildClientCredentialsCacheKey(scope core.ScopeRef, clientID string, requested []string) string {
	normalized := normalizeValues(requested)
	parts := append([]string(nil), normalized...)
	sort.Strings(parts)
	return fmt.Sprintf("%s:%s:%s:%s", clientID, strings.TrimSpace(scope.Type), strings.TrimSpace(scope.ID), strings.Join(parts, "|"))
}

func resolveClientCredentialsExpiresAt(now time.Time, expiresIn int64, fallback time.Duration) *time.Time {
	ttl := fallback
	if expiresIn > 0 {
		ttl = time.Duration(expiresIn) * time.Second
	}
	if ttl <= 0 {
		return nil
	}
	expiresAt := now.Add(ttl)
	return &expiresAt
}

func normalizeClientCredentialsTokenType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return "bearer"
	}
	return normalized
}

func parseClientCredentialScopes(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return []string{}
	}
	parts := strings.Fields(strings.ReplaceAll(trimmed, ",", " "))
	return normalizeValues(parts)
}

func describeClientCredentialsError(payload clientCredentialsTokenPayload) string {
	if strings.TrimSpace(payload.ErrorDescription) != "" {
		return strings.TrimSpace(payload.ErrorDescription)
	}
	if strings.TrimSpace(payload.ErrorCode) != "" {
		return strings.TrimSpace(payload.ErrorCode)
	}
	return "unknown error"
}

func parseClientCredentialsTokenPayload(body []byte, contentType string) (clientCredentialsTokenPayload, error) {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if strings.Contains(contentType, "json") {
		return parseClientCredentialsTokenPayloadJSON(body)
	}
	if strings.Contains(contentType, "x-www-form-urlencoded") || strings.Contains(contentType, "text/plain") {
		return parseClientCredentialsTokenPayloadForm(body)
	}
	if payload, err := parseClientCredentialsTokenPayloadJSON(body); err == nil {
		return payload, nil
	}
	return parseClientCredentialsTokenPayloadForm(body)
}

func parseClientCredentialsTokenPayloadJSON(body []byte) (clientCredentialsTokenPayload, error) {
	if len(strings.TrimSpace(string(body))) == 0 {
		return clientCredentialsTokenPayload{}, fmt.Errorf("empty payload")
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return clientCredentialsTokenPayload{}, err
	}
	return clientCredentialsTokenPayload{
		AccessToken:      readAnyString(decoded["access_token"]),
		TokenType:        readAnyString(decoded["token_type"]),
		Scope:            readAnyString(decoded["scope"]),
		ExpiresIn:        readAnyInt64(decoded["expires_in"]),
		ErrorCode:        readAnyString(decoded["error"]),
		ErrorDescription: readAnyString(decoded["error_description"]),
	}, nil
}

func parseClientCredentialsTokenPayloadForm(body []byte) (clientCredentialsTokenPayload, error) {
	if len(strings.TrimSpace(string(body))) == 0 {
		return clientCredentialsTokenPayload{}, fmt.Errorf("empty payload")
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return clientCredentialsTokenPayload{}, err
	}
	expiresIn, _ := strconv.ParseInt(strings.TrimSpace(values.Get("expires_in")), 10, 64)
	return clientCredentialsTokenPayload{
		AccessToken:      strings.TrimSpace(values.Get("access_token")),
		TokenType:        strings.TrimSpace(values.Get("token_type")),
		Scope:            strings.TrimSpace(values.Get("scope")),
		ExpiresIn:        expiresIn,
		ErrorCode:        strings.TrimSpace(values.Get("error")),
		ErrorDescription: strings.TrimSpace(values.Get("error_description")),
	}, nil
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
		if parsed, err := typed.Int64(); err == nil {
			return parsed
		}
		if parsed, err := typed.Float64(); err == nil {
			return int64(parsed)
		}
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}
