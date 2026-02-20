package services

import "github.com/goliatone/go-services/core"

type Config = core.Config

type InheritanceConfig = core.InheritanceConfig

type Option = core.Option

type Service = core.Service

type ServiceDependencies = core.ServiceDependencies
type EmbeddedAuthService = core.EmbeddedAuthService
type OAuthStateStore = core.OAuthStateStore
type ConnectionLocker = core.ConnectionLocker
type RefreshBackoffScheduler = core.RefreshBackoffScheduler
type MetricsRecorder = core.MetricsRecorder
type SecretProvider = core.SecretProvider
type TransportResolver = core.TransportResolver
type RateLimitPolicy = core.RateLimitPolicy
type RefreshRunOptions = core.RefreshRunOptions
type RefreshRunResult = core.RefreshRunResult
type GrantStore = core.GrantStore
type GrantStoreTransactional = core.GrantStoreTransactional
type PermissionEvaluator = core.PermissionEvaluator
type Signer = core.Signer
type SubscriptionStore = core.SubscriptionStore
type SyncCursorStore = core.SyncCursorStore
type InstallationStore = core.InstallationStore
type SyncJobStore = core.SyncJobStore
type CredentialCodec = core.CredentialCodec
type IdempotencyClaimStore = core.IdempotencyClaimStore
type CallbackURLResolver = core.CallbackURLResolver
type CallbackURLResolverFunc = core.CallbackURLResolverFunc
type CallbackURLResolveRequest = core.CallbackURLResolveRequest
type CallbackURLResolveFlow = core.CallbackURLResolveFlow

type ConnectRequest = core.ConnectRequest
type ReconsentRequest = core.ReconsentRequest

type CompleteAuthRequest = core.CompleteAuthRequest
type EmbeddedAuthRequest = core.EmbeddedAuthRequest
type EmbeddedAuthResult = core.EmbeddedAuthResult
type EmbeddedSessionClaims = core.EmbeddedSessionClaims
type EmbeddedAccessToken = core.EmbeddedAccessToken
type EmbeddedRequestedTokenType = core.EmbeddedRequestedTokenType

type RefreshRequest = core.RefreshRequest

type InvokeCapabilityRequest = core.InvokeCapabilityRequest
type CreateSyncJobRequest = core.CreateSyncJobRequest
type CreateSyncJobResult = core.CreateSyncJobResult
type GetSyncJobRequest = core.GetSyncJobRequest

const (
	CallbackURLResolveFlowConnect     = core.CallbackURLResolveFlowConnect
	CallbackURLResolveFlowReconsent   = core.CallbackURLResolveFlowReconsent
	EmbeddedRequestedTokenTypeOffline = core.EmbeddedRequestedTokenTypeOffline
	EmbeddedRequestedTokenTypeOnline  = core.EmbeddedRequestedTokenTypeOnline
)

var (
	WithLogger                  = core.WithLogger
	WithLoggerProvider          = core.WithLoggerProvider
	WithMetricsRecorder         = core.WithMetricsRecorder
	WithErrorFactory            = core.WithErrorFactory
	WithErrorMapper             = core.WithErrorMapper
	WithSecretProvider          = core.WithSecretProvider
	WithPersistenceClient       = core.WithPersistenceClient
	WithRepositoryFactory       = core.WithRepositoryFactory
	WithConfigProvider          = core.WithConfigProvider
	WithOptionsResolver         = core.WithOptionsResolver
	WithOAuthStateStore         = core.WithOAuthStateStore
	WithConnectionLocker        = core.WithConnectionLocker
	WithRefreshBackoffScheduler = core.WithRefreshBackoffScheduler
	WithTransportResolver       = core.WithTransportResolver
	WithRateLimitPolicy         = core.WithRateLimitPolicy
	WithInheritancePolicy       = core.WithInheritancePolicy
	WithRegistry                = core.WithRegistry
	WithConnectionStore         = core.WithConnectionStore
	WithCredentialStore         = core.WithCredentialStore
	WithSubscriptionStore       = core.WithSubscriptionStore
	WithSyncCursorStore         = core.WithSyncCursorStore
	WithInstallationStore       = core.WithInstallationStore
	WithSyncJobStore            = core.WithSyncJobStore
	WithGrantStore              = core.WithGrantStore
	WithPermissionEvaluator     = core.WithPermissionEvaluator
	WithSigner                  = core.WithSigner
	WithCredentialCodec         = core.WithCredentialCodec
	WithCallbackURLResolver     = core.WithCallbackURLResolver
)

func DefaultConfig() Config {
	return core.DefaultConfig()
}

func NewService(cfg Config, opts ...Option) (*Service, error) {
	return core.NewService(cfg, opts...)
}

func Setup(cfg Config, opts ...Option) (*Service, error) {
	return core.Setup(cfg, opts...)
}
