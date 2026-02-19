package command

import (
	"context"
	"fmt"
	"testing"

	gocmd "github.com/goliatone/go-command"
	"github.com/goliatone/go-services/core"
)

func TestConnectCommand_ExecuteDelegatesAndStoresResult(t *testing.T) {
	expected := core.BeginAuthResponse{URL: "https://example.com/auth", State: "st"}
	called := false

	svc := stubMutatingService{
		connectFn: func(_ context.Context, req core.ConnectRequest) (core.BeginAuthResponse, error) {
			called = true
			if req.ProviderID != "github" {
				t.Fatalf("expected provider github, got %q", req.ProviderID)
			}
			return expected, nil
		},
	}

	cmd := NewConnectCommand(svc)
	collector := gocmd.NewResult[core.BeginAuthResponse]()
	ctx := gocmd.ContextWithResult(context.Background(), collector)

	err := cmd.Execute(ctx, ConnectMessage{Request: core.ConnectRequest{
		ProviderID: "github",
		Scope:      core.ScopeRef{Type: "user", ID: "u1"},
	}})
	if err != nil {
		t.Fatalf("execute connect: %v", err)
	}
	if !called {
		t.Fatalf("expected connect service invocation")
	}
	result, ok := collector.Load()
	if !ok {
		t.Fatalf("expected result to be stored")
	}
	if result.URL != expected.URL || result.State != expected.State {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestMutationCommands_DelegateToService(t *testing.T) {
	t.Run("revoke", func(t *testing.T) {
		called := false
		svc := stubMutatingService{
			revokeFn: func(_ context.Context, connectionID string, reason string) error {
				called = true
				if connectionID != "conn_1" || reason != "manual" {
					t.Fatalf("unexpected revoke payload: %q %q", connectionID, reason)
				}
				return nil
			},
		}
		cmd := NewRevokeCommand(svc)
		if err := cmd.Execute(context.Background(), RevokeMessage{ConnectionID: "conn_1", Reason: "manual"}); err != nil {
			t.Fatalf("execute revoke: %v", err)
		}
		if !called {
			t.Fatalf("expected revoke invocation")
		}
	})

	t.Run("advance sync cursor", func(t *testing.T) {
		expected := core.SyncCursor{ConnectionID: "conn_1", ResourceType: "drive.file", ResourceID: "file_1", Cursor: "c2"}
		called := false
		svc := stubMutatingService{
			advanceSyncCursorFn: func(_ context.Context, in core.AdvanceSyncCursorInput) (core.SyncCursor, error) {
				called = true
				if in.ConnectionID != "conn_1" || in.Cursor != "c2" {
					t.Fatalf("unexpected sync cursor input: %#v", in)
				}
				return expected, nil
			},
		}
		cmd := NewAdvanceSyncCursorCommand(svc)
		collector := gocmd.NewResult[core.SyncCursor]()
		ctx := gocmd.ContextWithResult(context.Background(), collector)
		err := cmd.Execute(ctx, AdvanceSyncCursorMessage{Input: core.AdvanceSyncCursorInput{
			ConnectionID: "conn_1",
			ProviderID:   "google_drive",
			ResourceType: "drive.file",
			ResourceID:   "file_1",
			Cursor:       "c2",
		}})
		if err != nil {
			t.Fatalf("execute advance sync cursor: %v", err)
		}
		if !called {
			t.Fatalf("expected advance sync cursor invocation")
		}
		stored, ok := collector.Load()
		if !ok {
			t.Fatalf("expected sync cursor result")
		}
		if stored.Cursor != expected.Cursor {
			t.Fatalf("unexpected cursor result: %#v", stored)
		}
	})

	t.Run("subscription commands", func(t *testing.T) {
		sub := core.Subscription{ID: "sub_1", ConnectionID: "conn_1", ResourceType: "calendar.event", ResourceID: "cal_1"}
		calledSubscribe := false
		calledRenew := false
		calledCancel := false
		svc := stubMutatingService{
			subscribeFn: func(_ context.Context, req core.SubscribeRequest) (core.Subscription, error) {
				calledSubscribe = true
				if req.CallbackURL == "" {
					t.Fatalf("expected callback url")
				}
				return sub, nil
			},
			renewSubscriptionFn: func(_ context.Context, req core.RenewSubscriptionRequest) (core.Subscription, error) {
				calledRenew = true
				if req.SubscriptionID != sub.ID {
					t.Fatalf("unexpected renew id: %q", req.SubscriptionID)
				}
				return sub, nil
			},
			cancelSubscriptionFn: func(_ context.Context, req core.CancelSubscriptionRequest) error {
				calledCancel = true
				if req.SubscriptionID != sub.ID {
					t.Fatalf("unexpected cancel id: %q", req.SubscriptionID)
				}
				return nil
			},
		}

		subscribeCollector := gocmd.NewResult[core.Subscription]()
		subscribeCtx := gocmd.ContextWithResult(context.Background(), subscribeCollector)
		if err := NewSubscribeCommand(svc).Execute(subscribeCtx, SubscribeMessage{Request: core.SubscribeRequest{
			ConnectionID: "conn_1",
			ResourceType: "calendar.event",
			ResourceID:   "cal_1",
			CallbackURL:  "https://example.com/hooks",
		}}); err != nil {
			t.Fatalf("execute subscribe: %v", err)
		}
		if !calledSubscribe {
			t.Fatalf("expected subscribe invocation")
		}
		if _, ok := subscribeCollector.Load(); !ok {
			t.Fatalf("expected subscribe result")
		}

		renewCollector := gocmd.NewResult[core.Subscription]()
		renewCtx := gocmd.ContextWithResult(context.Background(), renewCollector)
		if err := NewRenewSubscriptionCommand(svc).Execute(renewCtx, RenewSubscriptionMessage{
			Request: core.RenewSubscriptionRequest{SubscriptionID: sub.ID},
		}); err != nil {
			t.Fatalf("execute renew subscription: %v", err)
		}
		if !calledRenew {
			t.Fatalf("expected renew invocation")
		}
		if _, ok := renewCollector.Load(); !ok {
			t.Fatalf("expected renew result")
		}

		if err := NewCancelSubscriptionCommand(svc).Execute(context.Background(), CancelSubscriptionMessage{
			Request: core.CancelSubscriptionRequest{SubscriptionID: sub.ID, Reason: "cleanup"},
		}); err != nil {
			t.Fatalf("execute cancel subscription: %v", err)
		}
		if !calledCancel {
			t.Fatalf("expected cancel invocation")
		}
	})

	t.Run("installation commands", func(t *testing.T) {
		calledUpsert := false
		calledUpdate := false
		svc := stubMutatingService{
			upsertInstallationFn: func(_ context.Context, in core.UpsertInstallationInput) (core.Installation, error) {
				calledUpsert = true
				if in.ProviderID != "github" || in.InstallType != "marketplace_app" {
					t.Fatalf("unexpected installation input: %#v", in)
				}
				return core.Installation{ID: "inst_1", ProviderID: "github"}, nil
			},
			updateInstallationStatusFn: func(
				_ context.Context,
				id string,
				status core.InstallationStatus,
				reason string,
			) error {
				calledUpdate = true
				if id != "inst_1" || status != core.InstallationStatusSuspended || reason != "policy" {
					t.Fatalf("unexpected installation status payload: %q %q %q", id, status, reason)
				}
				return nil
			},
		}

		upsertCollector := gocmd.NewResult[core.Installation]()
		upsertCtx := gocmd.ContextWithResult(context.Background(), upsertCollector)
		if err := NewUpsertInstallationCommand(svc).Execute(upsertCtx, UpsertInstallationMessage{
			Input: core.UpsertInstallationInput{
				ProviderID:  "github",
				Scope:       core.ScopeRef{Type: "org", ID: "org_1"},
				InstallType: "marketplace_app",
				Status:      core.InstallationStatusActive,
			},
		}); err != nil {
			t.Fatalf("execute upsert installation: %v", err)
		}
		if !calledUpsert {
			t.Fatalf("expected upsert installation invocation")
		}
		if _, ok := upsertCollector.Load(); !ok {
			t.Fatalf("expected upsert installation result")
		}

		if err := NewUpdateInstallationStatusCommand(svc).Execute(context.Background(), UpdateInstallationStatusMessage{
			InstallationID: "inst_1",
			Status:         core.InstallationStatusSuspended,
			Reason:         "policy",
		}); err != nil {
			t.Fatalf("execute update installation status: %v", err)
		}
		if !calledUpdate {
			t.Fatalf("expected update installation invocation")
		}
	})

	t.Run("create sync job", func(t *testing.T) {
		called := false
		svc := stubMutatingService{
			createSyncJobFn: func(_ context.Context, req core.CreateSyncJobRequest) (core.CreateSyncJobResult, error) {
				called = true
				if req.ProviderID != "github" || req.ScopeType != "org" || req.ScopeID != "org_1" {
					t.Fatalf("unexpected create sync job request: %#v", req)
				}
				return core.CreateSyncJobResult{
					Job: core.SyncJob{
						ID:         "job_1",
						ProviderID: req.ProviderID,
						Mode:       core.SyncJobModeFull,
						Status:     core.SyncJobStatusQueued,
					},
					Created: true,
				}, nil
			},
		}

		cmd := NewCreateSyncJobCommand(svc)
		collector := gocmd.NewResult[core.CreateSyncJobResult]()
		ctx := gocmd.ContextWithResult(context.Background(), collector)
		if err := cmd.Execute(ctx, CreateSyncJobMessage{
			Request: core.CreateSyncJobRequest{
				ProviderID: "github",
				ScopeType:  "org",
				ScopeID:    "org_1",
				Mode:       core.SyncJobModeFull,
			},
		}); err != nil {
			t.Fatalf("execute create sync job: %v", err)
		}
		if !called {
			t.Fatalf("expected create sync job invocation")
		}
		stored, ok := collector.Load()
		if !ok {
			t.Fatalf("expected create sync job result")
		}
		if !stored.Created || stored.Job.ID != "job_1" {
			t.Fatalf("unexpected create sync job result: %#v", stored)
		}
	})

	t.Run("callback and refresh commands", func(t *testing.T) {
		calledCallback := false
		calledCompleteReconsent := false
		calledStartReconsent := false
		calledRefresh := false
		calledCapability := false

		svc := stubMutatingService{
			startReconsentFn: func(_ context.Context, req core.ReconsentRequest) (core.BeginAuthResponse, error) {
				calledStartReconsent = true
				if req.ConnectionID == "" {
					t.Fatalf("expected connection id")
				}
				return core.BeginAuthResponse{URL: "https://example.com/reconsent"}, nil
			},
			completeReconsentFn: func(_ context.Context, req core.CompleteAuthRequest) (core.CallbackCompletion, error) {
				calledCompleteReconsent = true
				return core.CallbackCompletion{Connection: core.Connection{ID: "conn_1"}}, nil
			},
			completeCallbackFn: func(_ context.Context, req core.CompleteAuthRequest) (core.CallbackCompletion, error) {
				calledCallback = true
				if req.ProviderID != "github" {
					t.Fatalf("unexpected callback provider %q", req.ProviderID)
				}
				return core.CallbackCompletion{Connection: core.Connection{ID: "conn_1"}}, nil
			},
			refreshFn: func(_ context.Context, req core.RefreshRequest) (core.RefreshResult, error) {
				calledRefresh = true
				if req.ConnectionID == "" {
					t.Fatalf("expected connection id")
				}
				return core.RefreshResult{Credential: core.ActiveCredential{ConnectionID: req.ConnectionID}}, nil
			},
			invokeCapabilityFn: func(_ context.Context, req core.InvokeCapabilityRequest) (core.CapabilityResult, error) {
				calledCapability = true
				if req.Capability != "issues.list" {
					t.Fatalf("unexpected capability %q", req.Capability)
				}
				return core.CapabilityResult{Allowed: true}, nil
			},
		}

		startCollector := gocmd.NewResult[core.BeginAuthResponse]()
		startCtx := gocmd.ContextWithResult(context.Background(), startCollector)
		if err := NewStartReconsentCommand(svc).Execute(startCtx, StartReconsentMessage{Request: core.ReconsentRequest{ConnectionID: "conn_1"}}); err != nil {
			t.Fatalf("execute start reconsent: %v", err)
		}
		if !calledStartReconsent {
			t.Fatalf("expected start reconsent invocation")
		}

		completeCollector := gocmd.NewResult[core.CallbackCompletion]()
		completeCtx := gocmd.ContextWithResult(context.Background(), completeCollector)
		if err := NewCompleteReconsentCommand(svc).Execute(completeCtx, CompleteReconsentMessage{Request: core.CompleteAuthRequest{ProviderID: "github", Scope: core.ScopeRef{Type: "user", ID: "u1"}}}); err != nil {
			t.Fatalf("execute complete reconsent: %v", err)
		}
		if !calledCompleteReconsent {
			t.Fatalf("expected complete reconsent invocation")
		}

		callbackCollector := gocmd.NewResult[core.CallbackCompletion]()
		callbackCtx := gocmd.ContextWithResult(context.Background(), callbackCollector)
		if err := NewCompleteCallbackCommand(svc).Execute(callbackCtx, CompleteCallbackMessage{Request: core.CompleteAuthRequest{ProviderID: "github", Scope: core.ScopeRef{Type: "user", ID: "u1"}}}); err != nil {
			t.Fatalf("execute complete callback: %v", err)
		}
		if !calledCallback {
			t.Fatalf("expected complete callback invocation")
		}

		refreshCollector := gocmd.NewResult[core.RefreshResult]()
		refreshCtx := gocmd.ContextWithResult(context.Background(), refreshCollector)
		if err := NewRefreshCommand(svc).Execute(refreshCtx, RefreshMessage{Request: core.RefreshRequest{ProviderID: "github", ConnectionID: "conn_1"}}); err != nil {
			t.Fatalf("execute refresh: %v", err)
		}
		if !calledRefresh {
			t.Fatalf("expected refresh invocation")
		}

		capabilityCollector := gocmd.NewResult[core.CapabilityResult]()
		capabilityCtx := gocmd.ContextWithResult(context.Background(), capabilityCollector)
		if err := NewInvokeCapabilityCommand(svc).Execute(capabilityCtx, InvokeCapabilityMessage{Request: core.InvokeCapabilityRequest{ProviderID: "github", Scope: core.ScopeRef{Type: "user", ID: "u1"}, Capability: "issues.list"}}); err != nil {
			t.Fatalf("execute invoke capability: %v", err)
		}
		if !calledCapability {
			t.Fatalf("expected invoke capability invocation")
		}
	})
}

func TestMessageValidation(t *testing.T) {
	tests := []struct {
		name    string
		msg     interface{ Validate() error }
		wantErr bool
	}{
		{
			name: "create sync job valid",
			msg: CreateSyncJobMessage{Request: core.CreateSyncJobRequest{
				ProviderID: "github",
				ScopeType:  "org",
				ScopeID:    "org_1",
				Mode:       core.SyncJobModeDelta,
			}},
			wantErr: false,
		},
		{
			name: "create sync job invalid mode",
			msg: CreateSyncJobMessage{Request: core.CreateSyncJobRequest{
				ProviderID: "github",
				ScopeType:  "org",
				ScopeID:    "org_1",
				Mode:       core.SyncJobModeBootstrap,
			}},
			wantErr: true,
		},
		{
			name: "connect valid",
			msg: ConnectMessage{Request: core.ConnectRequest{
				ProviderID: "github",
				Scope:      core.ScopeRef{Type: "user", ID: "u1"},
			}},
			wantErr: false,
		},
		{
			name: "connect missing provider",
			msg: ConnectMessage{Request: core.ConnectRequest{
				Scope: core.ScopeRef{Type: "user", ID: "u1"},
			}},
			wantErr: true,
		},
		{
			name:    "refresh missing connection",
			msg:     RefreshMessage{Request: core.RefreshRequest{ProviderID: "github"}},
			wantErr: true,
		},
		{
			name:    "revoke missing connection",
			msg:     RevokeMessage{},
			wantErr: true,
		},
		{
			name: "advance sync cursor valid",
			msg: AdvanceSyncCursorMessage{Input: core.AdvanceSyncCursorInput{
				ConnectionID: "conn_1",
				ProviderID:   "google_drive",
				ResourceType: "drive.file",
				ResourceID:   "file_1",
				Cursor:       "c2",
			}},
			wantErr: false,
		},
		{
			name: "upsert installation valid",
			msg: UpsertInstallationMessage{Input: core.UpsertInstallationInput{
				ProviderID:  "github",
				Scope:       core.ScopeRef{Type: "org", ID: "org_1"},
				InstallType: "marketplace_app",
			}},
			wantErr: false,
		},
		{
			name:    "update installation missing id",
			msg:     UpdateInstallationStatusMessage{Status: core.InstallationStatusActive},
			wantErr: true,
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

type stubMutatingService struct {
	connectFn                  func(ctx context.Context, req core.ConnectRequest) (core.BeginAuthResponse, error)
	startReconsentFn           func(ctx context.Context, req core.ReconsentRequest) (core.BeginAuthResponse, error)
	completeReconsentFn        func(ctx context.Context, req core.CompleteAuthRequest) (core.CallbackCompletion, error)
	completeCallbackFn         func(ctx context.Context, req core.CompleteAuthRequest) (core.CallbackCompletion, error)
	refreshFn                  func(ctx context.Context, req core.RefreshRequest) (core.RefreshResult, error)
	revokeFn                   func(ctx context.Context, connectionID string, reason string) error
	invokeCapabilityFn         func(ctx context.Context, req core.InvokeCapabilityRequest) (core.CapabilityResult, error)
	subscribeFn                func(ctx context.Context, req core.SubscribeRequest) (core.Subscription, error)
	renewSubscriptionFn        func(ctx context.Context, req core.RenewSubscriptionRequest) (core.Subscription, error)
	cancelSubscriptionFn       func(ctx context.Context, req core.CancelSubscriptionRequest) error
	advanceSyncCursorFn        func(ctx context.Context, in core.AdvanceSyncCursorInput) (core.SyncCursor, error)
	upsertInstallationFn       func(ctx context.Context, in core.UpsertInstallationInput) (core.Installation, error)
	updateInstallationStatusFn func(ctx context.Context, id string, status core.InstallationStatus, reason string) error
	createSyncJobFn            func(ctx context.Context, req core.CreateSyncJobRequest) (core.CreateSyncJobResult, error)
}

func (s stubMutatingService) Connect(ctx context.Context, req core.ConnectRequest) (core.BeginAuthResponse, error) {
	if s.connectFn == nil {
		return core.BeginAuthResponse{}, fmt.Errorf("connect not configured")
	}
	return s.connectFn(ctx, req)
}

func (s stubMutatingService) StartReconsent(ctx context.Context, req core.ReconsentRequest) (core.BeginAuthResponse, error) {
	if s.startReconsentFn == nil {
		return core.BeginAuthResponse{}, fmt.Errorf("start reconsent not configured")
	}
	return s.startReconsentFn(ctx, req)
}

func (s stubMutatingService) CompleteReconsent(ctx context.Context, req core.CompleteAuthRequest) (core.CallbackCompletion, error) {
	if s.completeReconsentFn == nil {
		return core.CallbackCompletion{}, fmt.Errorf("complete reconsent not configured")
	}
	return s.completeReconsentFn(ctx, req)
}

func (s stubMutatingService) CompleteCallback(ctx context.Context, req core.CompleteAuthRequest) (core.CallbackCompletion, error) {
	if s.completeCallbackFn == nil {
		return core.CallbackCompletion{}, fmt.Errorf("complete callback not configured")
	}
	return s.completeCallbackFn(ctx, req)
}

func (s stubMutatingService) Refresh(ctx context.Context, req core.RefreshRequest) (core.RefreshResult, error) {
	if s.refreshFn == nil {
		return core.RefreshResult{}, fmt.Errorf("refresh not configured")
	}
	return s.refreshFn(ctx, req)
}

func (s stubMutatingService) Revoke(ctx context.Context, connectionID string, reason string) error {
	if s.revokeFn == nil {
		return fmt.Errorf("revoke not configured")
	}
	return s.revokeFn(ctx, connectionID, reason)
}

func (s stubMutatingService) InvokeCapability(ctx context.Context, req core.InvokeCapabilityRequest) (core.CapabilityResult, error) {
	if s.invokeCapabilityFn == nil {
		return core.CapabilityResult{}, fmt.Errorf("invoke capability not configured")
	}
	return s.invokeCapabilityFn(ctx, req)
}

func (s stubMutatingService) Subscribe(ctx context.Context, req core.SubscribeRequest) (core.Subscription, error) {
	if s.subscribeFn == nil {
		return core.Subscription{}, fmt.Errorf("subscribe not configured")
	}
	return s.subscribeFn(ctx, req)
}

func (s stubMutatingService) RenewSubscription(ctx context.Context, req core.RenewSubscriptionRequest) (core.Subscription, error) {
	if s.renewSubscriptionFn == nil {
		return core.Subscription{}, fmt.Errorf("renew subscription not configured")
	}
	return s.renewSubscriptionFn(ctx, req)
}

func (s stubMutatingService) CancelSubscription(ctx context.Context, req core.CancelSubscriptionRequest) error {
	if s.cancelSubscriptionFn == nil {
		return fmt.Errorf("cancel subscription not configured")
	}
	return s.cancelSubscriptionFn(ctx, req)
}

func (s stubMutatingService) AdvanceSyncCursor(ctx context.Context, in core.AdvanceSyncCursorInput) (core.SyncCursor, error) {
	if s.advanceSyncCursorFn == nil {
		return core.SyncCursor{}, fmt.Errorf("advance sync cursor not configured")
	}
	return s.advanceSyncCursorFn(ctx, in)
}

func (s stubMutatingService) UpsertInstallation(ctx context.Context, in core.UpsertInstallationInput) (core.Installation, error) {
	if s.upsertInstallationFn == nil {
		return core.Installation{}, fmt.Errorf("upsert installation not configured")
	}
	return s.upsertInstallationFn(ctx, in)
}

func (s stubMutatingService) UpdateInstallationStatus(
	ctx context.Context,
	id string,
	status core.InstallationStatus,
	reason string,
) error {
	if s.updateInstallationStatusFn == nil {
		return fmt.Errorf("update installation status not configured")
	}
	return s.updateInstallationStatusFn(ctx, id, status, reason)
}

func (s stubMutatingService) CreateSyncJob(
	ctx context.Context,
	req core.CreateSyncJobRequest,
) (core.CreateSyncJobResult, error) {
	if s.createSyncJobFn == nil {
		return core.CreateSyncJobResult{}, fmt.Errorf("create sync job not configured")
	}
	return s.createSyncJobFn(ctx, req)
}

var _ MutatingService = stubMutatingService{}
