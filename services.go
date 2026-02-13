package services

import "github.com/goliatone/go-services/core"

type Config = core.Config

type InheritanceConfig = core.InheritanceConfig

type Option = core.Option

type Service = core.Service

type ServiceDependencies = core.ServiceDependencies

type ConnectRequest = core.ConnectRequest

type CompleteAuthRequest = core.CompleteAuthRequest

type RefreshRequest = core.RefreshRequest

type InvokeCapabilityRequest = core.InvokeCapabilityRequest

var (
	WithLogger            = core.WithLogger
	WithLoggerProvider    = core.WithLoggerProvider
	WithErrorFactory      = core.WithErrorFactory
	WithErrorMapper       = core.WithErrorMapper
	WithPersistenceClient = core.WithPersistenceClient
	WithRepositoryFactory = core.WithRepositoryFactory
	WithConfigProvider    = core.WithConfigProvider
	WithOptionsResolver   = core.WithOptionsResolver
	WithInheritancePolicy = core.WithInheritancePolicy
	WithRegistry          = core.WithRegistry
	WithConnectionStore   = core.WithConnectionStore
	WithCredentialStore   = core.WithCredentialStore
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
