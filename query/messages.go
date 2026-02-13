package query

import (
	"fmt"
	"strings"

	"github.com/goliatone/go-services/core"
)

const (
	TypeLoadSyncCursor       = "services.query.sync_cursor.load"
	TypeListServicesActivity = "services.query.activity.list"
)

type LoadSyncCursorMessage struct {
	ConnectionID string
	ResourceType string
	ResourceID   string
}

func (LoadSyncCursorMessage) Type() string { return TypeLoadSyncCursor }

func (m LoadSyncCursorMessage) Validate() error {
	if strings.TrimSpace(m.ConnectionID) == "" {
		return fmt.Errorf("query: connection id is required")
	}
	if strings.TrimSpace(m.ResourceType) == "" {
		return fmt.Errorf("query: resource type is required")
	}
	if strings.TrimSpace(m.ResourceID) == "" {
		return fmt.Errorf("query: resource id is required")
	}
	return nil
}

type ListServicesActivityMessage struct {
	Filter core.ServicesActivityFilter
}

func (ListServicesActivityMessage) Type() string { return TypeListServicesActivity }

func (m ListServicesActivityMessage) Validate() error {
	if m.Filter.Page < 0 {
		return fmt.Errorf("query: page must be >= 0")
	}
	if m.Filter.PerPage < 0 {
		return fmt.Errorf("query: per_page must be >= 0")
	}
	return nil
}
