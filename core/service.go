package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	goerrors "github.com/goliatone/go-errors"
	glog "github.com/goliatone/go-logger/glog"
)

var (
	ErrProviderNotFound        = errors.New("core: provider not found")
	ErrCapabilityNotSupported  = errors.New("core: capability not supported")
	ErrEmbeddedAuthUnsupported = errors.New("core: embedded auth not supported")
)

type Service struct {
	config                  Config
	logger                  Logger
	loggerProvider          LoggerProvider
	metricsRecorder         MetricsRecorder
	errorFactory            ErrorFactory
	errorMapper             ErrorMapper
	secretProvider          SecretProvider
	persistenceClient       any
	repositoryFactory       any
	configProvider          ConfigProvider
	optionsResolver         OptionsResolver
	oauthStateStore         OAuthStateStore
	connectionLocker        ConnectionLocker
	refreshBackoffScheduler RefreshBackoffScheduler
	signer                  Signer
	transportResolver       TransportResolver
	rateLimitPolicy         RateLimitPolicy
	registry                Registry
	connectionStore         ConnectionStore
	credentialStore         CredentialStore
	subscriptionStore       SubscriptionStore
	syncCursorStore         SyncCursorStore
	installationStore       InstallationStore
	syncJobStore            SyncJobStore
	grantStore              GrantStore
	permissionEvaluator     PermissionEvaluator
	credentialCodec         CredentialCodec
	callbackURLResolver     CallbackURLResolver
	strictPolicy            InheritancePolicy
	inheritancePolicy       InheritancePolicy
}

type ServiceDependencies struct {
	Logger              Logger
	LoggerProvider      LoggerProvider
	MetricsRecorder     MetricsRecorder
	ErrorFactory        ErrorFactory
	ErrorMapper         ErrorMapper
	SecretProvider      SecretProvider
	PersistenceClient   any
	RepositoryFactory   any
	ConfigProvider      ConfigProvider
	OptionsResolver     OptionsResolver
	OAuthStateStore     OAuthStateStore
	ConnectionLocker    ConnectionLocker
	RefreshScheduler    RefreshBackoffScheduler
	Signer              Signer
	TransportResolver   TransportResolver
	RateLimitPolicy     RateLimitPolicy
	Registry            Registry
	ConnectionStore     ConnectionStore
	CredentialStore     CredentialStore
	SubscriptionStore   SubscriptionStore
	SyncCursorStore     SyncCursorStore
	InstallationStore   InstallationStore
	SyncJobStore        SyncJobStore
	GrantStore          GrantStore
	PermissionEvaluator PermissionEvaluator
	CredentialCodec     CredentialCodec
	CallbackURLResolver CallbackURLResolver
	InheritancePolicy   InheritancePolicy
}

func NewService(cfg Config, opts ...Option) (*Service, error) {
	builder := defaultServiceBuilder(cfg)
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&builder)
	}

	provider, logger := resolveServiceLogger(builder.loggerProvider, builder.logger)
	applyServiceBuilderDefaults(&builder)

	defaults := DefaultConfig()
	loaded, err := builder.configProvider.Load(context.Background(), defaults)
	if err != nil {
		return nil, mapBuildError(builder.errorMapper, err)
	}
	finalConfig, err := builder.optionsResolver.Resolve(defaults, loaded, builder.runtimeConfig)
	if err != nil {
		return nil, mapBuildError(builder.errorMapper, err)
	}

	if err := hydrateServiceStores(&builder); err != nil {
		return nil, mapBuildError(builder.errorMapper, err)
	}
	if builder.permissionEvaluator == nil {
		builder.permissionEvaluator = NewGrantPermissionEvaluator(
			builder.connectionStore,
			builder.grantStore,
			builder.registry,
		)
	}
	hydrateRateLimitPolicy(&builder)

	strict := &StrictIsolationPolicy{ConnectionStore: builder.connectionStore}
	inheritancePolicy := builder.inheritancePolicy
	if inheritancePolicy == nil {
		inheritancePolicy = strict
	}

	return buildService(builder, finalConfig, provider, logger, strict, inheritancePolicy), nil
}

func resolveServiceLogger(configuredProvider LoggerProvider, configured Logger) (LoggerProvider, Logger) {
	provider, logger := glog.Resolve("services", configuredProvider, configured)
	if provider != nil {
		if named := provider.GetLogger("services"); named != nil {
			logger = named
		}
	}
	return provider, glog.Ensure(logger)
}

func applyServiceBuilderDefaults(builder *serviceBuilder) {
	if builder.errorFactory == nil {
		builder.errorFactory = goerrors.New
	}
	if builder.metricsRecorder == nil {
		builder.metricsRecorder = NopMetricsRecorder{}
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
	if builder.credentialCodec == nil {
		builder.credentialCodec = JSONCredentialCodec{}
	}
}

func hydrateServiceStores(builder *serviceBuilder) error {
	if builder.repositoryFactory == nil {
		return nil
	}
	if err := hydrateConnectionAndCredentialStores(builder); err != nil {
		return err
	}
	hydrateAuxiliaryServiceStores(builder)
	return nil
}

func hydrateConnectionAndCredentialStores(builder *serviceBuilder) error {
	if builder.connectionStore != nil && builder.credentialStore != nil {
		return nil
	}
	provider, err := resolveStoreProvider(builder)
	if err != nil || provider == nil {
		return err
	}
	if builder.connectionStore == nil {
		builder.connectionStore = provider.ConnectionStore()
	}
	if builder.credentialStore == nil {
		builder.credentialStore = provider.CredentialStore()
	}
	return nil
}

func resolveStoreProvider(builder *serviceBuilder) (StoreProvider, error) {
	if factory, ok := builder.repositoryFactory.(RepositoryStoreFactory); ok {
		return factory.BuildStores(builder.persistenceClient)
	}
	provider, _ := builder.repositoryFactory.(StoreProvider)
	return provider, nil
}

func hydrateAuxiliaryServiceStores(builder *serviceBuilder) {
	factory := builder.repositoryFactory
	if provider, ok := factory.(interface{ SubscriptionStore() SubscriptionStore }); ok && builder.subscriptionStore == nil {
		builder.subscriptionStore = provider.SubscriptionStore()
	}
	if provider, ok := factory.(interface{ SyncCursorStore() SyncCursorStore }); ok && builder.syncCursorStore == nil {
		builder.syncCursorStore = provider.SyncCursorStore()
	}
	if provider, ok := factory.(interface{ InstallationStore() InstallationStore }); ok && builder.installationStore == nil {
		builder.installationStore = provider.InstallationStore()
	}
	hydrateSyncJobStore(builder)
}

func hydrateSyncJobStore(builder *serviceBuilder) {
	if builder.syncJobStore != nil {
		return
	}
	if provider, ok := builder.repositoryFactory.(interface{ SyncJobStoreCore() SyncJobStore }); ok {
		builder.syncJobStore = provider.SyncJobStoreCore()
		return
	}
	if provider, ok := builder.repositoryFactory.(interface{ SyncJobStore() SyncJobStore }); ok {
		builder.syncJobStore = provider.SyncJobStore()
	}
}

func hydrateRateLimitPolicy(builder *serviceBuilder) {
	if builder.rateLimitPolicy != nil || builder.repositoryFactory == nil {
		return
	}
	if provider, ok := builder.repositoryFactory.(interface{ RateLimitPolicy() RateLimitPolicy }); ok {
		builder.rateLimitPolicy = provider.RateLimitPolicy()
	}
}

func buildService(
	builder serviceBuilder,
	config Config,
	loggerProvider LoggerProvider,
	logger Logger,
	strict *StrictIsolationPolicy,
	inheritancePolicy InheritancePolicy,
) *Service {
	return &Service{
		config:                  config,
		logger:                  logger,
		loggerProvider:          loggerProvider,
		metricsRecorder:         builder.metricsRecorder,
		errorFactory:            builder.errorFactory,
		errorMapper:             builder.errorMapper,
		secretProvider:          builder.secretProvider,
		persistenceClient:       builder.persistenceClient,
		repositoryFactory:       builder.repositoryFactory,
		configProvider:          builder.configProvider,
		optionsResolver:         builder.optionsResolver,
		oauthStateStore:         builder.oauthStateStore,
		connectionLocker:        builder.connectionLocker,
		refreshBackoffScheduler: builder.refreshScheduler,
		signer:                  builder.signer,
		transportResolver:       builder.transportResolver,
		rateLimitPolicy:         builder.rateLimitPolicy,
		registry:                builder.registry,
		connectionStore:         builder.connectionStore,
		credentialStore:         builder.credentialStore,
		subscriptionStore:       builder.subscriptionStore,
		syncCursorStore:         builder.syncCursorStore,
		installationStore:       builder.installationStore,
		syncJobStore:            builder.syncJobStore,
		grantStore:              builder.grantStore,
		permissionEvaluator:     builder.permissionEvaluator,
		credentialCodec:         builder.credentialCodec,
		callbackURLResolver:     builder.callbackURLResolver,
		strictPolicy:            strict,
		inheritancePolicy:       inheritancePolicy,
	}
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
		Logger:              s.logger,
		LoggerProvider:      s.loggerProvider,
		MetricsRecorder:     s.metricsRecorder,
		ErrorFactory:        s.errorFactory,
		ErrorMapper:         s.errorMapper,
		SecretProvider:      s.secretProvider,
		PersistenceClient:   s.persistenceClient,
		RepositoryFactory:   s.repositoryFactory,
		ConfigProvider:      s.configProvider,
		OptionsResolver:     s.optionsResolver,
		OAuthStateStore:     s.oauthStateStore,
		ConnectionLocker:    s.connectionLocker,
		RefreshScheduler:    s.refreshBackoffScheduler,
		Signer:              s.signer,
		TransportResolver:   s.transportResolver,
		RateLimitPolicy:     s.rateLimitPolicy,
		Registry:            s.registry,
		ConnectionStore:     s.connectionStore,
		CredentialStore:     s.credentialStore,
		SubscriptionStore:   s.subscriptionStore,
		SyncCursorStore:     s.syncCursorStore,
		InstallationStore:   s.installationStore,
		SyncJobStore:        s.syncJobStore,
		GrantStore:          s.grantStore,
		PermissionEvaluator: s.permissionEvaluator,
		CredentialCodec:     s.credentialCodec,
		CallbackURLResolver: s.callbackURLResolver,
		InheritancePolicy:   s.inheritancePolicy,
	}
}

func (s *Service) resolveCallbackURL(ctx context.Context, req CallbackURLResolveRequest) (string, error) {
	if s == nil || s.callbackURLResolver == nil {
		return "", nil
	}
	resolved, err := s.callbackURLResolver.ResolveCallbackURL(ctx, req)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resolved), nil
}

func (s *Service) AuthenticateEmbedded(
	ctx context.Context,
	req EmbeddedAuthRequest,
) (result EmbeddedAuthResult, err error) {
	startedAt := time.Now().UTC()
	fields := map[string]any{
		"provider_id": req.ProviderID,
		"scope_type":  req.Scope.Type,
		"scope_id":    req.Scope.ID,
	}
	defer func() {
		if result.ShopDomain != "" {
			fields["shop_domain"] = result.ShopDomain
		}
		s.observeOperation(ctx, startedAt, "authenticate_embedded", err, fields)
	}()

	if err = req.Scope.Validate(); err != nil {
		err = s.mapError(err)
		return EmbeddedAuthResult{}, err
	}
	req.ProviderID = strings.TrimSpace(req.ProviderID)
	if req.ProviderID == "" {
		err = s.mapError(fmt.Errorf("core: provider id is required"))
		return EmbeddedAuthResult{}, err
	}

	provider, err := s.resolveProvider(req.ProviderID)
	if err != nil {
		return EmbeddedAuthResult{}, err
	}
	embeddedProvider, ok := provider.(EmbeddedAuthProvider)
	if !ok {
		err = s.mapError(fmt.Errorf("%w for provider %q", ErrEmbeddedAuthUnsupported, req.ProviderID))
		return EmbeddedAuthResult{}, err
	}
	req.ProviderID = provider.ID()
	result, err = embeddedProvider.AuthenticateEmbedded(ctx, req)
	if err != nil {
		err = s.mapError(err)
		return EmbeddedAuthResult{}, err
	}
	if strings.TrimSpace(result.ProviderID) == "" {
		result.ProviderID = req.ProviderID
	}
	return result, nil
}

func (s *Service) Connect(ctx context.Context, req ConnectRequest) (response BeginAuthResponse, err error) {
	startedAt := time.Now().UTC()
	fields := map[string]any{
		"provider_id": req.ProviderID,
		"scope_type":  req.Scope.Type,
		"scope_id":    req.Scope.ID,
	}
	defer func() {
		s.observeOperation(ctx, startedAt, "connect", err, fields)
	}()

	if err = req.Scope.Validate(); err != nil {
		err = s.mapError(err)
		return BeginAuthResponse{}, err
	}
	req.RedirectURI = strings.TrimSpace(req.RedirectURI)
	if req.RedirectURI == "" {
		resolved, resolveErr := s.resolveCallbackURL(ctx, CallbackURLResolveRequest{
			ProviderID:      req.ProviderID,
			Scope:           req.Scope,
			Flow:            CallbackURLResolveFlowConnect,
			RequestedGrants: append([]string(nil), req.RequestedGrants...),
			Metadata:        copyAnyMap(req.Metadata),
		})
		if resolveErr != nil {
			err = s.mapError(resolveErr)
			return BeginAuthResponse{}, err
		}
		req.RedirectURI = resolved
	}
	provider, err := s.resolveProvider(req.ProviderID)
	if err != nil {
		return BeginAuthResponse{}, err
	}
	strategy := s.resolveAuthStrategy(provider)
	if strategy == nil {
		err = s.mapError(fmt.Errorf("core: auth strategy is not configured"))
		return BeginAuthResponse{}, err
	}

	state := strings.TrimSpace(req.State)
	if state == "" && strategyRequiresCallbackState(strategy) {
		generated, generateErr := generateOAuthState()
		if generateErr != nil {
			err = s.mapError(generateErr)
			return BeginAuthResponse{}, err
		}
		state = generated
	}

	begin, err := strategy.Begin(ctx, AuthBeginRequest{
		Scope:        req.Scope,
		RedirectURI:  req.RedirectURI,
		State:        state,
		RequestedRaw: append([]string(nil), req.RequestedGrants...),
		Metadata:     req.Metadata,
	})
	if err != nil {
		err = s.mapError(err)
		return BeginAuthResponse{}, err
	}
	response = BeginAuthResponse{
		URL:             begin.URL,
		State:           begin.State,
		RequestedGrants: append([]string(nil), begin.RequestedGrants...),
		Metadata:        copyAnyMap(begin.Metadata),
	}
	if len(response.RequestedGrants) == 0 {
		response.RequestedGrants = append([]string(nil), req.RequestedGrants...)
	}
	if strings.TrimSpace(response.State) == "" {
		response.State = state
	}

	if s.oauthStateStore != nil && strategyRequiresCallbackState(strategy) {
		saveErr := s.oauthStateStore.Save(ctx, OAuthStateRecord{
			State:           response.State,
			ProviderID:      req.ProviderID,
			Scope:           req.Scope,
			RedirectURI:     req.RedirectURI,
			RequestedGrants: append([]string(nil), response.RequestedGrants...),
			Metadata:        copyAnyMap(req.Metadata),
			CreatedAt:       time.Now().UTC(),
		})
		if saveErr != nil {
			err = s.mapError(saveErr)
			return BeginAuthResponse{}, err
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
		ConnectionStatusNeedsReconsent,
		"re-consent requested",
	); updateErr != nil {
		return BeginAuthResponse{}, s.mapError(updateErr)
	}

	requested := append([]string(nil), req.RequestedGrants...)
	if len(requested) == 0 && s.grantStore != nil {
		if snapshot, found, snapshotErr := s.grantStore.GetLatestSnapshot(ctx, connectionID); snapshotErr == nil && found {
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
	metadata := copyAnyMap(req.Metadata)
	metadata["connection_id"] = connectionID

	redirectURI := strings.TrimSpace(req.RedirectURI)
	if redirectURI == "" {
		resolved, resolveErr := s.resolveCallbackURL(ctx, CallbackURLResolveRequest{
			ProviderID:      connection.ProviderID,
			Scope:           ScopeRef{Type: connection.ScopeType, ID: connection.ScopeID},
			ConnectionID:    connectionID,
			Flow:            CallbackURLResolveFlowReconsent,
			RequestedGrants: append([]string(nil), requested...),
			Metadata:        copyAnyMap(metadata),
		})
		if resolveErr != nil {
			return BeginAuthResponse{}, s.mapError(resolveErr)
		}
		redirectURI = resolved
	}

	return s.Connect(ctx, ConnectRequest{
		ProviderID:      connection.ProviderID,
		Scope:           ScopeRef{Type: connection.ScopeType, ID: connection.ScopeID},
		RedirectURI:     redirectURI,
		State:           req.State,
		RequestedGrants: requested,
		Metadata:        metadata,
	})
}

func (s *Service) CompleteReconsent(ctx context.Context, req CompleteAuthRequest) (CallbackCompletion, error) {
	return s.CompleteCallback(ctx, req)
}

func (s *Service) CompleteCallback(ctx context.Context, req CompleteAuthRequest) (completion CallbackCompletion, err error) {
	startedAt := time.Now().UTC()
	fields := map[string]any{
		"provider_id": req.ProviderID,
		"scope_type":  req.Scope.Type,
		"scope_id":    req.Scope.ID,
	}
	defer func() {
		if completion.Connection.ID != "" {
			fields["connection_id"] = completion.Connection.ID
		}
		s.observeOperation(ctx, startedAt, "complete_callback", err, fields)
	}()

	auth, err := s.completeCallbackAuth(ctx, req)
	if err != nil {
		return CallbackCompletion{}, s.mapError(err)
	}
	connection, wasNeedsReconsent, err := s.reconcileCallbackConnection(ctx, auth)
	if err != nil {
		return CallbackCompletion{}, s.mapError(err)
	}
	credential, err := s.storeCallbackCredential(ctx, connection.ID, auth.result.Credential)
	if err != nil {
		return CallbackCompletion{}, s.mapError(err)
	}
	if err := s.reconcileCallbackGrants(ctx, auth, connection.ID, wasNeedsReconsent); err != nil {
		return CallbackCompletion{}, s.mapError(err)
	}
	completion = CallbackCompletion{Connection: connection, Credential: credential}
	return completion, nil
}

type callbackAuthCompletion struct {
	req               CompleteAuthRequest
	provider          Provider
	result            AuthCompleteResponse
	providerID        string
	scope             ScopeRef
	externalAccountID string
}

func (s *Service) completeCallbackAuth(
	ctx context.Context,
	req CompleteAuthRequest,
) (callbackAuthCompletion, error) {
	if err := req.Scope.Validate(); err != nil {
		return callbackAuthCompletion{}, err
	}
	provider, err := s.resolveProvider(req.ProviderID)
	if err != nil {
		return callbackAuthCompletion{}, err
	}
	strategy := s.resolveAuthStrategy(provider)
	if strategy == nil {
		return callbackAuthCompletion{}, fmt.Errorf("core: auth strategy is not configured")
	}
	if strategyRequiresCallbackState(strategy) {
		state, stateErr := s.consumeOAuthCallbackState(ctx, req)
		if stateErr != nil {
			return callbackAuthCompletion{}, stateErr
		}
		req = applyOAuthStateContext(req, state)
	}
	result, err := strategy.Complete(ctx, AuthCompleteRequest{
		Scope:       req.Scope,
		Code:        req.Code,
		State:       req.State,
		RedirectURI: req.RedirectURI,
		Metadata:    copyAnyMap(req.Metadata),
	})
	if err != nil {
		return callbackAuthCompletion{}, err
	}
	externalAccountID := strings.TrimSpace(result.ExternalAccountID)
	if externalAccountID == "" {
		return callbackAuthCompletion{}, fmt.Errorf("core: external account id is required")
	}
	return callbackAuthCompletion{
		req:               req,
		provider:          provider,
		result:            result,
		providerID:        strings.TrimSpace(provider.ID()),
		scope:             ScopeRef{Type: strings.TrimSpace(strings.ToLower(req.Scope.Type)), ID: strings.TrimSpace(req.Scope.ID)},
		externalAccountID: externalAccountID,
	}, nil
}

func (s *Service) reconcileCallbackConnection(
	ctx context.Context,
	auth callbackAuthCompletion,
) (Connection, bool, error) {
	connection := Connection{
		ProviderID:        auth.providerID,
		ScopeType:         auth.scope.Type,
		ScopeID:           auth.scope.ID,
		ExternalAccountID: auth.externalAccountID,
		Status:            ConnectionStatusActive,
	}
	if s.connectionStore == nil {
		return connection, false, nil
	}
	existing, found, err := s.findCallbackConnection(
		ctx,
		auth.providerID,
		auth.scope,
		auth.externalAccountID,
		readStringMetadata(auth.req.Metadata, "connection_id"),
	)
	if err != nil {
		return Connection{}, false, err
	}
	if !found {
		created, err := s.connectionStore.Create(ctx, CreateConnectionInput{
			ProviderID:        auth.providerID,
			Scope:             auth.scope,
			ExternalAccountID: auth.externalAccountID,
			Status:            ConnectionStatusActive,
		})
		return created, false, err
	}
	wasNeedsReconsent := existing.Status == ConnectionStatusNeedsReconsent
	if err := s.connectionStore.UpdateStatus(ctx, existing.ID, ConnectionStatusActive, ""); err != nil {
		return Connection{}, false, err
	}
	existing.Status = ConnectionStatusActive
	existing.LastError = ""
	return existing, wasNeedsReconsent, nil
}

func (s *Service) storeCallbackCredential(
	ctx context.Context,
	connectionID string,
	active ActiveCredential,
) (Credential, error) {
	if s.credentialStore != nil {
		return s.persistActiveCredential(ctx, connectionID, active)
	}
	credential := Credential{
		ConnectionID:    connectionID,
		TokenType:       active.TokenType,
		RequestedScopes: append([]string(nil), active.RequestedScopes...),
		GrantedScopes:   append([]string(nil), active.GrantedScopes...),
		Status:          CredentialStatusActive,
	}
	if active.ExpiresAt != nil {
		credential.ExpiresAt = *active.ExpiresAt
	}
	if active.RotatesAt != nil {
		credential.RotatesAt = *active.RotatesAt
	}
	return credential, nil
}

func (s *Service) reconcileCallbackGrants(
	ctx context.Context,
	auth callbackAuthCompletion,
	connectionID string,
	wasNeedsReconsent bool,
) error {
	requested := append([]string(nil), auth.result.RequestedGrants...)
	if len(requested) == 0 {
		requested = append([]string(nil), auth.result.Credential.RequestedScopes...)
	}
	granted := append([]string(nil), auth.result.GrantedGrants...)
	if len(granted) == 0 {
		granted = append([]string(nil), auth.result.Credential.GrantedScopes...)
	}
	_, delta, err := s.reconcileGrantSnapshot(
		ctx, auth.provider, connectionID, requested, granted, auth.req.Metadata,
	)
	if err != nil {
		return err
	}
	if !wasNeedsReconsent || s.grantStore == nil {
		return nil
	}
	_ = s.grantStore.AppendEvent(ctx, AppendGrantEventInput{
		ConnectionID: connectionID,
		EventType:    GrantEventReconsentCompleted,
		Added:        append([]string(nil), delta.Added...),
		Removed:      append([]string(nil), delta.Removed...),
		OccurredAt:   time.Now().UTC(),
		Metadata:     copyAnyMap(auth.req.Metadata),
	})
	return nil
}

func (s *Service) consumeOAuthCallbackState(ctx context.Context, req CompleteAuthRequest) (OAuthStateRecord, error) {
	if s == nil || s.oauthStateStore == nil {
		return OAuthStateRecord{}, nil
	}
	state := strings.TrimSpace(req.State)
	if state == "" {
		return OAuthStateRecord{}, fmt.Errorf("core: oauth callback state is required")
	}

	record, err := s.oauthStateStore.Consume(ctx, state)
	if err != nil {
		return OAuthStateRecord{}, err
	}
	restoreOnValidationFailure := func(validationErr error) (OAuthStateRecord, error) {
		if saveErr := s.oauthStateStore.Save(ctx, cloneOAuthStateRecord(record)); saveErr != nil {
			return OAuthStateRecord{}, errors.Join(
				validationErr,
				fmt.Errorf("core: restore oauth callback state: %w", saveErr),
			)
		}
		return OAuthStateRecord{}, validationErr
	}
	if !strings.EqualFold(strings.TrimSpace(record.ProviderID), strings.TrimSpace(req.ProviderID)) {
		return restoreOnValidationFailure(fmt.Errorf("core: oauth callback state provider mismatch"))
	}
	if !strings.EqualFold(strings.TrimSpace(record.Scope.Type), strings.TrimSpace(req.Scope.Type)) ||
		strings.TrimSpace(record.Scope.ID) != strings.TrimSpace(req.Scope.ID) {
		return restoreOnValidationFailure(fmt.Errorf("core: oauth callback state scope mismatch"))
	}

	savedRedirect := strings.TrimSpace(record.RedirectURI)
	requestRedirect := strings.TrimSpace(req.RedirectURI)
	if savedRedirect != "" {
		if requestRedirect != "" && savedRedirect != requestRedirect {
			return restoreOnValidationFailure(fmt.Errorf("core: oauth callback state redirect mismatch"))
		}
		if requestRedirect == "" && s.requireCallbackRedirect(req.Metadata) {
			return restoreOnValidationFailure(fmt.Errorf("core: oauth callback redirect uri is required"))
		}
	}
	return cloneOAuthStateRecord(record), nil
}

func applyOAuthStateContext(req CompleteAuthRequest, record OAuthStateRecord) CompleteAuthRequest {
	if strings.TrimSpace(req.RedirectURI) == "" && strings.TrimSpace(record.RedirectURI) != "" {
		req.RedirectURI = strings.TrimSpace(record.RedirectURI)
	}
	mergedMetadata := copyAnyMap(record.Metadata)
	maps.Copy(mergedMetadata, req.Metadata)
	if len(record.RequestedGrants) > 0 {
		mergedMetadata["requested_grants"] = append([]string(nil), record.RequestedGrants...)
	}
	req.Metadata = mergedMetadata
	return req
}

func (s *Service) requireCallbackRedirect(metadata map[string]any) bool {
	required := false
	if s != nil {
		required = s.config.OAuth.RequireCallbackRedirect
	}
	if len(metadata) == 0 {
		return required
	}
	if override, ok := parseBoolOverride(metadata["require_callback_redirect"]); ok {
		if override {
			return true
		}
	}
	if override, ok := parseBoolOverride(metadata["strict_redirect_validation"]); ok {
		if override {
			return true
		}
	}
	return required
}

func parseBoolOverride(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		if err != nil {
			return false, false
		}
		return parsed, true
	default:
		return false, false
	}
}

func (s *Service) Refresh(ctx context.Context, req RefreshRequest) (result RefreshResult, err error) {
	startedAt := time.Now().UTC()
	fields := map[string]any{
		"provider_id":   req.ProviderID,
		"connection_id": req.ConnectionID,
	}
	defer func() {
		s.observeOperation(ctx, startedAt, "refresh", err, fields)
	}()

	req.ConnectionID = strings.TrimSpace(req.ConnectionID)
	if req.ConnectionID == "" {
		err = s.mapError(fmt.Errorf("core: connection id is required"))
		return RefreshResult{}, err
	}
	ctx, unlock, err := s.acquireRefreshLock(ctx, req.ConnectionID)
	if err != nil {
		return RefreshResult{}, s.mapError(err)
	}
	defer unlock()
	req.ProviderID, err = s.resolveRefreshProviderID(ctx, req.ConnectionID, req.ProviderID)
	if err != nil {
		return RefreshResult{}, s.mapError(err)
	}
	provider, err := s.resolveProvider(req.ProviderID)
	if err != nil {
		return RefreshResult{}, err
	}
	strategy := s.resolveAuthStrategy(provider)
	if strategy == nil {
		err = s.mapError(fmt.Errorf("core: auth strategy is not configured"))
		return RefreshResult{}, err
	}

	activeCred, err := s.resolveRefreshCredential(ctx, req)
	if err != nil {
		err = s.mapError(err)
		return RefreshResult{}, err
	}

	result, err = strategy.Refresh(ctx, activeCred)
	if err != nil {
		err = s.mapError(err)
		return RefreshResult{}, err
	}

	if err = s.persistRefreshOutcome(ctx, req.ConnectionID, provider, activeCred, result); err != nil {
		err = s.mapError(err)
		return RefreshResult{}, err
	}

	return result, nil
}

func (s *Service) acquireRefreshLock(
	ctx context.Context,
	connectionID string,
) (context.Context, func(), error) {
	if s.connectionLocker == nil || isRefreshLockHeld(ctx, connectionID) {
		return ctx, func() {}, nil
	}
	lockHandle, err := s.connectionLocker.Acquire(ctx, connectionID, defaultRefreshLockTTL)
	if err != nil {
		return ctx, nil, err
	}
	lockedContext := context.WithValue(ctx, refreshLockContextKey{}, connectionID)
	return lockedContext, func() { _ = lockHandle.Unlock(lockedContext) }, nil
}

func (s *Service) resolveRefreshProviderID(ctx context.Context, connectionID, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if s.connectionStore == nil {
		if requested == "" {
			return "", fmt.Errorf("core: provider id is required")
		}
		return requested, nil
	}
	connection, err := s.connectionStore.Get(ctx, connectionID)
	if err != nil {
		return "", err
	}
	stored := strings.TrimSpace(connection.ProviderID)
	if stored == "" {
		return "", fmt.Errorf("core: connection %q has no provider id", connectionID)
	}
	if requested == "" {
		return stored, nil
	}
	if !strings.EqualFold(requested, stored) {
		return "", fmt.Errorf(
			"core: provider mismatch for connection %q: got %q want %q",
			connectionID,
			requested,
			stored,
		)
	}
	return requested, nil
}

func (s *Service) resolveRefreshCredential(ctx context.Context, req RefreshRequest) (ActiveCredential, error) {
	if req.Credential != nil {
		return *req.Credential, nil
	}
	if s.credentialStore == nil {
		return ActiveCredential{}, fmt.Errorf("core: refresh requires credential input or credential store")
	}
	stored, err := s.credentialStore.GetActiveByConnection(ctx, req.ConnectionID)
	if err != nil {
		return ActiveCredential{}, err
	}
	return s.credentialToActive(ctx, stored)
}

func (s *Service) persistRefreshOutcome(
	ctx context.Context,
	connectionID string,
	provider Provider,
	previous ActiveCredential,
	result RefreshResult,
) error {
	if s.credentialStore != nil && shouldPersistRefreshedCredential(previous, result.Credential) {
		if _, err := s.persistActiveCredential(ctx, connectionID, result.Credential); err != nil {
			return err
		}
	}
	if s.connectionStore != nil {
		if err := s.connectionStore.UpdateStatus(ctx, connectionID, ConnectionStatusActive, ""); err != nil {
			return err
		}
	}
	snapshot, _, err := s.reconcileGrantSnapshot(
		ctx,
		provider,
		connectionID,
		result.Credential.RequestedScopes,
		resolveRefreshGrantedGrants(result),
		result.Metadata,
	)
	if err != nil {
		return err
	}
	if len(missingRequiredProviderGrants(provider.Capabilities(), snapshot.Granted)) == 0 {
		return nil
	}
	return s.transitionConnectionToNeedsReconsent(
		ctx,
		connectionID,
		"required grants missing after refresh",
	)
}

func (s *Service) Revoke(ctx context.Context, connectionID string, reason string) (err error) {
	startedAt := time.Now().UTC()
	fields := map[string]any{
		"connection_id": connectionID,
	}
	defer func() {
		s.observeOperation(ctx, startedAt, "revoke", err, fields)
	}()

	if strings.TrimSpace(connectionID) == "" {
		err = s.mapError(fmt.Errorf("core: connection id is required"))
		return err
	}
	if s.credentialStore != nil {
		if err = s.credentialStore.RevokeActive(ctx, connectionID, reason); err != nil {
			err = s.mapError(err)
			return err
		}
	}
	if s.connectionStore != nil {
		if err = s.connectionStore.UpdateStatus(ctx, connectionID, ConnectionStatusDisconnected, reason); err != nil {
			err = s.mapError(err)
			return err
		}
	}
	return nil
}

func (s *Service) InvokeCapability(ctx context.Context, req InvokeCapabilityRequest) (result CapabilityResult, err error) {
	startedAt := time.Now().UTC()
	fields := map[string]any{
		"provider_id": req.ProviderID,
		"scope_type":  req.Scope.Type,
		"scope_id":    req.Scope.ID,
		"capability":  req.Capability,
	}
	defer func() {
		if result.Connection.ID != "" {
			fields["connection_id"] = result.Connection.ID
		}
		if result.Allowed {
			fields["decision"] = "allowed"
		} else {
			fields["decision"] = "blocked"
		}
		s.observeOperation(ctx, startedAt, "invoke_capability", err, fields)
	}()

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
		err = wrapped.WithMetadata(map[string]any{"provider_id": req.ProviderID, "capability": req.Capability})
		return CapabilityResult{}, err
	}

	resolution, err := s.resolveCapabilityConnection(ctx, req)
	if err != nil {
		return CapabilityResult{}, s.mapError(err)
	}
	if resolution.Outcome == ConnectionResolutionNotFound || resolution.Outcome == ConnectionResolutionAmbiguous {
		result = CapabilityResult{
			Allowed: false,
			Mode:    CapabilityDeniedBehaviorBlock,
			Reason:  resolution.Reason,
		}
		return result, nil
	}

	decision, err := s.evaluateCapabilityPermission(ctx, resolution.Connection.ID, req.Capability, descriptor)
	if err != nil {
		return CapabilityResult{}, s.mapError(err)
	}

	metadata := map[string]any{
		"resolution": resolution.Outcome,
	}
	if len(decision.MissingGrants) > 0 {
		metadata["missing_grants"] = append([]string(nil), decision.MissingGrants...)
	}

	result = CapabilityResult{
		Allowed:    decision.Allowed,
		Mode:       decision.Mode,
		Reason:     decision.Reason,
		Connection: resolution.Connection,
		Metadata:   metadata,
	}
	return result, nil
}

func (s *Service) resolveCapabilityConnection(
	ctx context.Context,
	req InvokeCapabilityRequest,
) (ConnectionResolution, error) {
	connectionID := strings.TrimSpace(req.ConnectionID)
	if connectionID == "" {
		return s.resolveConnection(ctx, req.ProviderID, req.Scope)
	}
	if s.connectionStore == nil {
		return ConnectionResolution{}, fmt.Errorf("core: connection store unavailable")
	}
	connection, err := s.connectionStore.Get(ctx, connectionID)
	if err != nil {
		return ConnectionResolution{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(connection.ProviderID), strings.TrimSpace(req.ProviderID)) {
		return ConnectionResolution{}, fmt.Errorf("core: provider mismatch for connection %q", connectionID)
	}
	if err := validateRequestedConnectionScope(connection, req.Scope); err != nil {
		return ConnectionResolution{}, err
	}
	if connection.Status != ConnectionStatusActive {
		return ConnectionResolution{Outcome: ConnectionResolutionNotFound, Reason: "connection is not active"}, nil
	}
	return ConnectionResolution{Outcome: ConnectionResolutionDirect, Connection: connection}, nil
}

func validateRequestedConnectionScope(connection Connection, requested ScopeRef) error {
	if strings.TrimSpace(requested.Type) == "" && strings.TrimSpace(requested.ID) == "" {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(connection.ScopeType), strings.TrimSpace(requested.Type)) ||
		strings.TrimSpace(connection.ScopeID) != strings.TrimSpace(requested.ID) {
		return fmt.Errorf("core: scope mismatch for connection %q", connection.ID)
	}
	return nil
}

func (s *Service) evaluateCapabilityPermission(
	ctx context.Context,
	connectionID string,
	capability string,
	descriptor CapabilityDescriptor,
) (PermissionDecision, error) {
	decision := PermissionDecision{Allowed: true, Capability: capability, Mode: descriptor.DeniedBehavior}
	if s.permissionEvaluator == nil {
		return decision, nil
	}
	evaluated, err := s.permissionEvaluator.EvaluateCapability(ctx, connectionID, capability)
	if err != nil {
		return PermissionDecision{}, err
	}
	if evaluated.Mode == "" {
		evaluated.Mode = descriptor.DeniedBehavior
	}
	return evaluated, nil
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

func (s *Service) findCallbackConnection(
	ctx context.Context,
	providerID string,
	scope ScopeRef,
	externalAccountID string,
	targetConnectionID string,
) (Connection, bool, error) {
	if s == nil || s.connectionStore == nil {
		return Connection{}, false, nil
	}
	targetConnectionID = strings.TrimSpace(targetConnectionID)
	if targetConnectionID != "" {
		target, err := s.connectionStore.Get(ctx, targetConnectionID)
		if err != nil {
			return Connection{}, false, err
		}
		if !strings.EqualFold(strings.TrimSpace(target.ProviderID), strings.TrimSpace(providerID)) ||
			!strings.EqualFold(strings.TrimSpace(target.ScopeType), strings.TrimSpace(scope.Type)) ||
			strings.TrimSpace(target.ScopeID) != strings.TrimSpace(scope.ID) {
			return Connection{}, false, fmt.Errorf("core: callback target connection does not match provider/scope")
		}
		if strings.TrimSpace(target.ExternalAccountID) != strings.TrimSpace(externalAccountID) {
			return Connection{}, false, fmt.Errorf("core: callback external account id does not match target connection")
		}
		return target, true, nil
	}
	return s.connectionStore.FindByScopeAndExternalAccount(ctx, providerID, scope, externalAccountID)
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

func readStringMetadata(metadata map[string]any, keys ...string) string {
	if len(metadata) == 0 {
		return ""
	}
	for _, key := range keys {
		value, ok := metadata[key]
		if !ok || value == nil {
			continue
		}
		trimmed := strings.TrimSpace(fmt.Sprint(value))
		if trimmed != "" && trimmed != "<nil>" {
			return trimmed
		}
	}
	return ""
}

func (s *Service) credentialToActive(ctx context.Context, credential Credential) (ActiveCredential, error) {
	active := ActiveCredential{
		ConnectionID: credential.ConnectionID,
	}
	if len(credential.EncryptedPayload) > 0 {
		decoded, err := s.decodeCredentialPayload(ctx, credential)
		if err != nil {
			return ActiveCredential{}, err
		}
		active = decoded
	}
	applyCredentialFallbacks(&active, credential)
	return active, nil
}

func (s *Service) decodeCredentialPayload(ctx context.Context, credential Credential) (ActiveCredential, error) {
	if s == nil || s.secretProvider == nil {
		return ActiveCredential{}, fmt.Errorf("core: secret provider is required to decrypt credential payloads")
	}
	decrypted, err := s.secretProvider.Decrypt(ctx, credential.EncryptedPayload)
	if err != nil {
		return ActiveCredential{}, fmt.Errorf("core: decrypt credential payload: %w", err)
	}
	codec, err := s.codecForCredential(credential)
	if err != nil {
		return ActiveCredential{}, err
	}
	return codec.Decode(decrypted)
}

func applyCredentialFallbacks(active *ActiveCredential, credential Credential) {
	if strings.TrimSpace(active.ConnectionID) == "" {
		active.ConnectionID = credential.ConnectionID
	}
	if strings.TrimSpace(active.TokenType) == "" {
		active.TokenType = credential.TokenType
	}
	if len(active.RequestedScopes) == 0 {
		active.RequestedScopes = append([]string(nil), credential.RequestedScopes...)
	}
	if len(active.GrantedScopes) == 0 {
		active.GrantedScopes = append([]string(nil), credential.GrantedScopes...)
	}
	if credential.Refreshable && !active.Refreshable {
		active.Refreshable = true
	}
	if active.ExpiresAt == nil && !credential.ExpiresAt.IsZero() {
		expires := credential.ExpiresAt
		active.ExpiresAt = &expires
	}
	if active.RotatesAt == nil && !credential.RotatesAt.IsZero() {
		rotates := credential.RotatesAt
		active.RotatesAt = &rotates
	}
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

func shouldPersistRefreshedCredential(current ActiveCredential, refreshed ActiveCredential) bool {
	if !strings.EqualFold(strings.TrimSpace(current.TokenType), strings.TrimSpace(refreshed.TokenType)) {
		return true
	}

	currentToken := strings.TrimSpace(current.AccessToken)
	refreshedToken := strings.TrimSpace(refreshed.AccessToken)
	if refreshedToken != "" && currentToken != refreshedToken {
		return true
	}
	if refreshedToken == "" && currentToken == "" && strings.TrimSpace(refreshed.RefreshToken) != "" && strings.TrimSpace(current.RefreshToken) != strings.TrimSpace(refreshed.RefreshToken) {
		return true
	}

	if current.Refreshable != refreshed.Refreshable {
		return true
	}
	if !sameStringSliceSet(current.RequestedScopes, refreshed.RequestedScopes) {
		return true
	}
	if !sameStringSliceSet(current.GrantedScopes, refreshed.GrantedScopes) {
		return true
	}
	if !sameTimePointer(current.ExpiresAt, refreshed.ExpiresAt) {
		return true
	}
	if !sameTimePointer(current.RotatesAt, refreshed.RotatesAt) {
		return true
	}
	return false
}

func sameStringSliceSet(left, right []string) bool {
	lset := toGrantSet(left)
	rset := toGrantSet(right)
	if len(lset) != len(rset) {
		return false
	}
	for value := range lset {
		if _, ok := rset[value]; !ok {
			return false
		}
	}
	return true
}

func sameTimePointer(left, right *time.Time) bool {
	if left == nil && right == nil {
		return true
	}
	if left == nil || right == nil {
		return false
	}
	return left.UTC().Equal(right.UTC())
}

type secretProviderMetadata interface {
	Metadata() (string, int)
}

func (s *Service) persistActiveCredential(
	ctx context.Context,
	connectionID string,
	credential ActiveCredential,
) (Credential, error) {
	encryptedPayload, keyID, keyVersion, payloadFormat, payloadVersion, err := s.encryptCredentialPayload(ctx, credential)
	if err != nil {
		return Credential{}, err
	}
	return s.credentialStore.SaveNewVersion(ctx, SaveCredentialInput{
		ConnectionID:      connectionID,
		EncryptedPayload:  encryptedPayload,
		PayloadFormat:     payloadFormat,
		PayloadVersion:    payloadVersion,
		TokenType:         credential.TokenType,
		RequestedScopes:   append([]string(nil), credential.RequestedScopes...),
		GrantedScopes:     append([]string(nil), credential.GrantedScopes...),
		ExpiresAt:         credential.ExpiresAt,
		Refreshable:       credential.Refreshable,
		RotatesAt:         credential.RotatesAt,
		Status:            CredentialStatusActive,
		EncryptionKeyID:   keyID,
		EncryptionVersion: keyVersion,
	})
}

func (s *Service) encryptCredentialPayload(
	ctx context.Context,
	credential ActiveCredential,
) ([]byte, string, int, string, int, error) {
	if s == nil || s.secretProvider == nil {
		return nil, "", 0, "", 0, fmt.Errorf("core: secret provider is required to persist credential payloads")
	}
	codec := s.credentialCodec
	if codec == nil {
		codec = JSONCredentialCodec{}
	}
	plaintext, codecErr := codec.Encode(credential)
	if codecErr != nil {
		return nil, "", 0, "", 0, codecErr
	}
	if len(plaintext) == 0 {
		return nil, "", 0, "", 0, fmt.Errorf("core: credential payload codec encoded an empty payload")
	}
	encrypted, err := s.secretProvider.Encrypt(ctx, plaintext)
	if err != nil {
		return nil, "", 0, "", 0, fmt.Errorf("core: encrypt credential payload: %w", err)
	}
	if len(encrypted) == 0 {
		return nil, "", 0, "", 0, fmt.Errorf("core: encrypted credential payload is empty")
	}
	if bytes.Equal(encrypted, plaintext) {
		return nil, "", 0, "", 0, fmt.Errorf("core: encrypted credential payload is not encrypted")
	}

	keyID := "managed"
	version := 1
	if metadataProvider, ok := s.secretProvider.(secretProviderMetadata); ok {
		id, keyVersion := metadataProvider.Metadata()
		if strings.TrimSpace(id) != "" {
			keyID = strings.TrimSpace(id)
		}
		if keyVersion > 0 {
			version = keyVersion
		}
	}

	return encrypted, keyID, version, codec.Format(), codec.Version(), nil
}

func (s *Service) codecForCredential(credential Credential) (CredentialCodec, error) {
	format := strings.ToLower(strings.TrimSpace(credential.PayloadFormat))
	version := credential.PayloadVersion
	if format == "" {
		format = CredentialPayloadFormatLegacyToken
	}
	if version <= 0 {
		version = CredentialPayloadVersionV1
	}
	if s != nil && s.credentialCodec != nil {
		primaryFormat := strings.ToLower(strings.TrimSpace(s.credentialCodec.Format()))
		if format == primaryFormat {
			if version != s.credentialCodec.Version() {
				return nil, fmt.Errorf("core: unsupported credential payload version %d for format %q", version, format)
			}
			return s.credentialCodec, nil
		}
	}
	legacy := LegacyTokenCredentialCodec{}
	if format == legacy.Format() {
		if version != legacy.Version() {
			return nil, fmt.Errorf("core: unsupported credential payload version %d for format %q", version, format)
		}
		return legacy, nil
	}
	return nil, fmt.Errorf("core: unsupported credential payload format %q", format)
}
