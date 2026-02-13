package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/goliatone/go-services/core"
)

const (
	defaultAPIKeyHeader = "X-API-Key"
	defaultHMACHeader   = "X-Signature"
	defaultTimeHeader   = "X-Timestamp"
)

type APIKeyProfile struct {
	Header     string
	Prefix     string
	QueryParam string
}

type APIKeyStrategyConfig struct {
	Kind              string
	Profile           APIKeyProfile
	StaticKey         string
	ExternalAccountID string
}

type APIKeyStrategy struct {
	config APIKeyStrategyConfig
}

func NewAPIKeyStrategy(cfg APIKeyStrategyConfig) *APIKeyStrategy {
	kind := strings.TrimSpace(strings.ToLower(cfg.Kind))
	if kind == "" {
		kind = core.AuthKindAPIKey
	}
	profile := cfg.Profile
	if strings.TrimSpace(profile.Header) == "" {
		profile.Header = defaultAPIKeyHeader
	}
	if kind == core.AuthKindPAT {
		if strings.TrimSpace(profile.Header) == "" || strings.EqualFold(profile.Header, defaultAPIKeyHeader) {
			profile.Header = "Authorization"
		}
		if strings.TrimSpace(profile.Prefix) == "" {
			profile.Prefix = "token"
		}
	}
	return &APIKeyStrategy{
		config: APIKeyStrategyConfig{
			Kind:              kind,
			Profile:           profile,
			StaticKey:         strings.TrimSpace(cfg.StaticKey),
			ExternalAccountID: strings.TrimSpace(cfg.ExternalAccountID),
		},
	}
}

func NewPATStrategy(cfg APIKeyStrategyConfig) *APIKeyStrategy {
	cfg.Kind = core.AuthKindPAT
	return NewAPIKeyStrategy(cfg)
}

func (s *APIKeyStrategy) Type() string {
	if s == nil {
		return core.AuthKindAPIKey
	}
	return s.config.Kind
}

func (s *APIKeyStrategy) Begin(_ context.Context, req core.AuthBeginRequest) (core.AuthBeginResponse, error) {
	return core.AuthBeginResponse{
		State:           strings.TrimSpace(req.State),
		RequestedGrants: normalizeValues(req.RequestedRaw),
		Metadata: map[string]any{
			"auth_kind": s.Type(),
		},
	}, nil
}

func (s *APIKeyStrategy) Complete(_ context.Context, req core.AuthCompleteRequest) (core.AuthCompleteResponse, error) {
	metadata := cloneMetadata(req.Metadata)
	key := readString(metadata,
		"api_key",
		"pat",
		"token",
		"access_token",
	)
	if key == "" {
		key = s.config.StaticKey
	}
	if key == "" {
		return core.AuthCompleteResponse{}, fmt.Errorf("auth: %s credential token is required", s.Type())
	}

	requested := readStringSlice(metadata, "requested_grants", "requested_scopes")
	granted := readStringSlice(metadata, "granted_grants", "granted_scopes")
	if len(granted) == 0 {
		granted = append([]string(nil), requested...)
	}

	externalAccountID := readString(metadata, "external_account_id")
	if externalAccountID == "" {
		externalAccountID = s.config.ExternalAccountID
	}
	if externalAccountID == "" {
		externalAccountID = fmt.Sprintf("%s:%s:%s", s.Type(), req.Scope.Type, req.Scope.ID)
	}

	credentialMetadata := map[string]any{
		"auth_kind":           s.Type(),
		"api_key_header":      strings.TrimSpace(s.config.Profile.Header),
		"api_key_prefix":      strings.TrimSpace(s.config.Profile.Prefix),
		"api_key_query_param": strings.TrimSpace(s.config.Profile.QueryParam),
	}

	return core.AuthCompleteResponse{
		ExternalAccountID: externalAccountID,
		Credential: core.ActiveCredential{
			TokenType:       s.Type(),
			AccessToken:     key,
			RequestedScopes: append([]string(nil), requested...),
			GrantedScopes:   append([]string(nil), granted...),
			Refreshable:     false,
			Metadata:        credentialMetadata,
		},
		RequestedGrants: append([]string(nil), requested...),
		GrantedGrants:   append([]string(nil), granted...),
		Metadata: map[string]any{
			"auth_kind": s.Type(),
		},
	}, nil
}

func (s *APIKeyStrategy) Refresh(_ context.Context, cred core.ActiveCredential) (core.RefreshResult, error) {
	refreshed := cred
	if strings.TrimSpace(refreshed.TokenType) == "" {
		refreshed.TokenType = s.Type()
	}
	refreshed.Refreshable = false
	refreshed.Metadata = cloneMetadata(refreshed.Metadata)
	refreshed.Metadata["auth_kind"] = s.Type()
	return core.RefreshResult{
		Credential:    refreshed,
		GrantedGrants: append([]string(nil), refreshed.GrantedScopes...),
		Metadata: map[string]any{
			"auth_kind": s.Type(),
		},
	}, nil
}

type HMACStrategyConfig struct {
	Secret           string
	KeyID            string
	SignatureHeader  string
	TimestampHeader  string
	ExternalAccount  string
}

type HMACStrategy struct {
	config HMACStrategyConfig
}

func NewHMACStrategy(cfg HMACStrategyConfig) *HMACStrategy {
	signatureHeader := strings.TrimSpace(cfg.SignatureHeader)
	if signatureHeader == "" {
		signatureHeader = defaultHMACHeader
	}
	timestampHeader := strings.TrimSpace(cfg.TimestampHeader)
	if timestampHeader == "" {
		timestampHeader = defaultTimeHeader
	}

	return &HMACStrategy{
		config: HMACStrategyConfig{
			Secret:          strings.TrimSpace(cfg.Secret),
			KeyID:           strings.TrimSpace(cfg.KeyID),
			SignatureHeader: signatureHeader,
			TimestampHeader: timestampHeader,
			ExternalAccount: strings.TrimSpace(cfg.ExternalAccount),
		},
	}
}

func (*HMACStrategy) Type() string { return core.AuthKindHMAC }

func (s *HMACStrategy) Begin(_ context.Context, req core.AuthBeginRequest) (core.AuthBeginResponse, error) {
	return core.AuthBeginResponse{
		State:           strings.TrimSpace(req.State),
		RequestedGrants: normalizeValues(req.RequestedRaw),
		Metadata: map[string]any{
			"auth_kind": core.AuthKindHMAC,
		},
	}, nil
}

func (s *HMACStrategy) Complete(_ context.Context, req core.AuthCompleteRequest) (core.AuthCompleteResponse, error) {
	metadata := cloneMetadata(req.Metadata)
	secret := readString(metadata, "hmac_secret", "secret", "token", "access_token")
	if secret == "" {
		secret = s.config.Secret
	}
	if secret == "" {
		return core.AuthCompleteResponse{}, fmt.Errorf("auth: hmac secret is required")
	}

	keyID := readString(metadata, "hmac_key_id", "key_id")
	if keyID == "" {
		keyID = s.config.KeyID
	}
	if keyID == "" {
		keyID = "hmac-default"
	}

	requested := readStringSlice(metadata, "requested_grants", "requested_scopes")
	granted := readStringSlice(metadata, "granted_grants", "granted_scopes")
	if len(granted) == 0 {
		granted = append([]string(nil), requested...)
	}

	externalAccountID := readString(metadata, "external_account_id")
	if externalAccountID == "" {
		externalAccountID = s.config.ExternalAccount
	}
	if externalAccountID == "" {
		externalAccountID = fmt.Sprintf("%s:%s:%s", core.AuthKindHMAC, req.Scope.Type, req.Scope.ID)
	}

	credentialMetadata := map[string]any{
		"auth_kind":         core.AuthKindHMAC,
		"hmac_key_id":       keyID,
		"signature_header":  s.config.SignatureHeader,
		"timestamp_header":  s.config.TimestampHeader,
	}

	return core.AuthCompleteResponse{
		ExternalAccountID: externalAccountID,
		Credential: core.ActiveCredential{
			TokenType:       core.AuthKindHMAC,
			AccessToken:     secret,
			RequestedScopes: append([]string(nil), requested...),
			GrantedScopes:   append([]string(nil), granted...),
			Refreshable:     false,
			Metadata:        credentialMetadata,
		},
		RequestedGrants: append([]string(nil), requested...),
		GrantedGrants:   append([]string(nil), granted...),
		Metadata: map[string]any{
			"auth_kind": core.AuthKindHMAC,
		},
	}, nil
}

func (s *HMACStrategy) Refresh(_ context.Context, cred core.ActiveCredential) (core.RefreshResult, error) {
	refreshed := cred
	if strings.TrimSpace(refreshed.TokenType) == "" {
		refreshed.TokenType = core.AuthKindHMAC
	}
	refreshed.Refreshable = false
	refreshed.Metadata = cloneMetadata(refreshed.Metadata)
	refreshed.Metadata["auth_kind"] = core.AuthKindHMAC
	return core.RefreshResult{
		Credential:    refreshed,
		GrantedGrants: append([]string(nil), refreshed.GrantedScopes...),
		Metadata: map[string]any{
			"auth_kind": core.AuthKindHMAC,
		},
	}, nil
}

