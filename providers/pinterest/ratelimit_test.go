package pinterest

import (
	"context"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestNormalizeAPIResponse_MapsPinterestHeaders(t *testing.T) {
	meta, err := NormalizeAPIResponse(context.Background(), core.TransportResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"X-RateLimit-Limit":     "1000",
			"X-RateLimit-Remaining": "900",
			"X-RateLimit-Reset":     "1739872800",
			"X-Request-Id":          "pin_req_1",
		},
	})
	if err != nil {
		t.Fatalf("normalize response: %v", err)
	}
	if meta.Headers["X-RateLimit-Limit"] != "1000" {
		t.Fatalf("expected limit header 1000, got %q", meta.Headers["X-RateLimit-Limit"])
	}
	if meta.Headers["X-RateLimit-Remaining"] != "900" {
		t.Fatalf("expected remaining header 900, got %q", meta.Headers["X-RateLimit-Remaining"])
	}
	if got := meta.Metadata["pinterest_request_id"]; got != "pin_req_1" {
		t.Fatalf("expected request id metadata pin_req_1, got %#v", got)
	}
}

func TestNormalizeAPIResponse_MapsRetryMetadata(t *testing.T) {
	meta, err := NormalizeAPIResponse(context.Background(), core.TransportResponse{
		StatusCode: 429,
		Headers: map[string]string{
			"Retry-After": "6",
		},
	})
	if err != nil {
		t.Fatalf("normalize response: %v", err)
	}
	if meta.RetryAfter == nil || *meta.RetryAfter != 6*time.Second {
		t.Fatalf("expected retry-after 6s, got %#v", meta.RetryAfter)
	}
	if got := meta.Metadata["pinterest_retry_after_source"]; got != "header" {
		t.Fatalf("expected retry source header, got %#v", got)
	}

	fallback, err := NormalizeAPIResponse(context.Background(), core.TransportResponse{
		StatusCode: 429,
		Headers:    map[string]string{},
		Body:       []byte(`{"error":"quota exceeded"}`),
	})
	if err != nil {
		t.Fatalf("normalize fallback response: %v", err)
	}
	if fallback.RetryAfter == nil || *fallback.RetryAfter != defaultRetryAfter429 {
		t.Fatalf("expected default retry-after %s, got %#v", defaultRetryAfter429, fallback.RetryAfter)
	}
	if got := fallback.Metadata["pinterest_retry_after_source"]; got != "default" {
		t.Fatalf("expected retry source default, got %#v", got)
	}
	if got := fallback.Metadata["pinterest_error_type"]; got != "quota" {
		t.Fatalf("expected error type quota, got %#v", got)
	}
}
