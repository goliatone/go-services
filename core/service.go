package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
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
	grantStore              GrantStore
	permissionEvaluator     PermissionEvaluator
	credentialCodec         CredentialCodec
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
	GrantStore          GrantStore
	PermissionEvaluator PermissionEvaluator
	CredentialCodec     CredentialCodec
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
	if builder.subscriptionStore == nil && builder.repositoryFactory != nil {
		if provider, ok := builder.repositoryFactory.(interface{ SubscriptionStore() SubscriptionStore }); ok {
			builder.subscriptionStore = provider.SubscriptionStore()
		}
	}
	if builder.syncCursorStore == nil && builder.repositoryFactory != nil {
		if provider, ok := builder.repositoryFactory.(interface{ SyncCursorStore() SyncCursorStore }); ok {
			builder.syncCursorStore = provider.SyncCursorStore()
		}
	}
	if builder.installationStore == nil && builder.repositoryFactory != nil {
		if provider, ok := builder.repositoryFactory.(interface{ InstallationStore() InstallationStore }); ok {
			builder.installationStore = provider.InstallationStore()
		}
	}
	if builder.permissionEvaluator == nil {
		builder.permissionEvaluator = NewGrantPermissionEvaluator(
			builder.connectionStore,
			builder.grantStore,
			builder.registry,
		)
	}
	if builder.rateLimitPolicy == nil && builder.repositoryFactory != nil {
		if provider, ok := builder.repositoryFactory.(interface{ RateLimitPolicy() RateLimitPolicy }); ok {
			builder.rateLimitPolicy = provider.RateLimitPolicy()
		}
	}

	strict := &StrictIsolationPolicy{ConnectionStore: builder.connectionStore}
	inheritancePolicy := builder.inheritancePolicy
	if inheritancePolicy == nil {
		inheritancePolicy = strict
	}

	return &Service{
		config:                  finalConfig,
		logger:                  logger,
		loggerProvider:          provider,
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
		grantStore:              builder.grantStore,
		permissionEvaluator:     builder.permissionEvaluator,
		credentialCodec:         builder.credentialCodec,
		strictPolicy:            strict,
		inheritancePolicy:       inheritancePolicy,
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
		GrantStore:          s.grantStore,
		PermissionEvaluator: s.permissionEvaluator,
		CredentialCodec:     s.credentialCodec,
		InheritancePolicy:   s.inheritancePolicy,
	}
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
		string(ConnectionStatusNeedsReconsent),
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

	if err = req.Scope.Validate(); err != nil {
		err = s.mapError(err)
		return CallbackCompletion{}, err
	}
	provider, err := s.resolveProvider(req.ProviderID)
	if err != nil {
		return CallbackCompletion{}, err
	}
	strategy := s.resolveAuthStrategy(provider)
	if strategy == nil {
		err = s.mapError(fmt.Errorf("core: auth strategy is not configured"))
		return CallbackCompletion{}, err
	}
	if strategyRequiresCallbackState(strategy) {
		stateRecord, stateErr := s.consumeOAuthCallbackState(ctx, req)
		if stateErr != nil {
			err = stateErr
			err = s.mapError(err)
			return CallbackCompletion{}, err
		}
		req = applyOAuthStateContext(req, stateRecord)
	}
	result, err := strategy.Complete(ctx, AuthCompleteRequest{
		Scope:       req.Scope,
		Code:        req.Code,
		State:       req.State,
		RedirectURI: req.RedirectURI,
		Metadata:    copyAnyMap(req.Metadata),
	})
	if err != nil {
		err = s.mapError(err)
		return CallbackCompletion{}, err
	}
	externalAccountID := resolveExternalAccountID(req.ProviderID, req.Scope, result.ExternalAccountID)

	connection := Connection{
		ProviderID:        req.ProviderID,
		ScopeType:         req.Scope.Type,
		ScopeID:           req.Scope.ID,
		ExternalAccountID: externalAccountID,
		Status:            ConnectionStatusActive,
	}
	wasNeedsReconsent := false
	if s.connectionStore != nil {
		existing, found, findErr := s.findScopedConnection(ctx, req.ProviderID, req.Scope)
		if findErr != nil {
			err = s.mapError(findErr)
			return CallbackCompletion{}, err
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
				err = s.mapError(updateErr)
				return CallbackCompletion{}, err
			}
			connection.Status = ConnectionStatusActive
			connection.LastError = ""
		} else {
			connection, err = s.connectionStore.Create(ctx, CreateConnectionInput{
				ProviderID:        req.ProviderID,
				Scope:             req.Scope,
				ExternalAccountID: externalAccountID,
				Status:            ConnectionStatusActive,
			})
			if err != nil {
				err = s.mapError(err)
				return CallbackCompletion{}, err
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
		encryptedPayload, keyID, keyVersion, payloadFormat, payloadVersion, encryptErr := s.encryptCredentialPayload(ctx, result.Credential)
		if encryptErr != nil {
			err = s.mapError(encryptErr)
			return CallbackCompletion{}, err
		}
		credential, err = s.credentialStore.SaveNewVersion(ctx, SaveCredentialInput{
			ConnectionID:      connection.ID,
			EncryptedPayload:  encryptedPayload,
			PayloadFormat:     payloadFormat,
			PayloadVersion:    payloadVersion,
			TokenType:         result.Credential.TokenType,
			RequestedScopes:   append([]string(nil), result.Credential.RequestedScopes...),
			GrantedScopes:     append([]string(nil), result.Credential.GrantedScopes...),
			ExpiresAt:         result.Credential.ExpiresAt,
			Refreshable:       result.Credential.Refreshable,
			RotatesAt:         result.Credential.RotatesAt,
			Status:            CredentialStatusActive,
			EncryptionKeyID:   keyID,
			EncryptionVersion: keyVersion,
		})
		if err != nil {
			err = s.mapError(err)
			return CallbackCompletion{}, err
		}
	}

	requestedGrants := append([]string(nil), result.RequestedGrants...)
	if len(requestedGrants) == 0 {
		requestedGrants = append([]string(nil), result.Credential.RequestedScopes...)
	}
	grantedGrants := append([]string(nil), result.GrantedGrants...)
	if len(grantedGrants) == 0 {
		grantedGrants = append([]string(nil), result.Credential.GrantedScopes...)
	}

	_, delta, grantErr := s.reconcileGrantSnapshot(
		ctx,
		provider,
		connection.ID,
		requestedGrants,
		grantedGrants,
		req.Metadata,
	)
	if grantErr != nil {
		err = s.mapError(grantErr)
		return CallbackCompletion{}, err
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

	completion = CallbackCompletion{Connection: connection, Credential: credential}
	return completion, nil
}

func (s *Service) validateOAuthCallbackState(ctx context.Context, req CompleteAuthRequest) error {
	_, err := s.consumeOAuthCallbackState(ctx, req)
	return err
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
	if !strings.EqualFold(strings.TrimSpace(record.ProviderID), strings.TrimSpace(req.ProviderID)) {
		return OAuthStateRecord{}, fmt.Errorf("core: oauth callback state provider mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(record.Scope.Type), strings.TrimSpace(req.Scope.Type)) ||
		strings.TrimSpace(record.Scope.ID) != strings.TrimSpace(req.Scope.ID) {
		return OAuthStateRecord{}, fmt.Errorf("core: oauth callback state scope mismatch")
	}

	savedRedirect := strings.TrimSpace(record.RedirectURI)
	requestRedirect := strings.TrimSpace(req.RedirectURI)
	if savedRedirect != "" {
		if requestRedirect != "" && savedRedirect != requestRedirect {
			return OAuthStateRecord{}, fmt.Errorf("core: oauth callback state redirect mismatch")
		}
		if requestRedirect == "" && s.requireCallbackRedirect(req.Metadata) {
			return OAuthStateRecord{}, fmt.Errorf("core: oauth callback redirect uri is required")
		}
	}
	return cloneOAuthStateRecord(record), nil
}

func applyOAuthStateContext(req CompleteAuthRequest, record OAuthStateRecord) CompleteAuthRequest {
	if strings.TrimSpace(req.RedirectURI) == "" && strings.TrimSpace(record.RedirectURI) != "" {
		req.RedirectURI = strings.TrimSpace(record.RedirectURI)
	}
	mergedMetadata := copyAnyMap(record.Metadata)
	for key, value := range req.Metadata {
		mergedMetadata[key] = value
	}
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

	connectionID := strings.TrimSpace(req.ConnectionID)
	if connectionID == "" {
		err = s.mapError(fmt.Errorf("core: connection id is required"))
		return RefreshResult{}, err
	}
	req.ConnectionID = connectionID

	unlock := func() {}
	if s.connectionLocker != nil && !isRefreshLockHeld(ctx, connectionID) {
		lockHandle, lockErr := s.connectionLocker.Acquire(ctx, connectionID, defaultRefreshLockTTL)
		if lockErr != nil {
			err = s.mapError(lockErr)
			return RefreshResult{}, err
		}
		ctx = context.WithValue(ctx, refreshLockContextKey{}, connectionID)
		unlock = func() {
			_ = lockHandle.Unlock(ctx)
		}
	}
	defer unlock()

	resolvedProviderID := strings.TrimSpace(req.ProviderID)
	if s.connectionStore != nil {
		connection, loadErr := s.connectionStore.Get(ctx, req.ConnectionID)
		if loadErr != nil {
			err = s.mapError(loadErr)
			return RefreshResult{}, err
		}
		connectionProviderID := strings.TrimSpace(connection.ProviderID)
		if connectionProviderID == "" {
			err = s.mapError(fmt.Errorf("core: connection %q has no provider id", req.ConnectionID))
			return RefreshResult{}, err
		}
		if resolvedProviderID == "" {
			resolvedProviderID = connectionProviderID
		} else if !strings.EqualFold(resolvedProviderID, connectionProviderID) {
			err = s.mapError(
				fmt.Errorf(
					"core: provider mismatch for connection %q: got %q want %q",
					req.ConnectionID,
					resolvedProviderID,
					connectionProviderID,
				),
			)
			return RefreshResult{}, err
		}
	}
	if resolvedProviderID == "" {
		err = s.mapError(fmt.Errorf("core: provider id is required"))
		return RefreshResult{}, err
	}
	req.ProviderID = resolvedProviderID
	provider, err := s.resolveProvider(resolvedProviderID)
	if err != nil {
		return RefreshResult{}, err
	}
	strategy := s.resolveAuthStrategy(provider)
	if strategy == nil {
		err = s.mapError(fmt.Errorf("core: auth strategy is not configured"))
		return RefreshResult{}, err
	}

	activeCred := ActiveCredential{}
	if req.Credential != nil {
		activeCred = *req.Credential
	} else if s.credentialStore != nil {
		stored, loadErr := s.credentialStore.GetActiveByConnection(ctx, req.ConnectionID)
		if loadErr != nil {
			err = s.mapError(loadErr)
			return RefreshResult{}, err
		}
		activeCred, err = s.credentialToActive(ctx, stored)
		if err != nil {
			err = s.mapError(err)
			return RefreshResult{}, err
		}
	} else {
		err = s.mapError(fmt.Errorf("core: refresh requires credential input or credential store"))
		return RefreshResult{}, err
	}

	result, err = strategy.Refresh(ctx, activeCred)
	if err != nil {
		err = s.mapError(err)
		return RefreshResult{}, err
	}

	shouldPersist := shouldPersistRefreshedCredential(activeCred, result.Credential)
	if s.credentialStore != nil && shouldPersist {
		encryptedPayload, keyID, keyVersion, payloadFormat, payloadVersion, encryptErr := s.encryptCredentialPayload(ctx, result.Credential)
		if encryptErr != nil {
			err = s.mapError(encryptErr)
			return RefreshResult{}, err
		}
		_, saveErr := s.credentialStore.SaveNewVersion(ctx, SaveCredentialInput{
			ConnectionID:      req.ConnectionID,
			EncryptedPayload:  encryptedPayload,
			PayloadFormat:     payloadFormat,
			PayloadVersion:    payloadVersion,
			TokenType:         result.Credential.TokenType,
			RequestedScopes:   append([]string(nil), result.Credential.RequestedScopes...),
			GrantedScopes:     append([]string(nil), result.Credential.GrantedScopes...),
			ExpiresAt:         result.Credential.ExpiresAt,
			Refreshable:       result.Credential.Refreshable,
			RotatesAt:         result.Credential.RotatesAt,
			Status:            CredentialStatusActive,
			EncryptionKeyID:   keyID,
			EncryptionVersion: keyVersion,
		})
		if saveErr != nil {
			err = s.mapError(saveErr)
			return RefreshResult{}, err
		}
	}

	if s.connectionStore != nil {
		if updateErr := s.connectionStore.UpdateStatus(ctx, req.ConnectionID, string(ConnectionStatusActive), ""); updateErr != nil {
			err = s.mapError(updateErr)
			return RefreshResult{}, err
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
		err = s.mapError(grantErr)
		return RefreshResult{}, err
	}
	if len(missingRequiredProviderGrants(provider.Capabilities(), snapshot.Granted)) > 0 {
		if transitionErr := s.transitionConnectionToNeedsReconsent(
			ctx,
			req.ConnectionID,
			"required grants missing after refresh",
		); transitionErr != nil {
			err = s.mapError(transitionErr)
			return RefreshResult{}, err
		}
	}

	return result, nil
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
		if err = s.connectionStore.UpdateStatus(ctx, connectionID, string(ConnectionStatusDisconnected), reason); err != nil {
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

	resolution, err := s.resolveConnection(ctx, req.ProviderID, req.Scope)
	if err != nil {
		err = s.mapError(err)
		return CapabilityResult{}, err
	}
	if resolution.Outcome == ConnectionResolutionNotFound {
		result = CapabilityResult{
			Allowed: false,
			Mode:    CapabilityDeniedBehaviorBlock,
			Reason:  resolution.Reason,
		}
		return result, nil
	}

	decision := PermissionDecision{
		Allowed:    true,
		Capability: req.Capability,
		Mode:       descriptor.DeniedBehavior,
	}
	if s.permissionEvaluator != nil {
		decision, err = s.permissionEvaluator.EvaluateCapability(ctx, resolution.Connection.ID, req.Capability)
		if err != nil {
			err = s.mapError(err)
			return CapabilityResult{}, err
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

	result = CapabilityResult{
		Allowed:    decision.Allowed,
		Mode:       decision.Mode,
		Reason:     decision.Reason,
		Connection: resolution.Connection,
		Metadata:   metadata,
	}
	return result, nil
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

func resolveExternalAccountID(providerID string, scope ScopeRef, externalAccountID string) string {
	resolved := strings.TrimSpace(externalAccountID)
	if resolved != "" {
		return resolved
	}
	return fmt.Sprintf(
		"%s:%s:%s",
		strings.TrimSpace(providerID),
		strings.TrimSpace(scope.Type),
		strings.TrimSpace(scope.ID),
	)
}

func (s *Service) credentialToActive(ctx context.Context, credential Credential) (ActiveCredential, error) {
	active := ActiveCredential{
		ConnectionID: credential.ConnectionID,
	}
	if len(credential.EncryptedPayload) > 0 {
		if s == nil || s.secretProvider == nil {
			return ActiveCredential{}, fmt.Errorf("core: secret provider is required to decrypt credential payloads")
		}
		decrypted, err := s.secretProvider.Decrypt(ctx, credential.EncryptedPayload)
		if err != nil {
			return ActiveCredential{}, fmt.Errorf("core: decrypt credential payload: %w", err)
		}
		codec, codecErr := s.codecForCredential(credential)
		if codecErr != nil {
			return ActiveCredential{}, codecErr
		}
		decoded, decodeErr := codec.Decode(decrypted)
		if decodeErr != nil {
			return ActiveCredential{}, decodeErr
		}
		active = decoded
	}
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
	return active, nil
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
