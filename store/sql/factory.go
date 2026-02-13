package sqlstore

import (
	"fmt"

	persistence "github.com/goliatone/go-persistence-bun"
	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-services/core"
	"github.com/uptrace/bun"
)

type RepositoryFactory struct {
	db *bun.DB

	connectionStore           *ConnectionStore
	credentialStore           *CredentialStore
	subscriptionStore         *SubscriptionStore
	syncCursorStore           *SyncCursorStore
	syncJobStore              *SyncJobStore
	outboxStore               *OutboxStore
	notificationDispatchStore *NotificationDispatchStore
	activityStore             *ActivityStore
}

func NewRepositoryFactory() *RepositoryFactory {
	return &RepositoryFactory{}
}

func NewRepositoryFactoryFromPersistence(client *persistence.Client) (*RepositoryFactory, error) {
	factory := NewRepositoryFactory()
	if _, err := factory.BuildStores(client); err != nil {
		return nil, err
	}
	return factory, nil
}

func NewRepositoryFactoryFromDB(db *bun.DB) (*RepositoryFactory, error) {
	factory := NewRepositoryFactory()
	if _, err := factory.BuildStores(db); err != nil {
		return nil, err
	}
	return factory, nil
}

func (f *RepositoryFactory) BuildStores(persistenceClient any) (core.StoreProvider, error) {
	if f == nil {
		return nil, fmt.Errorf("sqlstore: repository factory is nil")
	}
	if f.db == nil {
		db, err := resolveBunDB(persistenceClient)
		if err != nil {
			return nil, err
		}
		f.db = db
	}
	if f.connectionStore != nil && f.credentialStore != nil {
		return f, nil
	}
	if err := f.initStores(); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *RepositoryFactory) ConnectionStore() core.ConnectionStore {
	if f == nil {
		return nil
	}
	return f.connectionStore
}

func (f *RepositoryFactory) CredentialStore() core.CredentialStore {
	if f == nil {
		return nil
	}
	return f.credentialStore
}

func (f *RepositoryFactory) DB() *bun.DB {
	if f == nil {
		return nil
	}
	return f.db
}

func (f *RepositoryFactory) SubscriptionStore() core.SubscriptionStore {
	if f == nil {
		return nil
	}
	return f.subscriptionStore
}

func (f *RepositoryFactory) SyncCursorStore() core.SyncCursorStore {
	if f == nil {
		return nil
	}
	return f.syncCursorStore
}

func (f *RepositoryFactory) SyncJobStore() *SyncJobStore {
	if f == nil {
		return nil
	}
	return f.syncJobStore
}

func (f *RepositoryFactory) OutboxStore() *OutboxStore {
	if f == nil {
		return nil
	}
	return f.outboxStore
}

func (f *RepositoryFactory) NotificationDispatchStore() *NotificationDispatchStore {
	if f == nil {
		return nil
	}
	return f.notificationDispatchStore
}

func (f *RepositoryFactory) ActivityStore() *ActivityStore {
	if f == nil {
		return nil
	}
	return f.activityStore
}

func (f *RepositoryFactory) initStores() error {
	connectionRepo := repository.NewRepository[*connectionRecord](f.db, connectionHandlers())
	if validator, ok := connectionRepo.(repository.Validator); ok {
		if err := validator.Validate(); err != nil {
			return fmt.Errorf("sqlstore: invalid connection repository wiring: %w", err)
		}
	}

	credentialRepo := repository.NewRepository[*credentialRecord](f.db, credentialHandlers())
	if validator, ok := credentialRepo.(repository.Validator); ok {
		if err := validator.Validate(); err != nil {
			return fmt.Errorf("sqlstore: invalid credential repository wiring: %w", err)
		}
	}

	f.connectionStore = &ConnectionStore{
		db:   f.db,
		repo: connectionRepo,
	}
	f.credentialStore = &CredentialStore{
		db:   f.db,
		repo: credentialRepo,
	}
	subscriptionStore, err := NewSubscriptionStore(f.db)
	if err != nil {
		return err
	}
	f.subscriptionStore = subscriptionStore
	syncCursorStore, err := NewSyncCursorStore(f.db)
	if err != nil {
		return err
	}
	f.syncCursorStore = syncCursorStore
	syncJobStore, err := NewSyncJobStore(f.db)
	if err != nil {
		return err
	}
	f.syncJobStore = syncJobStore
	outboxStore, err := NewOutboxStore(f.db)
	if err != nil {
		return err
	}
	f.outboxStore = outboxStore
	notificationDispatchStore, err := NewNotificationDispatchStore(f.db)
	if err != nil {
		return err
	}
	f.notificationDispatchStore = notificationDispatchStore
	activityStore, err := NewActivityStore(f.db)
	if err != nil {
		return err
	}
	f.activityStore = activityStore

	return nil
}

func resolveBunDB(candidate any) (*bun.DB, error) {
	switch typed := candidate.(type) {
	case nil:
		return nil, fmt.Errorf("sqlstore: persistence client is required")
	case *bun.DB:
		return typed, nil
	case interface{ DB() *bun.DB }:
		db := typed.DB()
		if db == nil {
			return nil, fmt.Errorf("sqlstore: persistence client returned nil bun db")
		}
		return db, nil
	default:
		return nil, fmt.Errorf("sqlstore: unsupported persistence client type %T", candidate)
	}
}
