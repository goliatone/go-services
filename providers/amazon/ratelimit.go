package amazon

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

const defaultRetryAfterThrottle = 2 * time.Second

func NormalizeAPIResponse(_ context.Context, response core.TransportResponse) (core.ProviderResponseMeta, error) {
	meta := core.ProviderResponseMeta{
		StatusCode: response.StatusCode,
		Headers:    copyResponseHeaders(response.Headers),
		Metadata:   copyResponseMetadata(response.Metadata),
	}

	if requestID := responseHeader(meta.Headers, "x-amzn-requestid"); requestID != "" {
		meta.Metadata["amazon_request_id"] = requestID
	}
	if requestID := responseHeader(meta.Headers, "x-amz-request-id"); requestID != "" {
		meta.Metadata["amazon_request_id"] = requestID
	}

	if limit, ok := parseHeaderFloat(meta.Headers, "x-amzn-ratelimit-limit"); ok {
		meta.Headers["X-RateLimit-Limit"] = trimFloat(limit)
		meta.Metadata["amazon_rate_limit"] = limit
	}
	if remaining, ok := parseHeaderFloat(meta.Headers, "x-amzn-ratelimit-remaining"); ok {
		meta.Headers["X-RateLimit-Remaining"] = trimFloat(remaining)
		meta.Metadata["amazon_rate_remaining"] = remaining
	}
	if resetAt, ok := parseHeaderInt(meta.Headers, "x-amzn-ratelimit-reset"); ok {
		meta.Headers["X-RateLimit-Reset"] = strconv.FormatInt(int64(resetAt), 10)
		meta.Metadata["amazon_rate_reset_unix"] = int64(resetAt)
	}

	if retryAfter, ok := parseRetryAfter(meta.Headers); ok {
		meta.RetryAfter = &retryAfter
		meta.Metadata["amazon_retry_after_source"] = "header"
	}
	if (meta.StatusCode == 429 || meta.StatusCode == 503) && meta.RetryAfter == nil {
		retryAfter := defaultRetryAfterThrottle
		meta.RetryAfter = &retryAfter
		meta.Metadata["amazon_retry_after_source"] = "default"
	}
	if meta.RetryAfter != nil {
		meta.Metadata["amazon_retry_after_seconds"] = int64(meta.RetryAfter.Seconds())
	}

	if errorType := parseErrorType(response.Body); errorType != "" {
		meta.Metadata["amazon_error_type"] = errorType
	}

	return meta, nil
}

func parseHeaderFloat(headers map[string]string, key string) (float64, bool) {
	raw := strings.TrimSpace(responseHeader(headers, key))
	if raw == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func parseHeaderInt(headers map[string]string, key string) (int, bool) {
	raw := strings.TrimSpace(responseHeader(headers, key))
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
	raw := strings.TrimSpace(responseHeader(headers, "retry-after"))
	if raw == "" {
		return 0, false
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

func parseErrorType(body []byte) string {
	trimmed := strings.TrimSpace(strings.ToLower(string(body)))
	if trimmed == "" {
		return ""
	}
	switch {
	case strings.Contains(trimmed, "quota"):
		return "quota"
	case strings.Contains(trimmed, "too many requests"):
		return "rate_limit"
	case strings.Contains(trimmed, "throttl"):
		return "throttle"
	default:
		return ""
	}
}

func trimFloat(value float64) string {
	formatted := strconv.FormatFloat(value, 'f', -1, 64)
	if strings.Contains(formatted, ".") {
		formatted = strings.TrimRight(formatted, "0")
		formatted = strings.TrimRight(formatted, ".")
	}
	return formatted
}

func responseHeader(headers map[string]string, key string) string {
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

func copyResponseHeaders(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func copyResponseMetadata(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
