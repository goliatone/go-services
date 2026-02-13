package webhooks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestProcessor_DedupesDeliveries(t *testing.T) {
	ledger := newMemoryDeliveryLedger()
	handler := &stubWebhookHandler{
		result: core.InboundResult{
			Accepted:   true,
			StatusCode: 202,
		},
	}
	processor := NewProcessor(stubVerifier{err: nil}, ledger, handler)

	req := core.InboundRequest{
		ProviderID: "github",
		Metadata: map[string]any{
			"delivery_id": "delivery-1",
		},
	}

	first, err := processor.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("process first webhook: %v", err)
	}
	if !first.Accepted {
		t.Fatalf("expected first delivery accepted")
	}
	if handler.calls != 1 {
		t.Fatalf("expected handler to be called once")
	}

	second, err := processor.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("process duplicate webhook: %v", err)
	}
	if !second.Accepted {
		t.Fatalf("expected duplicate to be accepted as deduped")
	}
	if second.Metadata["deduped"] != true {
		t.Fatalf("expected deduped metadata marker")
	}
	if handler.calls != 1 {
		t.Fatalf("expected handler call count to remain unchanged for duplicate")
	}
}

func TestProcessor_RecordsRetryOnHandlerFailure(t *testing.T) {
	ledger := newMemoryDeliveryLedger()
	handler := &stubWebhookHandler{
		err: errors.New("temporary failure"),
	}
	processor := NewProcessor(stubVerifier{}, ledger, handler)
	processor.RetryPolicy = ExponentialRetryPolicy{Initial: time.Second, Max: 4 * time.Second}
	processor.Now = func() time.Time {
		return time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	}

	req := core.InboundRequest{
		ProviderID: "google",
		Headers: map[string]string{
			"X-Goog-Message-Number": "42",
		},
	}
	if _, err := processor.Process(context.Background(), req); err == nil {
		t.Fatalf("expected retryable handler failure")
	}

	record, err := ledger.Get(context.Background(), "google", "42")
	if err != nil {
		t.Fatalf("load delivery record: %v", err)
	}
	if record.Status != DeliveryStatusRetryReady {
		t.Fatalf("expected retry-ready status, got %q", record.Status)
	}
	if record.Attempts != 2 {
		t.Fatalf("expected attempts to increment to 2, got %d", record.Attempts)
	}
}

func TestProcessor_RejectsInvalidSignature(t *testing.T) {
	ledger := newMemoryDeliveryLedger()
	handler := &stubWebhookHandler{}
	processor := NewProcessor(stubVerifier{err: errors.New("signature mismatch")}, ledger, handler)

	result, err := processor.Process(context.Background(), core.InboundRequest{
		ProviderID: "github",
		Metadata: map[string]any{
			"delivery_id": "delivery-2",
		},
	})
	if err == nil {
		t.Fatalf("expected verifier error")
	}
	if result.StatusCode != 401 {
		t.Fatalf("expected unauthorized status code, got %d", result.StatusCode)
	}
	if handler.calls != 0 {
		t.Fatalf("expected handler not to run when verification fails")
	}
}

func TestProcessor_CoalescesWebhookBurstsByChannel(t *testing.T) {
	ledger := newMemoryDeliveryLedger()
	handler := &stubWebhookHandler{
		result: core.InboundResult{Accepted: true, StatusCode: 202},
	}
	now := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	processor := NewProcessor(stubVerifier{}, ledger, handler)
	processor.Burst = NewBurstController(BurstOptions{
		Mode:   BurstModeCoalesce,
		Window: 10 * time.Second,
		Now: func() time.Time {
			return now
		},
	})

	first, err := processor.Process(context.Background(), core.InboundRequest{
		ProviderID: "github",
		Metadata: map[string]any{
			"delivery_id": "delivery-1",
			"channel_id":  "channel-1",
		},
	})
	if err != nil {
		t.Fatalf("process first burst webhook: %v", err)
	}
	if !first.Accepted {
		t.Fatalf("expected first webhook accepted")
	}
	if handler.calls != 1 {
		t.Fatalf("expected handler calls=1, got %d", handler.calls)
	}

	now = now.Add(2 * time.Second)
	second, err := processor.Process(context.Background(), core.InboundRequest{
		ProviderID: "github",
		Metadata: map[string]any{
			"delivery_id": "delivery-2",
			"channel_id":  "channel-1",
		},
	})
	if err != nil {
		t.Fatalf("process coalesced webhook: %v", err)
	}
	if !second.Accepted {
		t.Fatalf("expected coalesced webhook accepted")
	}
	if second.Metadata["coalesced"] != true {
		t.Fatalf("expected coalesced metadata marker")
	}
	if handler.calls != 1 {
		t.Fatalf("expected handler calls to remain 1 for coalesced webhook")
	}
}

func TestProcessor_DebounceWindowAllowsAfterQuietPeriod(t *testing.T) {
	ledger := newMemoryDeliveryLedger()
	handler := &stubWebhookHandler{
		result: core.InboundResult{Accepted: true, StatusCode: 202},
	}
	now := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	processor := NewProcessor(stubVerifier{}, ledger, handler)
	processor.Burst = NewBurstController(BurstOptions{
		Mode:   BurstModeDebounce,
		Window: 5 * time.Second,
		Now: func() time.Time {
			return now
		},
	})

	_, err := processor.Process(context.Background(), core.InboundRequest{
		ProviderID: "github",
		Metadata: map[string]any{
			"delivery_id": "delivery-1",
			"channel_id":  "channel-1",
		},
	})
	if err != nil {
		t.Fatalf("process first webhook: %v", err)
	}

	now = now.Add(2 * time.Second)
	second, err := processor.Process(context.Background(), core.InboundRequest{
		ProviderID: "github",
		Metadata: map[string]any{
			"delivery_id": "delivery-2",
			"channel_id":  "channel-1",
		},
	})
	if err != nil {
		t.Fatalf("process debounced webhook: %v", err)
	}
	if second.Metadata["debounced"] != true {
		t.Fatalf("expected debounced metadata marker")
	}
	if handler.calls != 1 {
		t.Fatalf("expected handler calls=1 while within debounce window")
	}

	now = now.Add(6 * time.Second)
	_, err = processor.Process(context.Background(), core.InboundRequest{
		ProviderID: "github",
		Metadata: map[string]any{
			"delivery_id": "delivery-3",
			"channel_id":  "channel-1",
		},
	})
	if err != nil {
		t.Fatalf("process webhook after debounce window: %v", err)
	}
	if handler.calls != 2 {
		t.Fatalf("expected handler calls=2 after quiet period, got %d", handler.calls)
	}
}

type stubVerifier struct {
	err error
}

func (v stubVerifier) Verify(context.Context, core.InboundRequest) error {
	return v.err
}

type stubWebhookHandler struct {
	result core.InboundResult
	err    error
	calls  int
}

func (h *stubWebhookHandler) Handle(context.Context, core.InboundRequest) (core.InboundResult, error) {
	h.calls++
	if h.err != nil {
		return core.InboundResult{}, h.err
	}
	return h.result, nil
}

type memoryDeliveryLedger struct {
	records map[string]DeliveryRecord
}

func newMemoryDeliveryLedger() *memoryDeliveryLedger {
	return &memoryDeliveryLedger{records: map[string]DeliveryRecord{}}
}

func (l *memoryDeliveryLedger) Reserve(_ context.Context, providerID string, deliveryID string, _ []byte) (DeliveryRecord, bool, error) {
	key := providerID + ":" + deliveryID
	record, ok := l.records[key]
	if ok {
		return record, true, nil
	}
	now := time.Now().UTC()
	record = DeliveryRecord{
		ID:         key,
		ProviderID: providerID,
		DeliveryID: deliveryID,
		Status:     DeliveryStatusPending,
		Attempts:   1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	l.records[key] = record
	return record, false, nil
}

func (l *memoryDeliveryLedger) Get(_ context.Context, providerID string, deliveryID string) (DeliveryRecord, error) {
	key := providerID + ":" + deliveryID
	record, ok := l.records[key]
	if !ok {
		return DeliveryRecord{}, errors.New("missing delivery")
	}
	return record, nil
}

func (l *memoryDeliveryLedger) MarkProcessed(_ context.Context, providerID string, deliveryID string) error {
	key := providerID + ":" + deliveryID
	record, ok := l.records[key]
	if !ok {
		return errors.New("missing delivery")
	}
	record.Status = DeliveryStatusProcessed
	record.UpdatedAt = time.Now().UTC()
	l.records[key] = record
	return nil
}

func (l *memoryDeliveryLedger) MarkRetry(
	_ context.Context,
	providerID string,
	deliveryID string,
	_ error,
	nextAttemptAt time.Time,
) error {
	key := providerID + ":" + deliveryID
	record, ok := l.records[key]
	if !ok {
		return errors.New("missing delivery")
	}
	record.Status = DeliveryStatusRetryReady
	record.Attempts++
	record.UpdatedAt = time.Now().UTC()
	record.NextAttemptAt = &nextAttemptAt
	l.records[key] = record
	return nil
}
