package command

import (
	"context"
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
}
