package inbound

import (
	"context"
	"errors"
	"testing"

	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-services/core"
)

func TestDefaultIdempotencyKeyExtractor_MissingKeyReturnsRichError(t *testing.T) {
	_, err := DefaultIdempotencyKeyExtractor(core.InboundRequest{})
	if err == nil {
		t.Fatalf("expected idempotency key error")
	}

	var rich *goerrors.Error
	if !goerrors.As(err, &rich) {
		t.Fatalf("expected go-errors envelope, got %T", err)
	}
	if rich.Category != goerrors.CategoryBadInput {
		t.Fatalf("expected bad_input category, got %q", rich.Category)
	}
}

func TestDispatch_VerificationFailureReturnsRichError(t *testing.T) {
	dispatcher := NewDispatcher(stubInboundVerifier{err: errors.New("invalid signature")}, NewInMemoryIdempotencyStore())
	handler := &stubInboundHandler{surface: SurfaceWebhook, result: core.InboundResult{Accepted: true, StatusCode: 200}}
	if err := dispatcher.Register(handler); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	_, err := dispatcher.Dispatch(context.Background(), core.InboundRequest{
		ProviderID: "github",
		Surface:    SurfaceWebhook,
		Metadata:   map[string]any{"delivery_id": "d1"},
	})
	if err == nil {
		t.Fatalf("expected verification error")
	}

	var rich *goerrors.Error
	if !goerrors.As(err, &rich) {
		t.Fatalf("expected go-errors envelope, got %T", err)
	}
	if rich.Category != goerrors.CategoryAuth {
		t.Fatalf("expected auth category, got %q", rich.Category)
	}
}
