package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	goerrors "github.com/goliatone/go-errors"
	glog "github.com/goliatone/go-logger/glog"
)

var (
	ErrProviderNotFound       = errors.New("core: provider not found")
	ErrCapabilityNotSupported = errors.New("core: capability not supported")
)

type Service struct {
	config            Config
	logger            Logger
	loggerProvider    LoggerProvider
	errorFactory      ErrorFactory
	errorMapper       ErrorMapper
	persistenceClient any
	repositoryFactory any
	configProvider    ConfigProvider
	optionsResolver   OptionsResolver
	registry          Registry
	connectionStore   ConnectionStore
	credentialStore   CredentialStore
	strictPolicy      InheritancePolicy
	inheritancePolicy InheritancePolicy
}

type ServiceDependencies struct {
	Logger            Logger
	LoggerProvider    LoggerProvider
	ErrorFactory      ErrorFactory
	ErrorMapper       ErrorMapper
	PersistenceClient any
	RepositoryFactory any
	ConfigProvider    ConfigProvider
	OptionsResolver   OptionsResolver
	Registry          Registry
	ConnectionStore   ConnectionStore
	CredentialStore   CredentialStore
	InheritancePolicy InheritancePolicy
}

func NewService(cfg Config, opts ...Option) (*Service, error) {
	builder := defaultServiceBuilder(cfg)
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&builder)
	}

	provider, logger := glog.Resolve("services", builder.loggerProvider, builder.logger)
	logger = glog.Ensure(logger)
	if provider != nil {
		if named := provider.GetLogger("services"); named != nil {
			logger = glog.Ensure(named)
		}
	}

	if builder.errorFactory == nil {
		builder.errorFactory = goerrors.New
	}
	if builder.errorMapper == nil {
		builder.errorMapper = defaultErrorMapper
	}
	if builder.configProvider == nil {
		builder.configProvider = NewCfgxConfigProvider(nil)
	}
	if builder.optionsResolver == nil {
		builder.optionsResolver = GoOptionsResolver{}
	}
	if builder.registry == nil {
		builder.registry = NewProviderRegistry()
	}

	defaults := DefaultConfig()
	loaded, err := builder.configProvider.Load(context.Background(), defaults)
	if err != nil {
		return nil, mapBuildError(builder.errorMapper, err)
	}
	finalConfig, err := builder.optionsResolver.Resolve(defaults, loaded, builder.runtimeConfig)
	if err != nil {
		return nil, mapBuildError(builder.errorMapper, err)
	}

	strict := &StrictIsolationPolicy{ConnectionStore: builder.connectionStore}
	inheritancePolicy := builder.inheritancePolicy
	if inheritancePolicy == nil {
		inheritancePolicy = strict
	}

	return &Service{
		config:            finalConfig,
		logger:            logger,
		loggerProvider:    provider,
		errorFactory:      builder.errorFactory,
		errorMapper:       builder.errorMapper,
		persistenceClient: builder.persistenceClient,
		repositoryFactory: builder.repositoryFactory,
		configProvider:    builder.configProvider,
		optionsResolver:   builder.optionsResolver,
		registry:          builder.registry,
		connectionStore:   builder.connectionStore,
		credentialStore:   builder.credentialStore,
		strictPolicy:      strict,
		inheritancePolicy: inheritancePolicy,
	}, nil
}

func Setup(cfg Config, opts ...Option) (*Service, error) {
	return NewService(cfg, opts...)
}

func mapBuildError(mapper ErrorMapper, err error) error {
	if err == nil {
		return nil
	}
	if mapper == nil {
		return err
	}
	mapped := mapper(err)
	if mapped == nil {
		return err
	}
	return mapped
}

func (s *Service) Config() Config {
	if s == nil {
		return Config{}
	}
	return s.config
}

func (s *Service) Dependencies() ServiceDependencies {
	if s == nil {
		return ServiceDependencies{}
	}
	return ServiceDependencies{
		Logger:            s.logger,
		LoggerProvider:    s.loggerProvider,
		ErrorFactory:      s.errorFactory,
		ErrorMapper:       s.errorMapper,
		PersistenceClient: s.persistenceClient,
		RepositoryFactory: s.repositoryFactory,
		ConfigProvider:    s.configProvider,
		OptionsResolver:   s.optionsResolver,
		Registry:          s.registry,
		ConnectionStore:   s.connectionStore,
		CredentialStore:   s.credentialStore,
		InheritancePolicy: s.inheritancePolicy,
	}
}

func (s *Service) Connect(ctx context.Context, req ConnectRequest) (BeginAuthResponse, error) {
	if err := req.Scope.Validate(); err != nil {
		return BeginAuthResponse{}, s.mapError(err)
	}
	provider, err := s.resolveProvider(req.ProviderID)
	if err != nil {
		return BeginAuthResponse{}, err
	}
	response, err := provider.BeginAuth(ctx, BeginAuthRequest{
		ProviderID:      req.ProviderID,
		Scope:           req.Scope,
		RedirectURI:     req.RedirectURI,
		State:           req.State,
		RequestedGrants: req.RequestedGrants,
		Metadata:        req.Metadata,
	})
	if err != nil {
		return BeginAuthResponse{}, s.mapError(err)
	}
	return response, nil
}

func (s *Service) CompleteCallback(ctx context.Context, req CompleteAuthRequest) (CallbackCompletion, error) {
	if err := req.Scope.Validate(); err != nil {
		return CallbackCompletion{}, s.mapError(err)
	}
	provider, err := s.resolveProvider(req.ProviderID)
	if err != nil {
		return CallbackCompletion{}, err
	}
	result, err := provider.CompleteAuth(ctx, req)
	if err != nil {
		return CallbackCompletion{}, s.mapError(err)
	}

	connection := Connection{
		ProviderID:        req.ProviderID,
		ScopeType:         req.Scope.Type,
		ScopeID:           req.Scope.ID,
		ExternalAccountID: result.ExternalAccountID,
		Status:            ConnectionStatusActive,
	}
	if s.connectionStore != nil {
		connection, err = s.connectionStore.Create(ctx, CreateConnectionInput{
			ProviderID:        req.ProviderID,
			Scope:             req.Scope,
			ExternalAccountID: result.ExternalAccountID,
			Status:            ConnectionStatusActive,
		})
		if err != nil {
			return CallbackCompletion{}, s.mapError(err)
		}
	}

	credential := Credential{
		ConnectionID:    connection.ID,
		TokenType:       result.Credential.TokenType,
		RequestedScopes: append([]string(nil), result.Credential.RequestedScopes...),
		GrantedScopes:   append([]string(nil), result.Credential.GrantedScopes...),
		Status:          CredentialStatusActive,
	}
	if result.Credential.ExpiresAt != nil {
		credential.ExpiresAt = *result.Credential.ExpiresAt
	}
	if result.Credential.RotatesAt != nil {
		credential.RotatesAt = *result.Credential.RotatesAt
	}

	if s.credentialStore != nil {
		credential, err = s.credentialStore.SaveNewVersion(ctx, SaveCredentialInput{
			ConnectionID:    connection.ID,
			TokenType:       result.Credential.TokenType,
			RequestedScopes: append([]string(nil), result.Credential.RequestedScopes...),
			GrantedScopes:   append([]string(nil), result.Credential.GrantedScopes...),
			ExpiresAt:       result.Credential.ExpiresAt,
			Refreshable:     result.Credential.Refreshable,
			RotatesAt:       result.Credential.RotatesAt,
			Status:          CredentialStatusActive,
		})
		if err != nil {
			return CallbackCompletion{}, s.mapError(err)
		}
	}

	return CallbackCompletion{Connection: connection, Credential: credential}, nil
}

func (s *Service) Refresh(ctx context.Context, req RefreshRequest) (RefreshResult, error) {
	provider, err := s.resolveProvider(req.ProviderID)
	if err != nil {
		return RefreshResult{}, err
	}

	activeCred := ActiveCredential{}
	if req.Credential != nil {
		activeCred = *req.Credential
	} else if s.credentialStore != nil {
		stored, loadErr := s.credentialStore.GetActiveByConnection(ctx, req.ConnectionID)
		if loadErr != nil {
			return RefreshResult{}, s.mapError(loadErr)
		}
		activeCred = credentialToActive(stored)
	} else {
		return RefreshResult{}, s.mapError(fmt.Errorf("core: refresh requires credential input or credential store"))
	}

	result, err := provider.Refresh(ctx, activeCred)
	if err != nil {
		return RefreshResult{}, s.mapError(err)
	}

	if s.credentialStore != nil {
		_, saveErr := s.credentialStore.SaveNewVersion(ctx, SaveCredentialInput{
			ConnectionID:    req.ConnectionID,
			TokenType:       result.Credential.TokenType,
			RequestedScopes: append([]string(nil), result.Credential.RequestedScopes...),
			GrantedScopes:   append([]string(nil), result.Credential.GrantedScopes...),
			ExpiresAt:       result.Credential.ExpiresAt,
			Refreshable:     result.Credential.Refreshable,
			RotatesAt:       result.Credential.RotatesAt,
			Status:          CredentialStatusActive,
		})
		if saveErr != nil {
			return RefreshResult{}, s.mapError(saveErr)
		}
	}

	if s.connectionStore != nil {
		if updateErr := s.connectionStore.UpdateStatus(ctx, req.ConnectionID, string(ConnectionStatusActive), ""); updateErr != nil {
			return RefreshResult{}, s.mapError(updateErr)
		}
	}

	return result, nil
}

func (s *Service) Revoke(ctx context.Context, connectionID string, reason string) error {
	if strings.TrimSpace(connectionID) == "" {
		return s.mapError(fmt.Errorf("core: connection id is required"))
	}
	if s.credentialStore != nil {
		if err := s.credentialStore.RevokeActive(ctx, connectionID, reason); err != nil {
			return s.mapError(err)
		}
	}
	if s.connectionStore != nil {
		if err := s.connectionStore.UpdateStatus(ctx, connectionID, string(ConnectionStatusDisconnected), reason); err != nil {
			return s.mapError(err)
		}
	}
	return nil
}

func (s *Service) InvokeCapability(ctx context.Context, req InvokeCapabilityRequest) (CapabilityResult, error) {
	provider, err := s.resolveProvider(req.ProviderID)
	if err != nil {
		return CapabilityResult{}, err
	}
	descriptor, ok := findCapabilityDescriptor(provider.Capabilities(), req.Capability)
	if !ok {
		wrapped := s.errorFactory(
			fmt.Sprintf("capability %q is not supported by provider %q", req.Capability, req.ProviderID),
			goerrors.CategoryOperation,
		).WithTextCode("SERVICE_CAPABILITY_UNSUPPORTED")
		return CapabilityResult{}, wrapped.WithMetadata(map[string]any{"provider_id": req.ProviderID, "capability": req.Capability})
	}

	resolution, err := s.resolveConnection(ctx, req.ProviderID, req.Scope)
	if err != nil {
		return CapabilityResult{}, s.mapError(err)
	}
	if resolution.Outcome == ConnectionResolutionNotFound {
		return CapabilityResult{
			Allowed: false,
			Mode:    CapabilityDeniedBehaviorBlock,
			Reason:  resolution.Reason,
		}, nil
	}

	return CapabilityResult{
		Allowed:    true,
		Mode:       descriptor.DeniedBehavior,
		Connection: resolution.Connection,
		Metadata: map[string]any{
			"resolution": resolution.Outcome,
		},
	}, nil
}

func (s *Service) resolveConnection(ctx context.Context, providerID string, requested ScopeRef) (ConnectionResolution, error) {
	if !allowProviderInheritance(providerID, s.config.Inheritance) {
		return s.strictPolicy.ResolveConnection(ctx, providerID, requested)
	}
	if s.inheritancePolicy == nil {
		return s.strictPolicy.ResolveConnection(ctx, providerID, requested)
	}
	return s.inheritancePolicy.ResolveConnection(ctx, providerID, requested)
}

func (s *Service) resolveProvider(providerID string) (Provider, error) {
	if s == nil || s.registry == nil {
		return nil, s.mapError(fmt.Errorf("core: registry unavailable"))
	}
	providerID = strings.TrimSpace(providerID)
	provider, ok := s.registry.Get(providerID)
	if ok {
		return provider, nil
	}
	wrapped := s.errorFactory(
		fmt.Sprintf("provider %q is not registered", providerID),
		goerrors.CategoryNotFound,
	).WithTextCode("SERVICE_PROVIDER_NOT_FOUND")
	return nil, wrapped.WithMetadata(map[string]any{"provider_id": providerID})
}

func (s *Service) mapError(err error) error {
	if err == nil {
		return nil
	}
	if s == nil || s.errorMapper == nil {
		return err
	}
	mapped := s.errorMapper(err)
	if mapped == nil {
		return err
	}
	return mapped
}

func findCapabilityDescriptor(capabilities []CapabilityDescriptor, capability string) (CapabilityDescriptor, bool) {
	for _, descriptor := range capabilities {
		if strings.EqualFold(strings.TrimSpace(descriptor.Name), strings.TrimSpace(capability)) {
			return descriptor, true
		}
	}
	return CapabilityDescriptor{}, false
}

func credentialToActive(credential Credential) ActiveCredential {
	active := ActiveCredential{
		ConnectionID:    credential.ConnectionID,
		TokenType:       credential.TokenType,
		RequestedScopes: append([]string(nil), credential.RequestedScopes...),
		GrantedScopes:   append([]string(nil), credential.GrantedScopes...),
		Refreshable:     credential.Refreshable,
	}
	if !credential.ExpiresAt.IsZero() {
		expires := credential.ExpiresAt
		active.ExpiresAt = &expires
	}
	if !credential.RotatesAt.IsZero() {
		rotates := credential.RotatesAt
		active.RotatesAt = &rotates
	}
	return active
}
