package core

import "strings"

const RedactedValue = "[REDACTED]"

func RedactSensitiveMap(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return map[string]any{}
	}
	return redactSensitiveMap(metadata)
}

func redactSensitiveMap(source map[string]any) map[string]any {
	target := make(map[string]any, len(source))
	for key, value := range source {
		if shouldRedactKey(key) {
			target[key] = RedactedValue
			continue
		}
		target[key] = redactSensitiveValue(value)
	}
	return target
}

func redactSensitiveValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return redactSensitiveMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = redactSensitiveValue(typed[i])
		}
		return out
	default:
		return value
	}
}

func shouldRedactKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" || isTraceabilityKey(key) {
		return false
	}
	sensitiveTokens := []string{
		"password",
		"secret",
		"token",
		"authorization",
		"api_key",
		"apikey",
		"access_key",
		"refresh",
		"credential",
		"signature",
	}
	for _, token := range sensitiveTokens {
		if strings.Contains(key, token) {
			return true
		}
	}
	return false
}

func isTraceabilityKey(key string) bool {
	switch key {
	case "provider_id",
		"scope_type",
		"scope_id",
		"connection_id",
		"sync_binding_id",
		"external_id",
		"source_version",
		"idempotency_key",
		"trace_id",
		"request_id":
		return true
	default:
		return false
	}
}
