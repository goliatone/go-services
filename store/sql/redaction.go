package sqlstore

import (
	"strings"
)

const redactedValue = "[REDACTED]"

func RedactMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return map[string]any{}
	}
	return redactMap(metadata)
}

func redactMap(source map[string]any) map[string]any {
	target := make(map[string]any, len(source))
	for key, value := range source {
		if isSensitiveKey(key) {
			target[key] = redactedValue
			continue
		}
		target[key] = redactValue(value)
	}
	return target
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return redactMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = redactValue(typed[i])
		}
		return out
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
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
