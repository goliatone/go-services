package core

import (
	"context"
	stderrors "errors"
	"strings"
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

func TestRefresh_RejectsProviderConnectionMismatch(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register github provider: %v", err)
	}
	if err := registry.Register(testProvider{id: "slack"}); err != nil {
		t.Fatalf("register slack provider: %v", err)
	}

	connectionStore := newMemoryConnectionStore()
	connection, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "user", ID: "u_mismatch"},
		ExternalAccountID: "acct",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.Refresh(ctx, RefreshRequest{
		ProviderID:   "slack",
		ConnectionID: connection.ID,
		Credential: &ActiveCredential{
			TokenType:   "bearer",
			AccessToken: "token",
			Refreshable: true,
		},
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "provider mismatch") {
		t.Fatalf("expected provider mismatch error, got %v", err)
	}
}
