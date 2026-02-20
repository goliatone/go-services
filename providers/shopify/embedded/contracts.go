package embedded

import (
	"context"

	"github.com/goliatone/go-services/core"
)

type ValidateSessionTokenRequest struct {
	SessionToken       string
	ExpectedShopDomain string
}

type SessionTokenValidator interface {
	ValidateSessionToken(ctx context.Context, req ValidateSessionTokenRequest) (core.EmbeddedSessionClaims, error)
}

type ExchangeSessionTokenRequest struct {
	ShopDomain         string
	SessionToken       string
	RequestedTokenType core.EmbeddedRequestedTokenType
}

type SessionTokenExchanger interface {
	ExchangeSessionToken(ctx context.Context, req ExchangeSessionTokenRequest) (core.EmbeddedAccessToken, error)
}
