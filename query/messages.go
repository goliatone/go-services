package query

import (
	"fmt"
	"strings"

	"github.com/goliatone/go-services/core"
)

const (
	TypeLoadSyncCursor       = "services.query.sync_cursor.load"
	TypeListServicesActivity = "services.query.activity.list"
	TypeGetInstallation      = "services.query.installation.get"
	TypeListInstallations    = "services.query.installation.list_by_scope"
	TypeGetSyncJob           = "services.query.sync_job.get"
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

type GetInstallationMessage struct {
	InstallationID string
}

func (GetInstallationMessage) Type() string { return TypeGetInstallation }

func (m GetInstallationMessage) Validate() error {
	if strings.TrimSpace(m.InstallationID) == "" {
		return fmt.Errorf("query: installation id is required")
	}
	return nil
}

type ListInstallationsMessage struct {
	ProviderID string
	Scope      core.ScopeRef
}

func (ListInstallationsMessage) Type() string { return TypeListInstallations }

func (m ListInstallationsMessage) Validate() error {
	if strings.TrimSpace(m.ProviderID) == "" {
		return fmt.Errorf("query: provider id is required")
	}
	if err := m.Scope.Validate(); err != nil {
		return fmt.Errorf("query: %w", err)
	}
	return nil
}

type GetSyncJobMessage struct {
	Request core.GetSyncJobRequest
}

func (GetSyncJobMessage) Type() string { return TypeGetSyncJob }

func (m GetSyncJobMessage) Validate() error {
	if strings.TrimSpace(m.Request.SyncJobID) == "" {
		return fmt.Errorf("query: sync job id is required")
	}
	scopeType := strings.TrimSpace(strings.ToLower(m.Request.ScopeType))
	scopeID := strings.TrimSpace(m.Request.ScopeID)
	if (scopeType == "") != (scopeID == "") {
		return fmt.Errorf("query: scope type and scope id must both be provided")
	}
	if scopeType != "" {
		if err := (core.ScopeRef{Type: scopeType, ID: scopeID}).Validate(); err != nil {
			return fmt.Errorf("query: %w", err)
		}
	}
	return nil
}
