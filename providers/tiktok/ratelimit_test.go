package tiktok

import (
	"context"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestNormalizeAPIResponse_MapsTikTokHeaders(t *testing.T) {
	meta, err := NormalizeAPIResponse(context.Background(), core.TransportResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"X-RateLimit-Limit":     "600",
			"X-RateLimit-Remaining": "590",
			"X-RateLimit-Reset":     "1739872800",
			"X-Tt-Logid":            "tt_req_1",
			"X-Tt-Trace-Id":         "trace_1",
		},
	})
	if err != nil {
		t.Fatalf("normalize response: %v", err)
	}
	if meta.Headers["X-RateLimit-Limit"] != "600" {
		t.Fatalf("expected limit header 600, got %q", meta.Headers["X-RateLimit-Limit"])
	}
	if meta.Headers["X-RateLimit-Remaining"] != "590" {
		t.Fatalf("expected remaining header 590, got %q", meta.Headers["X-RateLimit-Remaining"])
	}
	if got := meta.Metadata["tiktok_request_id"]; got != "tt_req_1" {
		t.Fatalf("expected request id metadata tt_req_1, got %#v", got)
	}
}

func TestNormalizeAPIResponse_MapsRetryMetadata(t *testing.T) {
	meta, err := NormalizeAPIResponse(context.Background(), core.TransportResponse{
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
	if got := meta.Metadata["tiktok_retry_after_source"]; got != "header" {
		t.Fatalf("expected retry source header, got %#v", got)
	}

	fallback, err := NormalizeAPIResponse(context.Background(), core.TransportResponse{
		StatusCode: 429,
		Headers:    map[string]string{},
		Body:       []byte(`{"error":"rate limit reached"}`),
	})
	if err != nil {
		t.Fatalf("normalize fallback response: %v", err)
	}
	if fallback.RetryAfter == nil || *fallback.RetryAfter != defaultRetryAfter429 {
		t.Fatalf("expected default retry-after %s, got %#v", defaultRetryAfter429, fallback.RetryAfter)
	}
	if got := fallback.Metadata["tiktok_retry_after_source"]; got != "default" {
		t.Fatalf("expected retry source default, got %#v", got)
	}
	if got := fallback.Metadata["tiktok_error_type"]; got != "rate_limit" {
		t.Fatalf("expected error type rate_limit, got %#v", got)
	}
}
