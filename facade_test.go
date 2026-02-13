package services

import (
	"context"
	"testing"

	servicescommand "github.com/goliatone/go-services/command"
	"github.com/goliatone/go-services/core"
	servicesquery "github.com/goliatone/go-services/query"
)

func TestNewFacade_WiresCommandsAndQueries(t *testing.T) {
	svc := &stubFacadeService{}
	activityReader := &stubFacadeActivityReader{}

	facade, err := NewFacade(svc, WithActivityReader(activityReader))
	if err != nil {
		t.Fatalf("new facade: %v", err)
	}

	commands := facade.Commands()
	if commands.Connect == nil || commands.Refresh == nil || commands.AdvanceSyncCursor == nil {
		t.Fatalf("expected command handlers to be wired")
	}
	queries := facade.Queries()
	if queries.LoadSyncCursor == nil || queries.ListServicesActivity == nil {
		t.Fatalf("expected query handlers to be wired")
	}
}

func TestFacade_CommandAndQueryDelegation(t *testing.T) {
	svc := &stubFacadeService{}
	activityReader := &stubFacadeActivityReader{}

	facade, err := NewFacade(svc, WithActivityReader(activityReader))
	if err != nil {
		t.Fatalf("new facade: %v", err)
	}

	if err := facade.Commands().Revoke.Execute(context.Background(), servicescommand.RevokeMessage{
		ConnectionID: "conn_1",
		Reason:       "manual",
	}); err != nil {
		t.Fatalf("execute revoke command: %v", err)
	}
	if svc.lastRevokeConnectionID != "conn_1" || svc.lastRevokeReason != "manual" {
		t.Fatalf("unexpected revoke delegation payload")
	}

	cursor, err := facade.Queries().LoadSyncCursor.Query(context.Background(), servicesquery.LoadSyncCursorMessage{
		ConnectionID: "conn_1",
		ResourceType: "drive.file",
		ResourceID:   "file_1",
	})
	if err != nil {
		t.Fatalf("query load sync cursor: %v", err)
	}
	if cursor.ConnectionID != "conn_1" || cursor.Cursor != "cursor_1" {
		t.Fatalf("unexpected sync cursor query result: %#v", cursor)
	}

	page, err := facade.Queries().ListServicesActivity.Query(context.Background(), servicesquery.ListServicesActivityMessage{
		Filter: core.ServicesActivityFilter{ProviderID: "github", Page: 1, PerPage: 20},
	})
	if err != nil {
		t.Fatalf("query list services activity: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("unexpected activity page result: %#v", page)
	}
}

func TestNewFacade_RequiresService(t *testing.T) {
	facade, err := NewFacade(nil)
	if err == nil {
		t.Fatalf("expected nil service error")
	}
	if facade != nil {
		t.Fatalf("expected nil facade on error")
	}
}

type stubFacadeService struct {
	lastRevokeConnectionID string
	lastRevokeReason       string
}

func (s *stubFacadeService) Connect(context.Context, core.ConnectRequest) (core.BeginAuthResponse, error) {
	return core.BeginAuthResponse{URL: "https://example.com/auth", State: "state"}, nil
}

func (s *stubFacadeService) StartReconsent(context.Context, core.ReconsentRequest) (core.BeginAuthResponse, error) {
	return core.BeginAuthResponse{URL: "https://example.com/reconsent", State: "state"}, nil
}

func (s *stubFacadeService) CompleteReconsent(context.Context, core.CompleteAuthRequest) (core.CallbackCompletion, error) {
	return core.CallbackCompletion{Connection: core.Connection{ID: "conn_1"}}, nil
}

func (s *stubFacadeService) CompleteCallback(context.Context, core.CompleteAuthRequest) (core.CallbackCompletion, error) {
	return core.CallbackCompletion{Connection: core.Connection{ID: "conn_1"}}, nil
}

func (s *stubFacadeService) Refresh(context.Context, core.RefreshRequest) (core.RefreshResult, error) {
	return core.RefreshResult{Credential: core.ActiveCredential{ConnectionID: "conn_1"}}, nil
}

func (s *stubFacadeService) Revoke(_ context.Context, connectionID string, reason string) error {
	s.lastRevokeConnectionID = connectionID
	s.lastRevokeReason = reason
	return nil
}

func (s *stubFacadeService) InvokeCapability(context.Context, core.InvokeCapabilityRequest) (core.CapabilityResult, error) {
	return core.CapabilityResult{Allowed: true}, nil
}

func (s *stubFacadeService) Subscribe(context.Context, core.SubscribeRequest) (core.Subscription, error) {
	return core.Subscription{ID: "sub_1"}, nil
}

func (s *stubFacadeService) RenewSubscription(context.Context, core.RenewSubscriptionRequest) (core.Subscription, error) {
	return core.Subscription{ID: "sub_1"}, nil
}

func (s *stubFacadeService) CancelSubscription(context.Context, core.CancelSubscriptionRequest) error {
	return nil
}

func (s *stubFacadeService) AdvanceSyncCursor(context.Context, core.AdvanceSyncCursorInput) (core.SyncCursor, error) {
	return core.SyncCursor{ConnectionID: "conn_1", Cursor: "cursor_2"}, nil
}

func (s *stubFacadeService) LoadSyncCursor(
	context.Context,
	string,
	string,
	string,
) (core.SyncCursor, error) {
	return core.SyncCursor{ConnectionID: "conn_1", ResourceType: "drive.file", ResourceID: "file_1", Cursor: "cursor_1"}, nil
}

type stubFacadeActivityReader struct{}

func (s *stubFacadeActivityReader) List(context.Context, core.ServicesActivityFilter) (core.ServicesActivityPage, error) {
	return core.ServicesActivityPage{
		Items: []core.ServiceActivityEntry{{ID: "evt_1", Action: "connected", Status: core.ServiceActivityStatusOK}},
		Total: 1,
	}, nil
}

var _ CommandQueryService = (*stubFacadeService)(nil)
