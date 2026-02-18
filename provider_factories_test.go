package services

import (
	"testing"

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
	"github.com/goliatone/go-services/providers/shopify"
	"github.com/goliatone/go-services/providers/tiktok"
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
		{
			name: "shopify",
			id:   shopify.ProviderID,
			fn: func() (string, error) {
				provider, err := ShopifyProvider(shopify.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					ShopDomain:   "merchant",
				})
				if err != nil {
					return "", err
				}
				return provider.ID(), nil
			},
		},
		{
			name: "meta_instagram",
			id:   instagram.ProviderID,
			fn: func() (string, error) {
				provider, err := InstagramProvider(instagram.Config{ClientID: "client", ClientSecret: "secret"})
				if err != nil {
					return "", err
				}
				return provider.ID(), nil
			},
		},
		{
			name: "meta_facebook",
			id:   facebook.ProviderID,
			fn: func() (string, error) {
				provider, err := FacebookProvider(facebook.Config{ClientID: "client", ClientSecret: "secret"})
				if err != nil {
					return "", err
				}
				return provider.ID(), nil
			},
		},
		{
			name: "tiktok",
			id:   tiktok.ProviderID,
			fn: func() (string, error) {
				provider, err := TikTokProvider(tiktok.Config{ClientID: "client", ClientSecret: "secret"})
				if err != nil {
					return "", err
				}
				return provider.ID(), nil
			},
		},
		{
			name: "pinterest",
			id:   pinterest.ProviderID,
			fn: func() (string, error) {
				provider, err := PinterestProvider(pinterest.Config{ClientID: "client", ClientSecret: "secret"})
				if err != nil {
					return "", err
				}
				return provider.ID(), nil
			},
		},
		{
			name: "google_shopping",
			id:   shopping.ProviderID,
			fn: func() (string, error) {
				provider, err := GoogleShoppingProvider(shopping.Config{ClientID: "client", ClientSecret: "secret"})
				if err != nil {
					return "", err
				}
				return provider.ID(), nil
			},
		},
		{
			name: "amazon",
			id:   amazon.ProviderID,
			fn: func() (string, error) {
				provider, err := AmazonProvider(amazon.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					SigV4: amazon.SigV4Config{
						AccessKeyID:     "AKIA_TEST",
						SecretAccessKey: "secret_key",
						Region:          "us-east-1",
						Service:         "execute-api",
					},
				})
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
