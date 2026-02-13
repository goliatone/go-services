package services

import (
	"testing"

	"github.com/goliatone/go-services/providers/github"
	"github.com/goliatone/go-services/providers/google/calendar"
	"github.com/goliatone/go-services/providers/google/docs"
	"github.com/goliatone/go-services/providers/google/drive"
	"github.com/goliatone/go-services/providers/google/gmail"
)

func TestBuiltInProviderFactories(t *testing.T) {
	cases := []struct {
		name string
		id   string
		fn   func() (string, error)
	}{
		{
			name: "github",
			id:   github.ProviderID,
			fn: func() (string, error) {
				provider, err := GitHubProvider(github.Config{ClientID: "client", ClientSecret: "secret"})
				if err != nil {
					return "", err
				}
				return provider.ID(), nil
			},
		},
		{
			name: "gmail",
			id:   gmail.ProviderID,
			fn: func() (string, error) {
				provider, err := GmailProvider(gmail.Config{ClientID: "client", ClientSecret: "secret"})
				if err != nil {
					return "", err
				}
				return provider.ID(), nil
			},
		},
		{
			name: "drive",
			id:   drive.ProviderID,
			fn: func() (string, error) {
				provider, err := DriveProvider(drive.Config{ClientID: "client", ClientSecret: "secret"})
				if err != nil {
					return "", err
				}
				return provider.ID(), nil
			},
		},
		{
			name: "docs",
			id:   docs.ProviderID,
			fn: func() (string, error) {
				provider, err := DocsProvider(docs.Config{ClientID: "client", ClientSecret: "secret"})
				if err != nil {
					return "", err
				}
				return provider.ID(), nil
			},
		},
		{
			name: "calendar",
			id:   calendar.ProviderID,
			fn: func() (string, error) {
				provider, err := CalendarProvider(calendar.Config{ClientID: "client", ClientSecret: "secret"})
				if err != nil {
					return "", err
				}
				return provider.ID(), nil
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := tc.fn()
			if err != nil {
				t.Fatalf("factory error: %v", err)
			}
			if id != tc.id {
				t.Fatalf("expected %q, got %q", tc.id, id)
			}
		})
	}
}
