package shopping

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers"
	"github.com/goliatone/go-services/providers/google/common"
)

const (
	ProviderID = "google_shopping"
	AuthURL    = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenURL   = "https://oauth2.googleapis.com/token"
)

const (
	ScopeContent = "https://www.googleapis.com/auth/content"
)

const (
	GrantContent = ScopeContent
)

const authKindServiceAccountJWT = "service_account_jwt"

type ServiceAccountConfig struct {
	Issuer            string
	Subject           string
	Audience          string
	SigningKey        string
	SigningAlgorithm  string
	KeyID             string
	ExternalAccountID string
}

type ServiceAccountProfile struct {
	Enabled  bool
	Metadata map[string]any
}

type Config struct {
	ClientID              string
	ClientSecret          string
	AuthURL               string
	TokenURL              string
	DefaultScopes         []string
	DisableIdentityScopes bool
	SupportedScopeTypes   []string
	TokenTTL              time.Duration
	ServiceAccount        ServiceAccountConfig
}

type Provider struct {
	*providers.OAuth2Provider
	serviceAccount ServiceAccountProfile
}

func DefaultConfig() Config {
	return Config{
		AuthURL:  AuthURL,
		TokenURL: TokenURL,
		DefaultScopes: []string{
			ScopeContent,
		},
		SupportedScopeTypes: []string{"org"},
	}
}

func New(cfg Config) (core.Provider, error) {
	defaults := DefaultConfig()
	if strings.TrimSpace(cfg.AuthURL) == "" {
		cfg.AuthURL = defaults.AuthURL
	}
	if strings.TrimSpace(cfg.TokenURL) == "" {
		cfg.TokenURL = defaults.TokenURL
	}
	if len(cfg.DefaultScopes) == 0 {
		cfg.DefaultScopes = defaults.DefaultScopes
	}
	if len(cfg.SupportedScopeTypes) == 0 {
		cfg.SupportedScopeTypes = defaults.SupportedScopeTypes
	}
	cfg.DefaultScopes = common.WithIdentityScopes(normalizeShoppingScopes(cfg.DefaultScopes), !cfg.DisableIdentityScopes)

	oauthProvider, err := providers.NewOAuth2Provider(providers.OAuth2Config{
		ID:                  ProviderID,
		AuthURL:             cfg.AuthURL,
		TokenURL:            cfg.TokenURL,
		ClientID:            cfg.ClientID,
		ClientSecret:        cfg.ClientSecret,
		DefaultScopes:       cfg.DefaultScopes,
		SupportedScopeTypes: cfg.SupportedScopeTypes,
		TokenTTL:            cfg.TokenTTL,
		Capabilities:        BaselineCapabilities(),
	})
	if err != nil {
		return nil, err
	}

	return &Provider{
		OAuth2Provider: oauthProvider,
		serviceAccount: normalizeServiceAccountProfile(cfg.ServiceAccount, cfg.TokenURL, cfg.DefaultScopes),
	}, nil
}

func BaselineCapabilities() []core.CapabilityDescriptor {
	return []core.CapabilityDescriptor{
		{
			Name:           "catalog.read",
			RequiredGrants: []string{GrantContent},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
		{
			Name:           "inventory.read",
			RequiredGrants: []string{GrantContent},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
		{
			Name:           "orders.read",
			RequiredGrants: []string{GrantContent},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
	}
}

func (p *Provider) NormalizeGrantedPermissions(_ context.Context, raw []string) ([]string, error) {
	_ = p
	return normalizeCanonicalGrants(raw), nil
}

func (p *Provider) ServiceAccountProfile() ServiceAccountProfile {
	if p == nil || !p.serviceAccount.Enabled {
		return ServiceAccountProfile{Enabled: false, Metadata: map[string]any{}}
	}
	return ServiceAccountProfile{
		Enabled:  true,
		Metadata: cloneMetadata(p.serviceAccount.Metadata),
	}
}

func normalizeServiceAccountProfile(
	cfg ServiceAccountConfig,
	tokenURL string,
	defaultScopes []string,
) ServiceAccountProfile {
	issuer := strings.TrimSpace(cfg.Issuer)
	audience := strings.TrimSpace(cfg.Audience)
	signingKey := strings.TrimSpace(cfg.SigningKey)
	if audience == "" {
		audience = strings.TrimSpace(tokenURL)
	}
	signingAlgorithm := strings.ToUpper(strings.TrimSpace(cfg.SigningAlgorithm))
	if signingAlgorithm == "" {
		signingAlgorithm = "RS256"
	}
	requestedGrants := normalizeCanonicalGrants(defaultScopes)
	enabled := issuer != "" && audience != "" && signingKey != ""
	if !enabled {
		return ServiceAccountProfile{Enabled: false, Metadata: map[string]any{}}
	}
	metadata := map[string]any{
		"auth_kind":           authKindServiceAccountJWT,
		"issuer":              issuer,
		"subject":             strings.TrimSpace(cfg.Subject),
		"audience":            audience,
		"signing_key":         signingKey,
		"signing_algorithm":   signingAlgorithm,
		"key_id":              strings.TrimSpace(cfg.KeyID),
		"external_account_id": strings.TrimSpace(cfg.ExternalAccountID),
		"requested_grants":    append([]string(nil), requestedGrants...),
		"granted_grants":      append([]string(nil), requestedGrants...),
	}
	return ServiceAccountProfile{Enabled: true, Metadata: metadata}
}

func normalizeShoppingScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{}
	}
	set := map[string]struct{}{}
	for _, scope := range scopes {
		normalized := normalizeShoppingScope(scope)
		if normalized == "" {
			continue
		}
		set[normalized] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for scope := range set {
		out = append(out, scope)
	}
	sort.Strings(out)
	return out
}

func normalizeCanonicalGrants(grants []string) []string {
	if len(grants) == 0 {
		return []string{}
	}
	set := map[string]struct{}{}
	for _, grant := range grants {
		normalized := normalizeShoppingGrant(grant)
		if normalized == "" {
			continue
		}
		set[normalized] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for grant := range set {
		out = append(out, grant)
	}
	sort.Strings(out)
	return out
}

func normalizeShoppingScope(value string) string {
	normalized := strings.TrimSpace(strings.ToLower(value))
	normalized = strings.TrimPrefix(normalized, "google_shopping:")
	normalized = strings.TrimPrefix(normalized, "google:")
	normalized = strings.TrimPrefix(normalized, "shopping:")
	switch normalized {
	case strings.ToLower(ScopeContent), "content":
		return ScopeContent
	default:
		return ""
	}
}

func normalizeShoppingGrant(value string) string {
	scope := normalizeShoppingScope(value)
	if scope == "" {
		return ""
	}
	return scope
}

func cloneMetadata(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

var _ core.Provider = (*Provider)(nil)
var _ core.GrantAwareProvider = (*Provider)(nil)
