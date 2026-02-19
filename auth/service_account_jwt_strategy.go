package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

type ServiceAccountJWTStrategyConfig struct {
	Issuer            string
	Subject           string
	Audience          string
	PrivateKey        string
	SigningKey        string
	SigningAlgorithm  string
	KeyID             string
	TokenTTL          time.Duration
	ExternalAccountID string
	Now               func() time.Time
}

type ServiceAccountJWTStrategy struct {
	config ServiceAccountJWTStrategyConfig
}

func NewServiceAccountJWTStrategy(cfg ServiceAccountJWTStrategyConfig) *ServiceAccountJWTStrategy {
	tokenTTL := cfg.TokenTTL
	if tokenTTL <= 0 {
		tokenTTL = time.Hour
	}
	signingKey := strings.TrimSpace(cfg.SigningKey)
	if signingKey == "" {
		signingKey = strings.TrimSpace(cfg.PrivateKey)
	}
	signingAlgorithm := strings.TrimSpace(strings.ToUpper(cfg.SigningAlgorithm))
	if signingAlgorithm == "" {
		signingAlgorithm = jwtAlgRS256
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &ServiceAccountJWTStrategy{
		config: ServiceAccountJWTStrategyConfig{
			Issuer:            strings.TrimSpace(cfg.Issuer),
			Subject:           strings.TrimSpace(cfg.Subject),
			Audience:          strings.TrimSpace(cfg.Audience),
			PrivateKey:        strings.TrimSpace(cfg.PrivateKey),
			SigningKey:        signingKey,
			SigningAlgorithm:  signingAlgorithm,
			KeyID:             strings.TrimSpace(cfg.KeyID),
			TokenTTL:          tokenTTL,
			ExternalAccountID: strings.TrimSpace(cfg.ExternalAccountID),
			Now:               now,
		},
	}
}

func (*ServiceAccountJWTStrategy) Type() core.AuthKind {
	return core.AuthKindServiceAccountJWT
}

func (s *ServiceAccountJWTStrategy) Begin(_ context.Context, req core.AuthBeginRequest) (core.AuthBeginResponse, error) {
	return core.AuthBeginResponse{
		State:           strings.TrimSpace(req.State),
		RequestedGrants: normalizeValues(req.RequestedRaw),
		Metadata: map[string]any{
			"auth_kind": core.AuthKindServiceAccountJWT,
		},
	}, nil
}

func (s *ServiceAccountJWTStrategy) Complete(_ context.Context, req core.AuthCompleteRequest) (core.AuthCompleteResponse, error) {
	metadata := cloneMetadata(req.Metadata)

	issuer := firstNonEmpty(
		readString(metadata, "issuer", "jwt_issuer", "client_email"),
		s.config.Issuer,
	)
	audience := firstNonEmpty(
		readString(metadata, "audience", "jwt_audience", "token_url"),
		s.config.Audience,
	)
	subject := firstNonEmpty(
		readString(metadata, "subject", "jwt_subject"),
		s.config.Subject,
		req.Scope.ID,
	)
	signingAlgorithm := firstNonEmpty(
		readString(metadata, "signing_algorithm", "jwt_signing_algorithm", "alg"),
		s.config.SigningAlgorithm,
		jwtAlgRS256,
	)
	signingKeyFromMetadata := readString(
		metadata,
		"signing_key",
		"jwt_signing_key",
		"private_key",
		"jwt_private_key",
		"jwt_secret",
	)
	signingKey := firstNonEmpty(
		signingKeyFromMetadata,
		s.config.SigningKey,
		s.config.PrivateKey,
	)
	keyID := firstNonEmpty(
		readString(metadata, "key_id", "jwt_key_id"),
		s.config.KeyID,
	)

	if issuer == "" {
		return core.AuthCompleteResponse{}, fmt.Errorf("auth: service_account_jwt issuer is required")
	}
	if audience == "" {
		return core.AuthCompleteResponse{}, fmt.Errorf("auth: service_account_jwt audience is required")
	}
	if signingKey == "" {
		return core.AuthCompleteResponse{}, fmt.Errorf("auth: service_account_jwt signing key is required")
	}

	now := s.config.Now().UTC()
	expiresAt := now.Add(s.config.TokenTTL)
	claims := map[string]any{
		"iss": issuer,
		"sub": subject,
		"aud": audience,
		"iat": now.Unix(),
		"exp": expiresAt.Unix(),
	}
	token, err := buildJWT(keyID, signingAlgorithm, signingKey, claims)
	if err != nil {
		return core.AuthCompleteResponse{}, err
	}

	requested := readStringSlice(metadata, "requested_grants", "requested_scopes")
	granted := readStringSlice(metadata, "granted_grants", "granted_scopes")
	if len(granted) == 0 {
		granted = append([]string(nil), requested...)
	}

	externalAccountID := firstNonEmpty(
		readString(metadata, "external_account_id"),
		s.config.ExternalAccountID,
		fmt.Sprintf("%s:%s:%s", core.AuthKindServiceAccountJWT, req.Scope.Type, req.Scope.ID),
	)

	credentialMetadata := map[string]any{
		"auth_kind":         core.AuthKindServiceAccountJWT,
		"issuer":            issuer,
		"subject":           subject,
		"audience":          audience,
		"signing_algorithm": strings.ToUpper(strings.TrimSpace(signingAlgorithm)),
	}
	if signingKeyFromMetadata != "" {
		credentialMetadata["signing_key"] = signingKeyFromMetadata
	}
	if keyID != "" {
		credentialMetadata["key_id"] = keyID
	}

	return core.AuthCompleteResponse{
		ExternalAccountID: externalAccountID,
		Credential: core.ActiveCredential{
			TokenType:       "bearer",
			AccessToken:     token,
			RequestedScopes: append([]string(nil), requested...),
			GrantedScopes:   append([]string(nil), granted...),
			ExpiresAt:       &expiresAt,
			Refreshable:     true,
			Metadata:        credentialMetadata,
		},
		RequestedGrants: append([]string(nil), requested...),
		GrantedGrants:   append([]string(nil), granted...),
		Metadata: map[string]any{
			"auth_kind": core.AuthKindServiceAccountJWT,
		},
	}, nil
}

func (s *ServiceAccountJWTStrategy) Refresh(_ context.Context, cred core.ActiveCredential) (core.RefreshResult, error) {
	metadata := cloneMetadata(cred.Metadata)
	issuer := firstNonEmpty(readString(metadata, "issuer"), s.config.Issuer)
	audience := firstNonEmpty(readString(metadata, "audience"), s.config.Audience)
	subject := firstNonEmpty(readString(metadata, "subject"), s.config.Subject)
	signingAlgorithm := firstNonEmpty(readString(metadata, "signing_algorithm"), s.config.SigningAlgorithm, jwtAlgRS256)
	signingKey := firstNonEmpty(
		readString(metadata, "signing_key", "jwt_signing_key", "private_key", "jwt_private_key", "jwt_secret"),
		s.config.SigningKey,
		s.config.PrivateKey,
	)
	keyID := firstNonEmpty(readString(metadata, "key_id"), s.config.KeyID)

	if issuer == "" || audience == "" || subject == "" || signingKey == "" {
		return core.RefreshResult{}, fmt.Errorf("auth: service_account_jwt refresh requires issuer/audience/subject/signing key")
	}

	now := s.config.Now().UTC()
	expiresAt := now.Add(s.config.TokenTTL)
	claims := map[string]any{
		"iss": issuer,
		"sub": subject,
		"aud": audience,
		"iat": now.Unix(),
		"exp": expiresAt.Unix(),
	}
	token, err := buildJWT(keyID, signingAlgorithm, signingKey, claims)
	if err != nil {
		return core.RefreshResult{}, err
	}

	refreshed := cred
	refreshed.TokenType = "bearer"
	refreshed.AccessToken = token
	refreshed.Refreshable = true
	refreshed.ExpiresAt = &expiresAt
	refreshed.Metadata = metadata
	refreshed.Metadata["auth_kind"] = core.AuthKindServiceAccountJWT
	refreshed.Metadata["signing_algorithm"] = strings.ToUpper(strings.TrimSpace(signingAlgorithm))
	if keyID != "" {
		refreshed.Metadata["key_id"] = keyID
	}

	return core.RefreshResult{
		Credential:    refreshed,
		GrantedGrants: append([]string(nil), refreshed.GrantedScopes...),
		Metadata: map[string]any{
			"auth_kind": core.AuthKindServiceAccountJWT,
		},
	}, nil
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
