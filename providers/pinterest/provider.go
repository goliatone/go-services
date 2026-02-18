package pinterest

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers"
)

const (
	ProviderID = "pinterest"
	AuthURL    = "https://www.pinterest.com/oauth/"
	TokenURL   = "https://api.pinterest.com/v5/oauth/token"
)

const (
	ScopeUserAccountsRead = "user_accounts:read"
	ScopeBoardsRead       = "boards:read"
	ScopePinsRead         = "pins:read"
)

const (
	GrantUserAccountsRead = "pinterest:user_accounts:read"
	GrantBoardsRead       = "pinterest:boards:read"
	GrantPinsRead         = "pinterest:pins:read"
)

type Config struct {
	ClientID            string
	ClientSecret        string
	AuthURL             string
	TokenURL            string
	DefaultScopes       []string
	SupportedScopeTypes []string
	TokenTTL            time.Duration
}

type Provider struct {
	*providers.OAuth2Provider
}

func DefaultConfig() Config {
	return Config{
		AuthURL:  AuthURL,
		TokenURL: TokenURL,
		DefaultScopes: []string{
			ScopeUserAccountsRead,
			ScopeBoardsRead,
			ScopePinsRead,
		},
		SupportedScopeTypes: []string{"user", "org"},
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

	oauthProvider, err := providers.NewOAuth2Provider(providers.OAuth2Config{
		ID:                  ProviderID,
		AuthURL:             cfg.AuthURL,
		TokenURL:            cfg.TokenURL,
		ClientID:            cfg.ClientID,
		ClientSecret:        cfg.ClientSecret,
		DefaultScopes:       normalizePinterestScopes(cfg.DefaultScopes),
		SupportedScopeTypes: cfg.SupportedScopeTypes,
		TokenTTL:            cfg.TokenTTL,
		Capabilities:        BaselineCapabilities(),
	})
	if err != nil {
		return nil, err
	}
	return &Provider{OAuth2Provider: oauthProvider}, nil
}

func BaselineCapabilities() []core.CapabilityDescriptor {
	return []core.CapabilityDescriptor{
		{
			Name:           "account.read",
			RequiredGrants: []string{GrantUserAccountsRead},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
		{
			Name:           "boards.read",
			RequiredGrants: []string{GrantBoardsRead},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
		{
			Name:           "pins.read",
			RequiredGrants: []string{GrantPinsRead},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
	}
}

func (p *Provider) NormalizeGrantedPermissions(_ context.Context, raw []string) ([]string, error) {
	_ = p
	return normalizeCanonicalGrants(raw), nil
}

func normalizePinterestScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{}
	}
	set := map[string]struct{}{}
	for _, scope := range scopes {
		normalized := normalizePinterestScope(scope)
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
		normalized := normalizePinterestGrant(grant)
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

func normalizePinterestScope(value string) string {
	normalized := strings.TrimSpace(strings.ToLower(value))
	normalized = strings.TrimPrefix(normalized, "pinterest:")
	switch normalized {
	case ScopeUserAccountsRead, ScopeBoardsRead, ScopePinsRead:
		return normalized
	default:
		return ""
	}
}

func normalizePinterestGrant(value string) string {
	scope := normalizePinterestScope(value)
	if scope == "" {
		return ""
	}
	return "pinterest:" + scope
}

var _ core.Provider = (*Provider)(nil)
var _ core.GrantAwareProvider = (*Provider)(nil)
