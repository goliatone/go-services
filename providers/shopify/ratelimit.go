package shopify

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

const defaultRetryAfter429 = 2 * time.Second

func NormalizeAdminAPIResponse(_ context.Context, response core.TransportResponse) (core.ProviderResponseMeta, error) {
	meta := core.ProviderResponseMeta{
		StatusCode: response.StatusCode,
		Headers:    copyStringMap(response.Headers),
		Metadata:   copyAnyMap(response.Metadata),
	}

	requestID := headerValue(meta.Headers, "x-request-id")
	if requestID != "" {
		meta.Metadata["shopify_request_id"] = requestID
	}
	apiVersion := headerValue(meta.Headers, "x-shopify-api-version")
	if apiVersion != "" {
		meta.Metadata["shopify_api_version"] = apiVersion
	}

	if used, limit, ok := parseShopifyCallLimit(headerValue(meta.Headers, "x-shopify-shop-api-call-limit")); ok {
		remaining := limit - used
		if remaining < 0 {
			remaining = 0
		}
		meta.Headers["X-RateLimit-Limit"] = strconv.Itoa(limit)
		meta.Headers["X-RateLimit-Remaining"] = strconv.Itoa(remaining)
		meta.Metadata["shopify_api_call_used"] = used
		meta.Metadata["shopify_api_call_limit"] = limit
		meta.Metadata["shopify_api_call_remaining"] = remaining
	}

	if retryAfter, ok := parseRetryAfter(meta.Headers); ok {
		meta.RetryAfter = &retryAfter
		meta.Metadata["shopify_retry_after_source"] = "header"
	}

	if meta.StatusCode == 429 && meta.RetryAfter == nil {
		retryAfter := defaultRetryAfter429
		meta.RetryAfter = &retryAfter
		meta.Metadata["shopify_retry_after_source"] = "default"
	}
	if meta.RetryAfter != nil {
		meta.Metadata["shopify_retry_after_seconds"] = int64(meta.RetryAfter.Seconds())
	}

	if retryReason := strings.TrimSpace(readErrorType(response.Body)); retryReason != "" {
		meta.Metadata["shopify_error_type"] = retryReason
	}

	return meta, nil
}

func parseShopifyCallLimit(value string) (used int, limit int, ok bool) {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 {
		return 0, 0, false
	}
	used, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || used < 0 {
		return 0, 0, false
	}
	limit, err = strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || limit <= 0 {
		return 0, 0, false
	}
	return used, limit, true
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
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	if strings.Contains(strings.ToLower(trimmed), "throttle") {
		return "throttle"
	}
	if strings.Contains(strings.ToLower(trimmed), "rate") {
		return "rate_limit"
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

func AssertAdminAPIResponse(response core.TransportResponse) error {
	meta, err := NormalizeAdminAPIResponse(context.Background(), response)
	if err != nil {
		return err
	}
	if meta.StatusCode <= 0 {
		return fmt.Errorf("providers/shopify: status code is required")
	}
	return nil
}
