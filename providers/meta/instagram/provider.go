package instagram

import (
	"context"
	"sort"
	"strings"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers"
	meta "github.com/goliatone/go-services/providers/meta/common"
)

const ProviderID = "meta_instagram"

const (
	ScopeInstagramBasic          = "instagram_basic"
	ScopeInstagramManageInsights = "instagram_manage_insights"
	ScopePagesShowList           = "pages_show_list"
)

const (
	GrantInstagramBasic          = "instagram:instagram_basic"
	GrantInstagramManageInsights = "instagram:instagram_manage_insights"
	GrantPagesShowList           = "instagram:pages_show_list"
)

type Config = meta.AuthConfig

type Provider struct {
	*providers.OAuth2Provider
}

func DefaultConfig() Config {
	return Config{
		AuthURL:  meta.MetaOAuthAuthURL,
		TokenURL: meta.MetaOAuthTokenURL,
		DefaultScopes: []string{
			ScopeInstagramBasic,
			ScopeInstagramManageInsights,
			ScopePagesShowList,
		},
		SupportedScopeTypes: []string{"user", "org"},
	}
}

func New(cfg Config) (core.Provider, error) {
	defaults := DefaultConfig()
	if len(cfg.DefaultScopes) == 0 {
		cfg.DefaultScopes = defaults.DefaultScopes
	}
	if len(cfg.SupportedScopeTypes) == 0 {
		cfg.SupportedScopeTypes = defaults.SupportedScopeTypes
	}

	oauthCfg, err := meta.ResolveOAuth2Config(ProviderID, cfg, defaults.DefaultScopes, BaselineCapabilities())
	if err != nil {
		return nil, err
	}
	oauthProvider, err := providers.NewOAuth2Provider(oauthCfg)
	if err != nil {
		return nil, err
	}

	return &Provider{OAuth2Provider: oauthProvider}, nil
}

func BaselineCapabilities() []core.CapabilityDescriptor {
	return []core.CapabilityDescriptor{
		{
			Name:           "media.read",
			RequiredGrants: []string{GrantInstagramBasic},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
		{
			Name:           "insights.read",
			RequiredGrants: []string{GrantInstagramManageInsights},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
		{
			Name:           "page.binding.read",
			RequiredGrants: []string{GrantPagesShowList},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
	}
}

func (p *Provider) NormalizeGrantedPermissions(_ context.Context, raw []string) ([]string, error) {
	_ = p
	return normalizeCanonicalGrants(raw), nil
}

func normalizeCanonicalGrants(grants []string) []string {
	if len(grants) == 0 {
		return []string{}
	}
	set := map[string]struct{}{}
	for _, grant := range grants {
		normalized := normalizeInstagramGrant(grant)
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

func normalizeInstagramGrant(value string) string {
	normalized := strings.TrimSpace(strings.ToLower(value))
	normalized = strings.TrimPrefix(normalized, "instagram:")
	normalized = strings.TrimPrefix(normalized, "meta:")

	switch normalized {
	case ScopeInstagramBasic, ScopeInstagramManageInsights, ScopePagesShowList:
		return "instagram:" + normalized
	default:
		return ""
	}
}

var _ core.Provider = (*Provider)(nil)
var _ core.GrantAwareProvider = (*Provider)(nil)
