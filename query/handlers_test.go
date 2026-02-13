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

func TestQueryMessageValidation(t *testing.T) {
	tests := []struct {
		name    string
		msg     interface{ Validate() error }
		wantErr bool
	}{
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

var (
	_ SyncCursorReader       = stubSyncCursorReader{}
	_ ServicesActivityReader = stubServicesActivityReader{}
)
