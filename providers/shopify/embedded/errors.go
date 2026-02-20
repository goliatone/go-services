package embedded

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidSessionToken     = errors.New("providers/shopify/embedded: invalid session token")
	ErrUnsupportedJWTAlgorithm = errors.New("providers/shopify/embedded: unsupported jwt algorithm")
	ErrInvalidAudience         = errors.New("providers/shopify/embedded: invalid audience")
	ErrInvalidDestination      = errors.New("providers/shopify/embedded: invalid destination")
	ErrInvalidRequestedTokenType = errors.New("providers/shopify/embedded: invalid requested token type")
	ErrMissingJTI              = errors.New("providers/shopify/embedded: missing jti claim")
	ErrReplayDetected          = errors.New("providers/shopify/embedded: replay detected")
	ErrTokenExchangeFailed     = errors.New("providers/shopify/embedded: token exchange failed")
)

type ValidationError struct {
	Code  string
	Field string
	Cause error
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ErrInvalidSessionToken.Error()
	}
	parts := []string{ErrInvalidSessionToken.Error()}
	if strings.TrimSpace(e.Code) != "" {
		parts = append(parts, "code="+strings.TrimSpace(e.Code))
	}
	if strings.TrimSpace(e.Field) != "" {
		parts = append(parts, "field="+strings.TrimSpace(e.Field))
	}
	if e.Cause != nil {
		parts = append(parts, e.Cause.Error())
	}
	return strings.Join(parts, ": ")
}

func (e *ValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type ExchangeError struct {
	StatusCode int
	ErrorCode  string
	Message    string
	Cause      error
}

func (e *ExchangeError) Error() string {
	if e == nil {
		return ErrTokenExchangeFailed.Error()
	}
	base := ErrTokenExchangeFailed.Error()
	if strings.TrimSpace(e.ErrorCode) != "" {
		base += ": " + strings.TrimSpace(e.ErrorCode)
	}
	if strings.TrimSpace(e.Message) != "" {
		base += ": " + strings.TrimSpace(e.Message)
	}
	if e.StatusCode > 0 {
		base += fmt.Sprintf(" (status=%d)", e.StatusCode)
	}
	if e.Cause != nil {
		base += ": " + e.Cause.Error()
	}
	return base
}

func (e *ExchangeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
