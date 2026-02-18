package shopify

import (
	"context"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestNormalizeAdminAPIResponse_MapsShopifyHeaders(t *testing.T) {
	meta, err := NormalizeAdminAPIResponse(context.Background(), core.TransportResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"X-Shopify-Shop-Api-Call-Limit": "10/40",
			"X-Request-Id":                  "req_1",
			"X-Shopify-Api-Version":         "2025-10",
		},
	})
	if err != nil {
		t.Fatalf("normalize response: %v", err)
	}

	if meta.Headers["X-RateLimit-Limit"] != "40" {
		t.Fatalf("expected limit header 40, got %q", meta.Headers["X-RateLimit-Limit"])
	}
	if meta.Headers["X-RateLimit-Remaining"] != "30" {
		t.Fatalf("expected remaining header 30, got %q", meta.Headers["X-RateLimit-Remaining"])
	}
	if got := meta.Metadata["shopify_request_id"]; got != "req_1" {
		t.Fatalf("expected request id metadata req_1, got %#v", got)
	}
	if got := meta.Metadata["shopify_api_call_remaining"]; got != 30 {
		t.Fatalf("expected remaining metadata 30, got %#v", got)
	}
}

func TestNormalizeAdminAPIResponse_MapsRetryMetadata(t *testing.T) {
	meta, err := NormalizeAdminAPIResponse(context.Background(), core.TransportResponse{
		StatusCode: 429,
		Headers: map[string]string{
			"Retry-After": "4",
		},
	})
	if err != nil {
		t.Fatalf("normalize response: %v", err)
	}
	if meta.RetryAfter == nil || *meta.RetryAfter != 4*time.Second {
		t.Fatalf("expected retry-after 4s, got %#v", meta.RetryAfter)
	}
	if got := meta.Metadata["shopify_retry_after_source"]; got != "header" {
		t.Fatalf("expected retry source header, got %#v", got)
	}

	fallback, err := NormalizeAdminAPIResponse(context.Background(), core.TransportResponse{
		StatusCode: 429,
		Headers:    map[string]string{},
		Body:       []byte(`{"error":"throttled"}`),
	})
	if err != nil {
		t.Fatalf("normalize fallback response: %v", err)
	}
	if fallback.RetryAfter == nil || *fallback.RetryAfter != defaultRetryAfter429 {
		t.Fatalf("expected default retry-after %s, got %#v", defaultRetryAfter429, fallback.RetryAfter)
	}
	if got := fallback.Metadata["shopify_retry_after_source"]; got != "default" {
		t.Fatalf("expected retry source default, got %#v", got)
	}
	if got := fallback.Metadata["shopify_error_type"]; got != "throttle" {
		t.Fatalf("expected error type throttle, got %#v", got)
	}
}
