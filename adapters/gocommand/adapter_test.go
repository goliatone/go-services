package gocommand

import (
	"context"
	"errors"
	"testing"

	"github.com/goliatone/go-command"
	jobqueuecommand "github.com/goliatone/go-job/queue/command"
)

type okMessage struct{}

func (okMessage) Type() string { return "services.command.ok" }

type invalidMessage struct{}

func (invalidMessage) Type() string { return "" }

type failingMessage struct{}

func (failingMessage) Type() string { return "services.command.fail" }

func (failingMessage) Validate() error { return errors.New("invalid payload") }

type dispatchMessage struct {
	ID string
}

func (dispatchMessage) Type() string { return "services.command.test" }

type queueMessage struct{}

func (queueMessage) Type() string { return "services.command.queue" }

func TestValidateMessageContract(t *testing.T) {
	if err := ValidateMessageContract(okMessage{}); err != nil {
		t.Fatalf("expected valid message, got %v", err)
	}
	if err := ValidateMessageContract(invalidMessage{}); err == nil {
		t.Fatalf("expected empty type to fail contract validation")
	}
	if err := ValidateMessageContract(failingMessage{}); err == nil {
		t.Fatalf("expected Validate() failure to bubble")
	}
}

func TestRegistryAndDispatchWiring(t *testing.T) {
	adapter := NewRegistryAdapter(command.NewRegistry())
	executed := 0
	customResolverCalled := 0

	cmd := command.CommandFunc[dispatchMessage](func(context.Context, dispatchMessage) error {
		executed++
		return nil
	})

	if _, err := RegisterAndSubscribe(adapter, cmd); err != nil {
		t.Fatalf("register and subscribe: %v", err)
	}
	if err := adapter.AddResolver("custom", func(any, command.CommandMeta, *command.Registry) error {
		customResolverCalled++
		return nil
	}); err != nil {
		t.Fatalf("add resolver: %v", err)
	}
	if !adapter.HasResolver("custom") {
		t.Fatalf("expected custom resolver to be registered")
	}
	if err := adapter.Initialize(); err != nil {
		t.Fatalf("initialize registry: %v", err)
	}
	if customResolverCalled == 0 {
		t.Fatalf("expected resolver hook to run during initialization")
	}

	if err := Dispatch(context.Background(), dispatchMessage{ID: "m1"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if executed != 1 {
		t.Fatalf("expected command execution count=1, got %d", executed)
	}
}

func TestQueueResolverHookWiring(t *testing.T) {
	adapter := NewRegistryAdapter(command.NewRegistry())
	queueRegistry := jobqueuecommand.NewRegistry()

	cmd := command.CommandFunc[queueMessage](func(context.Context, queueMessage) error { return nil })

	if err := adapter.AddQueueResolver("queue", queueRegistry); err != nil {
		t.Fatalf("add queue resolver: %v", err)
	}
	if err := adapter.RegisterCommand(cmd); err != nil {
		t.Fatalf("register command: %v", err)
	}
	if err := adapter.Initialize(); err != nil {
		t.Fatalf("initialize registry: %v", err)
	}

	if _, ok := queueRegistry.Get("services.command.queue"); !ok {
		t.Fatalf("expected command to be mirrored into queue registry")
	}
}
