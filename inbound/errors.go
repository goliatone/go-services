package inbound

import (
	"net/http"

	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-services/core"
)

func inboundError(
	message string,
	category goerrors.Category,
	code int,
	textCode string,
	metadata map[string]any,
) error {
	err := goerrors.New(message, category).
		WithCode(code).
		WithTextCode(textCode)
	if len(metadata) > 0 {
		err.WithMetadata(metadata)
	}
	return err
}

func inboundWrapError(
	source error,
	category goerrors.Category,
	message string,
	code int,
	textCode string,
	metadata map[string]any,
) error {
	if source == nil {
		return inboundError(message, category, code, textCode, metadata)
	}
	err := goerrors.Wrap(source, category, message).
		WithCode(code).
		WithTextCode(textCode)
	if len(metadata) > 0 {
		err.WithMetadata(metadata)
	}
	return err
}

func inboundBadInput(message string, metadata map[string]any) error {
	return inboundError(
		message,
		goerrors.CategoryBadInput,
		http.StatusBadRequest,
		core.ServiceErrorBadInput,
		metadata,
	)
}

func inboundInternal(message string, metadata map[string]any) error {
	return inboundError(
		message,
		goerrors.CategoryInternal,
		http.StatusInternalServerError,
		core.ServiceErrorInternal,
		metadata,
	)
}

