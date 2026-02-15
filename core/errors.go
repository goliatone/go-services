package core

import (
	"net/http"
	"strings"

	goerrors "github.com/goliatone/go-errors"
)

const (
	ServiceErrorBadInput                = "SERVICE_BAD_INPUT"
	ServiceErrorProviderNotFound        = "SERVICE_PROVIDER_NOT_FOUND"
	ServiceErrorCapabilityUnsupported   = "SERVICE_CAPABILITY_UNSUPPORTED"
	ServiceErrorOAuthStateInvalid       = "SERVICE_OAUTH_STATE_INVALID"
	ServiceErrorRefreshLocked           = "SERVICE_REFRESH_LOCKED"
	ServiceErrorPermissionDenied        = "SERVICE_PERMISSION_DENIED"
	ServiceErrorRateLimited             = "SERVICE_RATE_LIMITED"
	ServiceErrorProviderOperationFailed = "SERVICE_PROVIDER_OPERATION_FAILED"
	ServiceErrorInternal                = "SERVICE_INTERNAL_ERROR"
)

func serviceErrorMapper(err error) *goerrors.Error {
	if err == nil {
		return nil
	}

	var richErr *goerrors.Error
	if goerrors.As(err, &richErr) {
		return ensureServiceErrorEnvelope(richErr)
	}

	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "provider") && strings.Contains(msg, "not registered"):
		return newServiceError(err.Error(), goerrors.CategoryNotFound, ServiceErrorProviderNotFound)
	case strings.Contains(msg, "capability") && strings.Contains(msg, "not supported"):
		return newServiceError(err.Error(), goerrors.CategoryOperation, ServiceErrorCapabilityUnsupported)
	case strings.Contains(msg, "oauth callback state"), strings.Contains(msg, "oauth state"):
		return newServiceError(err.Error(), goerrors.CategoryAuth, ServiceErrorOAuthStateInvalid)
	case strings.Contains(msg, "lock already held"), strings.Contains(msg, "refresh lock"):
		return newServiceError(err.Error(), goerrors.CategoryConflict, ServiceErrorRefreshLocked)
	case strings.Contains(msg, "throttl"), strings.Contains(msg, "rate limit"):
		return newServiceError(err.Error(), goerrors.CategoryRateLimit, ServiceErrorRateLimited)
	case strings.Contains(msg, "required"), strings.Contains(msg, "invalid"), strings.Contains(msg, "mismatch"):
		return newServiceError(err.Error(), goerrors.CategoryBadInput, ServiceErrorBadInput)
	}

	mapped := goerrors.MapToError(err, goerrors.DefaultErrorMappers())
	return ensureServiceErrorEnvelope(mapped)
}

func newServiceError(message string, category goerrors.Category, textCode string) *goerrors.Error {
	return ensureServiceErrorEnvelope(
		goerrors.New(message, category).
			WithTextCode(textCode),
	)
}

func ensureServiceErrorEnvelope(err *goerrors.Error) *goerrors.Error {
	if err == nil {
		return nil
	}
	if err.Code == 0 {
		err.Code = serviceHTTPStatus(err.Category)
	}
	if strings.TrimSpace(err.TextCode) == "" {
		err.TextCode = defaultServiceTextCode(err.Category)
	}
	if err.Category == goerrors.CategoryInternal && strings.TrimSpace(err.Message) == "" {
		err.Message = "An unexpected error occurred"
	}
	return err
}

func defaultServiceTextCode(category goerrors.Category) string {
	switch category {
	case goerrors.CategoryBadInput, goerrors.CategoryValidation:
		return ServiceErrorBadInput
	case goerrors.CategoryNotFound:
		return ServiceErrorProviderNotFound
	case goerrors.CategoryAuth, goerrors.CategoryAuthz:
		return ServiceErrorPermissionDenied
	case goerrors.CategoryConflict:
		return ServiceErrorRefreshLocked
	case goerrors.CategoryRateLimit:
		return ServiceErrorRateLimited
	case goerrors.CategoryOperation:
		return ServiceErrorCapabilityUnsupported
	default:
		return ServiceErrorInternal
	}
}

func serviceHTTPStatus(category goerrors.Category) int {
	switch category {
	case goerrors.CategoryBadInput, goerrors.CategoryValidation:
		return http.StatusBadRequest
	case goerrors.CategoryNotFound:
		return http.StatusNotFound
	case goerrors.CategoryAuth:
		return http.StatusUnauthorized
	case goerrors.CategoryAuthz:
		return http.StatusForbidden
	case goerrors.CategoryConflict:
		return http.StatusConflict
	case goerrors.CategoryRateLimit:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}
