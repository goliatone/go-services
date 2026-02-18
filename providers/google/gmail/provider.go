package gmail

import (
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/identity"
	"github.com/goliatone/go-services/providers"
	"github.com/goliatone/go-services/providers/google/common"
)

const (
	ProviderID = "google_gmail"
	AuthURL    = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenURL   = "https://oauth2.googleapis.com/token"
)

type Config struct {
	ClientID              string
	ClientSecret          string
	AuthURL               string
	TokenURL              string
	DefaultScopes         []string
	DisableIdentityScopes bool
	SupportedScopeTypes   []string
	TokenTTL              time.Duration
}

func DefaultConfig() Config {
	return Config{
		AuthURL:  AuthURL,
		TokenURL: TokenURL,
		DefaultScopes: []string{
			"https://www.googleapis.com/auth/gmail.readonly",
			"https://www.googleapis.com/auth/gmail.send",
		},
	}
}

func New(cfg Config) (core.Provider, error) {
	defaults := DefaultConfig()
	if cfg.AuthURL == "" {
		cfg.AuthURL = defaults.AuthURL
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = defaults.TokenURL
	}
	if len(cfg.DefaultScopes) == 0 {
		cfg.DefaultScopes = defaults.DefaultScopes
	}
	cfg.DefaultScopes = common.WithIdentityScopes(cfg.DefaultScopes, !cfg.DisableIdentityScopes)
	return providers.NewOAuth2Provider(providers.OAuth2Config{
		ID:                  ProviderID,
		AuthURL:             cfg.AuthURL,
		TokenURL:            cfg.TokenURL,
		ClientID:            cfg.ClientID,
		ClientSecret:        cfg.ClientSecret,
		DefaultScopes:       cfg.DefaultScopes,
		ProfileResolver:     identity.DefaultResolver(),
		SupportedScopeTypes: cfg.SupportedScopeTypes,
		TokenTTL:            cfg.TokenTTL,
		Capabilities: []core.CapabilityDescriptor{
			{
				Name:           "mail.read",
				RequiredGrants: []string{"https://www.googleapis.com/auth/gmail.readonly"},
				DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
			},
			{
				Name:           "mail.send",
				RequiredGrants: []string{"https://www.googleapis.com/auth/gmail.send"},
				DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
			},
		},
	})
}
