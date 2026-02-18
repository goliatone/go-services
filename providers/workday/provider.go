package workday

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-services/auth"
	"github.com/goliatone/go-services/core"
)

const (
	ProviderID       = "workday"
	DefaultTenantURL = "https://api.workday.com"
)

const (
	GrantHRRead       = "workday:hr.read"
	GrantCompRead     = "workday:comp.read"
	GrantReportExport = "workday:report.export"
)

type Config struct {
	Issuer            string
	Subject           string
	Audience          string
	SigningKey        string
	SigningAlgorithm  string
	KeyID             string
	TokenTTL          time.Duration
	TenantURL         string
	SupportedScopes   []string
	ExternalAccountID string
}

type Provider struct {
	strategy     core.AuthStrategy
	tenantURL    string
	scopeTypes   []string
	capabilities []core.CapabilityDescriptor
}

func DefaultConfig() Config {
	return Config{
		TenantURL:       DefaultTenantURL,
		SupportedScopes: []string{"org"},
	}
}

func New(cfg Config) (core.Provider, error) {
	defaults := DefaultConfig()
	if strings.TrimSpace(cfg.TenantURL) == "" {
		cfg.TenantURL = defaults.TenantURL
	}
	if len(cfg.SupportedScopes) == 0 {
		cfg.SupportedScopes = defaults.SupportedScopes
	}
	if strings.TrimSpace(cfg.Issuer) == "" {
		return nil, fmt.Errorf("providers/workday: issuer is required")
	}
	if strings.TrimSpace(cfg.Audience) == "" {
		return nil, fmt.Errorf("providers/workday: audience is required")
	}
	if strings.TrimSpace(cfg.SigningKey) == "" {
		return nil, fmt.Errorf("providers/workday: signing key is required")
	}

	strategy := auth.NewServiceAccountJWTStrategy(auth.ServiceAccountJWTStrategyConfig{
		Issuer:            strings.TrimSpace(cfg.Issuer),
		Subject:           strings.TrimSpace(cfg.Subject),
		Audience:          strings.TrimSpace(cfg.Audience),
		SigningKey:        strings.TrimSpace(cfg.SigningKey),
		SigningAlgorithm:  strings.TrimSpace(cfg.SigningAlgorithm),
		KeyID:             strings.TrimSpace(cfg.KeyID),
		TokenTTL:          cfg.TokenTTL,
		ExternalAccountID: strings.TrimSpace(cfg.ExternalAccountID),
	})

	return &Provider{
		strategy:     strategy,
		tenantURL:    strings.TrimRight(strings.TrimSpace(cfg.TenantURL), "/"),
		scopeTypes:   normalizeScopeTypes(cfg.SupportedScopes),
		capabilities: Capabilities(),
	}, nil
}

func Capabilities() []core.CapabilityDescriptor {
	return []core.CapabilityDescriptor{
		{
			Name:           "hr.employees.read",
			RequiredGrants: []string{GrantHRRead},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		},
		{
			Name:           "hr.compensation.read",
			RequiredGrants: []string{GrantHRRead},
			OptionalGrants: []string{GrantCompRead},
			DeniedBehavior: core.CapabilityDeniedBehaviorDegrade,
		},
		{
			Name:           "hr.reports.export",
			RequiredGrants: []string{GrantHRRead},
			OptionalGrants: []string{GrantReportExport},
			DeniedBehavior: core.CapabilityDeniedBehaviorDegrade,
		},
	}
}

func (p *Provider) ID() string {
	return ProviderID
}

func (*Provider) AuthKind() string {
	return core.AuthKindServiceAccountJWT
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
		return core.BeginAuthResponse{}, fmt.Errorf("providers/workday: provider strategy is not configured")
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
		return core.CompleteAuthResponse{}, fmt.Errorf("providers/workday: provider strategy is not configured")
	}
	complete, err := p.strategy.Complete(ctx, core.AuthCompleteRequest{
		Scope:       req.Scope,
		Code:        req.Code,
		State:       req.State,
		RedirectURI: req.RedirectURI,
		Metadata:    cloneMetadata(req.Metadata),
	})
	if err != nil {
		return core.CompleteAuthResponse{}, err
	}
	complete.Credential.GrantedScopes = normalizeCanonicalGrants(complete.Credential.GrantedScopes)
	complete.Credential.RequestedScopes = normalizeCanonicalGrants(complete.Credential.RequestedScopes)
	complete.GrantedGrants = normalizeCanonicalGrants(complete.GrantedGrants)
	complete.RequestedGrants = normalizeCanonicalGrants(complete.RequestedGrants)
	return complete, nil
}

func (p *Provider) Refresh(ctx context.Context, cred core.ActiveCredential) (core.RefreshResult, error) {
	if p == nil || p.strategy == nil {
		return core.RefreshResult{}, fmt.Errorf("providers/workday: provider strategy is not configured")
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
		return core.ProviderOperationRequest{}, fmt.Errorf("providers/workday: provider is nil")
	}
	baseURL := p.tenantURL
	if baseURL == "" {
		baseURL = DefaultTenantURL
	}
	payload := cloneMetadata(req.Payload)
	bucketKey := firstNonEmpty(strings.TrimSpace(req.BucketKey), "workday.hr")
	operation := strings.TrimSpace(req.Operation)
	transportConfig := cloneMetadata(req.TransportConfig)

	switch strings.TrimSpace(req.Capability) {
	case "hr.employees.read":
		if operation == "" {
			operation = "workday.hr.employees.read"
		}
		return core.ProviderOperationRequest{
			ProviderID:      req.ProviderID,
			ConnectionID:    req.Connection.ID,
			Scope:           req.Scope,
			Operation:       operation,
			BucketKey:       bucketKey,
			TransportKind:   resolveTransportKind(req.TransportKind, "soap"),
			TransportConfig: transportConfig,
			Credential:      runtimeCredential(),
			TransportRequest: core.TransportRequest{
				Method: "POST",
				URL:    baseURL + "/ccx/service/Human_Resources/v43.0",
				Headers: map[string]string{
					"SOAPAction": "Get_Workers",
				},
				Body: resolveSOAPEnvelope(payload, defaultWorkersEnvelope()),
			},
			Normalize: normalizeWorkdayResponse,
		}, nil
	case "hr.compensation.read":
		if req.Decision.Mode == core.CapabilityDeniedBehaviorDegrade {
			if operation == "" {
				operation = "workday.hr.compensation.read.degraded"
			}
			return core.ProviderOperationRequest{
				ProviderID:      req.ProviderID,
				ConnectionID:    req.Connection.ID,
				Scope:           req.Scope,
				Operation:       operation,
				BucketKey:       bucketKey,
				TransportKind:   "soap",
				TransportConfig: transportConfig,
				Credential:      runtimeCredential(),
				TransportRequest: core.TransportRequest{
					Method: "POST",
					URL:    baseURL + "/ccx/service/Human_Resources/v43.0",
					Headers: map[string]string{
						"SOAPAction": "Get_Workers",
					},
					Body:     resolveSOAPEnvelope(payload, defaultWorkersEnvelope()),
					Metadata: map[string]any{"degraded": true},
				},
				Normalize: normalizeWorkdayResponse,
			}, nil
		}
		if operation == "" {
			operation = "workday.hr.compensation.read"
		}
		return core.ProviderOperationRequest{
			ProviderID:      req.ProviderID,
			ConnectionID:    req.Connection.ID,
			Scope:           req.Scope,
			Operation:       operation,
			BucketKey:       bucketKey,
			TransportKind:   resolveTransportKind(req.TransportKind, "soap"),
			TransportConfig: transportConfig,
			Credential:      runtimeCredential(),
			TransportRequest: core.TransportRequest{
				Method: "POST",
				URL:    baseURL + "/ccx/service/Compensation/v43.0",
				Headers: map[string]string{
					"SOAPAction": "Get_Compensation_Review_Results",
				},
				Body: resolveSOAPEnvelope(payload, defaultCompEnvelope()),
			},
			Normalize: normalizeWorkdayResponse,
		}, nil
	case "hr.reports.export":
		if req.Decision.Mode == core.CapabilityDeniedBehaviorDegrade {
			if operation == "" {
				operation = "workday.hr.reports.export.degraded"
			}
			return core.ProviderOperationRequest{
				ProviderID:      req.ProviderID,
				ConnectionID:    req.Connection.ID,
				Scope:           req.Scope,
				Operation:       operation,
				BucketKey:       bucketKey,
				TransportKind:   "stream",
				TransportConfig: transportConfig,
				Credential:      runtimeCredential(),
				TransportRequest: core.TransportRequest{
					Method: "GET",
					URL:    baseURL + "/ccx/api/reporting/v1/workers/summary",
					Query: map[string]string{
						"limit": resolveLimit(payload, 100),
					},
					Metadata: map[string]any{"degraded": true},
				},
				Normalize: normalizeWorkdayResponse,
			}, nil
		}
		if operation == "" {
			operation = "workday.hr.reports.export"
		}
		return core.ProviderOperationRequest{
			ProviderID:      req.ProviderID,
			ConnectionID:    req.Connection.ID,
			Scope:           req.Scope,
			Operation:       operation,
			BucketKey:       bucketKey,
			TransportKind:   resolveTransportKind(req.TransportKind, "file"),
			TransportConfig: transportConfig,
			Credential:      runtimeCredential(),
			TransportRequest: core.TransportRequest{
				Method: "POST",
				URL:    baseURL + "/ccx/exports/reports/workers",
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
				Body: resolveFilePayload(payload),
			},
			Normalize: normalizeWorkdayResponse,
		}, nil
	default:
		return core.ProviderOperationRequest{}, fmt.Errorf(
			"providers/workday: unsupported capability %q",
			req.Capability,
		)
	}
}

func normalizeCanonicalGrants(raw []string) []string {
	if len(raw) == 0 {
		return []string{}
	}
	set := map[string]struct{}{}
	for _, grant := range raw {
		normalized := strings.TrimSpace(strings.ToLower(grant))
		normalized = strings.TrimPrefix(normalized, "workday:")
		switch normalized {
		case "hr", "hr.read", "human_resources":
			set[GrantHRRead] = struct{}{}
		case "comp", "comp.read", "compensation":
			set[GrantCompRead] = struct{}{}
		case "report", "report.export", "reports", "reports.export":
			set[GrantReportExport] = struct{}{}
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
	for _, value := range raw {
		normalized := strings.TrimSpace(strings.ToLower(value))
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

func normalizeWorkdayResponse(
	_ context.Context,
	response core.TransportResponse,
) (core.ProviderResponseMeta, error) {
	meta := core.ProviderResponseMeta{
		StatusCode: response.StatusCode,
		Headers:    cloneStringMap(response.Headers),
		Metadata: map[string]any{
			"provider": "workday",
		},
	}
	if rawRetry := strings.TrimSpace(response.Headers["Retry-After"]); rawRetry != "" {
		if seconds, err := strconv.Atoi(rawRetry); err == nil && seconds > 0 {
			d := time.Duration(seconds) * time.Second
			meta.RetryAfter = &d
		}
	}
	return meta, nil
}

func resolveSOAPEnvelope(payload map[string]any, fallback string) []byte {
	if len(payload) > 0 {
		if value := strings.TrimSpace(fmt.Sprint(payload["soap_envelope"])); value != "" {
			return []byte(value)
		}
	}
	return []byte(fallback)
}

func resolveFilePayload(payload map[string]any) []byte {
	if len(payload) == 0 {
		return []byte(`{"report":"workers","format":"json"}`)
	}
	if raw, ok := payload["body"].([]byte); ok && len(raw) > 0 {
		return append([]byte(nil), raw...)
	}
	if text := strings.TrimSpace(fmt.Sprint(payload["body"])); text != "" && text != "<nil>" {
		return []byte(text)
	}
	return []byte(`{"report":"workers","format":"json"}`)
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

func defaultWorkersEnvelope() string {
	return `<wd:Get_Workers_Request xmlns:wd="urn:com.workday/bsvc"></wd:Get_Workers_Request>`
}

func defaultCompEnvelope() string {
	return `<wd:Get_Compensation_Review_Results_Request xmlns:wd="urn:com.workday/bsvc"></wd:Get_Compensation_Review_Results_Request>`
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
