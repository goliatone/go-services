package auth

import (
	"fmt"
	"sort"
	"strings"
)

func readString(metadata map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := metadata[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			trimmed := strings.TrimSpace(typed)
			if trimmed != "" {
				return trimmed
			}
		case []byte:
			trimmed := strings.TrimSpace(string(typed))
			if trimmed != "" {
				return trimmed
			}
		case fmt.Stringer:
			trimmed := strings.TrimSpace(typed.String())
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func readStringSlice(metadata map[string]any, keys ...string) []string {
	for _, key := range keys {
		value, ok := metadata[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case []string:
			return normalizeValues(typed)
		case []any:
			values := make([]string, 0, len(typed))
			for _, item := range typed {
				switch v := item.(type) {
				case string:
					values = append(values, v)
				case fmt.Stringer:
					values = append(values, v.String())
				}
			}
			return normalizeValues(values)
		case string:
			parts := strings.Split(typed, ",")
			return normalizeValues(parts)
		}
	}
	return []string{}
}

func normalizeValues(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		lowered := strings.ToLower(trimmed)
		if _, ok := seen[lowered]; ok {
			continue
		}
		seen[lowered] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func cloneMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		out[key] = value
	}
	return out
}
