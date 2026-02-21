package command

import (
	"context"
	"net/http"
	"testing"

	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-services/core"
)

func TestConnectMessage_ValidateReturnsRichError(t *testing.T) {
	err := (ConnectMessage{}).Validate()
	if err == nil {
		t.Fatalf("expected validation error")
	}

	var rich *goerrors.Error
	if !goerrors.As(err, &rich) {
		t.Fatalf("expected go-errors envelope, got %T", err)
	}
	if rich.Category != goerrors.CategoryValidation {
		t.Fatalf("expected validation category, got %q", rich.Category)
	}
	if rich.TextCode != core.ServiceErrorBadInput {
		t.Fatalf("expected %q text code, got %q", core.ServiceErrorBadInput, rich.TextCode)
	}
	if rich.Code != http.StatusBadRequest {
		t.Fatalf("expected %d code, got %d", http.StatusBadRequest, rich.Code)
	}
	validation := rich.AllValidationErrors()
	if len(validation) == 0 {
		t.Fatalf("expected validation errors in envelope")
	}
	if validation[0].Field != "provider_id" {
		t.Fatalf("expected provider_id validation field, got %q", validation[0].Field)
	}
}

func TestConnectCommand_NilServiceReturnsRichError(t *testing.T) {
	var cmd *ConnectCommand
	err := cmd.Execute(context.Background(), ConnectMessage{})
	if err == nil {
		t.Fatalf("expected command dependency error")
	}

	var rich *goerrors.Error
	if !goerrors.As(err, &rich) {
		t.Fatalf("expected go-errors envelope, got %T", err)
	}
	if rich.Category != goerrors.CategoryInternal {
		t.Fatalf("expected internal category, got %q", rich.Category)
	}
	if rich.TextCode != core.ServiceErrorInternal {
		t.Fatalf("expected %q text code, got %q", core.ServiceErrorInternal, rich.TextCode)
	}
	if rich.Code != http.StatusInternalServerError {
		t.Fatalf("expected %d code, got %d", http.StatusInternalServerError, rich.Code)
	}
}
