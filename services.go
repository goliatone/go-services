package services

import "github.com/goliatone/go-services/core"

type Config = core.Config

type InheritanceConfig = core.InheritanceConfig

type Option = core.Option

type Service = core.Service

type ServiceDependencies = core.ServiceDependencies
type OAuthStateStore = core.OAuthStateStore
type ConnectionLocker = core.ConnectionLocker
type RefreshBackoffScheduler = core.RefreshBackoffScheduler
type RefreshRunOptions = core.RefreshRunOptions
type RefreshRunResult = core.RefreshRunResult
type GrantStore = core.GrantStore
type PermissionEvaluator = core.PermissionEvaluator
type Signer = core.Signer
type SubscriptionStore = core.SubscriptionStore
type SyncCursorStore = core.SyncCursorStore

type ConnectRequest = core.ConnectRequest
type ReconsentRequest = core.ReconsentRequest

type CompleteAuthRequest = core.CompleteAuthRequest

type RefreshRequest = core.RefreshRequest

type InvokeCapabilityRequest = core.InvokeCapabilityRequest

var (
	WithLogger                  = core.WithLogger
	WithLoggerProvider          = core.WithLoggerProvider
	WithErrorFactory            = core.WithErrorFactory
	WithErrorMapper             = core.WithErrorMapper
	WithPersistenceClient       = core.WithPersistenceClient
	WithRepositoryFactory       = core.WithRepositoryFactory
	WithConfigProvider          = core.WithConfigProvider
	WithOptionsResolver         = core.WithOptionsResolver
	WithOAuthStateStore         = core.WithOAuthStateStore
	WithConnectionLocker        = core.WithConnectionLocker
	WithRefreshBackoffScheduler = core.WithRefreshBackoffScheduler
	WithInheritancePolicy       = core.WithInheritancePolicy
	WithRegistry                = core.WithRegistry
	WithConnectionStore         = core.WithConnectionStore
	WithCredentialStore         = core.WithCredentialStore
	WithSubscriptionStore       = core.WithSubscriptionStore
	WithSyncCursorStore         = core.WithSyncCursorStore
	WithGrantStore              = core.WithGrantStore
	WithPermissionEvaluator     = core.WithPermissionEvaluator
	WithSigner                  = core.WithSigner
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
