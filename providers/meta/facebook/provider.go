package facebook

import (
	"context"
	"sort"
	"strings"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers"
	meta "github.com/goliatone/go-services/providers/meta/common"
)

const ProviderID = "meta_facebook"

const (
	ScopePagesShowList       = "pages_show_list"
	ScopePagesReadEngagement = "pages_read_engagement"
	ScopeBusinessManagement  = "business_management"
)

const (
	GrantPagesShowList       = "facebook:pages_show_list"
	GrantPagesReadEngagement = "facebook:pages_read_engagement"
	GrantBusinessManagement  = "facebook:business_management"
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
			ScopePagesShowList,
			ScopePagesReadEngagement,
			ScopeBusinessManagement,
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
			Name:           "pages.read",
			RequiredGrants: []string{GrantPagesShowList},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
		{
			Name:           "engagement.read",
			RequiredGrants: []string{GrantPagesReadEngagement},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
		{
			Name:           "business.read",
			RequiredGrants: []string{GrantBusinessManagement},
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
		normalized := normalizeFacebookGrant(grant)
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

func normalizeFacebookGrant(value string) string {
	normalized := strings.TrimSpace(strings.ToLower(value))
	normalized = strings.TrimPrefix(normalized, "facebook:")
	normalized = strings.TrimPrefix(normalized, "meta:")

	switch normalized {
	case ScopePagesShowList, ScopePagesReadEngagement, ScopeBusinessManagement:
		return "facebook:" + normalized
	default:
		return ""
	}
}

var _ core.Provider = (*Provider)(nil)
var _ core.GrantAwareProvider = (*Provider)(nil)
