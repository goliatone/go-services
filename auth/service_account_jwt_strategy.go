package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

const defaultGoogleServiceAccountTokenTimeout = 30 * time.Second

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
	HTTPClient        *http.Client
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
			HTTPClient:        cfg.HTTPClient,
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

func (s *ServiceAccountJWTStrategy) Complete(ctx context.Context, req core.AuthCompleteRequest) (core.AuthCompleteResponse, error) {
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

	now := s.config.Now().UTC()
	expiresAt := now.Add(s.config.TokenTTL)
	requested := readStringSlice(metadata, "requested_grants", "requested_scopes", "scopes")
	granted := readStringSlice(metadata, "granted_grants", "granted_scopes")
	if len(granted) == 0 {
		granted = append([]string(nil), requested...)
	}

	googleSpec, googleExchange := s.googleServiceAccountSpec(metadata, issuer, audience, subject, signingAlgorithm, signingKey, keyID, requested)
	if googleExchange {
		return s.completeGoogleServiceAccount(ctx, req, googleSpec, requested, granted, now)
	}

	if issuer == "" {
		return core.AuthCompleteResponse{}, fmt.Errorf("auth: service_account_jwt issuer is required")
	}
	if audience == "" {
		return core.AuthCompleteResponse{}, fmt.Errorf("auth: service_account_jwt audience is required")
	}
	if signingKey == "" {
		return core.AuthCompleteResponse{}, fmt.Errorf("auth: service_account_jwt signing key is required")
	}

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

func (s *ServiceAccountJWTStrategy) Refresh(ctx context.Context, cred core.ActiveCredential) (core.RefreshResult, error) {
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

	if issuer == "" || audience == "" || signingKey == "" {
		return core.RefreshResult{}, fmt.Errorf("auth: service_account_jwt refresh requires issuer/audience/signing key")
	}

	now := s.config.Now().UTC()
	expiresAt := now.Add(s.config.TokenTTL)
	requested := readStringSlice(metadata, "requested_grants", "requested_scopes", "scopes")
	if len(requested) == 0 {
		requested = append([]string(nil), cred.RequestedScopes...)
	}
	googleSpec, googleExchange := s.googleServiceAccountSpec(metadata, issuer, audience, subject, signingAlgorithm, signingKey, keyID, requested)
	if googleExchange {
		refreshed, granted, err := s.exchangeGoogleServiceAccountJWT(ctx, googleSpec, now)
		if err != nil {
			return core.RefreshResult{}, err
		}
		refreshed.ConnectionID = cred.ConnectionID
		refreshed.RequestedScopes = append([]string(nil), requested...)
		if len(granted) == 0 {
			granted = append([]string(nil), cred.GrantedScopes...)
		}
		if len(granted) == 0 {
			granted = append([]string(nil), requested...)
		}
		refreshed.GrantedScopes = append([]string(nil), granted...)
		refreshed.Refreshable = true
		refreshed.Metadata = googleSpec.metadata()
		return core.RefreshResult{
			Credential:    refreshed,
			GrantedGrants: append([]string(nil), refreshed.GrantedScopes...),
			Metadata: map[string]any{
				"auth_kind": core.AuthKindServiceAccountJWT,
			},
		}, nil
	}
	if subject == "" {
		return core.RefreshResult{}, fmt.Errorf("auth: service_account_jwt refresh requires subject")
	}

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

type googleServiceAccountJSON struct {
	ClientEmail  string `json:"client_email"`
	PrivateKey   string `json:"private_key"`
	PrivateKeyID string `json:"private_key_id"`
	TokenURI     string `json:"token_uri"`
	ProjectID    string `json:"project_id"`
}

type googleServiceAccountSpec struct {
	issuer           string
	subject          string
	audience         string
	signingAlgorithm string
	signingKey       string
	keyID            string
	tokenURI         string
	projectID        string
	scopes           []string
	tokenTTL         time.Duration
}

func (s *ServiceAccountJWTStrategy) googleServiceAccountSpec(metadata map[string]any, issuer string, audience string, subject string, signingAlgorithm string, signingKey string, keyID string, scopes []string) (googleServiceAccountSpec, bool) {
	serviceAccountJSON := readString(metadata, "service_account_json", "google_service_account_json")
	spec := googleServiceAccountSpec{}
	if serviceAccountJSON != "" {
		parsed := googleServiceAccountJSON{}
		if err := json.Unmarshal([]byte(serviceAccountJSON), &parsed); err == nil {
			spec.issuer = strings.TrimSpace(parsed.ClientEmail)
			spec.signingKey = strings.TrimSpace(parsed.PrivateKey)
			spec.keyID = strings.TrimSpace(parsed.PrivateKeyID)
			spec.tokenURI = strings.TrimSpace(parsed.TokenURI)
			spec.projectID = strings.TrimSpace(parsed.ProjectID)
		}
	}
	spec.issuer = firstNonEmpty(readString(metadata, "client_email", "service_account_email"), spec.issuer, issuer)
	spec.signingKey = firstNonEmpty(readString(metadata, "private_key", "signing_key", "jwt_private_key", "jwt_signing_key"), spec.signingKey, signingKey)
	spec.keyID = firstNonEmpty(readString(metadata, "private_key_id", "key_id", "jwt_key_id"), spec.keyID, keyID)
	spec.tokenURI = firstNonEmpty(readString(metadata, "token_uri", "token_url"), spec.tokenURI, audience)
	spec.audience = firstNonEmpty(readString(metadata, "audience", "jwt_audience"), spec.tokenURI, audience)
	spec.projectID = firstNonEmpty(readString(metadata, "project_id"), spec.projectID)
	spec.subject = firstNonEmpty(readString(metadata, "service_account_subject", "delegation_subject", "workspace_subject"), s.config.Subject)
	if strings.TrimSpace(spec.subject) == "" && strings.TrimSpace(subject) != "" && strings.TrimSpace(subject) != strings.TrimSpace(issuer) {
		spec.subject = strings.TrimSpace(subject)
	}
	spec.signingAlgorithm = firstNonEmpty(signingAlgorithm, s.config.SigningAlgorithm, jwtAlgRS256)
	spec.scopes = normalizeValues(scopes)
	spec.tokenTTL = s.config.TokenTTL
	if spec.tokenTTL <= 0 {
		spec.tokenTTL = time.Hour
	}
	exchange := serviceAccountJSON != "" || readString(metadata, "token_uri") != "" || readString(metadata, "google_token_exchange") == "true"
	return spec, exchange
}

func (s *ServiceAccountJWTStrategy) completeGoogleServiceAccount(ctx context.Context, req core.AuthCompleteRequest, spec googleServiceAccountSpec, requested []string, granted []string, now time.Time) (core.AuthCompleteResponse, error) {
	credential, tokenGranted, err := s.exchangeGoogleServiceAccountJWT(ctx, spec, now)
	if err != nil {
		return core.AuthCompleteResponse{}, err
	}
	if len(tokenGranted) > 0 {
		granted = tokenGranted
	}
	if len(granted) == 0 {
		granted = append([]string(nil), requested...)
	}
	credential.RequestedScopes = append([]string(nil), requested...)
	credential.GrantedScopes = append([]string(nil), granted...)
	credential.Refreshable = true
	credential.Metadata = spec.metadata()

	externalAccountID := firstNonEmpty(
		readString(req.Metadata, "external_account_id"),
		s.config.ExternalAccountID,
		spec.issuer,
		fmt.Sprintf("%s:%s:%s", core.AuthKindServiceAccountJWT, req.Scope.Type, req.Scope.ID),
	)
	return core.AuthCompleteResponse{
		ExternalAccountID: externalAccountID,
		Credential:        credential,
		RequestedGrants:   append([]string(nil), requested...),
		GrantedGrants:     append([]string(nil), granted...),
		Metadata: map[string]any{
			"auth_kind": core.AuthKindServiceAccountJWT,
		},
	}, nil
}

func (s *ServiceAccountJWTStrategy) exchangeGoogleServiceAccountJWT(ctx context.Context, spec googleServiceAccountSpec, now time.Time) (core.ActiveCredential, []string, error) {
	if strings.TrimSpace(spec.issuer) == "" {
		return core.ActiveCredential{}, nil, fmt.Errorf("auth: google service account client_email is required")
	}
	if strings.TrimSpace(spec.signingKey) == "" {
		return core.ActiveCredential{}, nil, fmt.Errorf("auth: google service account private_key is required")
	}
	if strings.TrimSpace(spec.tokenURI) == "" {
		return core.ActiveCredential{}, nil, fmt.Errorf("auth: google service account token_uri is required")
	}
	if len(spec.scopes) == 0 {
		return core.ActiveCredential{}, nil, fmt.Errorf("auth: google service account scopes are required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	expiresAt := now.Add(spec.tokenTTL)
	claims := map[string]any{
		"iss":   spec.issuer,
		"scope": strings.Join(spec.scopes, " "),
		"aud":   firstNonEmpty(spec.audience, spec.tokenURI),
		"iat":   now.Unix(),
		"exp":   expiresAt.Unix(),
	}
	if strings.TrimSpace(spec.subject) != "" {
		claims["sub"] = strings.TrimSpace(spec.subject)
	}
	assertion, err := buildJWT(spec.keyID, spec.signingAlgorithm, spec.signingKey, claims)
	if err != nil {
		return core.ActiveCredential{}, nil, err
	}

	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(spec.tokenURI), bytes.NewBufferString(form.Encode()))
	if err != nil {
		return core.ActiveCredential{}, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpClient := s.config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultGoogleServiceAccountTokenTimeout}
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return core.ActiveCredential{}, nil, fmt.Errorf("auth: google service account token exchange failed")
	}
	defer func() { _ = resp.Body.Close() }()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return core.ActiveCredential{}, nil, fmt.Errorf("auth: google service account token response read failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return core.ActiveCredential{}, nil, fmt.Errorf("auth: google service account token exchange status %d", resp.StatusCode)
	}
	tokenResponse := struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
		Scope       string `json:"scope"`
	}{}
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return core.ActiveCredential{}, nil, fmt.Errorf("auth: google service account token response decode failed")
	}
	if strings.TrimSpace(tokenResponse.AccessToken) == "" {
		return core.ActiveCredential{}, nil, fmt.Errorf("auth: google service account token response missing access_token")
	}
	if tokenResponse.ExpiresIn > 0 {
		expiry := now.Add(time.Duration(tokenResponse.ExpiresIn) * time.Second)
		expiresAt = expiry
	}
	tokenType := firstNonEmpty(tokenResponse.TokenType, "Bearer")
	granted := normalizeValues(strings.Fields(tokenResponse.Scope))
	if len(granted) == 0 {
		granted = append([]string(nil), spec.scopes...)
	}
	return core.ActiveCredential{
		TokenType:   tokenType,
		AccessToken: strings.TrimSpace(tokenResponse.AccessToken),
		ExpiresAt:   &expiresAt,
		Refreshable: true,
		Metadata:    spec.metadata(),
	}, granted, nil
}

func (s googleServiceAccountSpec) metadata() map[string]any {
	metadata := map[string]any{
		"auth_kind":         core.AuthKindServiceAccountJWT,
		"issuer":            strings.TrimSpace(s.issuer),
		"client_email":      strings.TrimSpace(s.issuer),
		"audience":          firstNonEmpty(s.audience, s.tokenURI),
		"token_uri":         strings.TrimSpace(s.tokenURI),
		"signing_algorithm": strings.ToUpper(strings.TrimSpace(s.signingAlgorithm)),
		"signing_key":       strings.TrimSpace(s.signingKey),
	}
	if strings.TrimSpace(s.subject) != "" {
		metadata["subject"] = strings.TrimSpace(s.subject)
		metadata["service_account_subject"] = strings.TrimSpace(s.subject)
	}
	if strings.TrimSpace(s.keyID) != "" {
		metadata["key_id"] = strings.TrimSpace(s.keyID)
		metadata["private_key_id"] = strings.TrimSpace(s.keyID)
	}
	if strings.TrimSpace(s.projectID) != "" {
		metadata["project_id"] = strings.TrimSpace(s.projectID)
	}
	if len(s.scopes) > 0 {
		metadata["requested_scopes"] = append([]string(nil), s.scopes...)
	}
	return metadata
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
