package providers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/amazon"
	"github.com/goliatone/go-services/providers/github"
	"github.com/goliatone/go-services/providers/google/calendar"
	"github.com/goliatone/go-services/providers/google/docs"
	"github.com/goliatone/go-services/providers/google/drive"
	"github.com/goliatone/go-services/providers/google/gmail"
	"github.com/goliatone/go-services/providers/google/shopping"
	"github.com/goliatone/go-services/providers/meta/facebook"
	"github.com/goliatone/go-services/providers/meta/instagram"
	"github.com/goliatone/go-services/providers/pinterest"
	"github.com/goliatone/go-services/providers/salesforce"
	"github.com/goliatone/go-services/providers/shopify"
	"github.com/goliatone/go-services/providers/tiktok"
	"github.com/goliatone/go-services/providers/workday"
)

type providerFactory struct {
	name    string
	factory func() (core.Provider, error)
}

func TestBuiltInProviders_ExposeOAuth2AndBaselineCapabilities(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "authorization_code" {
			http.Error(w, "unsupported grant type", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access_token",
			"refresh_token": "refresh_token",
			"token_type":    "bearer",
			"expires_in":    3600,
			"scope":         "repo",
		})
	}))
	defer tokenServer.Close()

	factories := []providerFactory{
		{
			name: "github",
			factory: func() (core.Provider, error) {
				return github.New(github.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					TokenURL:     tokenServer.URL,
				})
			},
		},
		{
			name: "gmail",
			factory: func() (core.Provider, error) {
				return gmail.New(gmail.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					TokenURL:     tokenServer.URL,
				})
			},
		},
		{
			name: "drive",
			factory: func() (core.Provider, error) {
				return drive.New(drive.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					TokenURL:     tokenServer.URL,
				})
			},
		},
		{
			name: "docs",
			factory: func() (core.Provider, error) {
				return docs.New(docs.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					TokenURL:     tokenServer.URL,
				})
			},
		},
		{
			name: "calendar",
			factory: func() (core.Provider, error) {
				return calendar.New(calendar.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					TokenURL:     tokenServer.URL,
				})
			},
		},
		{
			name: "shopify",
			factory: func() (core.Provider, error) {
				return shopify.New(shopify.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					AuthURL:      "https://merchant.myshopify.com/admin/oauth/authorize",
					TokenURL:     tokenServer.URL,
				})
			},
		},
		{
			name: "meta_instagram",
			factory: func() (core.Provider, error) {
				return instagram.New(instagram.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					TokenURL:     tokenServer.URL,
				})
			},
		},
		{
			name: "meta_facebook",
			factory: func() (core.Provider, error) {
				return facebook.New(facebook.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					TokenURL:     tokenServer.URL,
				})
			},
		},
		{
			name: "tiktok",
			factory: func() (core.Provider, error) {
				return tiktok.New(tiktok.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					TokenURL:     tokenServer.URL,
				})
			},
		},
		{
			name: "pinterest",
			factory: func() (core.Provider, error) {
				return pinterest.New(pinterest.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					TokenURL:     tokenServer.URL,
				})
			},
		},
		{
			name: "google_shopping",
			factory: func() (core.Provider, error) {
				return shopping.New(shopping.Config{
					ClientID:              "client",
					ClientSecret:          "secret",
					TokenURL:              tokenServer.URL,
					DisableIdentityScopes: true,
				})
			},
		},
		{
			name: "amazon",
			factory: func() (core.Provider, error) {
				return amazon.New(amazon.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					TokenURL:     tokenServer.URL,
					SigV4: amazon.SigV4Config{
						AccessKeyID:     "AKIA_TEST",
						SecretAccessKey: "secret_key",
						Region:          "us-east-1",
						Service:         "execute-api",
					},
				})
			},
		},
	}

	for _, item := range factories {
		t.Run(item.name, func(t *testing.T) {
			provider, err := item.factory()
			if err != nil {
				t.Fatalf("new provider: %v", err)
			}
			if provider.ID() == "" {
				t.Fatalf("expected provider id")
			}
			if provider.AuthKind() != "oauth2_auth_code" {
				t.Fatalf("expected oauth2_auth_code, got %q", provider.AuthKind())
			}
			if len(provider.Capabilities()) == 0 {
				t.Fatalf("expected baseline capabilities")
			}

			begin, err := provider.BeginAuth(context.Background(), core.BeginAuthRequest{
				Scope:       core.ScopeRef{Type: "user", ID: "usr_1"},
				RedirectURI: "https://app.example/callback",
				State:       "state_1",
			})
			if err != nil {
				t.Fatalf("begin auth: %v", err)
			}
			if begin.URL == "" {
				t.Fatalf("expected begin auth url")
			}

			complete, err := provider.CompleteAuth(context.Background(), core.CompleteAuthRequest{
				Scope:    core.ScopeRef{Type: "user", ID: "usr_1"},
				Code:     "code_1",
				State:    begin.State,
				Metadata: map[string]any{"external_account_id": "acct_usr_1"},
			})
			if err != nil {
				t.Fatalf("complete auth: %v", err)
			}
			if complete.Credential.AccessToken == "" {
				t.Fatalf("expected access token")
			}
		})
	}
}

func TestGoogleBuiltInProviders_IdentityScopesDefaultAndOptOut(t *testing.T) {
	type buildResult struct {
		provider core.Provider
		err      error
	}
	factories := []struct {
		name          string
		buildDefault  func() buildResult
		buildOptOut   func() buildResult
		expectedScope string
	}{
		{
			name: "google_docs",
			buildDefault: func() buildResult {
				provider, err := docs.New(docs.Config{ClientID: "client", ClientSecret: "secret"})
				return buildResult{provider: provider, err: err}
			},
			buildOptOut: func() buildResult {
				provider, err := docs.New(docs.Config{
					ClientID:              "client",
					ClientSecret:          "secret",
					DisableIdentityScopes: true,
				})
				return buildResult{provider: provider, err: err}
			},
			expectedScope: "https://www.googleapis.com/auth/documents.readonly",
		},
		{
			name: "google_drive",
			buildDefault: func() buildResult {
				provider, err := drive.New(drive.Config{ClientID: "client", ClientSecret: "secret"})
				return buildResult{provider: provider, err: err}
			},
			buildOptOut: func() buildResult {
				provider, err := drive.New(drive.Config{
					ClientID:              "client",
					ClientSecret:          "secret",
					DisableIdentityScopes: true,
				})
				return buildResult{provider: provider, err: err}
			},
			expectedScope: "https://www.googleapis.com/auth/drive.readonly",
		},
		{
			name: "google_gmail",
			buildDefault: func() buildResult {
				provider, err := gmail.New(gmail.Config{ClientID: "client", ClientSecret: "secret"})
				return buildResult{provider: provider, err: err}
			},
			buildOptOut: func() buildResult {
				provider, err := gmail.New(gmail.Config{
					ClientID:              "client",
					ClientSecret:          "secret",
					DisableIdentityScopes: true,
				})
				return buildResult{provider: provider, err: err}
			},
			expectedScope: "https://www.googleapis.com/auth/gmail.readonly",
		},
		{
			name: "google_calendar",
			buildDefault: func() buildResult {
				provider, err := calendar.New(calendar.Config{ClientID: "client", ClientSecret: "secret"})
				return buildResult{provider: provider, err: err}
			},
			buildOptOut: func() buildResult {
				provider, err := calendar.New(calendar.Config{
					ClientID:              "client",
					ClientSecret:          "secret",
					DisableIdentityScopes: true,
				})
				return buildResult{provider: provider, err: err}
			},
			expectedScope: "https://www.googleapis.com/auth/calendar.readonly",
		},
	}

	for _, item := range factories {
		t.Run(item.name, func(t *testing.T) {
			defaultBuild := item.buildDefault()
			if defaultBuild.err != nil {
				t.Fatalf("new provider (default): %v", defaultBuild.err)
			}
			defaultScopeSet := beginScopeSet(t, defaultBuild.provider)
			for _, identityScope := range []string{"openid", "profile", "email"} {
				if !defaultScopeSet[identityScope] {
					t.Fatalf("expected identity scope %q in default begin auth scope set", identityScope)
				}
			}
			if !defaultScopeSet[item.expectedScope] {
				t.Fatalf("expected baseline scope %q in default begin auth scope set", item.expectedScope)
			}

			optOutBuild := item.buildOptOut()
			if optOutBuild.err != nil {
				t.Fatalf("new provider (opt-out): %v", optOutBuild.err)
			}
			optOutScopeSet := beginScopeSet(t, optOutBuild.provider)
			for _, identityScope := range []string{"openid", "profile", "email"} {
				if optOutScopeSet[identityScope] {
					t.Fatalf("expected identity scope %q to be absent when opt-out is enabled", identityScope)
				}
			}
			if !optOutScopeSet[item.expectedScope] {
				t.Fatalf("expected baseline scope %q in opt-out begin auth scope set", item.expectedScope)
			}
		})
	}
}

func TestBuiltInProviders_ExposeNonOAuthAdvancedAuthModes(t *testing.T) {
	t.Run("salesforce_client_credentials", func(t *testing.T) {
		tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if r.Form.Get("grant_type") != "client_credentials" {
				http.Error(w, "unsupported grant type", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "token_1",
				"token_type":   "bearer",
				"expires_in":   3600,
				"scope":        "api bulk_api",
			})
		}))
		defer tokenServer.Close()

		providerRaw, err := salesforce.New(salesforce.Config{
			ClientID:     "client",
			ClientSecret: "secret",
			TokenURL:     tokenServer.URL,
		})
		if err != nil {
			t.Fatalf("new provider: %v", err)
		}
		provider := providerRaw
		if provider.AuthKind() != core.AuthKindOAuth2ClientCredential {
			t.Fatalf("expected auth kind %q, got %q", core.AuthKindOAuth2ClientCredential, provider.AuthKind())
		}
		complete, err := provider.CompleteAuth(context.Background(), core.CompleteAuthRequest{
			Scope: core.ScopeRef{Type: "org", ID: "org_1"},
		})
		if err != nil {
			t.Fatalf("complete auth: %v", err)
		}
		if complete.Credential.AccessToken == "" {
			t.Fatalf("expected access token")
		}
	})

	t.Run("workday_service_account_jwt", func(t *testing.T) {
		providerRaw, err := workday.New(workday.Config{
			Issuer:     "svc-account@example.iam.gserviceaccount.com",
			Audience:   "https://api.workday.test/token",
			SigningKey: "secret-signing-key",
		})
		if err != nil {
			t.Fatalf("new provider: %v", err)
		}
		provider := providerRaw
		if provider.AuthKind() != core.AuthKindServiceAccountJWT {
			t.Fatalf("expected auth kind %q, got %q", core.AuthKindServiceAccountJWT, provider.AuthKind())
		}
		complete, err := provider.CompleteAuth(context.Background(), core.CompleteAuthRequest{
			Scope: core.ScopeRef{Type: "org", ID: "org_1"},
			Metadata: map[string]any{
				"subject": "tenant-admin@example.com",
			},
		})
		if err != nil {
			t.Fatalf("complete auth: %v", err)
		}
		if strings.Count(complete.Credential.AccessToken, ".") != 2 {
			t.Fatalf("expected jwt-like access token")
		}
	})
}

func beginScopeSet(t *testing.T, provider core.Provider) map[string]bool {
	t.Helper()
	begin, err := provider.BeginAuth(context.Background(), core.BeginAuthRequest{
		Scope: core.ScopeRef{Type: "user", ID: "usr_1"},
		State: "state_1",
	})
	if err != nil {
		t.Fatalf("begin auth: %v", err)
	}
	parsed, err := url.Parse(begin.URL)
	if err != nil {
		t.Fatalf("parse begin auth url: %v", err)
	}
	scopeSet := map[string]bool{}
	for _, scope := range strings.Fields(parsed.Query().Get("scope")) {
		scopeSet[strings.TrimSpace(scope)] = true
	}
	return scopeSet
}
