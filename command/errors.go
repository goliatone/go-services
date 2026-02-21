package command

import (
	"net/http"

	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-services/core"
)

func commandDependencyError(message string) error {
	return goerrors.New(message, goerrors.CategoryInternal).
		WithCode(http.StatusInternalServerError).
		WithTextCode(core.ServiceErrorInternal)
}

func commandValidationError(field string, message string) error {
	return goerrors.NewValidation("command: validation failed", goerrors.FieldError{
		Field:   field,
		Message: message,
	}).
		WithCode(http.StatusBadRequest).
		WithTextCode(core.ServiceErrorBadInput).
		WithSeverity(goerrors.SeverityError)
}

func commandInvalidInputError(message string) error {
	return goerrors.New(message, goerrors.CategoryBadInput).
		WithCode(http.StatusBadRequest).
		WithTextCode(core.ServiceErrorBadInput)
}

func commandWrapValidation(err error, message string) error {
	if err == nil {
		return nil
	}
	return goerrors.Wrap(err, goerrors.CategoryValidation, message).
		WithCode(http.StatusBadRequest).
		WithTextCode(core.ServiceErrorBadInput)
}
