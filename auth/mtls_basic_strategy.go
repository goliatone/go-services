package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/goliatone/go-services/core"
)

type BasicStrategyConfig struct {
	Username          string
	Password          string
	ExternalAccountID string
}

type BasicStrategy struct {
	config BasicStrategyConfig
}

func NewBasicStrategy(cfg BasicStrategyConfig) *BasicStrategy {
	return &BasicStrategy{
		config: BasicStrategyConfig{
			Username:          strings.TrimSpace(cfg.Username),
			Password:          strings.TrimSpace(cfg.Password),
			ExternalAccountID: strings.TrimSpace(cfg.ExternalAccountID),
		},
	}
}

func (*BasicStrategy) Type() core.AuthKind { return core.AuthKindBasic }

func (s *BasicStrategy) Begin(_ context.Context, req core.AuthBeginRequest) (core.AuthBeginResponse, error) {
	return core.AuthBeginResponse{
		State:           strings.TrimSpace(req.State),
		RequestedGrants: normalizeValues(req.RequestedRaw),
		Metadata: map[string]any{
			"auth_kind": core.AuthKindBasic,
		},
	}, nil
}

func (s *BasicStrategy) Complete(_ context.Context, req core.AuthCompleteRequest) (core.AuthCompleteResponse, error) {
	metadata := cloneMetadata(req.Metadata)
	username := firstNonEmpty(readString(metadata, "username", "basic_username"), s.config.Username)
	password := firstNonEmpty(readString(metadata, "password", "basic_password"), s.config.Password)
	if username == "" || password == "" {
		return core.AuthCompleteResponse{}, fmt.Errorf("auth: basic username/password are required")
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	requested := readStringSlice(metadata, "requested_grants", "requested_scopes")
	granted := readStringSlice(metadata, "granted_grants", "granted_scopes")
	if len(granted) == 0 {
		granted = append([]string(nil), requested...)
	}
	externalAccountID := firstNonEmpty(
		readString(metadata, "external_account_id"),
		s.config.ExternalAccountID,
		fmt.Sprintf("%s:%s:%s", core.AuthKindBasic, req.Scope.Type, req.Scope.ID),
	)

	return core.AuthCompleteResponse{
		ExternalAccountID: externalAccountID,
		Credential: core.ActiveCredential{
			TokenType:       string(core.AuthKindBasic),
			AccessToken:     encoded,
			RequestedScopes: append([]string(nil), requested...),
			GrantedScopes:   append([]string(nil), granted...),
			Refreshable:     false,
			Metadata: map[string]any{
				"auth_kind": core.AuthKindBasic,
				"username":  username,
			},
		},
		RequestedGrants: append([]string(nil), requested...),
		GrantedGrants:   append([]string(nil), granted...),
		Metadata: map[string]any{
			"auth_kind": core.AuthKindBasic,
		},
	}, nil
}

func (*BasicStrategy) Refresh(_ context.Context, cred core.ActiveCredential) (core.RefreshResult, error) {
	refreshed := cred
	refreshed.TokenType = string(core.AuthKindBasic)
	refreshed.Refreshable = false
	refreshed.Metadata = cloneMetadata(refreshed.Metadata)
	refreshed.Metadata["auth_kind"] = core.AuthKindBasic
	return core.RefreshResult{
		Credential:    refreshed,
		GrantedGrants: append([]string(nil), refreshed.GrantedScopes...),
		Metadata: map[string]any{
			"auth_kind": core.AuthKindBasic,
		},
	}, nil
}

type MTLSStrategyConfig struct {
	CertRef           string
	KeyRef            string
	Identity          string
	ExternalAccountID string
}

type MTLSStrategy struct {
	config MTLSStrategyConfig
}

func NewMTLSStrategy(cfg MTLSStrategyConfig) *MTLSStrategy {
	return &MTLSStrategy{
		config: MTLSStrategyConfig{
			CertRef:           strings.TrimSpace(cfg.CertRef),
			KeyRef:            strings.TrimSpace(cfg.KeyRef),
			Identity:          strings.TrimSpace(cfg.Identity),
			ExternalAccountID: strings.TrimSpace(cfg.ExternalAccountID),
		},
	}
}

func (*MTLSStrategy) Type() core.AuthKind { return core.AuthKindMTLS }

func (s *MTLSStrategy) Begin(_ context.Context, req core.AuthBeginRequest) (core.AuthBeginResponse, error) {
	return core.AuthBeginResponse{
		State:           strings.TrimSpace(req.State),
		RequestedGrants: normalizeValues(req.RequestedRaw),
		Metadata: map[string]any{
			"auth_kind": core.AuthKindMTLS,
		},
	}, nil
}

func (s *MTLSStrategy) Complete(_ context.Context, req core.AuthCompleteRequest) (core.AuthCompleteResponse, error) {
	metadata := cloneMetadata(req.Metadata)
	certRef := firstNonEmpty(readString(metadata, "cert_ref", "mtls_cert_ref"), s.config.CertRef)
	keyRef := firstNonEmpty(readString(metadata, "key_ref", "mtls_key_ref"), s.config.KeyRef)
	if certRef == "" || keyRef == "" {
		return core.AuthCompleteResponse{}, fmt.Errorf("auth: mtls cert/key references are required")
	}

	identity := firstNonEmpty(
		readString(metadata, "identity", "mtls_identity"),
		s.config.Identity,
		certRef,
	)
	requested := readStringSlice(metadata, "requested_grants", "requested_scopes")
	granted := readStringSlice(metadata, "granted_grants", "granted_scopes")
	if len(granted) == 0 {
		granted = append([]string(nil), requested...)
	}
	externalAccountID := firstNonEmpty(
		readString(metadata, "external_account_id"),
		s.config.ExternalAccountID,
		fmt.Sprintf("%s:%s:%s", core.AuthKindMTLS, req.Scope.Type, req.Scope.ID),
	)

	return core.AuthCompleteResponse{
		ExternalAccountID: externalAccountID,
		Credential: core.ActiveCredential{
			TokenType:       string(core.AuthKindMTLS),
			AccessToken:     "mtls:" + identity,
			RequestedScopes: append([]string(nil), requested...),
			GrantedScopes:   append([]string(nil), granted...),
			Refreshable:     false,
			Metadata: map[string]any{
				"auth_kind": core.AuthKindMTLS,
				"cert_ref":  certRef,
				"key_ref":   keyRef,
				"identity":  identity,
			},
		},
		RequestedGrants: append([]string(nil), requested...),
		GrantedGrants:   append([]string(nil), granted...),
		Metadata: map[string]any{
			"auth_kind": core.AuthKindMTLS,
		},
	}, nil
}

func (*MTLSStrategy) Refresh(_ context.Context, cred core.ActiveCredential) (core.RefreshResult, error) {
	refreshed := cred
	refreshed.TokenType = string(core.AuthKindMTLS)
	refreshed.Refreshable = false
	refreshed.Metadata = cloneMetadata(refreshed.Metadata)
	refreshed.Metadata["auth_kind"] = core.AuthKindMTLS
	return core.RefreshResult{
		Credential:    refreshed,
		GrantedGrants: append([]string(nil), refreshed.GrantedScopes...),
		Metadata: map[string]any{
			"auth_kind": core.AuthKindMTLS,
		},
	}, nil
}
