package core

import (
	"errors"
	"net/http"
	"strings"

	goerrors "github.com/goliatone/go-errors"
)

const (
	// Generic service text codes.
	ServiceErrorBadInput        = "SERVICE_BAD_INPUT"
	ServiceErrorNotFound        = "SERVICE_NOT_FOUND"
	ServiceErrorUnauthorized    = "SERVICE_UNAUTHORIZED"
	ServiceErrorForbidden       = "SERVICE_FORBIDDEN"
	ServiceErrorConflict        = "SERVICE_CONFLICT"
	ServiceErrorOperationFailed = "SERVICE_OPERATION_FAILED"
	ServiceErrorExternalFailure = "SERVICE_EXTERNAL_FAILURE"
	// Service/domain specific text codes.
	ServiceErrorProviderNotFound        = "SERVICE_PROVIDER_NOT_FOUND"
	ServiceErrorCapabilityUnsupported   = "SERVICE_CAPABILITY_UNSUPPORTED"
	ServiceErrorOAuthStateInvalid       = "SERVICE_OAUTH_STATE_INVALID"
	ServiceErrorEmbeddedAuthUnsupported = "SERVICE_EMBEDDED_AUTH_UNSUPPORTED"
	ServiceErrorEmbeddedSessionInvalid  = "SERVICE_EMBEDDED_SESSION_INVALID"
	ServiceErrorEmbeddedExchangeFailed  = "SERVICE_EMBEDDED_EXCHANGE_FAILED"
	ServiceErrorReplayDetected          = "SERVICE_REPLAY_DETECTED"
	ServiceErrorRefreshLocked           = "SERVICE_REFRESH_LOCKED"
	ServiceErrorPermissionDenied        = "SERVICE_PERMISSION_DENIED"
	ServiceErrorRateLimited             = "SERVICE_RATE_LIMITED"
	ServiceErrorProviderOperationFailed = "SERVICE_PROVIDER_OPERATION_FAILED"
	ServiceErrorSyncJobNotFound         = "SERVICE_SYNC_JOB_NOT_FOUND"
	ServiceErrorSyncCursorConflict      = "SERVICE_SYNC_CURSOR_CONFLICT"
	ServiceErrorProfileNotFound         = "SERVICE_PROFILE_NOT_FOUND"
	ServiceErrorInternal                = "SERVICE_INTERNAL_ERROR"
)

type serviceErrorConvertible interface {
	ToServiceError() *goerrors.Error
}

func serviceErrorMapper(err error) *goerrors.Error {
	if err == nil {
		return nil
	}

	var richErr *goerrors.Error
	if goerrors.As(err, &richErr) {
		return ensureServiceErrorEnvelope(richErr)
	}

	var convertible serviceErrorConvertible
	if errors.As(err, &convertible) {
		mapped := convertible.ToServiceError()
		if mapped != nil {
			return ensureServiceErrorEnvelope(mapped)
		}
	}

	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case errors.Is(err, ErrSyncJobNotFound):
		return newServiceError(err.Error(), goerrors.CategoryNotFound, ServiceErrorSyncJobNotFound)
	case errors.Is(err, ErrSyncCursorConflict):
		return newServiceError(err.Error(), goerrors.CategoryConflict, ServiceErrorSyncCursorConflict)
	case errors.Is(err, ErrInvalidSyncJobMode), errors.Is(err, ErrInvalidSyncJobScope):
		return newServiceError(err.Error(), goerrors.CategoryBadInput, ServiceErrorBadInput)
	case errors.Is(err, ErrEmbeddedAuthUnsupported):
		return newServiceError(err.Error(), goerrors.CategoryOperation, ServiceErrorEmbeddedAuthUnsupported)
	}

	switch {
	case strings.Contains(msg, "provider") && strings.Contains(msg, "not registered"):
		return newServiceError(err.Error(), goerrors.CategoryNotFound, ServiceErrorProviderNotFound)
	case strings.Contains(msg, "capability") && strings.Contains(msg, "not supported"):
		return newServiceError(err.Error(), goerrors.CategoryOperation, ServiceErrorCapabilityUnsupported)
	case strings.Contains(msg, "oauth callback state"), strings.Contains(msg, "oauth state"):
		return newServiceError(err.Error(), goerrors.CategoryAuth, ServiceErrorOAuthStateInvalid)
	case strings.Contains(msg, "replay"):
		return newServiceError(err.Error(), goerrors.CategoryConflict, ServiceErrorReplayDetected)
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
		return ServiceErrorNotFound
	case goerrors.CategoryAuth:
		return ServiceErrorUnauthorized
	case goerrors.CategoryAuthz:
		return ServiceErrorForbidden
	case goerrors.CategoryConflict:
		return ServiceErrorConflict
	case goerrors.CategoryRateLimit:
		return ServiceErrorRateLimited
	case goerrors.CategoryOperation:
		return ServiceErrorOperationFailed
	case goerrors.CategoryExternal:
		return ServiceErrorExternalFailure
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
