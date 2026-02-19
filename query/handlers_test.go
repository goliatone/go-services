package query

import (
	"context"
	"fmt"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestLoadSyncCursorQuery_QueryDelegates(t *testing.T) {
	expected := core.SyncCursor{
		ConnectionID: "conn_1",
		ResourceType: "drive.file",
		ResourceID:   "file_1",
		Cursor:       "cursor_2",
	}
	called := false
	reader := stubSyncCursorReader{
		loadFn: func(_ context.Context, connectionID string, resourceType string, resourceID string) (core.SyncCursor, error) {
			called = true
			if connectionID != "conn_1" || resourceType != "drive.file" || resourceID != "file_1" {
				t.Fatalf("unexpected load request: %q %q %q", connectionID, resourceType, resourceID)
			}
			return expected, nil
		},
	}

	qry := NewLoadSyncCursorQuery(reader)
	result, err := qry.Query(context.Background(), LoadSyncCursorMessage{
		ConnectionID: "conn_1",
		ResourceType: "drive.file",
		ResourceID:   "file_1",
	})
	if err != nil {
		t.Fatalf("query sync cursor: %v", err)
	}
	if !called {
		t.Fatalf("expected sync cursor reader invocation")
	}
	if result.Cursor != expected.Cursor {
		t.Fatalf("unexpected sync cursor result: %#v", result)
	}
}

func TestListServicesActivityQuery_QueryDelegates(t *testing.T) {
	expected := core.ServicesActivityPage{
		Items: []core.ServiceActivityEntry{
			{ID: "evt_1", Action: "connected", Channel: "services.lifecycle", Status: core.ServiceActivityStatusOK},
		},
		Page:    1,
		PerPage: 20,
		Total:   1,
	}
	called := false
	reader := stubServicesActivityReader{
		listFn: func(_ context.Context, filter core.ServicesActivityFilter) (core.ServicesActivityPage, error) {
			called = true
			if filter.ProviderID != "github" {
				t.Fatalf("unexpected filter provider: %q", filter.ProviderID)
			}
			return expected, nil
		},
	}

	qry := NewListServicesActivityQuery(reader)
	result, err := qry.Query(context.Background(), ListServicesActivityMessage{
		Filter: core.ServicesActivityFilter{ProviderID: "github", Page: 1, PerPage: 20},
	})
	if err != nil {
		t.Fatalf("query services activity: %v", err)
	}
	if !called {
		t.Fatalf("expected activity reader invocation")
	}
	if result.Total != expected.Total {
		t.Fatalf("unexpected activity page result: %#v", result)
	}
}

func TestInstallationQueries_Delegate(t *testing.T) {
	calledGet := false
	calledList := false
	reader := stubInstallationReader{
		getFn: func(_ context.Context, id string) (core.Installation, error) {
			calledGet = true
			if id != "inst_1" {
				t.Fatalf("unexpected installation id %q", id)
			}
			return core.Installation{ID: id, ProviderID: "github"}, nil
		},
		listFn: func(_ context.Context, providerID string, scope core.ScopeRef) ([]core.Installation, error) {
			calledList = true
			if providerID != "github" || scope.Type != "org" || scope.ID != "org_1" {
				t.Fatalf("unexpected list input: %q %#v", providerID, scope)
			}
			return []core.Installation{{ID: "inst_1", ProviderID: "github"}}, nil
		},
	}

	getResult, err := NewGetInstallationQuery(reader).Query(context.Background(), GetInstallationMessage{
		InstallationID: "inst_1",
	})
	if err != nil {
		t.Fatalf("query installation: %v", err)
	}
	if !calledGet || getResult.ID != "inst_1" {
		t.Fatalf("expected get installation delegation")
	}

	listResult, err := NewListInstallationsQuery(reader).Query(context.Background(), ListInstallationsMessage{
		ProviderID: "github",
		Scope:      core.ScopeRef{Type: "org", ID: "org_1"},
	})
	if err != nil {
		t.Fatalf("list installations query: %v", err)
	}
	if !calledList || len(listResult) != 1 {
		t.Fatalf("expected list installation delegation")
	}
}

func TestGetSyncJobQuery_QueryDelegates(t *testing.T) {
	called := false
	reader := stubSyncJobReader{
		getFn: func(_ context.Context, req core.GetSyncJobRequest) (core.SyncJob, error) {
			called = true
			if req.SyncJobID != "job_1" || req.ScopeType != "org" || req.ScopeID != "org_1" {
				t.Fatalf("unexpected get sync job request: %#v", req)
			}
			return core.SyncJob{
				ID:         "job_1",
				ProviderID: "github",
				Mode:       core.SyncJobModeDelta,
				Status:     core.SyncJobStatusQueued,
			}, nil
		},
	}

	result, err := NewGetSyncJobQuery(reader).Query(context.Background(), GetSyncJobMessage{
		Request: core.GetSyncJobRequest{
			SyncJobID: "job_1",
			ScopeType: "org",
			ScopeID:   "org_1",
		},
	})
	if err != nil {
		t.Fatalf("query get sync job: %v", err)
	}
	if !called {
		t.Fatalf("expected get sync job reader invocation")
	}
	if result.ID != "job_1" {
		t.Fatalf("unexpected get sync job result: %#v", result)
	}
}

func TestQueryMessageValidation(t *testing.T) {
	tests := []struct {
		name    string
		msg     interface{ Validate() error }
		wantErr bool
	}{
		{
			name: "get sync job valid",
			msg: GetSyncJobMessage{Request: core.GetSyncJobRequest{
				SyncJobID: "job_1",
				ScopeType: "org",
				ScopeID:   "org_1",
			}},
			wantErr: false,
		},
		{
			name:    "get sync job missing id",
			msg:     GetSyncJobMessage{Request: core.GetSyncJobRequest{}},
			wantErr: true,
		},
		{
			name: "get sync job scope guard incomplete",
			msg: GetSyncJobMessage{Request: core.GetSyncJobRequest{
				SyncJobID: "job_1",
				ScopeType: "org",
			}},
			wantErr: true,
		},
		{
			name: "load sync cursor valid",
			msg: LoadSyncCursorMessage{
				ConnectionID: "conn_1",
				ResourceType: "drive.file",
				ResourceID:   "file_1",
			},
			wantErr: false,
		},
		{
			name: "load sync cursor missing connection",
			msg: LoadSyncCursorMessage{
				ResourceType: "drive.file",
				ResourceID:   "file_1",
			},
			wantErr: true,
		},
		{
			name: "activity list invalid page",
			msg: ListServicesActivityMessage{Filter: core.ServicesActivityFilter{
				Page: -1,
			}},
			wantErr: true,
		},
		{
			name: "activity list valid",
			msg: ListServicesActivityMessage{Filter: core.ServicesActivityFilter{
				Page:    1,
				PerPage: 50,
			}},
			wantErr: false,
		},
		{
			name:    "get installation missing id",
			msg:     GetInstallationMessage{},
			wantErr: true,
		},
		{
			name: "list installations valid",
			msg: ListInstallationsMessage{
				ProviderID: "github",
				Scope:      core.ScopeRef{Type: "org", ID: "org_1"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type stubSyncCursorReader struct {
	loadFn func(
		ctx context.Context,
		connectionID string,
		resourceType string,
		resourceID string,
	) (core.SyncCursor, error)
}

func (s stubSyncCursorReader) LoadSyncCursor(
	ctx context.Context,
	connectionID string,
	resourceType string,
	resourceID string,
) (core.SyncCursor, error) {
	if s.loadFn == nil {
		return core.SyncCursor{}, fmt.Errorf("load sync cursor not configured")
	}
	return s.loadFn(ctx, connectionID, resourceType, resourceID)
}

type stubServicesActivityReader struct {
	listFn func(ctx context.Context, filter core.ServicesActivityFilter) (core.ServicesActivityPage, error)
}

func (s stubServicesActivityReader) List(
	ctx context.Context,
	filter core.ServicesActivityFilter,
) (core.ServicesActivityPage, error) {
	if s.listFn == nil {
		return core.ServicesActivityPage{}, fmt.Errorf("list services activity not configured")
	}
	return s.listFn(ctx, filter)
}

type stubInstallationReader struct {
	getFn  func(ctx context.Context, id string) (core.Installation, error)
	listFn func(ctx context.Context, providerID string, scope core.ScopeRef) ([]core.Installation, error)
}

func (s stubInstallationReader) GetInstallation(ctx context.Context, id string) (core.Installation, error) {
	if s.getFn == nil {
		return core.Installation{}, fmt.Errorf("get installation not configured")
	}
	return s.getFn(ctx, id)
}

func (s stubInstallationReader) ListInstallations(
	ctx context.Context,
	providerID string,
	scope core.ScopeRef,
) ([]core.Installation, error) {
	if s.listFn == nil {
		return nil, fmt.Errorf("list installations not configured")
	}
	return s.listFn(ctx, providerID, scope)
}

type stubSyncJobReader struct {
	getFn func(ctx context.Context, req core.GetSyncJobRequest) (core.SyncJob, error)
}

func (s stubSyncJobReader) GetSyncJob(ctx context.Context, req core.GetSyncJobRequest) (core.SyncJob, error) {
	if s.getFn == nil {
		return core.SyncJob{}, fmt.Errorf("get sync job not configured")
	}
	return s.getFn(ctx, req)
}

var (
	_ SyncCursorReader       = stubSyncCursorReader{}
	_ ServicesActivityReader = stubServicesActivityReader{}
	_ InstallationReader     = stubInstallationReader{}
	_ SyncJobReader          = stubSyncJobReader{}
)
