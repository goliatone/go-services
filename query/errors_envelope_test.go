package query

import (
	"context"
	"testing"

	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-services/core"
)

func TestLoadSyncCursorMessage_ValidateReturnsRichError(t *testing.T) {
	err := (LoadSyncCursorMessage{}).Validate()
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

func TestLoadSyncCursorQuery_NilReaderReturnsRichError(t *testing.T) {
	var q *LoadSyncCursorQuery
	_, err := q.Query(context.Background(), LoadSyncCursorMessage{})
	if err == nil {
		t.Fatalf("expected dependency error")
	}

	var rich *goerrors.Error
	if !goerrors.As(err, &rich) {
		t.Fatalf("expected go-errors envelope, got %T", err)
	}
	if rich.Category != goerrors.CategoryInternal {
		t.Fatalf("expected internal category, got %q", rich.Category)
	}
}
