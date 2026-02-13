package providers

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

const (
	defaultAuthKind = "oauth2_auth_code"
)

type OAuth2Config struct {
	ID                  string
	AuthURL             string
	TokenURL            string
	ClientID            string
	ClientSecret        string
	DefaultScopes       []string
	SupportedScopeTypes []string
	Capabilities        []core.CapabilityDescriptor
	TokenTTL            time.Duration
	Now                 func() time.Time
}

type OAuth2Provider struct {
	cfg OAuth2Config
}

func NewOAuth2Provider(cfg OAuth2Config) (*OAuth2Provider, error) {
	cfg.ID = strings.TrimSpace(strings.ToLower(cfg.ID))
	if cfg.ID == "" {
		return nil, fmt.Errorf("providers: provider id is required")
	}
	if strings.TrimSpace(cfg.AuthURL) == "" {
		return nil, fmt.Errorf("providers: auth url is required for provider %q", cfg.ID)
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, fmt.Errorf("providers: client id is required for provider %q", cfg.ID)
	}

	cfg.AuthURL = strings.TrimSpace(cfg.AuthURL)
	cfg.TokenURL = strings.TrimSpace(cfg.TokenURL)
	cfg.DefaultScopes = normalizeGrants(cfg.DefaultScopes)
	if len(cfg.SupportedScopeTypes) == 0 {
		cfg.SupportedScopeTypes = []string{"user", "org"}
	}
	cfg.SupportedScopeTypes = normalizeScopeTypes(cfg.SupportedScopeTypes)
	cfg.Capabilities = cloneCapabilities(cfg.Capabilities)
	if cfg.TokenTTL <= 0 {
		cfg.TokenTTL = time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time {
			return time.Now().UTC()
		}
	}

	return &OAuth2Provider{cfg: cfg}, nil
}

func (p *OAuth2Provider) ID() string {
	if p == nil {
		return ""
	}
	return p.cfg.ID
}

func (*OAuth2Provider) AuthKind() string {
	return defaultAuthKind
}

func (p *OAuth2Provider) SupportedScopeTypes() []string {
	if p == nil {
		return []string{}
	}
	return append([]string(nil), p.cfg.SupportedScopeTypes...)
}

func (p *OAuth2Provider) Capabilities() []core.CapabilityDescriptor {
	if p == nil {
		return []core.CapabilityDescriptor{}
	}
	return cloneCapabilities(p.cfg.Capabilities)
}

func (p *OAuth2Provider) BeginAuth(_ context.Context, req core.BeginAuthRequest) (core.BeginAuthResponse, error) {
	if p == nil {
		return core.BeginAuthResponse{}, fmt.Errorf("providers: oauth2 provider is nil")
	}
	if err := req.Scope.Validate(); err != nil {
		return core.BeginAuthResponse{}, err
	}
	state := strings.TrimSpace(req.State)
	if state == "" {
		state = fmt.Sprintf("state_%s_%s", p.cfg.ID, tokenPart(req.Scope.ID))
	}
	requested := normalizeGrants(req.RequestedGrants)
	if len(requested) == 0 {
		requested = append([]string(nil), p.cfg.DefaultScopes...)
	}

	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", p.cfg.ClientID)
	if strings.TrimSpace(req.RedirectURI) != "" {
		values.Set("redirect_uri", strings.TrimSpace(req.RedirectURI))
	}
	values.Set("scope", strings.Join(requested, " "))
	values.Set("state", state)

	authURL := p.cfg.AuthURL
	if strings.Contains(authURL, "?") {
		authURL += "&" + values.Encode()
	} else {
		authURL += "?" + values.Encode()
	}

	metadata := cloneMetadata(req.Metadata)
	metadata["provider_id"] = p.cfg.ID
	if p.cfg.TokenURL != "" {
		metadata["token_url"] = p.cfg.TokenURL
	}

	return core.BeginAuthResponse{
		URL:             authURL,
		State:           state,
		RequestedGrants: requested,
		Metadata:        metadata,
	}, nil
}

func (p *OAuth2Provider) CompleteAuth(_ context.Context, req core.CompleteAuthRequest) (core.CompleteAuthResponse, error) {
	if p == nil {
		return core.CompleteAuthResponse{}, fmt.Errorf("providers: oauth2 provider is nil")
	}
	if err := req.Scope.Validate(); err != nil {
		return core.CompleteAuthResponse{}, err
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		return core.CompleteAuthResponse{}, fmt.Errorf("providers: auth code is required")
	}

	requested := normalizeGrants(readStringSlice(req.Metadata, "requested_grants"))
	if len(requested) == 0 {
		requested = append([]string(nil), p.cfg.DefaultScopes...)
	}
	granted := normalizeGrants(readStringSlice(req.Metadata, "granted_grants"))
	if len(granted) == 0 {
		granted = append([]string(nil), requested...)
	}

	externalAccountID := strings.TrimSpace(readString(req.Metadata, "external_account_id"))
	if externalAccountID == "" {
		externalAccountID = fmt.Sprintf("%s:%s:%s", p.cfg.ID, req.Scope.Type, req.Scope.ID)
	}

	now := p.cfg.Now().UTC()
	expiresAt := now.Add(p.cfg.TokenTTL)
	credential := core.ActiveCredential{
		TokenType:       "bearer",
		AccessToken:     fmt.Sprintf("%s_access_%s", p.cfg.ID, tokenPart(code)),
		RefreshToken:    fmt.Sprintf("%s_refresh_%s", p.cfg.ID, tokenPart(code+externalAccountID)),
		RequestedScopes: append([]string(nil), requested...),
		GrantedScopes:   append([]string(nil), granted...),
		ExpiresAt:       &expiresAt,
		Refreshable:     true,
		Metadata: map[string]any{
			"provider_id": p.cfg.ID,
			"token_url":   p.cfg.TokenURL,
		},
	}

	return core.CompleteAuthResponse{
		ExternalAccountID: externalAccountID,
		Credential:        credential,
		RequestedGrants:   append([]string(nil), requested...),
		GrantedGrants:     append([]string(nil), granted...),
		Metadata: map[string]any{
			"provider_id": p.cfg.ID,
			"token_url":   p.cfg.TokenURL,
		},
	}, nil
}

func (p *OAuth2Provider) Refresh(_ context.Context, cred core.ActiveCredential) (core.RefreshResult, error) {
	if p == nil {
		return core.RefreshResult{}, fmt.Errorf("providers: oauth2 provider is nil")
	}
	if !cred.Refreshable {
		return core.RefreshResult{}, fmt.Errorf("providers: credential is not refreshable")
	}
	requested := normalizeGrants(cred.RequestedScopes)
	if len(requested) == 0 {
		requested = append([]string(nil), p.cfg.DefaultScopes...)
	}
	granted := normalizeGrants(cred.GrantedScopes)
	if len(granted) == 0 {
		granted = append([]string(nil), requested...)
	}
	now := p.cfg.Now().UTC()
	expiresAt := now.Add(p.cfg.TokenTTL)
	refreshed := cred
	refreshed.TokenType = "bearer"
	refreshed.AccessToken = fmt.Sprintf("%s_access_%d", p.cfg.ID, now.Unix())
	if strings.TrimSpace(refreshed.RefreshToken) == "" {
		refreshed.RefreshToken = fmt.Sprintf("%s_refresh_%d", p.cfg.ID, now.Unix())
	}
	refreshed.RequestedScopes = append([]string(nil), requested...)
	refreshed.GrantedScopes = append([]string(nil), granted...)
	refreshed.ExpiresAt = &expiresAt
	refreshed.Refreshable = true
	refreshed.Metadata = cloneMetadata(refreshed.Metadata)
	refreshed.Metadata["provider_id"] = p.cfg.ID

	return core.RefreshResult{
		Credential:    refreshed,
		GrantedGrants: append([]string(nil), granted...),
		Metadata: map[string]any{
			"provider_id": p.cfg.ID,
			"token_url":   p.cfg.TokenURL,
		},
	}, nil
}

func cloneCapabilities(input []core.CapabilityDescriptor) []core.CapabilityDescriptor {
	if len(input) == 0 {
		return []core.CapabilityDescriptor{}
	}
	output := make([]core.CapabilityDescriptor, 0, len(input))
	for _, descriptor := range input {
		copyDescriptor := descriptor
		copyDescriptor.RequiredGrants = append([]string(nil), descriptor.RequiredGrants...)
		copyDescriptor.OptionalGrants = append([]string(nil), descriptor.OptionalGrants...)
		output = append(output, copyDescriptor)
	}
	return output
}

func cloneMetadata(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func normalizeScopeTypes(input []string) []string {
	values := make([]string, 0, len(input))
	seen := map[string]struct{}{}
	for _, value := range input {
		normalized := strings.TrimSpace(strings.ToLower(value))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		values = append(values, normalized)
	}
	if len(values) == 0 {
		return []string{"user", "org"}
	}
	sort.Strings(values)
	return values
}

func normalizeGrants(input []string) []string {
	if len(input) == 0 {
		return []string{}
	}
	values := make([]string, 0, len(input))
	seen := map[string]struct{}{}
	for _, value := range input {
		normalized := strings.TrimSpace(strings.ToLower(value))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		values = append(values, normalized)
	}
	sort.Strings(values)
	return values
}

func tokenPart(value string) string {
	normalized := strings.Builder{}
	for _, char := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case char >= 'a' && char <= 'z':
			normalized.WriteRune(char)
		case char >= '0' && char <= '9':
			normalized.WriteRune(char)
		}
		if normalized.Len() >= 12 {
			break
		}
	}
	if normalized.Len() == 0 {
		return "token"
	}
	return normalized.String()
}

func readString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func readStringSlice(metadata map[string]any, key string) []string {
	if len(metadata) == 0 {
		return []string{}
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return []string{}
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			itemValue := strings.TrimSpace(fmt.Sprint(item))
			if itemValue != "" && itemValue != "<nil>" {
				items = append(items, itemValue)
			}
		}
		return items
	default:
		trimmed := strings.TrimSpace(fmt.Sprint(typed))
		if trimmed == "" || trimmed == "<nil>" {
			return []string{}
		}
		if !strings.Contains(trimmed, ",") {
			return []string{trimmed}
		}
		parts := strings.Split(trimmed, ",")
		items := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				items = append(items, part)
			}
		}
		return items
	}
}

var _ core.Provider = (*OAuth2Provider)(nil)
