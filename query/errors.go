package query

import (
	"net/http"

	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-services/core"
)

func queryDependencyError(message string) error {
	return goerrors.New(message, goerrors.CategoryInternal).
		WithCode(http.StatusInternalServerError).
		WithTextCode(core.ServiceErrorInternal)
}

func queryValidationError(field string, message string) error {
	return goerrors.NewValidation("query: validation failed", goerrors.FieldError{
		Field:   field,
		Message: message,
	}).
		WithCode(http.StatusBadRequest).
		WithTextCode(core.ServiceErrorBadInput).
		WithSeverity(goerrors.SeverityError)
}

func queryInvalidInputError(message string) error {
	return goerrors.New(message, goerrors.CategoryBadInput).
		WithCode(http.StatusBadRequest).
		WithTextCode(core.ServiceErrorBadInput)
}

func queryWrapValidation(err error, message string) error {
	if err == nil {
		return nil
	}
	return goerrors.Wrap(err, goerrors.CategoryValidation, message).
		WithCode(http.StatusBadRequest).
		WithTextCode(core.ServiceErrorBadInput)
}
