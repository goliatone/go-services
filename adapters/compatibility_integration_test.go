package adapters_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/goliatone/go-command"
	job "github.com/goliatone/go-job"
	jobqueuecommand "github.com/goliatone/go-job/queue/command"
	glog "github.com/goliatone/go-logger/glog"
	"github.com/goliatone/go-services/adapters/gocommand"
	"github.com/goliatone/go-services/adapters/gojob"
	"github.com/goliatone/go-services/adapters/gologger"
	servicescommand "github.com/goliatone/go-services/command"
	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/inbound"
)

func TestRuntimeCompatibility_GoJobGoCommandGoLogger(t *testing.T) {
	ctx := context.Background()

	logger := &compatLogger{}
	provider := &compatProvider{logger: logger}

	_, _, jobProvider, jobLogger := gologger.ResolveForJob("services", provider, nil)
	if jobProvider == nil || jobLogger == nil {
		t.Fatalf("expected go-job logger bridges")
	}

	enqueueProbe := &compatEnqueuer{}
	enqueueAdapter := gojob.NewEnqueuerAdapter(enqueueProbe)
	if err := enqueueAdapter.Enqueue(ctx, &core.JobExecutionMessage{
		JobID:          gojob.JobIDRefresh,
		ScriptPath:     "services.refresh",
		Parameters:     map[string]any{"connection_id": "conn_1"},
		IdempotencyKey: "idem_1",
		DedupPolicy:    "drop",
	}); err != nil {
		t.Fatalf("enqueue via gojob adapter: %v", err)
	}
	if enqueueProbe.last == nil || enqueueProbe.last.JobID != gojob.JobIDRefresh {
		t.Fatalf("expected go-job message mapping through enqueuer adapter")
	}

	queueRegistry := jobqueuecommand.NewRegistry()
	commandAdapter := gocommand.NewRegistryAdapter(command.NewRegistry())
	if err := commandAdapter.AddQueueResolver("queue", queueRegistry); err != nil {
		t.Fatalf("add queue resolver: %v", err)
	}
	if err := commandAdapter.RegisterCommand(command.CommandFunc[compatMessage](func(context.Context, compatMessage) error {
		return nil
	})); err != nil {
		t.Fatalf("register command: %v", err)
	}
	if err := commandAdapter.Initialize(); err != nil {
		t.Fatalf("initialize command registry: %v", err)
	}
	if _, ok := queueRegistry.Get("services.compat.command"); !ok {
		t.Fatalf("expected command resolver hook to mirror command into go-job queue registry")
	}
}

func TestRuntimeCompatibility_InboundCommandInteractionDispatchThroughWrappers(t *testing.T) {
	svc := &compatMutatingService{}
	adapter := gocommand.NewRegistryAdapter(command.NewRegistry())

	revokeSub, err := gocommand.RegisterAndSubscribe(adapter, servicescommand.NewRevokeCommand(svc))
	if err != nil {
		t.Fatalf("register revoke wrapper: %v", err)
	}
	defer revokeSub.Unsubscribe()

	capabilitySub, err := gocommand.RegisterAndSubscribe(adapter, servicescommand.NewInvokeCapabilityCommand(svc))
	if err != nil {
		t.Fatalf("register capability wrapper: %v", err)
	}
	defer capabilitySub.Unsubscribe()

	if err := adapter.Initialize(); err != nil {
		t.Fatalf("initialize adapter: %v", err)
	}

	dispatcher := inbound.NewDispatcher(nil, inbound.NewInMemoryClaimStore())
	commandHandler := &dispatchingInboundHandler{
		surface: inbound.SurfaceCommand,
		run: func(ctx context.Context, req core.InboundRequest) error {
			return gocommand.Dispatch(ctx, servicescommand.RevokeMessage{
				ConnectionID: metadataString(req.Metadata, "connection_id"),
				Reason:       metadataString(req.Metadata, "reason"),
			})
		},
	}
	interactionHandler := &dispatchingInboundHandler{
		surface: inbound.SurfaceInteraction,
		run: func(ctx context.Context, req core.InboundRequest) error {
			return gocommand.Dispatch(ctx, servicescommand.InvokeCapabilityMessage{
				Request: core.InvokeCapabilityRequest{
					ProviderID: metadataString(req.Metadata, "provider_id"),
					Scope: core.ScopeRef{
						Type: metadataString(req.Metadata, "scope_type"),
						ID:   metadataString(req.Metadata, "scope_id"),
					},
					Capability: metadataString(req.Metadata, "capability"),
				},
			})
		},
	}
	if err := dispatcher.Register(commandHandler); err != nil {
		t.Fatalf("register command inbound handler: %v", err)
	}
	if err := dispatcher.Register(interactionHandler); err != nil {
		t.Fatalf("register interaction inbound handler: %v", err)
	}

	commandResult, err := dispatcher.Dispatch(context.Background(), core.InboundRequest{
		ProviderID: "github",
		Surface:    inbound.SurfaceCommand,
		Metadata: map[string]any{
			"idempotency_key": "cmd-1",
			"connection_id":   "conn_1",
			"reason":          "manual",
		},
	})
	if err != nil {
		t.Fatalf("dispatch command inbound request: %v", err)
	}
	if !commandResult.Accepted {
		t.Fatalf("expected command inbound request accepted")
	}
	if svc.revokeCalls != 1 || svc.lastRevokeConnectionID != "conn_1" || svc.lastRevokeReason != "manual" {
		t.Fatalf("expected revoke wrapper invocation through inbound dispatch")
	}

	interactionResult, err := dispatcher.Dispatch(context.Background(), core.InboundRequest{
		ProviderID: "github",
		Surface:    inbound.SurfaceInteraction,
		Metadata: map[string]any{
			"idempotency_key": "int-1",
			"provider_id":     "github",
			"scope_type":      "user",
			"scope_id":        "u1",
			"capability":      "issues.list",
		},
	})
	if err != nil {
		t.Fatalf("dispatch interaction inbound request: %v", err)
	}
	if !interactionResult.Accepted {
		t.Fatalf("expected interaction inbound request accepted")
	}
	if svc.invokeCapabilityCalls != 1 || svc.lastCapability != "issues.list" {
		t.Fatalf("expected capability wrapper invocation through inbound dispatch")
	}
}

type compatMessage struct{}

func (compatMessage) Type() string { return "services.compat.command" }

type compatEnqueuer struct {
	last *job.ExecutionMessage
}

func (e *compatEnqueuer) Enqueue(_ context.Context, msg *job.ExecutionMessage) error {
	e.last = msg
	return nil
}

type compatProvider struct {
	logger glog.Logger
}

func (p *compatProvider) GetLogger(string) glog.Logger {
	if p == nil || p.logger == nil {
		return glog.Nop()
	}
	return p.logger
}

type compatLogger struct{}

func (compatLogger) Trace(string, ...any)                    {}
func (compatLogger) Debug(string, ...any)                    {}
func (compatLogger) Info(string, ...any)                     {}
func (compatLogger) Warn(string, ...any)                     {}
func (compatLogger) Error(string, ...any)                    {}
func (compatLogger) Fatal(string, ...any)                    {}
func (compatLogger) WithContext(context.Context) glog.Logger { return compatLogger{} }

type dispatchingInboundHandler struct {
	surface string
	run     func(ctx context.Context, req core.InboundRequest) error
}

func (h *dispatchingInboundHandler) Surface() string {
	return h.surface
}

func (h *dispatchingInboundHandler) Handle(ctx context.Context, req core.InboundRequest) (core.InboundResult, error) {
	if h == nil || h.run == nil {
		return core.InboundResult{}, fmt.Errorf("handler is not configured")
	}
	if err := h.run(ctx, req); err != nil {
		return core.InboundResult{Accepted: false, StatusCode: 500}, err
	}
	return core.InboundResult{Accepted: true, StatusCode: 202}, nil
}

type compatMutatingService struct {
	revokeCalls            int
	lastRevokeConnectionID string
	lastRevokeReason       string
	invokeCapabilityCalls  int
	lastCapability         string
}

func (s *compatMutatingService) Connect(context.Context, core.ConnectRequest) (core.BeginAuthResponse, error) {
	return core.BeginAuthResponse{}, nil
}

func (s *compatMutatingService) StartReconsent(context.Context, core.ReconsentRequest) (core.BeginAuthResponse, error) {
	return core.BeginAuthResponse{}, nil
}

func (s *compatMutatingService) CompleteReconsent(context.Context, core.CompleteAuthRequest) (core.CallbackCompletion, error) {
	return core.CallbackCompletion{}, nil
}

func (s *compatMutatingService) CompleteCallback(context.Context, core.CompleteAuthRequest) (core.CallbackCompletion, error) {
	return core.CallbackCompletion{}, nil
}

func (s *compatMutatingService) Refresh(context.Context, core.RefreshRequest) (core.RefreshResult, error) {
	return core.RefreshResult{}, nil
}

func (s *compatMutatingService) Revoke(_ context.Context, connectionID string, reason string) error {
	s.revokeCalls++
	s.lastRevokeConnectionID = connectionID
	s.lastRevokeReason = reason
	return nil
}

func (s *compatMutatingService) InvokeCapability(_ context.Context, req core.InvokeCapabilityRequest) (core.CapabilityResult, error) {
	s.invokeCapabilityCalls++
	s.lastCapability = req.Capability
	return core.CapabilityResult{Allowed: true}, nil
}

func (s *compatMutatingService) Subscribe(context.Context, core.SubscribeRequest) (core.Subscription, error) {
	return core.Subscription{}, nil
}

func (s *compatMutatingService) RenewSubscription(context.Context, core.RenewSubscriptionRequest) (core.Subscription, error) {
	return core.Subscription{}, nil
}

func (s *compatMutatingService) CancelSubscription(context.Context, core.CancelSubscriptionRequest) error {
	return nil
}

func (s *compatMutatingService) AdvanceSyncCursor(context.Context, core.AdvanceSyncCursorInput) (core.SyncCursor, error) {
	return core.SyncCursor{}, nil
}

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	raw, ok := metadata[key]
	if !ok {
		return ""
	}
	return fmt.Sprint(raw)
}
