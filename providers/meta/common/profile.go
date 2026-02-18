package common

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers"
)

const (
	MetaOAuthAuthURL  = "https://www.facebook.com/v23.0/dialog/oauth"
	MetaOAuthTokenURL = "https://graph.facebook.com/v23.0/oauth/access_token"
)

type AuthConfig struct {
	ClientID            string
	ClientSecret        string
	AuthURL             string
	TokenURL            string
	DefaultScopes       []string
	SupportedScopeTypes []string
	TokenTTL            time.Duration
}

func ResolveOAuth2Config(
	providerID string,
	cfg AuthConfig,
	fallbackScopes []string,
	capabilities []core.CapabilityDescriptor,
) (providers.OAuth2Config, error) {
	providerID = strings.TrimSpace(strings.ToLower(providerID))
	if providerID == "" {
		return providers.OAuth2Config{}, fmt.Errorf("providers/meta/common: provider id is required")
	}

	authURL := strings.TrimSpace(cfg.AuthURL)
	if authURL == "" {
		authURL = MetaOAuthAuthURL
	}
	tokenURL := strings.TrimSpace(cfg.TokenURL)
	if tokenURL == "" {
		tokenURL = MetaOAuthTokenURL
	}
	scopes := NormalizeScopes(cfg.DefaultScopes)
	if len(scopes) == 0 {
		scopes = NormalizeScopes(fallbackScopes)
	}

	return providers.OAuth2Config{
		ID:                  providerID,
		AuthURL:             authURL,
		TokenURL:            tokenURL,
		ClientID:            strings.TrimSpace(cfg.ClientID),
		ClientSecret:        strings.TrimSpace(cfg.ClientSecret),
		DefaultScopes:       scopes,
		SupportedScopeTypes: normalizeScopeTypes(cfg.SupportedScopeTypes),
		TokenTTL:            cfg.TokenTTL,
		Capabilities:        copyCapabilities(capabilities),
	}, nil
}

func NormalizeScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		normalized := strings.TrimSpace(strings.ToLower(scope))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func normalizeScopeTypes(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.TrimSpace(strings.ToLower(value))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func copyCapabilities(capabilities []core.CapabilityDescriptor) []core.CapabilityDescriptor {
	if len(capabilities) == 0 {
		return []core.CapabilityDescriptor{}
	}
	out := make([]core.CapabilityDescriptor, 0, len(capabilities))
	for _, capability := range capabilities {
		cloned := capability
		cloned.RequiredGrants = append([]string(nil), capability.RequiredGrants...)
		cloned.OptionalGrants = append([]string(nil), capability.OptionalGrants...)
		out = append(out, cloned)
	}
	return out
}
