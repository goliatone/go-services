package providers

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestOAuth2Provider_BeginCompleteRefresh(t *testing.T) {
	provider, err := NewOAuth2Provider(OAuth2Config{
		ID:            "github",
		AuthURL:       "https://github.com/login/oauth/authorize",
		TokenURL:      "https://github.com/login/oauth/access_token",
		ClientID:      "client-123",
		ClientSecret:  "secret-456",
		DefaultScopes: []string{"repo", "read:user"},
		TokenTTL:      30 * time.Minute,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	begin, err := provider.BeginAuth(context.Background(), core.BeginAuthRequest{
		Scope:       core.ScopeRef{Type: "user", ID: "usr_1"},
		RedirectURI: "https://app.example/callback",
		State:       "state_1",
	})
	if err != nil {
		t.Fatalf("begin auth: %v", err)
	}
	if begin.State != "state_1" {
		t.Fatalf("expected state_1, got %q", begin.State)
	}

	parsed, err := url.Parse(begin.URL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	query := parsed.Query()
	if query.Get("client_id") != "client-123" {
		t.Fatalf("expected client_id query value")
	}
	if query.Get("state") != "state_1" {
		t.Fatalf("expected state query value")
	}
	if !strings.Contains(query.Get("scope"), "repo") {
		t.Fatalf("expected scope query to include repo")
	}

	complete, err := provider.CompleteAuth(context.Background(), core.CompleteAuthRequest{
		Scope: core.ScopeRef{Type: "user", ID: "usr_1"},
		Code:  "code_123",
		State: begin.State,
	})
	if err != nil {
		t.Fatalf("complete auth: %v", err)
	}
	if complete.ExternalAccountID == "" {
		t.Fatalf("expected external account id")
	}
	if complete.Credential.TokenType != "bearer" {
		t.Fatalf("expected bearer token type")
	}
	if complete.Credential.AccessToken == "" {
		t.Fatalf("expected access token")
	}
	if !complete.Credential.Refreshable {
		t.Fatalf("expected refreshable credential")
	}
	if complete.Credential.ExpiresAt == nil {
		t.Fatalf("expected expires at")
	}

	refreshed, err := provider.Refresh(context.Background(), complete.Credential)
	if err != nil {
		t.Fatalf("refresh credential: %v", err)
	}
	if refreshed.Credential.AccessToken == "" {
		t.Fatalf("expected refreshed access token")
	}
	if refreshed.Credential.ExpiresAt == nil {
		t.Fatalf("expected refreshed expires at")
	}
	if len(refreshed.GrantedGrants) == 0 {
		t.Fatalf("expected granted grants")
	}
}

func TestNewOAuth2Provider_RequiresIDAuthURLAndClientID(t *testing.T) {
	_, err := NewOAuth2Provider(OAuth2Config{})
	if err == nil {
		t.Fatalf("expected validation error")
	}

	_, err = NewOAuth2Provider(OAuth2Config{ID: "github", AuthURL: "https://example.com/auth"})
	if err == nil {
		t.Fatalf("expected missing client id validation error")
	}
}
