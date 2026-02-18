package services

import (
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
	"github.com/goliatone/go-services/providers/shopify"
	"github.com/goliatone/go-services/providers/tiktok"
)

func GitHubProvider(cfg github.Config) (core.Provider, error) {
	return github.New(cfg)
}

func GmailProvider(cfg gmail.Config) (core.Provider, error) {
	return gmail.New(cfg)
}

func DriveProvider(cfg drive.Config) (core.Provider, error) {
	return drive.New(cfg)
}

func DocsProvider(cfg docs.Config) (core.Provider, error) {
	return docs.New(cfg)
}

func CalendarProvider(cfg calendar.Config) (core.Provider, error) {
	return calendar.New(cfg)
}

func ShopifyProvider(cfg shopify.Config) (core.Provider, error) {
	return shopify.New(cfg)
}

func InstagramProvider(cfg instagram.Config) (core.Provider, error) {
	return instagram.New(cfg)
}

func FacebookProvider(cfg facebook.Config) (core.Provider, error) {
	return facebook.New(cfg)
}

func TikTokProvider(cfg tiktok.Config) (core.Provider, error) {
	return tiktok.New(cfg)
}

func PinterestProvider(cfg pinterest.Config) (core.Provider, error) {
	return pinterest.New(cfg)
}

func GoogleShoppingProvider(cfg shopping.Config) (core.Provider, error) {
	return shopping.New(cfg)
}

func AmazonProvider(cfg amazon.Config) (core.Provider, error) {
	return amazon.New(cfg)
}
