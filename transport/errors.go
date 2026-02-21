package transport

import (
	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-services/core"
)

func transportError(
	message string,
	category goerrors.Category,
	code int,
	metadata map[string]any,
) error {
	err := goerrors.New(message, category).
		WithCode(code).
		WithTextCode(transportTextCode(category))
	if len(metadata) > 0 {
		err.WithMetadata(metadata)
	}
	return err
}

func transportWrapError(
	source error,
	category goerrors.Category,
	message string,
	code int,
	metadata map[string]any,
) error {
	if source == nil {
		return transportError(message, category, code, metadata)
	}
	err := goerrors.Wrap(source, category, message).
		WithCode(code).
		WithTextCode(transportTextCode(category))
	if len(metadata) > 0 {
		err.WithMetadata(metadata)
	}
	return err
}

func transportTextCode(category goerrors.Category) string {
	switch category {
	case goerrors.CategoryBadInput, goerrors.CategoryValidation:
		return core.ServiceErrorBadInput
	case goerrors.CategoryAuth:
		return core.ServiceErrorUnauthorized
	case goerrors.CategoryAuthz:
		return core.ServiceErrorForbidden
	case goerrors.CategoryRateLimit:
		return core.ServiceErrorRateLimited
	case goerrors.CategoryOperation:
		return core.ServiceErrorOperationFailed
	case goerrors.CategoryExternal:
		return core.ServiceErrorExternalFailure
	default:
		return core.ServiceErrorInternal
	}
}
