package ratelimit

import (
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestThrottledError_ToServiceError(t *testing.T) {
	err := ThrottledError{
		ProviderID: "shopify",
		BucketKey:  "orders",
		RetryAfter: 3 * time.Second,
	}

	mapped := err.ToServiceError()
	if mapped == nil {
		t.Fatalf("expected mapped error")
	}
	if mapped.TextCode != core.ServiceErrorRateLimited {
		t.Fatalf("expected %q text code, got %q", core.ServiceErrorRateLimited, mapped.TextCode)
	}
	if mapped.Code != 429 {
		t.Fatalf("expected status code 429, got %d", mapped.Code)
	}
}
