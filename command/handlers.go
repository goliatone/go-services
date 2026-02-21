package command

import (
	"context"

	gocmd "github.com/goliatone/go-command"
	"github.com/goliatone/go-services/core"
)

type MutatingService interface {
	Connect(ctx context.Context, req core.ConnectRequest) (core.BeginAuthResponse, error)
	StartReconsent(ctx context.Context, req core.ReconsentRequest) (core.BeginAuthResponse, error)
	CompleteReconsent(ctx context.Context, req core.CompleteAuthRequest) (core.CallbackCompletion, error)
	CompleteCallback(ctx context.Context, req core.CompleteAuthRequest) (core.CallbackCompletion, error)
	Refresh(ctx context.Context, req core.RefreshRequest) (core.RefreshResult, error)
	Revoke(ctx context.Context, connectionID string, reason string) error
	InvokeCapability(ctx context.Context, req core.InvokeCapabilityRequest) (core.CapabilityResult, error)
	Subscribe(ctx context.Context, req core.SubscribeRequest) (core.Subscription, error)
	RenewSubscription(ctx context.Context, req core.RenewSubscriptionRequest) (core.Subscription, error)
	CancelSubscription(ctx context.Context, req core.CancelSubscriptionRequest) error
	AdvanceSyncCursor(ctx context.Context, in core.AdvanceSyncCursorInput) (core.SyncCursor, error)
	UpsertInstallation(ctx context.Context, in core.UpsertInstallationInput) (core.Installation, error)
	UpdateInstallationStatus(ctx context.Context, id string, status core.InstallationStatus, reason string) error
}

type SyncJobMutatingService interface {
	CreateSyncJob(ctx context.Context, req core.CreateSyncJobRequest) (core.CreateSyncJobResult, error)
}

type ConnectCommand struct {
	service MutatingService
}

func NewConnectCommand(service MutatingService) *ConnectCommand {
	return &ConnectCommand{service: service}
}

func (c *ConnectCommand) Execute(ctx context.Context, msg ConnectMessage) error {
	if c == nil || c.service == nil {
		return commandDependencyError("command: connect service is required")
	}
	out, err := c.service.Connect(ctx, msg.Request)
	if err != nil {
		return err
	}
	storeResult(ctx, out)
	return nil
}

type StartReconsentCommand struct {
	service MutatingService
}

func NewStartReconsentCommand(service MutatingService) *StartReconsentCommand {
	return &StartReconsentCommand{service: service}
}

func (c *StartReconsentCommand) Execute(ctx context.Context, msg StartReconsentMessage) error {
	if c == nil || c.service == nil {
		return commandDependencyError("command: re-consent service is required")
	}
	out, err := c.service.StartReconsent(ctx, msg.Request)
	if err != nil {
		return err
	}
	storeResult(ctx, out)
	return nil
}

type CompleteReconsentCommand struct {
	service MutatingService
}

func NewCompleteReconsentCommand(service MutatingService) *CompleteReconsentCommand {
	return &CompleteReconsentCommand{service: service}
}

func (c *CompleteReconsentCommand) Execute(ctx context.Context, msg CompleteReconsentMessage) error {
	if c == nil || c.service == nil {
		return commandDependencyError("command: complete re-consent service is required")
	}
	out, err := c.service.CompleteReconsent(ctx, msg.Request)
	if err != nil {
		return err
	}
	storeResult(ctx, out)
	return nil
}

type CompleteCallbackCommand struct {
	service MutatingService
}

func NewCompleteCallbackCommand(service MutatingService) *CompleteCallbackCommand {
	return &CompleteCallbackCommand{service: service}
}

func (c *CompleteCallbackCommand) Execute(ctx context.Context, msg CompleteCallbackMessage) error {
	if c == nil || c.service == nil {
		return commandDependencyError("command: callback service is required")
	}
	out, err := c.service.CompleteCallback(ctx, msg.Request)
	if err != nil {
		return err
	}
	storeResult(ctx, out)
	return nil
}

type RefreshCommand struct {
	service MutatingService
}

func NewRefreshCommand(service MutatingService) *RefreshCommand {
	return &RefreshCommand{service: service}
}

func (c *RefreshCommand) Execute(ctx context.Context, msg RefreshMessage) error {
	if c == nil || c.service == nil {
		return commandDependencyError("command: refresh service is required")
	}
	out, err := c.service.Refresh(ctx, msg.Request)
	if err != nil {
		return err
	}
	storeResult(ctx, out)
	return nil
}

type RevokeCommand struct {
	service MutatingService
}

func NewRevokeCommand(service MutatingService) *RevokeCommand {
	return &RevokeCommand{service: service}
}

func (c *RevokeCommand) Execute(ctx context.Context, msg RevokeMessage) error {
	if c == nil || c.service == nil {
		return commandDependencyError("command: revoke service is required")
	}
	return c.service.Revoke(ctx, msg.ConnectionID, msg.Reason)
}

type InvokeCapabilityCommand struct {
	service MutatingService
}

func NewInvokeCapabilityCommand(service MutatingService) *InvokeCapabilityCommand {
	return &InvokeCapabilityCommand{service: service}
}

func (c *InvokeCapabilityCommand) Execute(ctx context.Context, msg InvokeCapabilityMessage) error {
	if c == nil || c.service == nil {
		return commandDependencyError("command: capability service is required")
	}
	out, err := c.service.InvokeCapability(ctx, msg.Request)
	if err != nil {
		return err
	}
	storeResult(ctx, out)
	return nil
}

type SubscribeCommand struct {
	service MutatingService
}

func NewSubscribeCommand(service MutatingService) *SubscribeCommand {
	return &SubscribeCommand{service: service}
}

func (c *SubscribeCommand) Execute(ctx context.Context, msg SubscribeMessage) error {
	if c == nil || c.service == nil {
		return commandDependencyError("command: subscribe service is required")
	}
	out, err := c.service.Subscribe(ctx, msg.Request)
	if err != nil {
		return err
	}
	storeResult(ctx, out)
	return nil
}

type RenewSubscriptionCommand struct {
	service MutatingService
}

func NewRenewSubscriptionCommand(service MutatingService) *RenewSubscriptionCommand {
	return &RenewSubscriptionCommand{service: service}
}

func (c *RenewSubscriptionCommand) Execute(ctx context.Context, msg RenewSubscriptionMessage) error {
	if c == nil || c.service == nil {
		return commandDependencyError("command: renew subscription service is required")
	}
	out, err := c.service.RenewSubscription(ctx, msg.Request)
	if err != nil {
		return err
	}
	storeResult(ctx, out)
	return nil
}

type CancelSubscriptionCommand struct {
	service MutatingService
}

func NewCancelSubscriptionCommand(service MutatingService) *CancelSubscriptionCommand {
	return &CancelSubscriptionCommand{service: service}
}

func (c *CancelSubscriptionCommand) Execute(ctx context.Context, msg CancelSubscriptionMessage) error {
	if c == nil || c.service == nil {
		return commandDependencyError("command: cancel subscription service is required")
	}
	return c.service.CancelSubscription(ctx, msg.Request)
}

type AdvanceSyncCursorCommand struct {
	service MutatingService
}

func NewAdvanceSyncCursorCommand(service MutatingService) *AdvanceSyncCursorCommand {
	return &AdvanceSyncCursorCommand{service: service}
}

func (c *AdvanceSyncCursorCommand) Execute(ctx context.Context, msg AdvanceSyncCursorMessage) error {
	if c == nil || c.service == nil {
		return commandDependencyError("command: sync cursor service is required")
	}
	out, err := c.service.AdvanceSyncCursor(ctx, msg.Input)
	if err != nil {
		return err
	}
	storeResult(ctx, out)
	return nil
}

type UpsertInstallationCommand struct {
	service MutatingService
}

func NewUpsertInstallationCommand(service MutatingService) *UpsertInstallationCommand {
	return &UpsertInstallationCommand{service: service}
}

func (c *UpsertInstallationCommand) Execute(ctx context.Context, msg UpsertInstallationMessage) error {
	if c == nil || c.service == nil {
		return commandDependencyError("command: installation service is required")
	}
	out, err := c.service.UpsertInstallation(ctx, msg.Input)
	if err != nil {
		return err
	}
	storeResult(ctx, out)
	return nil
}

type UpdateInstallationStatusCommand struct {
	service MutatingService
}

func NewUpdateInstallationStatusCommand(service MutatingService) *UpdateInstallationStatusCommand {
	return &UpdateInstallationStatusCommand{service: service}
}

func (c *UpdateInstallationStatusCommand) Execute(ctx context.Context, msg UpdateInstallationStatusMessage) error {
	if c == nil || c.service == nil {
		return commandDependencyError("command: installation service is required")
	}
	return c.service.UpdateInstallationStatus(ctx, msg.InstallationID, msg.Status, msg.Reason)
}

type CreateSyncJobCommand struct {
	service SyncJobMutatingService
}

func NewCreateSyncJobCommand(service SyncJobMutatingService) *CreateSyncJobCommand {
	return &CreateSyncJobCommand{service: service}
}

func (c *CreateSyncJobCommand) Execute(ctx context.Context, msg CreateSyncJobMessage) error {
	if c == nil || c.service == nil {
		return commandDependencyError("command: sync job service is required")
	}
	out, err := c.service.CreateSyncJob(ctx, msg.Request)
	if err != nil {
		return err
	}
	storeResult(ctx, out)
	return nil
}

func storeResult[T any](ctx context.Context, value T) {
	collector := gocmd.ResultFromContext[T](ctx)
	if collector == nil {
		return
	}
	collector.Store(value)
}
