package webhooks

import (
	"context"
	"errors"
	"strconv"
	"strings"
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
	if record.Attempts != 1 {
		t.Fatalf("expected attempts to remain 1 after first failed claim, got %d", record.Attempts)
	}
}

func TestProcessor_AcceptedServerErrorsRetryByDefault(t *testing.T) {
	ledger := newMemoryDeliveryLedger()
	handler := &stubWebhookHandler{
		result: core.InboundResult{
			Accepted:   true,
			StatusCode: 502,
		},
	}
	processor := NewProcessor(stubVerifier{}, ledger, handler)

	_, err := processor.Process(context.Background(), core.InboundRequest{
		ProviderID: "github",
		Metadata: map[string]any{
			"delivery_id": "delivery-accepted-5xx",
		},
	})
	if err == nil {
		t.Fatalf("expected accepted 5xx response to trigger retry error by default")
	}

	record, getErr := ledger.Get(context.Background(), "github", "delivery-accepted-5xx")
	if getErr != nil {
		t.Fatalf("load retry record: %v", getErr)
	}
	if record.Status != DeliveryStatusRetryReady {
		t.Fatalf("expected retry_ready status, got %q", record.Status)
	}
}

func TestProcessor_AcceptedServerErrorsCanBeAllowed(t *testing.T) {
	ledger := newMemoryDeliveryLedger()
	handler := &stubWebhookHandler{
		result: core.InboundResult{
			Accepted:   true,
			StatusCode: 503,
		},
	}
	processor := NewProcessor(stubVerifier{}, ledger, handler)
	processor.AllowAcceptedServerErrors = true

	result, err := processor.Process(context.Background(), core.InboundRequest{
		ProviderID: "github",
		Metadata: map[string]any{
			"delivery_id": "delivery-accepted-allowed",
		},
	})
	if err != nil {
		t.Fatalf("expected accepted 5xx response to be allowed when opt-in is enabled: %v", err)
	}
	if !result.Accepted {
		t.Fatalf("expected accepted response")
	}

	record, getErr := ledger.Get(context.Background(), "github", "delivery-accepted-allowed")
	if getErr != nil {
		t.Fatalf("load processed record: %v", getErr)
	}
	if record.Status != DeliveryStatusProcessed {
		t.Fatalf("expected processed status, got %q", record.Status)
	}
}

func TestProcessor_ReprocessesRetryReadyDeliveries(t *testing.T) {
	ledger := newMemoryDeliveryLedger()
	handler := &stubWebhookHandler{
		err: errors.New("temporary failure"),
	}
	now := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	processor := NewProcessor(stubVerifier{}, ledger, handler)
	processor.RetryPolicy = ExponentialRetryPolicy{Initial: time.Second, Max: time.Second}
	processor.Now = func() time.Time { return now }

	req := core.InboundRequest{
		ProviderID: "github",
		Metadata: map[string]any{
			"delivery_id": "delivery-retry-ready",
		},
	}
	if _, err := processor.Process(context.Background(), req); err == nil {
		t.Fatalf("expected first processing attempt to fail")
	}
	record, err := ledger.Get(context.Background(), "github", "delivery-retry-ready")
	if err != nil {
		t.Fatalf("get retry-ready record: %v", err)
	}
	if record.Status != DeliveryStatusRetryReady {
		t.Fatalf("expected retry_ready status after first failure, got %q", record.Status)
	}
	if handler.calls != 1 {
		t.Fatalf("expected handler calls=1 after first attempt, got %d", handler.calls)
	}

	now = now.Add(2 * time.Second)
	handler.err = nil
	handler.result = core.InboundResult{Accepted: true, StatusCode: 202}
	if _, err := processor.Process(context.Background(), req); err != nil {
		t.Fatalf("expected retry-ready delivery to be reprocessed: %v", err)
	}
	record, err = ledger.Get(context.Background(), "github", "delivery-retry-ready")
	if err != nil {
		t.Fatalf("get processed record: %v", err)
	}
	if record.Status != DeliveryStatusProcessed {
		t.Fatalf("expected processed status after retry, got %q", record.Status)
	}
	if record.Attempts != 2 {
		t.Fatalf("expected attempts to increment on re-claim, got %d", record.Attempts)
	}
	if handler.calls != 2 {
		t.Fatalf("expected handler to run twice with retry-ready reprocessing, got %d", handler.calls)
	}
}

func TestProcessor_MarksDeliveryDeadAfterMaxAttempts(t *testing.T) {
	ledger := newMemoryDeliveryLedger()
	handler := &stubWebhookHandler{
		err: errors.New("still failing"),
	}
	now := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	processor := NewProcessor(stubVerifier{}, ledger, handler)
	processor.MaxAttempts = 2
	processor.RetryPolicy = ExponentialRetryPolicy{Initial: time.Second, Max: time.Second}
	processor.Now = func() time.Time { return now }

	req := core.InboundRequest{
		ProviderID: "github",
		Metadata: map[string]any{
			"delivery_id": "delivery-dead",
		},
	}
	if _, err := processor.Process(context.Background(), req); err == nil {
		t.Fatalf("expected first attempt failure")
	}
	now = now.Add(2 * time.Second)
	if _, err := processor.Process(context.Background(), req); err == nil {
		t.Fatalf("expected second attempt failure")
	}

	record, err := ledger.Get(context.Background(), "github", "delivery-dead")
	if err != nil {
		t.Fatalf("get dead record: %v", err)
	}
	if record.Status != DeliveryStatusDead {
		t.Fatalf("expected dead status after max attempts, got %q", record.Status)
	}
	if handler.calls != 2 {
		t.Fatalf("expected exactly two handler attempts before dead-letter, got %d", handler.calls)
	}

	now = now.Add(3 * time.Second)
	result, err := processor.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("expected dead delivery to dedupe without processing: %v", err)
	}
	if result.Metadata["deduped"] != true {
		t.Fatalf("expected deduped marker for dead delivery")
	}
	if handler.calls != 2 {
		t.Fatalf("expected no additional handler calls for dead delivery, got %d", handler.calls)
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
	now     func() time.Time
}

func newMemoryDeliveryLedger() *memoryDeliveryLedger {
	return &memoryDeliveryLedger{
		records: map[string]DeliveryRecord{},
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (l *memoryDeliveryLedger) Claim(
	_ context.Context,
	providerID string,
	deliveryID string,
	_ []byte,
	lease time.Duration,
) (DeliveryRecord, bool, error) {
	key := providerID + ":" + deliveryID
	now := l.currentTime()
	if lease <= 0 {
		lease = 30 * time.Second
	}
	record, ok := l.records[key]
	if !ok {
		record = DeliveryRecord{
			ID:         key,
			ProviderID: providerID,
			DeliveryID: deliveryID,
			Status:     DeliveryStatusPending,
			Attempts:   0,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
	}

	switch record.Status {
	case DeliveryStatusProcessed, DeliveryStatusDead:
		l.records[key] = record
		return record, false, nil
	case DeliveryStatusRetryReady, DeliveryStatusProcessing:
		if record.NextAttemptAt != nil && now.Before(record.NextAttemptAt.UTC()) {
			l.records[key] = record
			return record, false, nil
		}
	}

	record.Status = DeliveryStatusProcessing
	record.Attempts++
	record.ClaimID = key + ":" + strconv.Itoa(record.Attempts)
	leaseUntil := now.Add(lease)
	record.NextAttemptAt = &leaseUntil
	record.UpdatedAt = now
	l.records[key] = record
	return record, true, nil
}

func (l *memoryDeliveryLedger) Get(_ context.Context, providerID string, deliveryID string) (DeliveryRecord, error) {
	key := providerID + ":" + deliveryID
	record, ok := l.records[key]
	if !ok {
		return DeliveryRecord{}, errors.New("missing delivery")
	}
	return record, nil
}

func (l *memoryDeliveryLedger) Complete(_ context.Context, claimID string) error {
	key, attempt, err := parseMemoryClaimID(claimID)
	if err != nil {
		return err
	}
	record, ok := l.records[key]
	if !ok {
		return errors.New("missing delivery")
	}
	if record.Status != DeliveryStatusProcessing || record.Attempts != attempt {
		return nil
	}
	record.Status = DeliveryStatusProcessed
	record.NextAttemptAt = nil
	record.UpdatedAt = l.currentTime()
	l.records[key] = record
	return nil
}

func (l *memoryDeliveryLedger) Fail(
	_ context.Context,
	claimID string,
	_ error,
	nextAttemptAt time.Time,
	maxAttempts int,
) error {
	key, attempt, err := parseMemoryClaimID(claimID)
	if err != nil {
		return err
	}
	record, ok := l.records[key]
	if !ok {
		return errors.New("missing delivery")
	}
	if record.Status != DeliveryStatusProcessing || record.Attempts != attempt {
		return nil
	}
	if maxAttempts <= 0 {
		maxAttempts = 8
	}
	if record.Attempts >= maxAttempts {
		record.Status = DeliveryStatusDead
		record.NextAttemptAt = nil
	} else {
		record.Status = DeliveryStatusRetryReady
		if nextAttemptAt.IsZero() {
			nextAttemptAt = l.currentTime()
		}
	}
	record.UpdatedAt = l.currentTime()
	record.NextAttemptAt = &nextAttemptAt
	if record.Status == DeliveryStatusDead {
		record.NextAttemptAt = nil
	}
	l.records[key] = record
	return nil
}

func (l *memoryDeliveryLedger) currentTime() time.Time {
	if l != nil && l.now != nil {
		return l.now().UTC()
	}
	return time.Now().UTC()
}

func parseMemoryClaimID(claimID string) (string, int, error) {
	parts := strings.Split(strings.TrimSpace(claimID), ":")
	if len(parts) < 3 {
		return "", 0, errors.New("invalid claim id")
	}
	attempt, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil || attempt <= 0 {
		return "", 0, errors.New("invalid claim id")
	}
	key := strings.Join(parts[:len(parts)-1], ":")
	return key, attempt, nil
}
