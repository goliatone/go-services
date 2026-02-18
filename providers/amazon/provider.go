package amazon

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers"
)

const (
	ProviderID = "amazon"
	AuthURL    = "https://sellercentral.amazon.com/apps/authorize/consent"
	TokenURL   = "https://api.amazon.com/auth/o2/token"
)

const (
	ScopeCatalogRead   = "sellingpartnerapi::catalog"
	ScopeInventoryRead = "sellingpartnerapi::inventory"
	ScopeOrdersRead    = "sellingpartnerapi::orders"
)

const (
	GrantCatalogRead   = "amazon:catalog.read"
	GrantInventoryRead = "amazon:inventory.read"
	GrantOrdersRead    = "amazon:orders.read"
)

const (
	defaultAWSService           = "execute-api"
	defaultAWSSigningMode       = "header"
	defaultAWSAccessTokenHeader = "x-amz-access-token"
)

var defaultAmazonHostRegionMap = map[string]string{
	"sellingpartnerapi-na.amazon.com":         "us-east-1",
	"sandbox.sellingpartnerapi-na.amazon.com": "us-east-1",
	"sellingpartnerapi-eu.amazon.com":         "eu-west-1",
	"sandbox.sellingpartnerapi-eu.amazon.com": "eu-west-1",
	"sellingpartnerapi-fe.amazon.com":         "us-west-2",
	"sandbox.sellingpartnerapi-fe.amazon.com": "us-west-2",
}

type SigV4Config struct {
	AccessKeyID       string
	SecretAccessKey   string
	SessionToken      string
	Region            string
	Service           string
	SigningMode       string
	SigningExpiresSec int
	UnsignedPayload   bool
	AccessTokenHeader string
	HostRegionMap     map[string]string
}

type Config struct {
	ClientID            string
	ClientSecret        string
	AuthURL             string
	TokenURL            string
	DefaultScopes       []string
	SupportedScopeTypes []string
	TokenTTL            time.Duration
	SigV4               SigV4Config
}

type Provider struct {
	*providers.OAuth2Provider
	sigv4 SigV4Config
}

func DefaultConfig() Config {
	return Config{
		AuthURL:  AuthURL,
		TokenURL: TokenURL,
		DefaultScopes: []string{
			ScopeCatalogRead,
			ScopeInventoryRead,
			ScopeOrdersRead,
		},
		SupportedScopeTypes: []string{"org"},
		SigV4: SigV4Config{
			Region:            "us-east-1",
			Service:           defaultAWSService,
			SigningMode:       defaultAWSSigningMode,
			AccessTokenHeader: defaultAWSAccessTokenHeader,
			HostRegionMap:     copyStringMap(defaultAmazonHostRegionMap),
		},
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

	normalizedSigV4, err := normalizeSigV4Config(cfg.SigV4)
	if err != nil {
		return nil, err
	}

	oauthProvider, err := providers.NewOAuth2Provider(providers.OAuth2Config{
		ID:                  ProviderID,
		AuthURL:             cfg.AuthURL,
		TokenURL:            cfg.TokenURL,
		ClientID:            cfg.ClientID,
		ClientSecret:        cfg.ClientSecret,
		DefaultScopes:       normalizeAmazonScopes(cfg.DefaultScopes),
		SupportedScopeTypes: cfg.SupportedScopeTypes,
		TokenTTL:            cfg.TokenTTL,
		Capabilities:        BaselineCapabilities(),
	})
	if err != nil {
		return nil, err
	}
	return &Provider{OAuth2Provider: oauthProvider, sigv4: normalizedSigV4}, nil
}

func BaselineCapabilities() []core.CapabilityDescriptor {
	return []core.CapabilityDescriptor{
		{
			Name:           "catalog.read",
			RequiredGrants: []string{GrantCatalogRead},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
		{
			Name:           "inventory.read",
			RequiredGrants: []string{GrantInventoryRead},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
		{
			Name:           "orders.read",
			RequiredGrants: []string{GrantOrdersRead},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
	}
}

func (p *Provider) NormalizeGrantedPermissions(_ context.Context, raw []string) ([]string, error) {
	_ = p
	return normalizeCanonicalGrants(raw), nil
}

func (p *Provider) CompleteAuth(ctx context.Context, req core.CompleteAuthRequest) (core.CompleteAuthResponse, error) {
	if p == nil || p.OAuth2Provider == nil {
		return core.CompleteAuthResponse{}, fmt.Errorf("providers/amazon: provider is nil")
	}
	complete, err := p.OAuth2Provider.CompleteAuth(ctx, req)
	if err != nil {
		return core.CompleteAuthResponse{}, err
	}
	complete.Credential.Metadata, err = p.applySigV4Metadata(complete.Credential.Metadata, req.Metadata, "")
	if err != nil {
		return core.CompleteAuthResponse{}, err
	}
	complete.Metadata = mergeMetadata(complete.Metadata, map[string]any{
		"signing_profile": core.AuthKindAWSSigV4,
		"aws_region":      complete.Credential.Metadata["aws_region"],
		"aws_service":     complete.Credential.Metadata["aws_service"],
	})
	return complete, nil
}

func (p *Provider) Refresh(ctx context.Context, cred core.ActiveCredential) (core.RefreshResult, error) {
	if p == nil || p.OAuth2Provider == nil {
		return core.RefreshResult{}, fmt.Errorf("providers/amazon: provider is nil")
	}
	refreshed, err := p.OAuth2Provider.Refresh(ctx, cred)
	if err != nil {
		return core.RefreshResult{}, err
	}
	refreshed.Credential.Metadata, err = p.applySigV4Metadata(refreshed.Credential.Metadata, cred.Metadata, "")
	if err != nil {
		return core.RefreshResult{}, err
	}
	refreshed.Metadata = mergeMetadata(refreshed.Metadata, map[string]any{
		"signing_profile": core.AuthKindAWSSigV4,
		"aws_region":      refreshed.Credential.Metadata["aws_region"],
		"aws_service":     refreshed.Credential.Metadata["aws_service"],
	})
	return refreshed, nil
}

func (p *Provider) Signer() core.Signer {
	if p == nil {
		return nil
	}
	return amazonSigV4Signer{profile: p.sigv4}
}

func (p *Provider) applySigV4Metadata(
	base map[string]any,
	runtime map[string]any,
	host string,
) (map[string]any, error) {
	metadata := cloneAnyMap(base)
	metadata["auth_kind"] = core.AuthKindAWSSigV4
	metadata["aws_access_key_id"] = firstNonEmpty(
		readString(runtime, "aws_access_key_id", "access_key_id"),
		p.sigv4.AccessKeyID,
		readString(metadata, "aws_access_key_id", "access_key_id"),
	)
	metadata["aws_secret_access_key"] = firstNonEmpty(
		readString(runtime, "aws_secret_access_key", "secret_access_key"),
		p.sigv4.SecretAccessKey,
		readString(metadata, "aws_secret_access_key", "secret_access_key"),
	)
	metadata["aws_session_token"] = firstNonEmpty(
		readString(runtime, "aws_session_token", "session_token"),
		p.sigv4.SessionToken,
		readString(metadata, "aws_session_token", "session_token"),
	)

	region := firstNonEmpty(
		readString(runtime, "aws_region", "region"),
		readString(metadata, "aws_region", "region"),
	)
	if hostRegion := p.resolveRegionForHost(host); hostRegion != "" {
		region = hostRegion
	}
	if region == "" {
		region = p.sigv4.Region
	}
	metadata["aws_region"] = strings.ToLower(strings.TrimSpace(region))

	metadata["aws_service"] = firstNonEmpty(
		readString(runtime, "aws_service", "service"),
		p.sigv4.Service,
		readString(metadata, "aws_service", "service"),
	)
	metadata["aws_signing_mode"] = normalizeSigningMode(firstNonEmpty(
		readString(runtime, "aws_signing_mode", "signing_mode"),
		p.sigv4.SigningMode,
		readString(metadata, "aws_signing_mode", "signing_mode"),
	))
	metadata["aws_access_token_header"] = firstNonEmpty(
		strings.ToLower(readString(runtime, "aws_access_token_header")),
		strings.ToLower(strings.TrimSpace(p.sigv4.AccessTokenHeader)),
		strings.ToLower(readString(metadata, "aws_access_token_header")),
	)
	if metadata["aws_access_token_header"] == "" {
		metadata["aws_access_token_header"] = defaultAWSAccessTokenHeader
	}
	if p.sigv4.SigningExpiresSec > 0 {
		metadata["aws_signing_expires"] = strconv.Itoa(p.sigv4.SigningExpiresSec)
	}
	metadata["aws_unsigned_payload"] = p.sigv4.UnsignedPayload

	if strings.TrimSpace(readString(metadata, "aws_access_key_id", "access_key_id")) == "" {
		return nil, fmt.Errorf("providers/amazon: aws_access_key_id is required for sigv4 signing")
	}
	if strings.TrimSpace(readString(metadata, "aws_secret_access_key", "secret_access_key")) == "" {
		return nil, fmt.Errorf("providers/amazon: aws_secret_access_key is required for sigv4 signing")
	}
	if strings.TrimSpace(readString(metadata, "aws_region", "region")) == "" {
		return nil, fmt.Errorf("providers/amazon: aws_region is required for sigv4 signing")
	}
	if strings.TrimSpace(readString(metadata, "aws_service", "service")) == "" {
		return nil, fmt.Errorf("providers/amazon: aws_service is required for sigv4 signing")
	}
	return metadata, nil
}

func (p *Provider) resolveRegionForHost(host string) string {
	normalized := normalizeHost(host)
	if normalized == "" {
		return ""
	}
	if region := strings.TrimSpace(p.sigv4.HostRegionMap[normalized]); region != "" {
		return strings.ToLower(strings.TrimSpace(region))
	}
	return ""
}

type amazonSigV4Signer struct {
	profile SigV4Config
}

func (s amazonSigV4Signer) Sign(ctx context.Context, req *http.Request, cred core.ActiveCredential) error {
	if req == nil {
		return fmt.Errorf("providers/amazon: http request is required")
	}
	metadata := cloneAnyMap(cred.Metadata)
	metadata["auth_kind"] = core.AuthKindAWSSigV4
	metadata["aws_access_key_id"] = firstNonEmpty(
		readString(metadata, "aws_access_key_id", "access_key_id"),
		s.profile.AccessKeyID,
	)
	metadata["aws_secret_access_key"] = firstNonEmpty(
		readString(metadata, "aws_secret_access_key", "secret_access_key"),
		s.profile.SecretAccessKey,
	)
	metadata["aws_session_token"] = firstNonEmpty(
		readString(metadata, "aws_session_token", "session_token"),
		s.profile.SessionToken,
	)
	metadata["aws_service"] = firstNonEmpty(
		readString(metadata, "aws_service", "service"),
		s.profile.Service,
	)
	metadata["aws_signing_mode"] = normalizeSigningMode(firstNonEmpty(
		readString(metadata, "aws_signing_mode", "signing_mode"),
		s.profile.SigningMode,
	))
	metadata["aws_access_token_header"] = firstNonEmpty(
		strings.ToLower(readString(metadata, "aws_access_token_header")),
		strings.ToLower(s.profile.AccessTokenHeader),
	)
	if metadata["aws_access_token_header"] == "" {
		metadata["aws_access_token_header"] = defaultAWSAccessTokenHeader
	}
	if s.profile.SigningExpiresSec > 0 {
		metadata["aws_signing_expires"] = strconv.Itoa(s.profile.SigningExpiresSec)
	}
	metadata["aws_unsigned_payload"] = s.profile.UnsignedPayload

	hostRegionMap := copyStringMap(defaultAmazonHostRegionMap)
	for host, region := range s.profile.HostRegionMap {
		hostRegionMap[normalizeHost(host)] = strings.ToLower(strings.TrimSpace(region))
	}
	host := normalizeHost(req.URL.Host)
	if hostRegion := strings.TrimSpace(hostRegionMap[host]); hostRegion != "" {
		metadata["aws_region"] = strings.ToLower(hostRegion)
	} else {
		metadata["aws_region"] = firstNonEmpty(
			readString(metadata, "aws_region", "region"),
			s.profile.Region,
		)
	}

	resolved := cred
	resolved.Metadata = metadata
	return core.AWSSigV4Signer{}.Sign(ctx, req, resolved)
}

func normalizeSigV4Config(cfg SigV4Config) (SigV4Config, error) {
	normalized := SigV4Config{
		AccessKeyID:       strings.TrimSpace(cfg.AccessKeyID),
		SecretAccessKey:   strings.TrimSpace(cfg.SecretAccessKey),
		SessionToken:      strings.TrimSpace(cfg.SessionToken),
		Region:            strings.ToLower(strings.TrimSpace(cfg.Region)),
		Service:           strings.TrimSpace(cfg.Service),
		SigningMode:       normalizeSigningMode(cfg.SigningMode),
		SigningExpiresSec: cfg.SigningExpiresSec,
		UnsignedPayload:   cfg.UnsignedPayload,
		AccessTokenHeader: strings.ToLower(strings.TrimSpace(cfg.AccessTokenHeader)),
		HostRegionMap:     copyStringMap(defaultAmazonHostRegionMap),
	}
	if normalized.Service == "" {
		normalized.Service = defaultAWSService
	}
	if normalized.SigningMode == "" {
		normalized.SigningMode = defaultAWSSigningMode
	}
	if normalized.AccessTokenHeader == "" {
		normalized.AccessTokenHeader = defaultAWSAccessTokenHeader
	}
	for host, region := range cfg.HostRegionMap {
		normalizedHost := normalizeHost(host)
		normalizedRegion := strings.ToLower(strings.TrimSpace(region))
		if normalizedHost == "" || normalizedRegion == "" {
			continue
		}
		normalized.HostRegionMap[normalizedHost] = normalizedRegion
	}
	if normalized.Region == "" {
		normalized.Region = normalized.HostRegionMap["sellingpartnerapi-na.amazon.com"]
	}
	if normalized.AccessKeyID == "" {
		return SigV4Config{}, fmt.Errorf("providers/amazon: sigv4 access key id is required")
	}
	if normalized.SecretAccessKey == "" {
		return SigV4Config{}, fmt.Errorf("providers/amazon: sigv4 secret access key is required")
	}
	if normalized.Region == "" {
		return SigV4Config{}, fmt.Errorf("providers/amazon: sigv4 region is required")
	}
	return normalized, nil
}

func normalizeAmazonScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{}
	}
	set := map[string]struct{}{}
	for _, scope := range scopes {
		normalized := normalizeAmazonScope(scope)
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
		normalized := normalizeAmazonGrant(grant)
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

func normalizeAmazonScope(value string) string {
	normalized := strings.TrimSpace(strings.ToLower(value))
	normalized = strings.TrimPrefix(normalized, "amazon:")
	normalized = strings.TrimPrefix(normalized, "amazon_sp_api:")
	normalized = strings.ReplaceAll(normalized, " ", "")
	switch normalized {
	case ScopeCatalogRead, "catalog", "catalog.read", "catalog_read", "sellingpartnerapi::catalogitems":
		return ScopeCatalogRead
	case ScopeInventoryRead, "inventory", "inventory.read", "inventory_read", "sellingpartnerapi::inventoryitems":
		return ScopeInventoryRead
	case ScopeOrdersRead, "orders", "orders.read", "orders_read", "sellingpartnerapi::orders_v0":
		return ScopeOrdersRead
	default:
		return ""
	}
}

func normalizeAmazonGrant(value string) string {
	scope := normalizeAmazonScope(value)
	switch scope {
	case ScopeCatalogRead:
		return GrantCatalogRead
	case ScopeInventoryRead:
		return GrantInventoryRead
	case ScopeOrdersRead:
		return GrantOrdersRead
	default:
		return ""
	}
}

func normalizeSigningMode(mode string) string {
	normalized := strings.TrimSpace(strings.ToLower(mode))
	if normalized == "query" {
		return "query"
	}
	return "header"
}

func normalizeHost(host string) string {
	trimmed := strings.TrimSpace(strings.ToLower(host))
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, ":") {
		if parsed, _, found := strings.Cut(trimmed, ":"); found {
			trimmed = parsed
		}
	}
	return strings.TrimSpace(trimmed)
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = strings.TrimSpace(value)
	}
	return out
}

func mergeMetadata(base map[string]any, extras map[string]any) map[string]any {
	out := cloneAnyMap(base)
	for key, value := range extras {
		out[key] = value
	}
	return out
}

func readString(metadata map[string]any, keys ...string) string {
	for _, key := range keys {
		if metadata == nil {
			continue
		}
		value, ok := metadata[key]
		if !ok || value == nil {
			continue
		}
		trimmed := strings.TrimSpace(fmt.Sprint(value))
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
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

var _ core.Provider = (*Provider)(nil)
var _ core.GrantAwareProvider = (*Provider)(nil)
var _ core.ProviderSigner = (*Provider)(nil)
