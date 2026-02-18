package common

import (
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestResolveOAuth2Config_UsesSharedDefaultsAndFallbackScopes(t *testing.T) {
	oauthCfg, err := ResolveOAuth2Config(
		"meta_instagram",
		AuthConfig{ClientID: "client", ClientSecret: "secret", TokenTTL: 90 * time.Minute},
		[]string{" pages_show_list ", "instagram_basic", "instagram_basic"},
		[]core.CapabilityDescriptor{{Name: "media.read", RequiredGrants: []string{"instagram:instagram_basic"}}},
	)
	if err != nil {
		t.Fatalf("resolve oauth config: %v", err)
	}
	if oauthCfg.ID != "meta_instagram" {
		t.Fatalf("expected provider id meta_instagram, got %q", oauthCfg.ID)
	}
	if oauthCfg.AuthURL != MetaOAuthAuthURL {
		t.Fatalf("expected default auth url %q, got %q", MetaOAuthAuthURL, oauthCfg.AuthURL)
	}
	if oauthCfg.TokenURL != MetaOAuthTokenURL {
		t.Fatalf("expected default token url %q, got %q", MetaOAuthTokenURL, oauthCfg.TokenURL)
	}
	expectedScopes := []string{"instagram_basic", "pages_show_list"}
	if len(oauthCfg.DefaultScopes) != len(expectedScopes) {
		t.Fatalf("expected %d scopes, got %d (%v)", len(expectedScopes), len(oauthCfg.DefaultScopes), oauthCfg.DefaultScopes)
	}
	for idx := range expectedScopes {
		if oauthCfg.DefaultScopes[idx] != expectedScopes[idx] {
			t.Fatalf("expected scope %q at index %d, got %q", expectedScopes[idx], idx, oauthCfg.DefaultScopes[idx])
		}
	}
	if oauthCfg.TokenTTL != 90*time.Minute {
		t.Fatalf("expected token ttl 90m, got %s", oauthCfg.TokenTTL)
	}
	if len(oauthCfg.Capabilities) != 1 {
		t.Fatalf("expected one capability descriptor, got %d", len(oauthCfg.Capabilities))
	}
}

func TestResolveOAuth2Config_UsesExplicitOverrides(t *testing.T) {
	oauthCfg, err := ResolveOAuth2Config(
		"meta_facebook",
		AuthConfig{
			ClientID:            "client",
			ClientSecret:        "secret",
			AuthURL:             "https://example.com/auth",
			TokenURL:            "https://example.com/token",
			DefaultScopes:       []string{"pages_read_engagement", "PAGES_SHOW_LIST", ""},
			SupportedScopeTypes: []string{"org", "user", "org"},
		},
		[]string{"ignored_scope"},
		nil,
	)
	if err != nil {
		t.Fatalf("resolve oauth config: %v", err)
	}
	if oauthCfg.AuthURL != "https://example.com/auth" {
		t.Fatalf("expected override auth url, got %q", oauthCfg.AuthURL)
	}
	if oauthCfg.TokenURL != "https://example.com/token" {
		t.Fatalf("expected override token url, got %q", oauthCfg.TokenURL)
	}
	expectedScopes := []string{"pages_read_engagement", "pages_show_list"}
	if len(oauthCfg.DefaultScopes) != len(expectedScopes) {
		t.Fatalf("expected %d scopes, got %d (%v)", len(expectedScopes), len(oauthCfg.DefaultScopes), oauthCfg.DefaultScopes)
	}
	for idx := range expectedScopes {
		if oauthCfg.DefaultScopes[idx] != expectedScopes[idx] {
			t.Fatalf("expected scope %q at index %d, got %q", expectedScopes[idx], idx, oauthCfg.DefaultScopes[idx])
		}
	}
	expectedScopeTypes := []string{"org", "user"}
	if len(oauthCfg.SupportedScopeTypes) != len(expectedScopeTypes) {
		t.Fatalf(
			"expected %d supported scope types, got %d (%v)",
			len(expectedScopeTypes),
			len(oauthCfg.SupportedScopeTypes),
			oauthCfg.SupportedScopeTypes,
		)
	}
	for idx := range expectedScopeTypes {
		if oauthCfg.SupportedScopeTypes[idx] != expectedScopeTypes[idx] {
			t.Fatalf(
				"expected supported scope type %q at index %d, got %q",
				expectedScopeTypes[idx],
				idx,
				oauthCfg.SupportedScopeTypes[idx],
			)
		}
	}
}

func TestResolveOAuth2Config_RequiresProviderID(t *testing.T) {
	_, err := ResolveOAuth2Config("", AuthConfig{}, nil, nil)
	if err == nil {
		t.Fatalf("expected error when provider id is missing")
	}
}
