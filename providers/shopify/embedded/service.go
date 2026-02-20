package embedded

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

const defaultProviderID = "shopify"

type ServiceConfig struct {
	ProviderID          string
	ClientID            string
	ClientSecret        string
	ExpectedShopDomain  string
	ClockSkew           time.Duration
	MaxIssuedAtAge      time.Duration
	ReplayTTL           time.Duration
	ReplayMaxEntries    int
	TokenRequestTimeout time.Duration
	Now                 func() time.Time
	HTTPClient          HTTPDoer
	ReplayLedger        core.ReplayLedger
	Validator           SessionTokenValidator
	Exchanger           SessionTokenExchanger
}

type Service struct {
	config    ServiceConfig
	ledger    core.ReplayLedger
	validator SessionTokenValidator
	exchanger SessionTokenExchanger
}

func NewService(cfg ServiceConfig) (*Service, error) {
	providerID := strings.TrimSpace(cfg.ProviderID)
	if providerID == "" {
		providerID = defaultProviderID
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	replayTTL := cfg.ReplayTTL
	if replayTTL <= 0 {
		replayTTL = 5 * time.Minute
	}

	ledger := cfg.ReplayLedger
	if ledger == nil {
		ledger = core.NewMemoryReplayLedgerWithLimits(replayTTL, cfg.ReplayMaxEntries)
	}
	validator := cfg.Validator
	if validator == nil {
		validator = NewSessionTokenValidator(SessionTokenValidatorConfig{
			AppSecret:      cfg.ClientSecret,
			ClientID:       cfg.ClientID,
			ClockSkew:      cfg.ClockSkew,
			MaxIssuedAtAge: cfg.MaxIssuedAtAge,
			Now:            now,
		})
	}
	exchanger := cfg.Exchanger
	if exchanger == nil {
		exchanger = NewSessionTokenExchangeClient(ExchangeClientConfig{
			ClientID:            cfg.ClientID,
			ClientSecret:        cfg.ClientSecret,
			TokenRequestTimeout: cfg.TokenRequestTimeout,
			HTTPClient:          cfg.HTTPClient,
			Now:                 now,
		})
	}

	return &Service{
		config: ServiceConfig{
			ProviderID:          providerID,
			ClientID:            strings.TrimSpace(cfg.ClientID),
			ClientSecret:        strings.TrimSpace(cfg.ClientSecret),
			ExpectedShopDomain:  strings.TrimSpace(cfg.ExpectedShopDomain),
			ClockSkew:           cfg.ClockSkew,
			MaxIssuedAtAge:      cfg.MaxIssuedAtAge,
			ReplayTTL:           replayTTL,
			ReplayMaxEntries:    cfg.ReplayMaxEntries,
			TokenRequestTimeout: cfg.TokenRequestTimeout,
			Now:                 now,
		},
		ledger:    ledger,
		validator: validator,
		exchanger: exchanger,
	}, nil
}

func (s *Service) AuthenticateEmbedded(
	ctx context.Context,
	req core.EmbeddedAuthRequest,
) (core.EmbeddedAuthResult, error) {
	if s == nil {
		return core.EmbeddedAuthResult{}, fmt.Errorf("providers/shopify/embedded: service is not configured")
	}
	if err := req.Scope.Validate(); err != nil {
		return core.EmbeddedAuthResult{}, err
	}
	sessionToken := strings.TrimSpace(req.SessionToken)
	if sessionToken == "" {
		return core.EmbeddedAuthResult{}, &ValidationError{
			Code:  "session_token_required",
			Field: "session_token",
			Cause: ErrInvalidSessionToken,
		}
	}
	expectedShop := strings.TrimSpace(req.ExpectedShopDomain)
	if expectedShop == "" {
		expectedShop = s.config.ExpectedShopDomain
	}

	claims, err := s.validator.ValidateSessionToken(ctx, ValidateSessionTokenRequest{
		SessionToken:       sessionToken,
		ExpectedShopDomain: expectedShop,
	})
	if err != nil {
		return core.EmbeddedAuthResult{}, err
	}
	requestedTokenType, _, err := resolveRequestedTokenType(req.RequestedTokenType)
	if err != nil {
		return core.EmbeddedAuthResult{}, &ValidationError{
			Code:  "invalid_requested_token_type",
			Field: "requested_token_type",
			Cause: err,
		}
	}
	replayTTL := req.ReplayTTL
	if replayTTL <= 0 {
		replayTTL = s.config.ReplayTTL
	}
	replayKey := buildReplayKey(s.config.ProviderID, claims.ShopDomain, claims.JTI)
	accepted, err := s.ledger.Claim(ctx, replayKey, replayTTL)
	if err != nil {
		return core.EmbeddedAuthResult{}, err
	}
	if !accepted {
		return core.EmbeddedAuthResult{}, ErrReplayDetected
	}

	token, err := s.exchanger.ExchangeSessionToken(ctx, ExchangeSessionTokenRequest{
		ShopDomain:         claims.ShopDomain,
		SessionToken:       sessionToken,
		RequestedTokenType: requestedTokenType,
	})
	if err != nil {
		return core.EmbeddedAuthResult{}, err
	}

	credentialMetadata := map[string]any{
		"provider_id":           s.config.ProviderID,
		"shop_domain":           claims.ShopDomain,
		"requested_token_type":  requestedTokenType,
		"embedded_session_jti":  claims.JTI,
		"embedded_session_dest": claims.Destination,
	}
	for key, value := range token.Metadata {
		credentialMetadata[key] = value
	}
	tokenType := strings.ToLower(strings.TrimSpace(token.TokenType))
	if tokenType == "" {
		tokenType = "bearer"
	}
	credential := core.ActiveCredential{
		TokenType:       tokenType,
		AccessToken:     token.AccessToken,
		RequestedScopes: append([]string(nil), token.Scope...),
		GrantedScopes:   append([]string(nil), token.Scope...),
		ExpiresAt:       token.ExpiresAt,
		Refreshable:     false,
		Metadata:        credentialMetadata,
	}

	metadata := map[string]any{
		"provider_id":          s.config.ProviderID,
		"shop_domain":          claims.ShopDomain,
		"requested_token_type": requestedTokenType,
		"embedded_session_jti": claims.JTI,
	}

	return core.EmbeddedAuthResult{
		ProviderID:        s.config.ProviderID,
		Scope:             req.Scope,
		ShopDomain:        claims.ShopDomain,
		ExternalAccountID: claims.ShopDomain,
		Claims:            claims,
		Token:             token,
		Credential:        credential,
		Metadata:          metadata,
	}, nil
}

func buildReplayKey(providerID string, shopDomain string, jti string) string {
	return strings.TrimSpace(providerID) + ":" + strings.TrimSpace(shopDomain) + ":" + strings.TrimSpace(jti)
}

var _ core.EmbeddedAuthService = (*Service)(nil)
