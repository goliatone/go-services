package inbound

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestDispatcher_SharedVerificationAndIdempotency(t *testing.T) {
	store := NewInMemoryIdempotencyStore()
	store.Now = func() time.Time {
		return time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	}
	handler := &stubInboundHandler{
		surface: SurfaceCommand,
		result: core.InboundResult{
			Accepted:   true,
			StatusCode: 202,
		},
	}
	dispatcher := NewDispatcher(stubInboundVerifier{}, store)
	if err := dispatcher.Register(handler); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	req := core.InboundRequest{
		ProviderID: "github",
		Surface:    SurfaceCommand,
		Metadata: map[string]any{
			"idempotency_key": "req-1",
		},
	}
	first, err := dispatcher.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("dispatch first request: %v", err)
	}
	if !first.Accepted {
		t.Fatalf("expected first request accepted")
	}
	if handler.calls != 1 {
		t.Fatalf("expected handler to be called once")
	}

	second, err := dispatcher.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("dispatch duplicate request: %v", err)
	}
	if second.Metadata["deduped"] != true {
		t.Fatalf("expected deduped marker on repeated idempotency key")
	}
	if handler.calls != 1 {
		t.Fatalf("expected handler call count unchanged for duplicate")
	}
}

func TestDispatcher_RejectsInvalidInboundSignature(t *testing.T) {
	store := NewInMemoryIdempotencyStore()
	handler := &stubInboundHandler{
		surface: SurfaceWebhook,
		result: core.InboundResult{
			Accepted:   true,
			StatusCode: 200,
		},
	}
	dispatcher := NewDispatcher(stubInboundVerifier{err: errors.New("invalid signature")}, store)
	if err := dispatcher.Register(handler); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	result, err := dispatcher.Dispatch(context.Background(), core.InboundRequest{
		ProviderID: "github",
		Surface:    SurfaceWebhook,
		Metadata: map[string]any{
			"delivery_id": "del-1",
		},
	})
	if err == nil {
		t.Fatalf("expected verifier failure")
	}
	if result.StatusCode != 401 {
		t.Fatalf("expected unauthorized status, got %d", result.StatusCode)
	}
	if handler.calls != 0 {
		t.Fatalf("expected handler not called on failed verification")
	}
}

func TestDispatcher_SupportsAllInboundSurfaces(t *testing.T) {
	surfaces := []string{
		SurfaceWebhook,
		SurfaceCommand,
		SurfaceInteraction,
		SurfaceEventCallback,
	}
	dispatcher := NewDispatcher(stubInboundVerifier{}, NewInMemoryIdempotencyStore())
	for _, surface := range surfaces {
		handler := &stubInboundHandler{
			surface: surface,
			result: core.InboundResult{
				Accepted:   true,
				StatusCode: 200,
			},
		}
		if err := dispatcher.Register(handler); err != nil {
			t.Fatalf("register %s handler: %v", surface, err)
		}
		_, err := dispatcher.Dispatch(context.Background(), core.InboundRequest{
			ProviderID: "github",
			Surface:    surface,
			Metadata: map[string]any{
				"idempotency_key": "key-" + surface,
			},
		})
		if err != nil {
			t.Fatalf("dispatch %s surface: %v", surface, err)
		}
	}
}

type stubInboundVerifier struct {
	err error
}

func (v stubInboundVerifier) Verify(context.Context, core.InboundRequest) error {
	return v.err
}

type stubInboundHandler struct {
	surface string
	result  core.InboundResult
	err     error
	calls   int
}

func (h *stubInboundHandler) Surface() string {
	return h.surface
}

func (h *stubInboundHandler) Handle(context.Context, core.InboundRequest) (core.InboundResult, error) {
	h.calls++
	if h.err != nil {
		return core.InboundResult{}, h.err
	}
	return h.result, nil
}
