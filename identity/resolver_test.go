package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestResolver_Resolve_FromIDToken(t *testing.T) {
	resolver := NewResolver(Config{})
	profile, err := resolver.Resolve(
		context.Background(),
		"google_docs",
		core.ActiveCredential{
			AccessToken: "access_1",
			Metadata: map[string]any{
				"id_token": mustJWTToken(map[string]any{
					"iss":            "https://accounts.google.com",
					"sub":            "sub_123",
					"email":          "user@example.com",
					"email_verified": true,
					"name":           "User Name",
					"picture":        "https://example.com/p.png",
					"locale":         "en",
				}),
			},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("resolve profile from id token: %v", err)
	}
	if profile.Subject != "sub_123" {
		t.Fatalf("expected subject sub_123, got %q", profile.Subject)
	}
	if profile.ExternalAccountID() != "https://accounts.google.com|sub_123" {
		t.Fatalf("expected issuer-qualified external account id, got %q", profile.ExternalAccountID())
	}
	if profile.Email != "user@example.com" {
		t.Fatalf("expected email from id token, got %q", profile.Email)
	}
}

func TestResolver_Resolve_FromUserInfoEndpoint(t *testing.T) {
	var authorizationHeader string
	userInfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorizationHeader = strings.TrimSpace(r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sub":            "google_sub_1",
			"email":          "google@example.com",
			"email_verified": true,
			"name":           "Google User",
			"picture":        "https://example.com/avatar.jpg",
		})
	}))
	defer userInfoServer.Close()

	resolver := NewResolver(Config{
		ProviderUserInfo: map[string]ProviderUserInfoConfig{
			"google_docs": {
				URL:    userInfoServer.URL,
				Issuer: "https://accounts.google.com",
			},
		},
	})
	profile, err := resolver.Resolve(
		context.Background(),
		"google_docs",
		core.ActiveCredential{AccessToken: "access_userinfo_1"},
		nil,
	)
	if err != nil {
		t.Fatalf("resolve profile from userinfo: %v", err)
	}
	if authorizationHeader != "Bearer access_userinfo_1" {
		t.Fatalf("expected bearer token authorization header, got %q", authorizationHeader)
	}
	if profile.Subject != "google_sub_1" {
		t.Fatalf("expected subject from userinfo endpoint, got %q", profile.Subject)
	}
	if profile.ExternalAccountID() != "https://accounts.google.com|google_sub_1" {
		t.Fatalf("expected issuer-qualified external account id, got %q", profile.ExternalAccountID())
	}
}

func TestResolver_Resolve_ReturnsNotFoundWhenUnavailable(t *testing.T) {
	resolver := NewResolver(Config{ProviderUserInfo: map[string]ProviderUserInfoConfig{}})
	_, err := resolver.Resolve(
		context.Background(),
		"custom_provider",
		core.ActiveCredential{},
		nil,
	)
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("expected ErrProfileNotFound, got %v", err)
	}
}

func TestResolver_Resolve_UsesConfiguredIDTokenVerifier(t *testing.T) {
	called := false
	resolver := NewResolver(Config{
		IDTokenVerifier: func(
			_ context.Context,
			_ string,
			_ string,
			_ map[string]any,
		) (map[string]any, error) {
			called = true
			return map[string]any{
				"iss": "https://accounts.google.com",
				"sub": "verified_sub_1",
			}, nil
		},
	})
	profile, err := resolver.Resolve(
		context.Background(),
		"google_docs",
		core.ActiveCredential{
			AccessToken: "access_1",
			Metadata: map[string]any{
				"id_token": "ignored.by.verifier",
			},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("resolve profile with id token verifier: %v", err)
	}
	if !called {
		t.Fatalf("expected configured id token verifier to be called")
	}
	if profile.Subject != "verified_sub_1" {
		t.Fatalf("expected verifier-provided subject, got %q", profile.Subject)
	}
}

func mustJWTToken(claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		panic(err)
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	return header + "." + encodedPayload + ".signature"
}
