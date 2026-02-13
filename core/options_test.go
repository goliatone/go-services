package core

import (
	"context"
	"errors"
	"testing"

	goerrors "github.com/goliatone/go-errors"
)

type fixedConfigProvider struct {
	cfg Config
}

func (p *fixedConfigProvider) Load(context.Context, Config) (Config, error) {
	return p.cfg, nil
}

type fixedOptionsResolver struct {
	cfg Config
}

func (r *fixedOptionsResolver) Resolve(Config, Config, Config) (Config, error) {
	return r.cfg, nil
}

func TestNewService_DefaultDependencies(t *testing.T) {
	svc, err := NewService(Config{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	deps := svc.Dependencies()
	if deps.Logger == nil {
		t.Fatalf("expected default logger")
	}
	if deps.LoggerProvider == nil {
		t.Fatalf("expected default logger provider")
	}
	if deps.ErrorFactory == nil {
		t.Fatalf("expected default error factory")
	}
	if deps.ErrorMapper == nil {
		t.Fatalf("expected default error mapper")
	}
	if deps.ConfigProvider == nil {
		t.Fatalf("expected default config provider")
	}
	if deps.OptionsResolver == nil {
		t.Fatalf("expected default options resolver")
	}
	if got := svc.Config().ServiceName; got != "services" {
		t.Fatalf("expected default config service_name=services, got %q", got)
	}
}

func TestNewService_WithXOverrides(t *testing.T) {
	customLogger := stubLogger{}
	customProvider := stubLoggerProvider{logger: customLogger}
	customFactory := func(message string, category ...goerrors.Category) *goerrors.Error {
		return goerrors.New("custom:"+message, category...)
	}
	sentinel := errors.New("sentinel")
	customMapper := func(error) *goerrors.Error {
		return goerrors.Wrap(sentinel, goerrors.CategoryOperation, "mapped")
	}
	persistenceClient := &struct{ Name string }{Name: "persistence"}
	repositoryFactory := &struct{ Name string }{Name: "repo"}
	configProvider := &fixedConfigProvider{cfg: Config{ServiceName: "from-provider"}}
	optionsResolver := &fixedOptionsResolver{cfg: Config{ServiceName: "resolved"}}
	secretProvider := testSecretProvider{}

	svc, err := NewService(Config{ServiceName: "runtime"},
		WithLogger(customLogger),
		WithLoggerProvider(customProvider),
		WithErrorFactory(customFactory),
		WithErrorMapper(customMapper),
		WithSecretProvider(secretProvider),
		WithPersistenceClient(persistenceClient),
		WithRepositoryFactory(repositoryFactory),
		WithConfigProvider(configProvider),
		WithOptionsResolver(optionsResolver),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	deps := svc.Dependencies()
	if deps.Logger != customLogger {
		t.Fatalf("expected custom logger override")
	}
	if deps.LoggerProvider == nil {
		t.Fatalf("expected custom logger provider override")
	}
	if resolved := deps.LoggerProvider.GetLogger("services.override"); resolved != customLogger {
		t.Fatalf("expected logger provider to resolve custom logger")
	}
	if deps.PersistenceClient != persistenceClient {
		t.Fatalf("expected custom persistence client override")
	}
	if deps.RepositoryFactory != repositoryFactory {
		t.Fatalf("expected custom repository factory override")
	}
	if deps.ConfigProvider != configProvider {
		t.Fatalf("expected custom config provider override")
	}
	if deps.OptionsResolver != optionsResolver {
		t.Fatalf("expected custom options resolver override")
	}
	if deps.SecretProvider != secretProvider {
		t.Fatalf("expected custom secret provider override")
	}
	if got := svc.Config().ServiceName; got != "resolved" {
		t.Fatalf("expected options resolver output config, got %q", got)
	}
}

func TestNewService_ConfigLayeringPrecedence(t *testing.T) {
	provider := NewCfgxConfigProvider(mapRawLoader{values: map[string]any{
		"service_name": "from-config",
		"inheritance": map[string]any{
			"enabled_providers": []string{"github", "google"},
		},
	}})

	svc, err := NewService(Config{ServiceName: "from-runtime"}, WithConfigProvider(provider))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	cfg := svc.Config()
	if cfg.ServiceName != "from-runtime" {
		t.Fatalf("expected runtime value to override config/default, got %q", cfg.ServiceName)
	}
	if len(cfg.Inheritance.EnabledProviders) != 2 {
		t.Fatalf("expected config layer values for inheritance, got %#v", cfg.Inheritance.EnabledProviders)
	}
}
