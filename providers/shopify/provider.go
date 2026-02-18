package shopify

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers"
)

const (
	ProviderID = "shopify"

	defaultAuthorizePath = "/admin/oauth/authorize"
	defaultTokenPath     = "/admin/oauth/access_token"
	defaultDomainSuffix  = ".myshopify.com"
)

const (
	ScopeReadProducts  = "read_products"
	ScopeReadInventory = "read_inventory"
	ScopeReadOrders    = "read_orders"
)

const (
	GrantReadProducts  = "shopify:read_products"
	GrantReadInventory = "shopify:read_inventory"
	GrantReadOrders    = "shopify:read_orders"
)

type Config struct {
	ClientID            string
	ClientSecret        string
	ShopDomain          string
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
		DefaultScopes:       []string{ScopeReadProducts, ScopeReadInventory, ScopeReadOrders},
		SupportedScopeTypes: []string{"org"},
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

	authURL, tokenURL, err := resolveOAuthEndpoints(cfg)
	if err != nil {
		return nil, err
	}

	oauthProvider, err := providers.NewOAuth2Provider(providers.OAuth2Config{
		ID:                  ProviderID,
		AuthURL:             authURL,
		TokenURL:            tokenURL,
		ClientID:            cfg.ClientID,
		ClientSecret:        cfg.ClientSecret,
		ClientSecretInBody:  true,
		DefaultScopes:       normalizeShopifyScopes(cfg.DefaultScopes),
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
			Name:           "catalog.read",
			RequiredGrants: []string{GrantReadProducts},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
		{
			Name:           "inventory.read",
			RequiredGrants: []string{GrantReadInventory},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
		{
			Name:           "orders.read",
			RequiredGrants: []string{GrantReadOrders},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
	}
}

func (p *Provider) NormalizeGrantedPermissions(_ context.Context, raw []string) ([]string, error) {
	_ = p
	return normalizeCanonicalGrants(raw), nil
}

func resolveOAuthEndpoints(cfg Config) (string, string, error) {
	authURL := strings.TrimSpace(cfg.AuthURL)
	tokenURL := strings.TrimSpace(cfg.TokenURL)
	if authURL != "" && tokenURL != "" {
		return authURL, tokenURL, nil
	}

	domain, err := normalizeShopDomain(cfg.ShopDomain)
	if err != nil {
		if authURL == "" || tokenURL == "" {
			return "", "", fmt.Errorf(
				"providers/shopify: auth_url and token_url are required when shop_domain is not configured: %w",
				err,
			)
		}
		return authURL, tokenURL, nil
	}
	if authURL == "" {
		authURL = (&url.URL{Scheme: "https", Host: domain, Path: defaultAuthorizePath}).String()
	}
	if tokenURL == "" {
		tokenURL = (&url.URL{Scheme: "https", Host: domain, Path: defaultTokenPath}).String()
	}
	return authURL, tokenURL, nil
}

func normalizeShopDomain(value string) (string, error) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return "", fmt.Errorf("providers/shopify: shop_domain is required")
	}
	if strings.Contains(trimmed, "://") {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return "", fmt.Errorf("providers/shopify: parse shop_domain: %w", err)
		}
		trimmed = strings.TrimSpace(strings.ToLower(parsed.Hostname()))
	}
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" || strings.Contains(trimmed, "/") {
		return "", fmt.Errorf("providers/shopify: invalid shop_domain")
	}
	if !strings.Contains(trimmed, ".") {
		trimmed += defaultDomainSuffix
	}
	if !strings.HasSuffix(trimmed, defaultDomainSuffix) {
		return "", fmt.Errorf("providers/shopify: shop_domain must end with %q", defaultDomainSuffix)
	}
	return trimmed, nil
}

func normalizeShopifyScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{}
	}
	set := map[string]struct{}{}
	for _, scope := range scopes {
		normalized := normalizeShopifyScope(scope)
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
		normalized := normalizeShopifyGrant(grant)
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

func normalizeShopifyScope(value string) string {
	normalized := strings.TrimSpace(strings.ToLower(value))
	normalized = strings.TrimPrefix(normalized, "shopify:")
	if normalized == "" {
		return ""
	}
	return normalized
}

func normalizeShopifyGrant(value string) string {
	scope := normalizeShopifyScope(value)
	if scope == "" {
		return ""
	}
	return "shopify:" + scope
}

var _ core.Provider = (*Provider)(nil)
var _ core.GrantAwareProvider = (*Provider)(nil)
