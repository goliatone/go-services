package query

import (
	gocmd "github.com/goliatone/go-command"
	"github.com/goliatone/go-services/core"
)

var (
	_ gocmd.Querier[LoadSyncCursorMessage, core.SyncCursor]                 = (*LoadSyncCursorQuery)(nil)
	_ gocmd.Querier[ListServicesActivityMessage, core.ServicesActivityPage] = (*ListServicesActivityQuery)(nil)
	_ gocmd.Querier[GetInstallationMessage, core.Installation]              = (*GetInstallationQuery)(nil)
	_ gocmd.Querier[ListInstallationsMessage, []core.Installation]          = (*ListInstallationsQuery)(nil)
)
