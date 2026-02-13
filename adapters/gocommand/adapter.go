package gocommand

import (
	"context"
	"fmt"
	"strings"

	"github.com/goliatone/go-command"
	commanddispatcher "github.com/goliatone/go-command/dispatcher"
	"github.com/goliatone/go-command/runner"
	jobqueuecommand "github.com/goliatone/go-job/queue/command"
)

// ValidateMessageContract enforces Type() plus optional Validate() contract.
func ValidateMessageContract(msg any) error {
	if err := command.ValidateMessage(msg); err != nil {
		return err
	}
	m, ok := msg.(command.Message)
	if !ok {
		return fmt.Errorf("gocommand: message must implement Type() string")
	}
	if strings.TrimSpace(m.Type()) == "" {
		return fmt.Errorf("gocommand: message type is required")
	}
	return nil
}

type RegistryAdapter struct {
	registry *command.Registry
}

func NewRegistryAdapter(registry *command.Registry) *RegistryAdapter {
	if registry == nil {
		registry = command.NewRegistry()
	}
	return &RegistryAdapter{registry: registry}
}

func (a *RegistryAdapter) Registry() *command.Registry {
	if a == nil {
		return nil
	}
	return a.registry
}

func (a *RegistryAdapter) RegisterCommand(cmd any) error {
	if a == nil || a.registry == nil {
		return fmt.Errorf("gocommand: registry is not configured")
	}
	return a.registry.RegisterCommand(cmd)
}

func (a *RegistryAdapter) RegisterQuery(qry any) error {
	if a == nil || a.registry == nil {
		return fmt.Errorf("gocommand: registry is not configured")
	}
	return a.registry.RegisterCommand(qry)
}

func (a *RegistryAdapter) AddResolver(key string, resolver command.Resolver) error {
	if a == nil || a.registry == nil {
		return fmt.Errorf("gocommand: registry is not configured")
	}
	return a.registry.AddResolver(strings.TrimSpace(key), resolver)
}

func (a *RegistryAdapter) AddQueueResolver(key string, queueRegistry *jobqueuecommand.Registry) error {
	if queueRegistry == nil {
		return fmt.Errorf("gocommand: queue registry is required")
	}
	return a.AddResolver(key, jobqueuecommand.QueueResolver(queueRegistry))
}

func (a *RegistryAdapter) HasResolver(key string) bool {
	if a == nil || a.registry == nil {
		return false
	}
	return a.registry.HasResolver(strings.TrimSpace(key))
}

func (a *RegistryAdapter) Initialize() error {
	if a == nil || a.registry == nil {
		return fmt.Errorf("gocommand: registry is not configured")
	}
	return a.registry.Initialize()
}

func SubscribeCommand[T any](cmd command.Commander[T], runnerOpts ...runner.Option) commanddispatcher.Subscription {
	return commanddispatcher.SubscribeCommand(cmd, runnerOpts...)
}

func SubscribeCommandFunc[T any](handler command.CommandFunc[T], runnerOpts ...runner.Option) commanddispatcher.Subscription {
	return commanddispatcher.SubscribeCommand(handler, runnerOpts...)
}

func SubscribeQuery[T any, R any](qry command.Querier[T, R], runnerOpts ...runner.Option) commanddispatcher.Subscription {
	return commanddispatcher.SubscribeQuery(qry, runnerOpts...)
}

func SubscribeQueryFunc[T any, R any](qry command.QueryFunc[T, R], runnerOpts ...runner.Option) commanddispatcher.Subscription {
	return commanddispatcher.SubscribeQuery(qry, runnerOpts...)
}

func Dispatch[T any](ctx context.Context, msg T) error {
	return commanddispatcher.Dispatch(ctx, msg)
}

func Query[T any, R any](ctx context.Context, msg T) (R, error) {
	return commanddispatcher.Query[T, R](ctx, msg)
}

func RegisterAndSubscribe[T any](
	adapter *RegistryAdapter,
	cmd command.Commander[T],
	runnerOpts ...runner.Option,
) (commanddispatcher.Subscription, error) {
	if adapter == nil || adapter.registry == nil {
		return nil, fmt.Errorf("gocommand: registry is not configured")
	}
	if cmd == nil {
		return nil, fmt.Errorf("gocommand: command is required")
	}
	subscription := SubscribeCommand(cmd, runnerOpts...)
	if err := adapter.RegisterCommand(cmd); err != nil {
		if subscription != nil {
			subscription.Unsubscribe()
		}
		return nil, err
	}
	return subscription, nil
}

func RegisterAndSubscribeQuery[T any, R any](
	adapter *RegistryAdapter,
	qry command.Querier[T, R],
	runnerOpts ...runner.Option,
) (commanddispatcher.Subscription, error) {
	if adapter == nil || adapter.registry == nil {
		return nil, fmt.Errorf("gocommand: registry is not configured")
	}
	if qry == nil {
		return nil, fmt.Errorf("gocommand: query is required")
	}
	subscription := SubscribeQuery(qry, runnerOpts...)
	if err := adapter.RegisterQuery(qry); err != nil {
		if subscription != nil {
			subscription.Unsubscribe()
		}
		return nil, err
	}
	return subscription, nil
}
