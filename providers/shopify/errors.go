package shopify

import "errors"

var (
	ErrAuthFlowUnsupported              = errors.New("providers/shopify: oauth2 auth-code flow is not supported")
	ErrEmbeddedAuthServiceNotConfigured = errors.New("providers/shopify: embedded auth service is not configured")
)
