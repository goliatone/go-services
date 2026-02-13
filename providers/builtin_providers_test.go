package providers_test

import (
	"context"
	"testing"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/github"
	"github.com/goliatone/go-services/providers/google/calendar"
	"github.com/goliatone/go-services/providers/google/docs"
	"github.com/goliatone/go-services/providers/google/drive"
	"github.com/goliatone/go-services/providers/google/gmail"
)

type providerFactory struct {
	name    string
	factory func() (core.Provider, error)
}

func TestBuiltInProviders_ExposeOAuth2AndBaselineCapabilities(t *testing.T) {
	factories := []providerFactory{
		{
			name: "github",
			factory: func() (core.Provider, error) {
				return github.New(github.Config{ClientID: "client", ClientSecret: "secret"})
			},
		},
		{
			name: "gmail",
			factory: func() (core.Provider, error) {
				return gmail.New(gmail.Config{ClientID: "client", ClientSecret: "secret"})
			},
		},
		{
			name: "drive",
			factory: func() (core.Provider, error) {
				return drive.New(drive.Config{ClientID: "client", ClientSecret: "secret"})
			},
		},
		{
			name: "docs",
			factory: func() (core.Provider, error) {
				return docs.New(docs.Config{ClientID: "client", ClientSecret: "secret"})
			},
		},
		{
			name: "calendar",
			factory: func() (core.Provider, error) {
				return calendar.New(calendar.Config{ClientID: "client", ClientSecret: "secret"})
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
				Scope: core.ScopeRef{Type: "user", ID: "usr_1"},
				Code:  "code_1",
				State: begin.State,
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
