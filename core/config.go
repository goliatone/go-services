package core

import (
	"fmt"
	"strings"
)

type InheritanceConfig struct {
	EnabledProviders []string `koanf:"enabled_providers" mapstructure:"enabled_providers"`
}

type Config struct {
	ServiceName string            `koanf:"service_name" mapstructure:"service_name"`
	Inheritance InheritanceConfig `koanf:"inheritance" mapstructure:"inheritance"`
}

func DefaultConfig() Config {
	return Config{
		ServiceName: "services",
		Inheritance: InheritanceConfig{},
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.ServiceName) == "" {
		return fmt.Errorf("core: service_name is required")
	}
	return nil
}
