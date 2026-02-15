package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/goliatone/go-config/cfgx"
	goerrors "github.com/goliatone/go-errors"
	glog "github.com/goliatone/go-logger/glog"
	opts "github.com/goliatone/go-options"
)

type ErrorFactory func(message string, category ...goerrors.Category) *goerrors.Error

type ErrorMapper func(err error) *goerrors.Error

type ConfigProvider interface {
	Load(ctx context.Context, defaults Config) (Config, error)
}

type RawConfigLoader interface {
	LoadRaw(ctx context.Context) (map[string]any, error)
}

type OptionsResolver interface {
	Resolve(defaults Config, loaded Config, runtime Config) (Config, error)
}

type serviceBuilder struct {
	runtimeConfig       Config
	logger              Logger
	loggerProvider      LoggerProvider
	metricsRecorder     MetricsRecorder
	errorFactory        ErrorFactory
	errorMapper         ErrorMapper
	secretProvider      SecretProvider
	persistenceClient   any
	repositoryFactory   any
	configProvider      ConfigProvider
	optionsResolver     OptionsResolver
	oauthStateStore     OAuthStateStore
	connectionLocker    ConnectionLocker
	refreshScheduler    RefreshBackoffScheduler
	signer              Signer
	transportResolver   TransportResolver
	rateLimitPolicy     RateLimitPolicy
	inheritancePolicy   InheritancePolicy
	registry            Registry
	connectionStore     ConnectionStore
	credentialStore     CredentialStore
	subscriptionStore   SubscriptionStore
	syncCursorStore     SyncCursorStore
	installationStore   InstallationStore
	grantStore          GrantStore
	permissionEvaluator PermissionEvaluator
	credentialCodec     CredentialCodec
}

type Option func(*serviceBuilder)

func WithLogger(logger Logger) Option {
	return func(b *serviceBuilder) {
		b.logger = logger
	}
}

func WithLoggerProvider(provider LoggerProvider) Option {
	return func(b *serviceBuilder) {
		b.loggerProvider = provider
	}
}

func WithMetricsRecorder(recorder MetricsRecorder) Option {
	return func(b *serviceBuilder) {
		b.metricsRecorder = recorder
	}
}

func WithErrorFactory(factory ErrorFactory) Option {
	return func(b *serviceBuilder) {
		b.errorFactory = factory
	}
}

func WithErrorMapper(mapper ErrorMapper) Option {
	return func(b *serviceBuilder) {
		b.errorMapper = mapper
	}
}

func WithSecretProvider(provider SecretProvider) Option {
	return func(b *serviceBuilder) {
		b.secretProvider = provider
	}
}

func WithPersistenceClient(client any) Option {
	return func(b *serviceBuilder) {
		b.persistenceClient = client
	}
}

func WithRepositoryFactory(factory any) Option {
	return func(b *serviceBuilder) {
		b.repositoryFactory = factory
	}
}

func WithConfigProvider(provider ConfigProvider) Option {
	return func(b *serviceBuilder) {
		b.configProvider = provider
	}
}

func WithOptionsResolver(resolver OptionsResolver) Option {
	return func(b *serviceBuilder) {
		b.optionsResolver = resolver
	}
}

func WithOAuthStateStore(store OAuthStateStore) Option {
	return func(b *serviceBuilder) {
		b.oauthStateStore = store
	}
}

func WithConnectionLocker(locker ConnectionLocker) Option {
	return func(b *serviceBuilder) {
		b.connectionLocker = locker
	}
}

func WithRefreshBackoffScheduler(scheduler RefreshBackoffScheduler) Option {
	return func(b *serviceBuilder) {
		b.refreshScheduler = scheduler
	}
}

func WithSigner(signer Signer) Option {
	return func(b *serviceBuilder) {
		b.signer = signer
	}
}

func WithTransportResolver(resolver TransportResolver) Option {
	return func(b *serviceBuilder) {
		b.transportResolver = resolver
	}
}

func WithRateLimitPolicy(policy RateLimitPolicy) Option {
	return func(b *serviceBuilder) {
		b.rateLimitPolicy = policy
	}
}

func WithInheritancePolicy(policy InheritancePolicy) Option {
	return func(b *serviceBuilder) {
		b.inheritancePolicy = policy
	}
}

func WithRegistry(registry Registry) Option {
	return func(b *serviceBuilder) {
		b.registry = registry
	}
}

func WithConnectionStore(store ConnectionStore) Option {
	return func(b *serviceBuilder) {
		b.connectionStore = store
	}
}

func WithCredentialStore(store CredentialStore) Option {
	return func(b *serviceBuilder) {
		b.credentialStore = store
	}
}

func WithSubscriptionStore(store SubscriptionStore) Option {
	return func(b *serviceBuilder) {
		b.subscriptionStore = store
	}
}

func WithSyncCursorStore(store SyncCursorStore) Option {
	return func(b *serviceBuilder) {
		b.syncCursorStore = store
	}
}

func WithInstallationStore(store InstallationStore) Option {
	return func(b *serviceBuilder) {
		b.installationStore = store
	}
}

func WithGrantStore(store GrantStore) Option {
	return func(b *serviceBuilder) {
		b.grantStore = store
	}
}

func WithPermissionEvaluator(evaluator PermissionEvaluator) Option {
	return func(b *serviceBuilder) {
		b.permissionEvaluator = evaluator
	}
}

func WithCredentialCodec(codec CredentialCodec) Option {
	return func(b *serviceBuilder) {
		b.credentialCodec = codec
	}
}

func defaultServiceBuilder(runtime Config) serviceBuilder {
	loggerProvider, logger := glog.Resolve("services", nil, nil)
	return serviceBuilder{
		runtimeConfig:   runtime,
		loggerProvider:  loggerProvider,
		logger:          logger,
		metricsRecorder: NopMetricsRecorder{},
		errorFactory:    goerrors.New,
		errorMapper:     defaultErrorMapper,
		configProvider:  NewCfgxConfigProvider(nil),
		optionsResolver: GoOptionsResolver{},
		registry:        NewProviderRegistry(),
		credentialCodec: JSONCredentialCodec{},
	}
}

func defaultErrorMapper(err error) *goerrors.Error {
	if err == nil {
		return nil
	}
	return serviceErrorMapper(err)
}

type staticRawConfigLoader struct {
	Values map[string]any
}

func (l staticRawConfigLoader) LoadRaw(context.Context) (map[string]any, error) {
	if len(l.Values) == 0 {
		return map[string]any{}, nil
	}
	out := make(map[string]any, len(l.Values))
	for key, value := range l.Values {
		out[key] = value
	}
	return out, nil
}

type CfgxConfigProvider struct {
	Loader RawConfigLoader
}

func NewCfgxConfigProvider(loader RawConfigLoader) *CfgxConfigProvider {
	return &CfgxConfigProvider{Loader: loader}
}

func (p *CfgxConfigProvider) Load(ctx context.Context, defaults Config) (Config, error) {
	if p == nil {
		return defaults, nil
	}
	loader := p.Loader
	if loader == nil {
		loader = staticRawConfigLoader{}
	}
	raw, err := loader.LoadRaw(ctx)
	if err != nil {
		return Config{}, err
	}
	cfg, err := cfgx.Build[Config](raw,
		cfgx.WithDefaults(defaults),
		cfgx.WithValidator[Config]((*Config).Validate),
	)
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

type GoOptionsResolver struct{}

func (GoOptionsResolver) Resolve(defaults Config, loaded Config, runtime Config) (Config, error) {
	defaultLayer := configToLayerMap(defaults, true)
	loadedLayer := configToLayerMap(loaded, false)
	runtimeLayer := configToLayerMap(runtime, false)

	stack, err := opts.NewStack(
		opts.NewLayer(
			opts.NewScope("defaults", 0),
			defaultLayer,
			opts.WithSnapshotID[map[string]any]("defaults"),
		),
		opts.NewLayer(
			opts.NewScope("config", 10),
			loadedLayer,
			opts.WithSnapshotID[map[string]any]("config"),
		),
		opts.NewLayer(
			opts.NewScope("runtime", 20),
			runtimeLayer,
			opts.WithSnapshotID[map[string]any]("runtime"),
		),
	)
	if err != nil {
		return Config{}, fmt.Errorf("core: options stack build failed: %w", err)
	}
	merged, err := stack.Merge()
	if err != nil {
		return Config{}, fmt.Errorf("core: options merge failed: %w", err)
	}
	resolved, err := cfgx.Build[Config](merged.Value,
		cfgx.WithDefaults(defaults),
		cfgx.WithValidator[Config]((*Config).Validate),
	)
	if err != nil {
		return Config{}, err
	}
	if err := resolved.Validate(); err != nil {
		return Config{}, err
	}
	return resolved, nil
}

func configToLayerMap(cfg Config, includeZero bool) map[string]any {
	layer := map[string]any{}
	if includeZero || strings.TrimSpace(cfg.ServiceName) != "" {
		layer["service_name"] = cfg.ServiceName
	}

	if includeZero || len(cfg.Inheritance.EnabledProviders) > 0 {
		layer["inheritance"] = map[string]any{
			"enabled_providers": append([]string(nil), cfg.Inheritance.EnabledProviders...),
		}
	}
	if includeZero || cfg.OAuth.RequireCallbackRedirect {
		layer["oauth"] = map[string]any{
			"require_callback_redirect": cfg.OAuth.RequireCallbackRedirect,
		}
	}
	return layer
}
