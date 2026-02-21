package query

import (
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
		return queryValidationError("connection_id", "connection id is required")
	}
	if strings.TrimSpace(m.ResourceType) == "" {
		return queryValidationError("resource_type", "resource type is required")
	}
	if strings.TrimSpace(m.ResourceID) == "" {
		return queryValidationError("resource_id", "resource id is required")
	}
	return nil
}

type ListServicesActivityMessage struct {
	Filter core.ServicesActivityFilter
}

func (ListServicesActivityMessage) Type() string { return TypeListServicesActivity }

func (m ListServicesActivityMessage) Validate() error {
	if m.Filter.Page < 0 {
		return queryInvalidInputError("query: page must be >= 0")
	}
	if m.Filter.PerPage < 0 {
		return queryInvalidInputError("query: per_page must be >= 0")
	}
	return nil
}

type GetInstallationMessage struct {
	InstallationID string
}

func (GetInstallationMessage) Type() string { return TypeGetInstallation }

func (m GetInstallationMessage) Validate() error {
	if strings.TrimSpace(m.InstallationID) == "" {
		return queryValidationError("installation_id", "installation id is required")
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
		return queryValidationError("provider_id", "provider id is required")
	}
	if err := m.Scope.Validate(); err != nil {
		return queryWrapValidation(err, "query: invalid scope")
	}
	return nil
}

type GetSyncJobMessage struct {
	Request core.GetSyncJobRequest
}

func (GetSyncJobMessage) Type() string { return TypeGetSyncJob }

func (m GetSyncJobMessage) Validate() error {
	if strings.TrimSpace(m.Request.SyncJobID) == "" {
		return queryValidationError("sync_job_id", "sync job id is required")
	}
	scopeType := strings.TrimSpace(strings.ToLower(m.Request.ScopeType))
	scopeID := strings.TrimSpace(m.Request.ScopeID)
	if (scopeType == "") != (scopeID == "") {
		return queryInvalidInputError("query: scope type and scope id must both be provided")
	}
	if scopeType != "" {
		if err := (core.ScopeRef{Type: scopeType, ID: scopeID}).Validate(); err != nil {
			return queryWrapValidation(err, "query: invalid scope")
		}
	}
	return nil
}
