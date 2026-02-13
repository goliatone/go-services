package core

import (
	"context"
	stderrors "errors"
	"testing"

	goerrors "github.com/goliatone/go-errors"
)

func TestServiceErrorMapper_AssignsStableCodes(t *testing.T) {
	mapped := serviceErrorMapper(stderrors.New("core: oauth callback state mismatch"))
	if mapped.TextCode != ServiceErrorOAuthStateInvalid {
		t.Fatalf("expected oauth state text code, got %q", mapped.TextCode)
	}
	if mapped.Code == 0 {
		t.Fatalf("expected http status code on mapped error")
	}

	mapped = serviceErrorMapper(stderrors.New("core: refresh lock already held for connection"))
	if mapped.TextCode != ServiceErrorRefreshLocked {
		t.Fatalf("expected refresh lock code, got %q", mapped.TextCode)
	}
	if mapped.Category != goerrors.CategoryConflict {
		t.Fatalf("expected conflict category, got %q", mapped.Category)
	}
}

func TestServiceMethods_MapErrorsToStableServiceCodes(t *testing.T) {
	ctx := context.Background()
	svc, err := NewService(Config{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.Refresh(ctx, RefreshRequest{
		ProviderID:   "github",
		ConnectionID: "",
	})
	if err == nil {
		t.Fatalf("expected refresh validation error")
	}
	var richErr *goerrors.Error
	if !goerrors.As(err, &richErr) {
		t.Fatalf("expected go-errors type, got %T", err)
	}
	if richErr.TextCode != ServiceErrorBadInput {
		t.Fatalf("expected bad input text code, got %q", richErr.TextCode)
	}

	_, err = svc.Connect(ctx, ConnectRequest{
		ProviderID: "github",
		Scope:      ScopeRef{Type: "user", ID: "u1"},
	})
	if err == nil {
		t.Fatalf("expected provider not found")
	}
	if !goerrors.As(err, &richErr) {
		t.Fatalf("expected go-errors type, got %T", err)
	}
	if richErr.TextCode != ServiceErrorProviderNotFound {
		t.Fatalf("expected provider not found code, got %q", richErr.TextCode)
	}
}
