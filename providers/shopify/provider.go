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
	shopifyembedded "github.com/goliatone/go-services/providers/shopify/embedded"
)

const (
	ProviderID = "shopify"

	defaultAuthorizePath = "/admin/oauth/authorize"
	defaultTokenPath     = "/admin/oauth/access_token"
	defaultDomainSuffix  = ".myshopify.com"
)

type Mode string

const (
	ModeHybrid       Mode = "hybrid"
	ModeEmbeddedOnly Mode = "embedded_only"
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
	Mode                Mode
	ClientID            string
	ClientSecret        string
	ShopDomain          string
	AuthURL             string
	TokenURL            string
	DefaultScopes       []string
	SupportedScopeTypes []string
	TokenTTL            time.Duration
	TokenRequestTimeout time.Duration
	HTTPClient          providers.HTTPDoer

	EmbeddedAuthService        core.EmbeddedAuthService
	EmbeddedExpectedShopDomain string
	EmbeddedClockSkew          time.Duration
	EmbeddedMaxIssuedAtAge     time.Duration
	EmbeddedReplayTTL          time.Duration
	EmbeddedReplayMaxEntries   int
}

type EmbeddedConfig struct {
	ClientID            string
	ClientSecret        string
	SupportedScopeTypes []string
	TokenRequestTimeout time.Duration
	HTTPClient          providers.HTTPDoer

	EmbeddedAuthService        core.EmbeddedAuthService
	EmbeddedExpectedShopDomain string
	EmbeddedClockSkew          time.Duration
	EmbeddedMaxIssuedAtAge     time.Duration
	EmbeddedReplayTTL          time.Duration
	EmbeddedReplayMaxEntries   int
}

type Provider struct {
	id                  string
	mode                Mode
	oauth               *providers.OAuth2Provider
	embeddedAuth        core.EmbeddedAuthService
	supportedScopeTypes []string
	capabilities        []core.CapabilityDescriptor
}

func DefaultConfig() Config {
	return Config{
		Mode:                ModeHybrid,
		DefaultScopes:       []string{ScopeReadProducts, ScopeReadInventory, ScopeReadOrders},
		SupportedScopeTypes: []string{"org"},
	}
}

func NewEmbedded(cfg EmbeddedConfig) (core.Provider, error) {
	return New(Config{
		Mode:                       ModeEmbeddedOnly,
		ClientID:                   cfg.ClientID,
		ClientSecret:               cfg.ClientSecret,
		SupportedScopeTypes:        append([]string(nil), cfg.SupportedScopeTypes...),
		TokenRequestTimeout:        cfg.TokenRequestTimeout,
		HTTPClient:                 cfg.HTTPClient,
		EmbeddedAuthService:        cfg.EmbeddedAuthService,
		EmbeddedExpectedShopDomain: cfg.EmbeddedExpectedShopDomain,
		EmbeddedClockSkew:          cfg.EmbeddedClockSkew,
		EmbeddedMaxIssuedAtAge:     cfg.EmbeddedMaxIssuedAtAge,
		EmbeddedReplayTTL:          cfg.EmbeddedReplayTTL,
		EmbeddedReplayMaxEntries:   cfg.EmbeddedReplayMaxEntries,
	})
}

func New(cfg Config) (core.Provider, error) {
	defaults := DefaultConfig()
	mode, err := normalizeMode(cfg.Mode)
	if err != nil {
		return nil, err
	}
	cfg.Mode = mode
	if len(cfg.DefaultScopes) == 0 {
		cfg.DefaultScopes = defaults.DefaultScopes
	}
	if len(cfg.SupportedScopeTypes) == 0 {
		cfg.SupportedScopeTypes = defaults.SupportedScopeTypes
	}

	var oauthProvider *providers.OAuth2Provider
	if cfg.Mode != ModeEmbeddedOnly {
		authURL, tokenURL, resolveErr := resolveOAuthEndpoints(cfg)
		if resolveErr != nil {
			return nil, resolveErr
		}

		oauthProvider, err = providers.NewOAuth2Provider(providers.OAuth2Config{
			ID:                  ProviderID,
			AuthURL:             authURL,
			TokenURL:            tokenURL,
			ClientID:            cfg.ClientID,
			ClientSecret:        cfg.ClientSecret,
			ClientSecretInBody:  true,
			DefaultScopes:       normalizeShopifyScopes(cfg.DefaultScopes),
			SupportedScopeTypes: cfg.SupportedScopeTypes,
			TokenTTL:            cfg.TokenTTL,
			TokenRequestTimeout: cfg.TokenRequestTimeout,
			HTTPClient:          cfg.HTTPClient,
			Capabilities:        BaselineCapabilities(),
		})
		if err != nil {
			return nil, err
		}
	}

	embeddedAuth, err := resolveEmbeddedAuthService(cfg)
	if err != nil {
		return nil, err
	}

	return &Provider{
		id:                  ProviderID,
		mode:                cfg.Mode,
		oauth:               oauthProvider,
		embeddedAuth:        embeddedAuth,
		supportedScopeTypes: append([]string(nil), cfg.SupportedScopeTypes...),
		capabilities:        cloneCapabilities(BaselineCapabilities()),
	}, nil
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

func (p *Provider) ID() string {
	if p == nil {
		return ""
	}
	return p.id
}

func (p *Provider) AuthKind() core.AuthKind {
	_ = p
	return core.AuthKindOAuth2AuthCode
}

func (p *Provider) SupportedScopeTypes() []string {
	if p == nil {
		return []string{}
	}
	return append([]string(nil), p.supportedScopeTypes...)
}

func (p *Provider) Capabilities() []core.CapabilityDescriptor {
	if p == nil {
		return []core.CapabilityDescriptor{}
	}
	return cloneCapabilities(p.capabilities)
}

func (p *Provider) BeginAuth(ctx context.Context, req core.BeginAuthRequest) (core.BeginAuthResponse, error) {
	if p == nil {
		return core.BeginAuthResponse{}, fmt.Errorf("providers/shopify: provider is nil")
	}
	if p.oauth == nil {
		return core.BeginAuthResponse{}, p.authFlowUnsupportedError("begin auth")
	}
	return p.oauth.BeginAuth(ctx, req)
}

func (p *Provider) CompleteAuth(
	ctx context.Context,
	req core.CompleteAuthRequest,
) (core.CompleteAuthResponse, error) {
	if p == nil {
		return core.CompleteAuthResponse{}, fmt.Errorf("providers/shopify: provider is nil")
	}
	if p.oauth == nil {
		return core.CompleteAuthResponse{}, p.authFlowUnsupportedError("complete auth")
	}
	return p.oauth.CompleteAuth(ctx, req)
}

func (p *Provider) Refresh(ctx context.Context, cred core.ActiveCredential) (core.RefreshResult, error) {
	if p == nil {
		return core.RefreshResult{}, fmt.Errorf("providers/shopify: provider is nil")
	}
	if p.oauth == nil {
		return core.RefreshResult{}, p.authFlowUnsupportedError("refresh")
	}
	return p.oauth.Refresh(ctx, cred)
}

func (p *Provider) AuthenticateEmbedded(
	ctx context.Context,
	req core.EmbeddedAuthRequest,
) (core.EmbeddedAuthResult, error) {
	if p == nil || p.embeddedAuth == nil {
		return core.EmbeddedAuthResult{}, fmt.Errorf(
			"providers/shopify: embedded auth service is not configured: %w",
			ErrEmbeddedAuthServiceNotConfigured,
		)
	}
	if strings.TrimSpace(req.ProviderID) == "" {
		req.ProviderID = ProviderID
	}
	return p.embeddedAuth.AuthenticateEmbedded(ctx, req)
}

func (p *Provider) authFlowUnsupportedError(action string) error {
	mode := ModeHybrid
	if p != nil && strings.TrimSpace(string(p.mode)) != "" {
		mode = p.mode
	}
	return fmt.Errorf(
		"providers/shopify: oauth2 auth-code %s is disabled in mode %q: %w",
		action,
		mode,
		ErrAuthFlowUnsupported,
	)
}

func normalizeMode(mode Mode) (Mode, error) {
	normalized := Mode(strings.TrimSpace(strings.ToLower(string(mode))))
	if normalized == "" {
		return ModeHybrid, nil
	}
	switch normalized {
	case ModeHybrid, ModeEmbeddedOnly:
		return normalized, nil
	default:
		return "", fmt.Errorf("providers/shopify: unsupported mode %q", mode)
	}
}

func resolveEmbeddedAuthService(cfg Config) (core.EmbeddedAuthService, error) {
	embeddedAuth := cfg.EmbeddedAuthService
	if embeddedAuth == nil &&
		strings.TrimSpace(cfg.ClientID) != "" &&
		strings.TrimSpace(cfg.ClientSecret) != "" {
		embeddedService, err := shopifyembedded.NewService(shopifyembedded.ServiceConfig{
			ProviderID:          ProviderID,
			ClientID:            cfg.ClientID,
			ClientSecret:        cfg.ClientSecret,
			ExpectedShopDomain:  firstNonEmpty(cfg.EmbeddedExpectedShopDomain, cfg.ShopDomain),
			ClockSkew:           cfg.EmbeddedClockSkew,
			MaxIssuedAtAge:      cfg.EmbeddedMaxIssuedAtAge,
			ReplayTTL:           cfg.EmbeddedReplayTTL,
			ReplayMaxEntries:    cfg.EmbeddedReplayMaxEntries,
			TokenRequestTimeout: cfg.TokenRequestTimeout,
			HTTPClient:          cfg.HTTPClient,
		})
		if err != nil {
			return nil, err
		}
		embeddedAuth = embeddedService
	}
	return embeddedAuth, nil
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneCapabilities(input []core.CapabilityDescriptor) []core.CapabilityDescriptor {
	if len(input) == 0 {
		return []core.CapabilityDescriptor{}
	}
	out := make([]core.CapabilityDescriptor, 0, len(input))
	for _, descriptor := range input {
		out = append(out, core.CapabilityDescriptor{
			Name:           descriptor.Name,
			RequiredGrants: append([]string(nil), descriptor.RequiredGrants...),
			OptionalGrants: append([]string(nil), descriptor.OptionalGrants...),
			DeniedBehavior: descriptor.DeniedBehavior,
		})
	}
	return out
}

var _ core.Provider = (*Provider)(nil)
var _ core.GrantAwareProvider = (*Provider)(nil)
var _ core.EmbeddedAuthProvider = (*Provider)(nil)
