package adapters_test

import (
	"context"
	"testing"

	"github.com/goliatone/go-command"
	job "github.com/goliatone/go-job"
	jobqueuecommand "github.com/goliatone/go-job/queue/command"
	glog "github.com/goliatone/go-logger/glog"
	"github.com/goliatone/go-services/adapters/gocommand"
	"github.com/goliatone/go-services/adapters/gojob"
	"github.com/goliatone/go-services/adapters/gologger"
	"github.com/goliatone/go-services/core"
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
