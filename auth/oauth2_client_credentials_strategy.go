package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/goliatone/go-services/core"
)

type OAuth2ClientCredentialsStrategyConfig struct {
	ClientID          string
	ClientSecret      string
	TokenURL          string
	DefaultScopes     []string
	TokenTTL          time.Duration
	RenewBefore       time.Duration
	ExternalAccountID string
	Now               func() time.Time
}

type cachedClientCredential struct {
	credential core.ActiveCredential
	expiresAt  time.Time
}

type OAuth2ClientCredentialsStrategy struct {
	config OAuth2ClientCredentialsStrategyConfig
	mu     sync.Mutex
	cache  map[string]cachedClientCredential
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
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	return &OAuth2ClientCredentialsStrategy{
		config: OAuth2ClientCredentialsStrategyConfig{
			ClientID:          strings.TrimSpace(cfg.ClientID),
			ClientSecret:      strings.TrimSpace(cfg.ClientSecret),
			TokenURL:          strings.TrimSpace(cfg.TokenURL),
			DefaultScopes:     normalizeValues(cfg.DefaultScopes),
			TokenTTL:          tokenTTL,
			RenewBefore:       renewBefore,
			ExternalAccountID: strings.TrimSpace(cfg.ExternalAccountID),
			Now:               now,
		},
		cache: map[string]cachedClientCredential{},
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

func (s *OAuth2ClientCredentialsStrategy) Complete(_ context.Context, req core.AuthCompleteRequest) (core.AuthCompleteResponse, error) {
	metadata := cloneMetadata(req.Metadata)
	clientID := firstNonEmpty(
		readString(metadata, "client_id"),
		s.config.ClientID,
	)
	clientSecret := firstNonEmpty(
		readString(metadata, "client_secret"),
		s.config.ClientSecret,
	)
	if clientID == "" {
		return core.AuthCompleteResponse{}, fmt.Errorf("auth: oauth2 client credentials client_id is required")
	}
	if clientSecret == "" {
		return core.AuthCompleteResponse{}, fmt.Errorf("auth: oauth2 client credentials client_secret is required")
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
		return s.buildCompleteResponse(req, cached, requested, granted), nil
	}

	issued, err := s.issueCredential(req.Scope, clientID, clientSecret, requested, granted, cacheKey)
	if err != nil {
		return core.AuthCompleteResponse{}, err
	}
	s.storeCachedCredential(cacheKey, issued)

	return s.buildCompleteResponse(req, issued, requested, granted), nil
}

func (s *OAuth2ClientCredentialsStrategy) Refresh(_ context.Context, cred core.ActiveCredential) (core.RefreshResult, error) {
	metadata := cloneMetadata(cred.Metadata)
	clientID := firstNonEmpty(readString(metadata, "client_id"), s.config.ClientID)
	clientSecret := firstNonEmpty(readString(metadata, "client_secret"), s.config.ClientSecret)
	if clientID == "" || clientSecret == "" {
		return core.RefreshResult{}, fmt.Errorf("auth: oauth2 client credentials refresh requires configured client credentials")
	}

	requested := normalizeValues(cred.RequestedScopes)
	granted := normalizeValues(cred.GrantedScopes)
	cacheKey := firstNonEmpty(readString(metadata, "cache_key"), buildClientCredentialsCacheKey(core.ScopeRef{}, clientID, requested))

	issued, err := s.issueCredential(core.ScopeRef{}, clientID, clientSecret, requested, granted, cacheKey)
	if err != nil {
		return core.RefreshResult{}, err
	}
	s.storeCachedCredential(cacheKey, issued)

	return core.RefreshResult{
		Credential:    issued,
		GrantedGrants: append([]string(nil), issued.GrantedScopes...),
		Metadata: map[string]any{
			"auth_kind": core.AuthKindOAuth2ClientCredential,
			"token_url": s.config.TokenURL,
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
			"token_url": s.config.TokenURL,
		},
	}
}

func (s *OAuth2ClientCredentialsStrategy) issueCredential(
	scope core.ScopeRef,
	clientID string,
	clientSecret string,
	requested []string,
	granted []string,
	cacheKey string,
) (core.ActiveCredential, error) {
	now := s.config.Now().UTC()
	expiresAt := now.Add(s.config.TokenTTL)
	token := buildClientCredentialsToken(clientID, clientSecret, requested, now)
	return core.ActiveCredential{
		TokenType:       "bearer",
		AccessToken:     token,
		RequestedScopes: append([]string(nil), requested...),
		GrantedScopes:   append([]string(nil), granted...),
		ExpiresAt:       &expiresAt,
		Refreshable:     true,
		Metadata: map[string]any{
			"auth_kind":  core.AuthKindOAuth2ClientCredential,
			"client_id":  clientID,
			"token_url":  s.config.TokenURL,
			"cache_key":  cacheKey,
			"scope_type": scope.Type,
			"scope_id":   scope.ID,
		},
	}, nil
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

func buildClientCredentialsToken(clientID string, clientSecret string, scopes []string, now time.Time) string {
	sum := sha256.Sum256([]byte(clientID + "|" + clientSecret + "|" + strings.Join(normalizeValues(scopes), ",") + "|" + fmt.Sprintf("%d", now.UnixNano())))
	return "cc_" + hex.EncodeToString(sum[:16])
}

