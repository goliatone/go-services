package common

import "strings"

const (
	ScopeOpenID  = "openid"
	ScopeEmail   = "email"
	ScopeProfile = "profile"
)

func WithIdentityScopes(scopes []string, include bool) []string {
	normalized := normalizeScopes(scopes)
	if !include {
		return normalized
	}
	return normalizeScopes(append(normalized, ScopeOpenID, ScopeProfile, ScopeEmail))
}

func normalizeScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		trimmed := strings.TrimSpace(scope)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
