package amazon

import (
	"context"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestNormalizeAPIResponse_MapsAmazonHeaders(t *testing.T) {
	meta, err := NormalizeAPIResponse(context.Background(), core.TransportResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"X-Amzn-RequestId":       "request_1",
			"X-Amzn-RateLimit-Limit": "2.0",
			"X-Amzn-RateLimit-Reset": "1739875200",
		},
	})
	if err != nil {
		t.Fatalf("normalize response: %v", err)
	}

	if meta.Headers["X-RateLimit-Limit"] != "2" {
		t.Fatalf("expected X-RateLimit-Limit header 2, got %q", meta.Headers["X-RateLimit-Limit"])
	}
	if got := meta.Metadata["amazon_request_id"]; got != "request_1" {
		t.Fatalf("expected amazon request id metadata request_1, got %#v", got)
	}
	if got := meta.Metadata["amazon_rate_limit"]; got != 2.0 {
		t.Fatalf("expected amazon rate limit metadata 2.0, got %#v", got)
	}
	if got := meta.Metadata["amazon_rate_reset_unix"]; got != int64(1739875200) {
		t.Fatalf("expected rate reset metadata 1739875200, got %#v", got)
	}
}

func TestNormalizeAPIResponse_MapsRetryMetadata(t *testing.T) {
	meta, err := NormalizeAPIResponse(context.Background(), core.TransportResponse{
		StatusCode: 429,
		Headers: map[string]string{
			"Retry-After": "3",
		},
	})
	if err != nil {
		t.Fatalf("normalize response: %v", err)
	}
	if meta.RetryAfter == nil || *meta.RetryAfter != 3*time.Second {
		t.Fatalf("expected retry-after 3s, got %#v", meta.RetryAfter)
	}
	if got := meta.Metadata["amazon_retry_after_source"]; got != "header" {
		t.Fatalf("expected retry source header, got %#v", got)
	}

	fallback, err := NormalizeAPIResponse(context.Background(), core.TransportResponse{
		StatusCode: 503,
		Headers:    map[string]string{},
		Body:       []byte(`{"message":"QuotaExceeded"}`),
	})
	if err != nil {
		t.Fatalf("normalize fallback response: %v", err)
	}
	if fallback.RetryAfter == nil || *fallback.RetryAfter != defaultRetryAfterThrottle {
		t.Fatalf("expected default retry-after %s, got %#v", defaultRetryAfterThrottle, fallback.RetryAfter)
	}
	if got := fallback.Metadata["amazon_retry_after_source"]; got != "default" {
		t.Fatalf("expected retry source default, got %#v", got)
	}
	if got := fallback.Metadata["amazon_error_type"]; got != "quota" {
		t.Fatalf("expected error type quota, got %#v", got)
	}
}
