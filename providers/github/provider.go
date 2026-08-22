package github

import (
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/identity"
	"github.com/goliatone/go-services/providers"
	"github.com/goliatone/go-services/providers/tracker"
)

const (
	ProviderID = "github"
	AuthURL    = "https://github.com/login/oauth/authorize"
	TokenURL   = "https://github.com/login/oauth/access_token"
)

type Config struct {
	ClientID            string
	ClientSecret        string
	AuthURL             string
	TokenURL            string
	DefaultScopes       []string
	SupportedScopeTypes []string
	TokenTTL            time.Duration
	TrackerRuntime      tracker.Runtime
}

func DefaultConfig() Config {
	return Config{
		AuthURL:       AuthURL,
		TokenURL:      TokenURL,
		DefaultScopes: []string{"repo", "read:user"},
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
	base, err := providers.NewOAuth2Provider(providers.OAuth2Config{
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
			{Name: "repo.read", RequiredGrants: []string{"repo"}, DeniedBehavior: core.CapabilityDeniedBehaviorBlock},
			{Name: "repo.write", RequiredGrants: []string{"repo"}, DeniedBehavior: core.CapabilityDeniedBehaviorBlock},
			{Name: "issues.read", RequiredGrants: []string{"repo"}, DeniedBehavior: core.CapabilityDeniedBehaviorBlock},
			{Name: "issues.write", RequiredGrants: []string{"repo"}, DeniedBehavior: core.CapabilityDeniedBehaviorBlock},
		},
	})
	if err != nil {
		return nil, err
	}
	return &Provider{Provider: base, runtime: cfg.TrackerRuntime}, nil
}
