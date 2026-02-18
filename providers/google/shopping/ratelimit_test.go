package shopping

import (
	"context"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestNormalizeAPIResponse_MapsGoogleShoppingHeaders(t *testing.T) {
	meta, err := NormalizeAPIResponse(context.Background(), core.TransportResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"X-RateLimit-Limit":     "1200",
			"X-RateLimit-Remaining": "1180",
			"X-RateLimit-Reset":     "1739872800",
			"X-Google-Request-Id":   "goog_req_1",
		},
	})
	if err != nil {
		t.Fatalf("normalize response: %v", err)
	}
	if meta.Headers["X-RateLimit-Limit"] != "1200" {
		t.Fatalf("expected limit header 1200, got %q", meta.Headers["X-RateLimit-Limit"])
	}
	if meta.Headers["X-RateLimit-Remaining"] != "1180" {
		t.Fatalf("expected remaining header 1180, got %q", meta.Headers["X-RateLimit-Remaining"])
	}
	if got := meta.Metadata["google_shopping_request_id"]; got != "goog_req_1" {
		t.Fatalf("expected request id metadata goog_req_1, got %#v", got)
	}
}

func TestNormalizeAPIResponse_MapsRetryMetadata(t *testing.T) {
	meta, err := NormalizeAPIResponse(context.Background(), core.TransportResponse{
		StatusCode: 429,
		Headers: map[string]string{
			"Retry-After": "7",
		},
	})
	if err != nil {
		t.Fatalf("normalize response: %v", err)
	}
	if meta.RetryAfter == nil || *meta.RetryAfter != 7*time.Second {
		t.Fatalf("expected retry-after 7s, got %#v", meta.RetryAfter)
	}
	if got := meta.Metadata["google_shopping_retry_after_source"]; got != "header" {
		t.Fatalf("expected retry source header, got %#v", got)
	}

	fallback, err := NormalizeAPIResponse(context.Background(), core.TransportResponse{
		StatusCode: 429,
		Headers:    map[string]string{},
		Body:       []byte(`{"error":{"status":"RESOURCE_EXHAUSTED","message":"quota exceeded"}}`),
	})
	if err != nil {
		t.Fatalf("normalize fallback response: %v", err)
	}
	if fallback.RetryAfter == nil || *fallback.RetryAfter != defaultRetryAfter429 {
		t.Fatalf("expected default retry-after %s, got %#v", defaultRetryAfter429, fallback.RetryAfter)
	}
	if got := fallback.Metadata["google_shopping_retry_after_source"]; got != "default" {
		t.Fatalf("expected retry source default, got %#v", got)
	}
	if got := fallback.Metadata["google_shopping_error_type"]; got != "quota" {
		t.Fatalf("expected error type quota, got %#v", got)
	}
}
