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

func TestDispatcher_IdempotencyWindowExpiresByKeyTTL(t *testing.T) {
	store := NewInMemoryIdempotencyStore()
	now := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	handler := &stubInboundHandler{
		surface: SurfaceCommand,
		result: core.InboundResult{
			Accepted:   true,
			StatusCode: 202,
		},
	}
	dispatcher := NewDispatcher(stubInboundVerifier{}, store)
	dispatcher.KeyTTL = time.Minute
	if err := dispatcher.Register(handler); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	req := core.InboundRequest{
		ProviderID: "github",
		Surface:    SurfaceCommand,
		Metadata: map[string]any{
			"idempotency_key": "ttl-key",
		},
	}
	if _, err := dispatcher.Dispatch(context.Background(), req); err != nil {
		t.Fatalf("dispatch first request: %v", err)
	}
	if handler.calls != 1 {
		t.Fatalf("expected one handler call, got %d", handler.calls)
	}

	deduped, err := dispatcher.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("dispatch duplicate request: %v", err)
	}
	if deduped.Metadata["deduped"] != true {
		t.Fatalf("expected deduped marker before ttl expiry")
	}
	if handler.calls != 1 {
		t.Fatalf("expected duplicate suppression before ttl expiry")
	}

	now = now.Add(2 * time.Minute)
	result, err := dispatcher.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("dispatch after ttl expiry: %v", err)
	}
	if !result.Accepted {
		t.Fatalf("expected request accepted after ttl expiry")
	}
	if handler.calls != 2 {
		t.Fatalf("expected handler to be called again after ttl expiry, got %d", handler.calls)
	}
}

func TestDispatcher_RetriesAfterTransientHandlerFailure(t *testing.T) {
	store := NewInMemoryClaimStore()
	now := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	handler := &stubInboundHandler{
		surface: SurfaceCommand,
		err:     errors.New("temporary inbound failure"),
	}
	dispatcher := NewDispatcher(stubInboundVerifier{}, store)
	if err := dispatcher.Register(handler); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	req := core.InboundRequest{
		ProviderID: "github",
		Surface:    SurfaceCommand,
		Metadata: map[string]any{
			"idempotency_key": "retry-me",
		},
	}
	if _, err := dispatcher.Dispatch(context.Background(), req); err == nil {
		t.Fatalf("expected transient failure to bubble")
	}
	if handler.calls != 1 {
		t.Fatalf("expected one handler call after first failure, got %d", handler.calls)
	}

	handler.err = nil
	handler.result = core.InboundResult{Accepted: true, StatusCode: 202}
	now = now.Add(time.Second)
	result, err := dispatcher.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("expected retry to succeed: %v", err)
	}
	if !result.Accepted {
		t.Fatalf("expected successful retry result")
	}
	if handler.calls != 2 {
		t.Fatalf("expected handler to be called again after failure, got %d", handler.calls)
	}
}

func TestInMemoryClaimStore_RecoversAfterLeaseExpiry(t *testing.T) {
	store := NewInMemoryClaimStore()
	now := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	claimID, accepted, err := store.Claim(context.Background(), "provider:surface:key", time.Minute)
	if err != nil {
		t.Fatalf("claim first: %v", err)
	}
	if !accepted || claimID == "" {
		t.Fatalf("expected first claim to be accepted")
	}

	if _, accepted, err := store.Claim(context.Background(), "provider:surface:key", time.Minute); err != nil {
		t.Fatalf("claim while lease active: %v", err)
	} else if accepted {
		t.Fatalf("expected claim to be rejected while lease is active")
	}

	now = now.Add(2 * time.Minute)
	reclaimID, accepted, err := store.Claim(context.Background(), "provider:surface:key", time.Minute)
	if err != nil {
		t.Fatalf("claim after lease expiry: %v", err)
	}
	if !accepted || reclaimID == "" {
		t.Fatalf("expected claim recovery after lease expiry")
	}
	if reclaimID == claimID {
		t.Fatalf("expected new claim id after lease-expiry recovery")
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
