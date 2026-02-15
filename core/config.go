package core

import (
	"fmt"
	"strings"
)

type InheritanceConfig struct {
	EnabledProviders []string `koanf:"enabled_providers" mapstructure:"enabled_providers"`
}

type OAuthConfig struct {
	RequireCallbackRedirect bool `koanf:"require_callback_redirect" mapstructure:"require_callback_redirect"`
}

type Config struct {
	ServiceName string            `koanf:"service_name" mapstructure:"service_name"`
	Inheritance InheritanceConfig `koanf:"inheritance" mapstructure:"inheritance"`
	OAuth       OAuthConfig       `koanf:"oauth" mapstructure:"oauth"`
}

func DefaultConfig() Config {
	return Config{
		ServiceName: "services",
		Inheritance: InheritanceConfig{},
		OAuth:       OAuthConfig{},
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.ServiceName) == "" {
		return fmt.Errorf("core: service_name is required")
	}
	return nil
}
