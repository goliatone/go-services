package sqlstore

import (
	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/ratelimit"
	servicesync "github.com/goliatone/go-services/sync"
)

var (
	_ core.ConnectionStore            = (*ConnectionStore)(nil)
	_ core.CredentialStore            = (*CredentialStore)(nil)
	_ core.SubscriptionStore          = (*SubscriptionStore)(nil)
	_ core.SyncCursorStore            = (*SyncCursorStore)(nil)
	_ core.InstallationStore          = (*InstallationStore)(nil)
	_ ratelimit.StateStore            = (*RateLimitStateStore)(nil)
	_ core.GrantStore                 = (*GrantStore)(nil)
	_ core.OutboxStore                = (*OutboxStore)(nil)
	_ core.NotificationDispatchLedger = (*NotificationDispatchStore)(nil)
	_ core.ServicesActivitySink       = (*ActivityStore)(nil)
	_ core.ActivityRetentionPruner    = (*ActivityStore)(nil)
	_ servicesync.SyncJobStore        = (*SyncJobStore)(nil)
	_ core.StoreProvider              = (*RepositoryFactory)(nil)
	_ core.RepositoryStoreFactory     = (*RepositoryFactory)(nil)
)
