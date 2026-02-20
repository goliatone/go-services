package embedded

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-services/core"
)

var (
	ErrInvalidSessionToken       = errors.New("providers/shopify/embedded: invalid session token")
	ErrUnsupportedJWTAlgorithm   = errors.New("providers/shopify/embedded: unsupported jwt algorithm")
	ErrInvalidAudience           = errors.New("providers/shopify/embedded: invalid audience")
	ErrInvalidDestination        = errors.New("providers/shopify/embedded: invalid destination")
	ErrInvalidRequestedTokenType = errors.New("providers/shopify/embedded: invalid requested token type")
	ErrMissingJTI                = errors.New("providers/shopify/embedded: missing jti claim")
	ErrReplayDetected            = errors.New("providers/shopify/embedded: replay detected")
	ErrTokenExchangeFailed       = errors.New("providers/shopify/embedded: token exchange failed")
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

func (e *ValidationError) ToServiceError() *goerrors.Error {
	if e == nil {
		return goerrors.New("providers/shopify/embedded: validation failed", goerrors.CategoryValidation).
			WithCode(http.StatusBadRequest).
			WithTextCode(core.ServiceErrorBadInput)
	}

	category := goerrors.CategoryValidation
	code := http.StatusBadRequest
	textCode := core.ServiceErrorBadInput

	switch {
	case errors.Is(e.Cause, ErrInvalidSessionToken),
		errors.Is(e.Cause, ErrInvalidAudience),
		errors.Is(e.Cause, ErrInvalidDestination),
		errors.Is(e.Cause, ErrUnsupportedJWTAlgorithm),
		errors.Is(e.Cause, ErrMissingJTI):
		category = goerrors.CategoryAuth
		code = http.StatusUnauthorized
		textCode = core.ServiceErrorEmbeddedSessionInvalid
	case errors.Is(e.Cause, ErrInvalidRequestedTokenType):
		category = goerrors.CategoryBadInput
		code = http.StatusBadRequest
		textCode = core.ServiceErrorBadInput
	}

	metadata := map[string]any{
		"code":  strings.TrimSpace(e.Code),
		"field": strings.TrimSpace(e.Field),
	}

	return goerrors.New(e.Error(), category).
		WithCode(code).
		WithTextCode(textCode).
		WithMetadata(metadata)
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

func (e *ExchangeError) ToServiceError() *goerrors.Error {
	if e == nil {
		return goerrors.New(ErrTokenExchangeFailed.Error(), goerrors.CategoryExternal).
			WithCode(http.StatusBadGateway).
			WithTextCode(core.ServiceErrorEmbeddedExchangeFailed)
	}

	category := goerrors.CategoryExternal
	textCode := core.ServiceErrorEmbeddedExchangeFailed
	code := http.StatusBadGateway

	switch e.StatusCode {
	case http.StatusTooManyRequests:
		category = goerrors.CategoryRateLimit
		textCode = core.ServiceErrorRateLimited
		code = http.StatusTooManyRequests
	case http.StatusUnauthorized:
		category = goerrors.CategoryAuth
		textCode = core.ServiceErrorUnauthorized
		code = http.StatusUnauthorized
	case http.StatusForbidden:
		category = goerrors.CategoryAuthz
		textCode = core.ServiceErrorForbidden
		code = http.StatusForbidden
	default:
		if e.StatusCode >= 400 {
			code = e.StatusCode
		}
	}

	return goerrors.New(e.Error(), category).
		WithCode(code).
		WithTextCode(textCode).
		WithMetadata(map[string]any{
			"exchange_error_code": strings.TrimSpace(e.ErrorCode),
		})
}
