package query

import (
	"context"
	"fmt"

	"github.com/goliatone/go-services/core"
)

type SyncCursorReader interface {
	LoadSyncCursor(
		ctx context.Context,
		connectionID string,
		resourceType string,
		resourceID string,
	) (core.SyncCursor, error)
}

type ServicesActivityReader interface {
	List(ctx context.Context, filter core.ServicesActivityFilter) (core.ServicesActivityPage, error)
}

type InstallationReader interface {
	GetInstallation(ctx context.Context, id string) (core.Installation, error)
	ListInstallations(ctx context.Context, providerID string, scope core.ScopeRef) ([]core.Installation, error)
}

type LoadSyncCursorQuery struct {
	reader SyncCursorReader
}

func NewLoadSyncCursorQuery(reader SyncCursorReader) *LoadSyncCursorQuery {
	return &LoadSyncCursorQuery{reader: reader}
}

func (q *LoadSyncCursorQuery) Query(ctx context.Context, msg LoadSyncCursorMessage) (core.SyncCursor, error) {
	if q == nil || q.reader == nil {
		return core.SyncCursor{}, fmt.Errorf("query: sync cursor reader is required")
	}
	return q.reader.LoadSyncCursor(ctx, msg.ConnectionID, msg.ResourceType, msg.ResourceID)
}

type ListServicesActivityQuery struct {
	reader ServicesActivityReader
}

func NewListServicesActivityQuery(reader ServicesActivityReader) *ListServicesActivityQuery {
	return &ListServicesActivityQuery{reader: reader}
}

func (q *ListServicesActivityQuery) Query(
	ctx context.Context,
	msg ListServicesActivityMessage,
) (core.ServicesActivityPage, error) {
	if q == nil || q.reader == nil {
		return core.ServicesActivityPage{}, fmt.Errorf("query: services activity reader is required")
	}
	return q.reader.List(ctx, msg.Filter)
}

type GetInstallationQuery struct {
	reader InstallationReader
}

func NewGetInstallationQuery(reader InstallationReader) *GetInstallationQuery {
	return &GetInstallationQuery{reader: reader}
}

func (q *GetInstallationQuery) Query(ctx context.Context, msg GetInstallationMessage) (core.Installation, error) {
	if q == nil || q.reader == nil {
		return core.Installation{}, fmt.Errorf("query: installation reader is required")
	}
	return q.reader.GetInstallation(ctx, msg.InstallationID)
}

type ListInstallationsQuery struct {
	reader InstallationReader
}

func NewListInstallationsQuery(reader InstallationReader) *ListInstallationsQuery {
	return &ListInstallationsQuery{reader: reader}
}

func (q *ListInstallationsQuery) Query(
	ctx context.Context,
	msg ListInstallationsMessage,
) ([]core.Installation, error) {
	if q == nil || q.reader == nil {
		return nil, fmt.Errorf("query: installation reader is required")
	}
	return q.reader.ListInstallations(ctx, msg.ProviderID, msg.Scope)
}
