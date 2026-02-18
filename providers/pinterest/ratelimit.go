package pinterest

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

const defaultRetryAfter429 = 5 * time.Second

func NormalizeAPIResponse(_ context.Context, response core.TransportResponse) (core.ProviderResponseMeta, error) {
	meta := core.ProviderResponseMeta{
		StatusCode: response.StatusCode,
		Headers:    copyStringMap(response.Headers),
		Metadata:   copyAnyMap(response.Metadata),
	}

	if requestID := headerValue(meta.Headers, "x-request-id"); requestID != "" {
		meta.Metadata["pinterest_request_id"] = requestID
	}

	if limit, ok := parseHeaderInt(meta.Headers, "x-ratelimit-limit"); ok {
		meta.Headers["X-RateLimit-Limit"] = strconv.Itoa(limit)
		meta.Metadata["pinterest_rate_limit"] = limit
	}
	if remaining, ok := parseHeaderInt(meta.Headers, "x-ratelimit-remaining"); ok {
		meta.Headers["X-RateLimit-Remaining"] = strconv.Itoa(remaining)
		meta.Metadata["pinterest_rate_remaining"] = remaining
	}
	if resetAt, ok := parseHeaderInt(meta.Headers, "x-ratelimit-reset"); ok {
		meta.Headers["X-RateLimit-Reset"] = strconv.Itoa(resetAt)
		meta.Metadata["pinterest_rate_reset_unix"] = int64(resetAt)
	}

	if retryAfter, ok := parseRetryAfter(meta.Headers); ok {
		meta.RetryAfter = &retryAfter
		meta.Metadata["pinterest_retry_after_source"] = "header"
	}
	if meta.StatusCode == 429 && meta.RetryAfter == nil {
		retryAfter := defaultRetryAfter429
		meta.RetryAfter = &retryAfter
		meta.Metadata["pinterest_retry_after_source"] = "default"
	}
	if meta.RetryAfter != nil {
		meta.Metadata["pinterest_retry_after_seconds"] = int64(meta.RetryAfter.Seconds())
	}

	if errorType := readErrorType(response.Body); errorType != "" {
		meta.Metadata["pinterest_error_type"] = errorType
	}

	return meta, nil
}

func parseHeaderInt(headers map[string]string, key string) (int, bool) {
	raw := strings.TrimSpace(headerValue(headers, key))
	if raw == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func parseRetryAfter(headers map[string]string) (time.Duration, bool) {
	raw := strings.TrimSpace(headerValue(headers, "retry-after"))
	if raw == "" {
		return 0, false
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

func readErrorType(body []byte) string {
	trimmed := strings.TrimSpace(strings.ToLower(string(body)))
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "rate") {
		return "rate_limit"
	}
	if strings.Contains(trimmed, "quota") {
		return "quota"
	}
	return ""
}

func headerValue(headers map[string]string, key string) string {
	if len(headers) == 0 {
		return ""
	}
	for existing, value := range headers {
		if strings.EqualFold(strings.TrimSpace(existing), strings.TrimSpace(key)) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func copyAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
