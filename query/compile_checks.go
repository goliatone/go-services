package query

import (
	gocmd "github.com/goliatone/go-command"
	"github.com/goliatone/go-services/core"
)

var (
	_ gocmd.Querier[LoadSyncCursorMessage, core.SyncCursor]                 = (*LoadSyncCursorQuery)(nil)
	_ gocmd.Querier[ListServicesActivityMessage, core.ServicesActivityPage] = (*ListServicesActivityQuery)(nil)
)
