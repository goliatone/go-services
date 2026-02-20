package core

import (
	"context"
	"testing"
	"time"
)

func TestMemoryReplayLedger_FirstClaimAccepted(t *testing.T) {
	ledger := NewMemoryReplayLedger(time.Minute)
	accepted, err := ledger.Claim(context.Background(), "shopify:shop:jti_1", time.Minute)
	if err != nil {
		t.Fatalf("claim first: %v", err)
	}
	if !accepted {
		t.Fatalf("expected first claim to be accepted")
	}
}

func TestMemoryReplayLedger_ReplayRejectedWithinTTL(t *testing.T) {
	ledger := NewMemoryReplayLedger(time.Minute)
	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
	ledger.Now = func() time.Time { return now }

	if accepted, err := ledger.Claim(context.Background(), "shopify:shop:jti_2", time.Minute); err != nil {
		t.Fatalf("claim first: %v", err)
	} else if !accepted {
		t.Fatalf("expected first claim to be accepted")
	}

	if accepted, err := ledger.Claim(context.Background(), "shopify:shop:jti_2", time.Minute); err != nil {
		t.Fatalf("claim replay: %v", err)
	} else if accepted {
		t.Fatalf("expected replay claim to be rejected")
	}
}

func TestMemoryReplayLedger_AcceptsAfterTTLExpiry(t *testing.T) {
	ledger := NewMemoryReplayLedger(time.Minute)
	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
	ledger.Now = func() time.Time { return now }

	if accepted, err := ledger.Claim(context.Background(), "shopify:shop:jti_3", time.Minute); err != nil {
		t.Fatalf("claim first: %v", err)
	} else if !accepted {
		t.Fatalf("expected first claim to be accepted")
	}

	now = now.Add(2 * time.Minute)
	if accepted, err := ledger.Claim(context.Background(), "shopify:shop:jti_3", time.Minute); err != nil {
		t.Fatalf("claim after ttl expiry: %v", err)
	} else if !accepted {
		t.Fatalf("expected claim after ttl expiry to be accepted")
	}
}
