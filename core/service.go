package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
	oauthStateStore   OAuthStateStore
	connectionLocker  ConnectionLocker
	refreshBackoffScheduler RefreshBackoffScheduler
	signer            Signer
	registry          Registry
	connectionStore   ConnectionStore
	credentialStore   CredentialStore
	grantStore        GrantStore
	permissionEvaluator PermissionEvaluator
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
	OAuthStateStore   OAuthStateStore
	ConnectionLocker  ConnectionLocker
	RefreshScheduler  RefreshBackoffScheduler
	Signer            Signer
	Registry          Registry
	ConnectionStore   ConnectionStore
	CredentialStore   CredentialStore
	GrantStore        GrantStore
	PermissionEvaluator PermissionEvaluator
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
	if builder.oauthStateStore == nil {
		builder.oauthStateStore = NewMemoryOAuthStateStore(defaultOAuthStateTTL)
	}
	if builder.connectionLocker == nil {
		builder.connectionLocker = NewMemoryConnectionLocker()
	}
	if builder.refreshScheduler == nil {
		builder.refreshScheduler = ExponentialBackoffScheduler{
			Initial: defaultRefreshInitialBackoff,
			Max:     defaultRefreshMaxBackoff,
		}
	}
	if builder.signer == nil {
		builder.signer = BearerTokenSigner{}
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

	if (builder.connectionStore == nil || builder.credentialStore == nil) && builder.repositoryFactory != nil {
		if storeFactory, ok := builder.repositoryFactory.(RepositoryStoreFactory); ok {
			provider, buildErr := storeFactory.BuildStores(builder.persistenceClient)
			if buildErr != nil {
				return nil, mapBuildError(builder.errorMapper, buildErr)
			}
			if provider != nil {
				if builder.connectionStore == nil {
					builder.connectionStore = provider.ConnectionStore()
				}
				if builder.credentialStore == nil {
					builder.credentialStore = provider.CredentialStore()
				}
			}
		} else if provider, ok := builder.repositoryFactory.(StoreProvider); ok {
			if builder.connectionStore == nil {
				builder.connectionStore = provider.ConnectionStore()
			}
			if builder.credentialStore == nil {
				builder.credentialStore = provider.CredentialStore()
			}
		}
	}
	if builder.permissionEvaluator == nil {
		builder.permissionEvaluator = NewGrantPermissionEvaluator(
			builder.connectionStore,
			builder.grantStore,
			builder.registry,
		)
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
		oauthStateStore:   builder.oauthStateStore,
		connectionLocker:  builder.connectionLocker,
		refreshBackoffScheduler: builder.refreshScheduler,
		signer:            builder.signer,
		registry:          builder.registry,
		connectionStore:   builder.connectionStore,
		credentialStore:   builder.credentialStore,
		grantStore:        builder.grantStore,
		permissionEvaluator: builder.permissionEvaluator,
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
		OAuthStateStore:   s.oauthStateStore,
		ConnectionLocker:  s.connectionLocker,
		RefreshScheduler:  s.refreshBackoffScheduler,
		Signer:            s.signer,
		Registry:          s.registry,
		ConnectionStore:   s.connectionStore,
		CredentialStore:   s.credentialStore,
		GrantStore:        s.grantStore,
		PermissionEvaluator: s.permissionEvaluator,
		InheritancePolicy: s.inheritancePolicy,
	}
}

func (s *Service) Connect(ctx context.Context, req ConnectRequest) (BeginAuthResponse, error) {
	if err := req.Scope.Validate(); err != nil {
		return BeginAuthResponse{}, s.mapError(err)
	}
	state := strings.TrimSpace(req.State)
	if state == "" {
		generated, generateErr := generateOAuthState()
		if generateErr != nil {
			return BeginAuthResponse{}, s.mapError(generateErr)
		}
		state = generated
	}

	provider, err := s.resolveProvider(req.ProviderID)
	if err != nil {
		return BeginAuthResponse{}, err
	}
	response, err := provider.BeginAuth(ctx, BeginAuthRequest{
		ProviderID:      req.ProviderID,
		Scope:           req.Scope,
		RedirectURI:     req.RedirectURI,
		State:           state,
		RequestedGrants: req.RequestedGrants,
		Metadata:        req.Metadata,
	})
	if err != nil {
		return BeginAuthResponse{}, s.mapError(err)
	}
	if strings.TrimSpace(response.State) == "" {
		response.State = state
	}

	if s.oauthStateStore != nil {
		saveErr := s.oauthStateStore.Save(ctx, OAuthStateRecord{
			State:           response.State,
			ProviderID:      req.ProviderID,
			Scope:           req.Scope,
			RedirectURI:     req.RedirectURI,
			RequestedGrants: append([]string(nil), req.RequestedGrants...),
			Metadata:        copyAnyMap(req.Metadata),
			CreatedAt:       time.Now().UTC(),
		})
		if saveErr != nil {
			return BeginAuthResponse{}, s.mapError(saveErr)
		}
	}

	return response, nil
}

func (s *Service) StartReconsent(ctx context.Context, req ReconsentRequest) (BeginAuthResponse, error) {
	if s == nil || s.connectionStore == nil {
		return BeginAuthResponse{}, s.mapError(fmt.Errorf("core: connection store is required for re-consent"))
	}
	connectionID := strings.TrimSpace(req.ConnectionID)
	if connectionID == "" {
		return BeginAuthResponse{}, s.mapError(fmt.Errorf("core: connection id is required for re-consent"))
	}

	connection, err := s.connectionStore.Get(ctx, connectionID)
	if err != nil {
		return BeginAuthResponse{}, s.mapError(err)
	}

	if updateErr := s.connectionStore.UpdateStatus(
		ctx,
		connectionID,
		string(ConnectionStatusNeedsReconsent),
		"re-consent requested",
	); updateErr != nil {
		return BeginAuthResponse{}, s.mapError(updateErr)
	}

	requested := append([]string(nil), req.RequestedGrants...)
	if len(requested) == 0 && s.grantStore != nil {
		if snapshot, snapshotErr := s.grantStore.GetLatestSnapshot(ctx, connectionID); snapshotErr == nil {
			requested = append([]string(nil), snapshot.Requested...)
		}
	}

	if s.grantStore != nil {
		_ = s.grantStore.AppendEvent(ctx, AppendGrantEventInput{
			ConnectionID: connectionID,
			EventType:    GrantEventReconsentRequested,
			Added:        normalizeGrants(requested),
			Removed:      []string{},
			OccurredAt:   time.Now().UTC(),
			Metadata:     copyAnyMap(req.Metadata),
		})
	}

	return s.Connect(ctx, ConnectRequest{
		ProviderID:      connection.ProviderID,
		Scope:           ScopeRef{Type: connection.ScopeType, ID: connection.ScopeID},
		RedirectURI:     req.RedirectURI,
		State:           req.State,
		RequestedGrants: requested,
		Metadata:        copyAnyMap(req.Metadata),
	})
}

func (s *Service) CompleteReconsent(ctx context.Context, req CompleteAuthRequest) (CallbackCompletion, error) {
	return s.CompleteCallback(ctx, req)
}

func (s *Service) CompleteCallback(ctx context.Context, req CompleteAuthRequest) (CallbackCompletion, error) {
	if err := req.Scope.Validate(); err != nil {
		return CallbackCompletion{}, s.mapError(err)
	}
	if err := s.validateOAuthCallbackState(ctx, req); err != nil {
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
	wasNeedsReconsent := false
	if s.connectionStore != nil {
		existing, found, findErr := s.findScopedConnection(ctx, req.ProviderID, req.Scope)
		if findErr != nil {
			return CallbackCompletion{}, s.mapError(findErr)
		}
		if found {
			connection = existing
			wasNeedsReconsent = existing.Status == ConnectionStatusNeedsReconsent
			if updateErr := s.connectionStore.UpdateStatus(
				ctx,
				connection.ID,
				string(ConnectionStatusActive),
				"",
			); updateErr != nil {
				return CallbackCompletion{}, s.mapError(updateErr)
			}
			connection.Status = ConnectionStatusActive
			connection.LastError = ""
		} else {
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

	_, delta, grantErr := s.reconcileGrantSnapshot(
		ctx,
		provider,
		connection.ID,
		resolveRequestedGrants(result),
		resolveGrantedGrants(result),
		req.Metadata,
	)
	if grantErr != nil {
		return CallbackCompletion{}, s.mapError(grantErr)
	}
	if wasNeedsReconsent && s.grantStore != nil {
		_ = s.grantStore.AppendEvent(ctx, AppendGrantEventInput{
			ConnectionID: connection.ID,
			EventType:    GrantEventReconsentCompleted,
			Added:        append([]string(nil), delta.Added...),
			Removed:      append([]string(nil), delta.Removed...),
			OccurredAt:   time.Now().UTC(),
			Metadata:     copyAnyMap(req.Metadata),
		})
	}

	return CallbackCompletion{Connection: connection, Credential: credential}, nil
}

func (s *Service) validateOAuthCallbackState(ctx context.Context, req CompleteAuthRequest) error {
	if s == nil || s.oauthStateStore == nil {
		return nil
	}
	state := strings.TrimSpace(req.State)
	if state == "" {
		return fmt.Errorf("core: oauth callback state is required")
	}

	record, err := s.oauthStateStore.Consume(ctx, state)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(record.ProviderID), strings.TrimSpace(req.ProviderID)) {
		return fmt.Errorf("core: oauth callback state provider mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(record.Scope.Type), strings.TrimSpace(req.Scope.Type)) ||
		strings.TrimSpace(record.Scope.ID) != strings.TrimSpace(req.Scope.ID) {
		return fmt.Errorf("core: oauth callback state scope mismatch")
	}

	savedRedirect := strings.TrimSpace(record.RedirectURI)
	requestRedirect := strings.TrimSpace(req.RedirectURI)
	if savedRedirect != "" && requestRedirect != "" && savedRedirect != requestRedirect {
		return fmt.Errorf("core: oauth callback state redirect mismatch")
	}
	return nil
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

	snapshot, _, grantErr := s.reconcileGrantSnapshot(
		ctx,
		provider,
		req.ConnectionID,
		result.Credential.RequestedScopes,
		resolveRefreshGrantedGrants(result),
		result.Metadata,
	)
	if grantErr != nil {
		return RefreshResult{}, s.mapError(grantErr)
	}
	if len(missingRequiredProviderGrants(provider.Capabilities(), snapshot.Granted)) > 0 {
		if transitionErr := s.transitionConnectionToNeedsReconsent(
			ctx,
			req.ConnectionID,
			"required grants missing after refresh",
		); transitionErr != nil {
			return RefreshResult{}, s.mapError(transitionErr)
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

	decision := PermissionDecision{
		Allowed:    true,
		Capability: req.Capability,
		Mode:       descriptor.DeniedBehavior,
	}
	if s.permissionEvaluator != nil {
		decision, err = s.permissionEvaluator.EvaluateCapability(ctx, resolution.Connection.ID, req.Capability)
		if err != nil {
			return CapabilityResult{}, s.mapError(err)
		}
		if decision.Mode == "" {
			decision.Mode = descriptor.DeniedBehavior
		}
	}

	metadata := map[string]any{
		"resolution": resolution.Outcome,
	}
	if len(decision.MissingGrants) > 0 {
		metadata["missing_grants"] = append([]string(nil), decision.MissingGrants...)
	}

	return CapabilityResult{
		Allowed:    decision.Allowed,
		Mode:       decision.Mode,
		Reason:     decision.Reason,
		Connection: resolution.Connection,
		Metadata:   metadata,
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

func (s *Service) findScopedConnection(ctx context.Context, providerID string, scope ScopeRef) (Connection, bool, error) {
	if s == nil || s.connectionStore == nil {
		return Connection{}, false, nil
	}
	connections, err := s.connectionStore.FindByScope(ctx, providerID, scope)
	if err != nil {
		return Connection{}, false, err
	}
	if len(connections) == 0 {
		return Connection{}, false, nil
	}

	for _, connection := range connections {
		if connection.Status == ConnectionStatusNeedsReconsent {
			return connection, true, nil
		}
	}
	for _, connection := range connections {
		if connection.Status == ConnectionStatusActive {
			return connection, true, nil
		}
	}
	return connections[0], true, nil
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
		AccessToken:     strings.TrimSpace(string(credential.EncryptedPayload)),
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

func resolveRequestedGrants(result CompleteAuthResponse) []string {
	if len(result.RequestedGrants) > 0 {
		return append([]string(nil), result.RequestedGrants...)
	}
	if len(result.Credential.RequestedScopes) > 0 {
		return append([]string(nil), result.Credential.RequestedScopes...)
	}
	return []string{}
}

func resolveGrantedGrants(result CompleteAuthResponse) []string {
	if len(result.GrantedGrants) > 0 {
		return append([]string(nil), result.GrantedGrants...)
	}
	if len(result.Credential.GrantedScopes) > 0 {
		return append([]string(nil), result.Credential.GrantedScopes...)
	}
	return []string{}
}

func resolveRefreshGrantedGrants(result RefreshResult) []string {
	if len(result.GrantedGrants) > 0 {
		return append([]string(nil), result.GrantedGrants...)
	}
	if len(result.Credential.GrantedScopes) > 0 {
		return append([]string(nil), result.Credential.GrantedScopes...)
	}
	return []string{}
}
