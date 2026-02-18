package tiktok

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers"
)

const (
	ProviderID = "tiktok"
	AuthURL    = "https://www.tiktok.com/v2/auth/authorize/"
	TokenURL   = "https://open.tiktokapis.com/v2/oauth/token/"
)

const (
	ScopeUserInfoBasic = "user.info.basic"
	ScopeVideoList     = "video.list"
	ScopeVideoInsights = "video.insights"
)

const (
	GrantUserInfoBasic = "tiktok:user.info.basic"
	GrantVideoList     = "tiktok:video.list"
	GrantVideoInsights = "tiktok:video.insights"
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
			ScopeUserInfoBasic,
			ScopeVideoList,
			ScopeVideoInsights,
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
		DefaultScopes:       normalizeTikTokScopes(cfg.DefaultScopes),
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
			Name:           "profile.read",
			RequiredGrants: []string{GrantUserInfoBasic},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
		{
			Name:           "video.read",
			RequiredGrants: []string{GrantVideoList},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
		{
			Name:           "analytics.read",
			RequiredGrants: []string{GrantVideoInsights},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
	}
}

func (p *Provider) NormalizeGrantedPermissions(_ context.Context, raw []string) ([]string, error) {
	_ = p
	return normalizeCanonicalGrants(raw), nil
}

func normalizeTikTokScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{}
	}
	set := map[string]struct{}{}
	for _, scope := range scopes {
		normalized := normalizeTikTokScope(scope)
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
		normalized := normalizeTikTokGrant(grant)
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

func normalizeTikTokScope(value string) string {
	normalized := strings.TrimSpace(strings.ToLower(value))
	normalized = strings.TrimPrefix(normalized, "tiktok:")
	switch normalized {
	case ScopeUserInfoBasic, ScopeVideoList, ScopeVideoInsights:
		return normalized
	default:
		return ""
	}
}

func normalizeTikTokGrant(value string) string {
	scope := normalizeTikTokScope(value)
	if scope == "" {
		return ""
	}
	return "tiktok:" + scope
}

var _ core.Provider = (*Provider)(nil)
var _ core.GrantAwareProvider = (*Provider)(nil)
