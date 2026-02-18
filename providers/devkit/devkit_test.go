package devkit

import (
	"context"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/webhooks"
)

func TestFakeTransportAdapter_ScriptsAndCapturesRequests(t *testing.T) {
	adapter := NewFakeTransportAdapter("rest",
		TransportScript{Response: core.TransportResponse{StatusCode: 429}},
		TransportScript{Response: core.TransportResponse{StatusCode: 200}},
	)

	first, err := adapter.Do(context.Background(), core.TransportRequest{
		Method: "GET",
		URL:    "https://api.example.test/items",
	})
	if err != nil {
		t.Fatalf("first fake call: %v", err)
	}
	if first.StatusCode != 429 {
		t.Fatalf("expected first scripted status 429, got %d", first.StatusCode)
	}

	second, err := adapter.Do(context.Background(), core.TransportRequest{
		Method: "GET",
		URL:    "https://api.example.test/items",
	})
	if err != nil {
		t.Fatalf("second fake call: %v", err)
	}
	if second.StatusCode != 200 {
		t.Fatalf("expected second scripted status 200, got %d", second.StatusCode)
	}

	requests := adapter.Requests()
	if len(requests) != 2 {
		t.Fatalf("expected two captured requests, got %d", len(requests))
	}
}

func TestWebhookDeliveryLedgerFixture_ClaimFailAndConformance(t *testing.T) {
	ledger := NewWebhookDeliveryLedgerFixture()
	ctx := context.Background()

	record, accepted, err := ledger.Claim(ctx, "github", "delivery_1", nil, time.Second)
	if err != nil {
		t.Fatalf("claim delivery: %v", err)
	}
	if !accepted {
		t.Fatalf("expected first claim to be accepted")
	}
	if err := ledger.Fail(ctx, record.ClaimID, nil, time.Now().UTC().Add(time.Second), 8); err != nil {
		t.Fatalf("fail delivery: %v", err)
	}
	loaded, err := ledger.Get(ctx, "github", "delivery_1")
	if err != nil {
		t.Fatalf("get delivery: %v", err)
	}
	if loaded.Status != webhooks.DeliveryStatusRetryReady {
		t.Fatalf("expected retry_ready status, got %q", loaded.Status)
	}

	if err := ValidateWebhookLedgerConformance(ctx, NewWebhookDeliveryLedgerFixture(), "github", "delivery_conformance"); err != nil {
		t.Fatalf("validate webhook ledger conformance: %v", err)
	}
}

func TestIdempotencyFixtureAndConformance(t *testing.T) {
	store := NewIdempotencyClaimStoreFixture()
	if err := ValidateIdempotencyClaimStoreConformance(context.Background(), store, "provider:surface:key_1"); err != nil {
		t.Fatalf("validate idempotency claim store conformance: %v", err)
	}
}

func TestValidateTransportAdapterConformance(t *testing.T) {
	adapter := NewFakeTransportAdapter("rest", TransportScript{
		Response: core.TransportResponse{StatusCode: 200},
	})
	if err := ValidateTransportAdapterConformance(context.Background(), adapter, core.TransportRequest{
		Method: "GET",
		URL:    "https://api.example.test/items",
	}); err != nil {
		t.Fatalf("validate transport adapter conformance: %v", err)
	}
}

func TestValidateSigV4SigningAndReplayConformance(t *testing.T) {
	ctx := context.Background()
	fixedNow := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	signer := core.AWSSigV4Signer{
		Now: func() time.Time { return fixedNow },
	}

	headerFixture := NewSigV4HeaderFixture()
	if err := ValidateSigV4SigningConformance(ctx, signer, headerFixture); err != nil {
		t.Fatalf("validate sigv4 header conformance: %v", err)
	}
	if err := ValidateSigV4ReplayWindow(ctx, headerFixture.Request, 5*time.Minute, fixedNow.Add(2*time.Minute)); err != nil {
		t.Fatalf("validate sigv4 header replay window: %v", err)
	}

	queryFixture := NewSigV4QueryFixture()
	if err := ValidateSigV4SigningConformance(ctx, signer, queryFixture); err != nil {
		t.Fatalf("validate sigv4 query conformance: %v", err)
	}
	if err := ValidateSigV4ReplayWindow(ctx, queryFixture.Request, 5*time.Minute, fixedNow.Add(7*time.Minute)); err == nil {
		t.Fatalf("expected replay window validation to fail for stale query signature")
	}
}

func TestValidateWebhookTemplateConformance(t *testing.T) {
	ctx := context.Background()
	for _, fixture := range NewWebhookTemplateFixtures() {
		if err := ValidateWebhookTemplateConformance(ctx, fixture); err != nil {
			t.Fatalf("validate webhook template %s: %v", fixture.Name, err)
		}
	}
}
