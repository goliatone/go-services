package salesforce

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-services/auth"
	"github.com/goliatone/go-services/core"
)

const (
	ProviderID        = "salesforce"
	DefaultTokenURL   = "https://login.salesforce.com/services/oauth2/token"
	DefaultAPIBase    = "https://api.salesforce.com"
	defaultAPIVersion = "v60.0"
)

const (
	ScopeAPI    = "api"
	ScopeBulk   = "bulk_api"
	ScopeManage = "full"
)

const (
	GrantAPIRead    = "salesforce:api.read"
	GrantAPIWrite   = "salesforce:api.write"
	GrantBulkExport = "salesforce:bulk.export"
)

type Config struct {
	ClientID          string
	ClientSecret      string
	TokenURL          string
	DefaultScopes     []string
	SupportedScopes   []string
	InstanceURL       string
	ExternalAccountID string
}

type Provider struct {
	strategy      core.AuthStrategy
	instanceURL   string
	capabilities  []core.CapabilityDescriptor
	scopeTypes    []string
	externalAcct  string
	defaultGrants []string
}

func DefaultConfig() Config {
	return Config{
		TokenURL:        DefaultTokenURL,
		DefaultScopes:   []string{ScopeAPI},
		SupportedScopes: []string{"org"},
		InstanceURL:     DefaultAPIBase,
	}
}

func New(cfg Config) (core.Provider, error) {
	defaults := DefaultConfig()
	if strings.TrimSpace(cfg.TokenURL) == "" {
		cfg.TokenURL = defaults.TokenURL
	}
	if len(cfg.DefaultScopes) == 0 {
		cfg.DefaultScopes = defaults.DefaultScopes
	}
	if len(cfg.SupportedScopes) == 0 {
		cfg.SupportedScopes = defaults.SupportedScopes
	}
	if strings.TrimSpace(cfg.InstanceURL) == "" {
		cfg.InstanceURL = defaults.InstanceURL
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, fmt.Errorf("providers/salesforce: client id is required")
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		return nil, fmt.Errorf("providers/salesforce: client secret is required")
	}

	normalizedScopes := normalizeScopes(cfg.DefaultScopes)
	defaultGrants := normalizeCanonicalGrants(normalizedScopes)
	strategy := auth.NewOAuth2ClientCredentialsStrategy(auth.OAuth2ClientCredentialsStrategyConfig{
		ClientID:          strings.TrimSpace(cfg.ClientID),
		ClientSecret:      strings.TrimSpace(cfg.ClientSecret),
		TokenURL:          strings.TrimSpace(cfg.TokenURL),
		DefaultScopes:     normalizedScopes,
		ExternalAccountID: strings.TrimSpace(cfg.ExternalAccountID),
	})

	return &Provider{
		strategy:      strategy,
		instanceURL:   strings.TrimRight(strings.TrimSpace(cfg.InstanceURL), "/"),
		capabilities:  Capabilities(),
		scopeTypes:    normalizeScopeTypes(cfg.SupportedScopes),
		externalAcct:  strings.TrimSpace(cfg.ExternalAccountID),
		defaultGrants: defaultGrants,
	}, nil
}

func Capabilities() []core.CapabilityDescriptor {
	return []core.CapabilityDescriptor{
		{
			Name:           "crm.accounts.read",
			RequiredGrants: []string{GrantAPIRead},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
		{
			Name:           "crm.accounts.write",
			RequiredGrants: []string{GrantAPIWrite},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
		{
			Name:           "crm.accounts.bulk_export",
			RequiredGrants: []string{GrantAPIRead},
			OptionalGrants: []string{GrantBulkExport},
			DeniedBehavior: core.CapabilityDeniedBehaviorDegrade,
		},
	}
}

func (p *Provider) ID() string {
	return ProviderID
}

func (*Provider) AuthKind() core.AuthKind {
	return core.AuthKindOAuth2ClientCredential
}

func (p *Provider) SupportedScopeTypes() []string {
	if p == nil {
		return []string{}
	}
	return append([]string(nil), p.scopeTypes...)
}

func (p *Provider) Capabilities() []core.CapabilityDescriptor {
	if p == nil {
		return []core.CapabilityDescriptor{}
	}
	out := make([]core.CapabilityDescriptor, len(p.capabilities))
	copy(out, p.capabilities)
	return out
}

func (p *Provider) AuthStrategy() core.AuthStrategy {
	if p == nil {
		return nil
	}
	return p.strategy
}

func (p *Provider) BeginAuth(ctx context.Context, req core.BeginAuthRequest) (core.BeginAuthResponse, error) {
	if p == nil || p.strategy == nil {
		return core.BeginAuthResponse{}, fmt.Errorf("providers/salesforce: provider strategy is not configured")
	}
	begin, err := p.strategy.Begin(ctx, core.AuthBeginRequest{
		Scope:        req.Scope,
		RedirectURI:  req.RedirectURI,
		State:        req.State,
		RequestedRaw: append([]string(nil), req.RequestedGrants...),
		Metadata:     cloneMetadata(req.Metadata),
	})
	if err != nil {
		return core.BeginAuthResponse{}, err
	}
	return core.BeginAuthResponse{
		URL:             begin.URL,
		State:           begin.State,
		RequestedGrants: append([]string(nil), begin.RequestedGrants...),
		Metadata:        cloneMetadata(begin.Metadata),
	}, nil
}

func (p *Provider) CompleteAuth(ctx context.Context, req core.CompleteAuthRequest) (core.CompleteAuthResponse, error) {
	if p == nil || p.strategy == nil {
		return core.CompleteAuthResponse{}, fmt.Errorf("providers/salesforce: provider strategy is not configured")
	}
	metadata := cloneMetadata(req.Metadata)
	if _, ok := metadata["requested_grants"]; !ok {
		metadata["requested_grants"] = append([]string(nil), p.defaultGrants...)
	}
	if _, ok := metadata["granted_grants"]; !ok {
		metadata["granted_grants"] = append([]string(nil), p.defaultGrants...)
	}
	complete, err := p.strategy.Complete(ctx, core.AuthCompleteRequest{
		Scope:       req.Scope,
		Code:        req.Code,
		State:       req.State,
		RedirectURI: req.RedirectURI,
		Metadata:    metadata,
	})
	if err != nil {
		return core.CompleteAuthResponse{}, err
	}
	complete.Credential.GrantedScopes = normalizeCanonicalGrants(complete.Credential.GrantedScopes)
	complete.Credential.RequestedScopes = normalizeCanonicalGrants(complete.Credential.RequestedScopes)
	complete.GrantedGrants = normalizeCanonicalGrants(complete.GrantedGrants)
	complete.RequestedGrants = normalizeCanonicalGrants(complete.RequestedGrants)
	if strings.TrimSpace(complete.ExternalAccountID) == "" && p.externalAcct != "" {
		complete.ExternalAccountID = p.externalAcct
	}
	return core.CompleteAuthResponse{
		ExternalAccountID: complete.ExternalAccountID,
		Credential:        complete.Credential,
		RequestedGrants:   append([]string(nil), complete.RequestedGrants...),
		GrantedGrants:     append([]string(nil), complete.GrantedGrants...),
		Metadata:          cloneMetadata(complete.Metadata),
	}, nil
}

func (p *Provider) Refresh(ctx context.Context, cred core.ActiveCredential) (core.RefreshResult, error) {
	if p == nil || p.strategy == nil {
		return core.RefreshResult{}, fmt.Errorf("providers/salesforce: provider strategy is not configured")
	}
	refreshed, err := p.strategy.Refresh(ctx, cred)
	if err != nil {
		return core.RefreshResult{}, err
	}
	refreshed.Credential.GrantedScopes = normalizeCanonicalGrants(refreshed.Credential.GrantedScopes)
	refreshed.Credential.RequestedScopes = normalizeCanonicalGrants(refreshed.Credential.RequestedScopes)
	refreshed.GrantedGrants = normalizeCanonicalGrants(refreshed.GrantedGrants)
	return refreshed, nil
}

func (p *Provider) NormalizeGrantedPermissions(_ context.Context, raw []string) ([]string, error) {
	return normalizeCanonicalGrants(raw), nil
}

func (p *Provider) ResolveCapabilityOperation(
	_ context.Context,
	req core.CapabilityOperationResolveRequest,
) (core.ProviderOperationRequest, error) {
	if p == nil {
		return core.ProviderOperationRequest{}, fmt.Errorf("providers/salesforce: provider is nil")
	}
	baseURL := strings.TrimRight(p.instanceURL, "/")
	if baseURL == "" {
		baseURL = DefaultAPIBase
	}
	bucketKey := firstNonEmpty(strings.TrimSpace(req.BucketKey), "crm.accounts")
	operation := strings.TrimSpace(req.Operation)
	payload := cloneMetadata(req.Payload)
	transportConfig := cloneMetadata(req.TransportConfig)

	switch strings.TrimSpace(req.Capability) {
	case "crm.accounts.read":
		if operation == "" {
			operation = "salesforce.accounts.read"
		}
		return core.ProviderOperationRequest{
			ProviderID:      req.ProviderID,
			ConnectionID:    req.Connection.ID,
			Scope:           req.Scope,
			Operation:       operation,
			BucketKey:       bucketKey,
			TransportKind:   resolveTransportKind(req.TransportKind, "rest"),
			TransportConfig: transportConfig,
			Credential:      runtimeCredential(),
			TransportRequest: core.TransportRequest{
				Method: "GET",
				URL:    baseURL + "/services/data/" + defaultAPIVersion + "/sobjects/Account",
				Query: map[string]string{
					"limit": resolveLimit(payload, 50),
				},
			},
			Normalize: normalizeSalesforceResponse,
		}, nil
	case "crm.accounts.write":
		if operation == "" {
			operation = "salesforce.accounts.write"
		}
		body, err := marshalPayload(payload, map[string]any{"Name": "New Account"})
		if err != nil {
			return core.ProviderOperationRequest{}, err
		}
		return core.ProviderOperationRequest{
			ProviderID:      req.ProviderID,
			ConnectionID:    req.Connection.ID,
			Scope:           req.Scope,
			Operation:       operation,
			BucketKey:       bucketKey,
			TransportKind:   resolveTransportKind(req.TransportKind, "rest"),
			TransportConfig: transportConfig,
			Credential:      runtimeCredential(),
			TransportRequest: core.TransportRequest{
				Method: "POST",
				URL:    baseURL + "/services/data/" + defaultAPIVersion + "/sobjects/Account",
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
				Body: body,
			},
			Normalize: normalizeSalesforceResponse,
		}, nil
	case "crm.accounts.bulk_export":
		if shouldDegrade(req.Decision) {
			if operation == "" {
				operation = "salesforce.accounts.bulk_export.degraded"
			}
			return core.ProviderOperationRequest{
				ProviderID:      req.ProviderID,
				ConnectionID:    req.Connection.ID,
				Scope:           req.Scope,
				Operation:       operation,
				BucketKey:       bucketKey,
				TransportKind:   "rest",
				TransportConfig: transportConfig,
				Credential:      runtimeCredential(),
				TransportRequest: core.TransportRequest{
					Method: "GET",
					URL:    baseURL + "/services/data/" + defaultAPIVersion + "/sobjects/Account",
					Query: map[string]string{
						"limit": resolveLimit(payload, 25),
					},
					Metadata: map[string]any{"degraded": true},
				},
				Normalize: normalizeSalesforceResponse,
			}, nil
		}
		if operation == "" {
			operation = "salesforce.accounts.bulk_export"
		}
		query := strings.TrimSpace(fmt.Sprint(payload["query"]))
		if query == "" {
			query = "SELECT Id, Name FROM Account LIMIT 200"
		}
		body, err := marshalPayload(map[string]any{
			"operation": "query",
			"query":     query,
		}, nil)
		if err != nil {
			return core.ProviderOperationRequest{}, err
		}
		return core.ProviderOperationRequest{
			ProviderID:      req.ProviderID,
			ConnectionID:    req.Connection.ID,
			Scope:           req.Scope,
			Operation:       operation,
			BucketKey:       bucketKey,
			TransportKind:   resolveTransportKind(req.TransportKind, "bulk"),
			TransportConfig: transportConfig,
			Credential:      runtimeCredential(),
			TransportRequest: core.TransportRequest{
				Method: "POST",
				URL:    baseURL + "/services/data/" + defaultAPIVersion + "/jobs/query",
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
				Body: body,
			},
			Normalize: normalizeSalesforceResponse,
		}, nil
	default:
		return core.ProviderOperationRequest{}, fmt.Errorf(
			"providers/salesforce: unsupported capability %q",
			req.Capability,
		)
	}
}

func normalizeScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{}
	}
	set := map[string]struct{}{}
	for _, scope := range scopes {
		normalized := strings.TrimSpace(strings.ToLower(scope))
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

func normalizeCanonicalGrants(raw []string) []string {
	if len(raw) == 0 {
		return []string{}
	}
	set := map[string]struct{}{}
	for _, grant := range raw {
		normalized := strings.TrimSpace(strings.ToLower(grant))
		normalized = strings.TrimPrefix(normalized, "salesforce:")
		switch normalized {
		case "api", "api.read":
			set[GrantAPIRead] = struct{}{}
		case "full", "api.write":
			set[GrantAPIWrite] = struct{}{}
		case "bulk", "bulk_api", "bulk.export":
			set[GrantBulkExport] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for grant := range set {
		out = append(out, grant)
	}
	sort.Strings(out)
	return out
}

func normalizeScopeTypes(raw []string) []string {
	if len(raw) == 0 {
		return []string{"org"}
	}
	set := map[string]struct{}{}
	for _, item := range raw {
		normalized := strings.TrimSpace(strings.ToLower(item))
		if normalized == "" {
			continue
		}
		set[normalized] = struct{}{}
	}
	if len(set) == 0 {
		return []string{"org"}
	}
	out := make([]string, 0, len(set))
	for scope := range set {
		out = append(out, scope)
	}
	sort.Strings(out)
	return out
}

func normalizeSalesforceResponse(
	_ context.Context,
	response core.TransportResponse,
) (core.ProviderResponseMeta, error) {
	meta := core.ProviderResponseMeta{
		StatusCode: response.StatusCode,
		Headers:    cloneStringMap(response.Headers),
		Metadata: map[string]any{
			"provider": "salesforce",
		},
	}
	if rawRetry := strings.TrimSpace(response.Headers["Retry-After"]); rawRetry != "" {
		if seconds, err := strconv.Atoi(rawRetry); err == nil && seconds > 0 {
			d := time.Duration(seconds) * time.Second
			meta.RetryAfter = &d
		}
	}
	if raw := strings.TrimSpace(response.Headers["Sforce-Limit-Info"]); raw != "" {
		meta.Metadata["limit_info"] = raw
	}
	return meta, nil
}

func resolveLimit(payload map[string]any, fallback int) string {
	if len(payload) == 0 {
		return strconv.Itoa(fallback)
	}
	if value, ok := payload["limit"]; ok {
		trimmed := strings.TrimSpace(fmt.Sprint(value))
		if trimmed != "" {
			return trimmed
		}
	}
	return strconv.Itoa(fallback)
}

func marshalPayload(payload map[string]any, fallback map[string]any) ([]byte, error) {
	selected := payload
	if len(selected) == 0 {
		selected = fallback
	}
	if len(selected) == 0 {
		selected = map[string]any{}
	}
	body, err := json.Marshal(selected)
	if err != nil {
		return nil, fmt.Errorf("providers/salesforce: encode payload: %w", err)
	}
	return body, nil
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

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
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

func resolveTransportKind(raw string, fallback string) string {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	if trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(strings.ToLower(fallback))
}

func shouldDegrade(decision core.CapabilityResult) bool {
	if decision.Mode != core.CapabilityDeniedBehaviorDegrade {
		return false
	}
	if len(decision.Metadata) == 0 {
		return false
	}
	raw, ok := decision.Metadata["missing_grants"]
	if !ok || raw == nil {
		return false
	}
	switch typed := raw.(type) {
	case []string:
		return len(typed) > 0
	case []any:
		return len(typed) > 0
	default:
		return strings.TrimSpace(fmt.Sprint(raw)) != ""
	}
}

func runtimeCredential() *core.ActiveCredential {
	return &core.ActiveCredential{
		TokenType:   "bearer",
		AccessToken: "runtime_access_token",
	}
}

var _ core.Provider = (*Provider)(nil)
var _ core.AuthStrategyProvider = (*Provider)(nil)
var _ core.GrantAwareProvider = (*Provider)(nil)
var _ core.CapabilityOperationResolver = (*Provider)(nil)
