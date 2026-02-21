package command

import (
	"strings"

	"github.com/goliatone/go-services/core"
)

const (
	TypeConnect            = "services.command.connect"
	TypeStartReconsent     = "services.command.reconsent.start"
	TypeCompleteReconsent  = "services.command.reconsent.complete"
	TypeCompleteCallback   = "services.command.callback.complete"
	TypeRefresh            = "services.command.refresh"
	TypeRevoke             = "services.command.revoke"
	TypeInvokeCapability   = "services.command.capability.invoke"
	TypeSubscribe          = "services.command.subscription.subscribe"
	TypeRenewSubscription  = "services.command.subscription.renew"
	TypeCancelSubscription = "services.command.subscription.cancel"
	TypeAdvanceSyncCursor  = "services.command.sync_cursor.advance"
	TypeUpsertInstallation = "services.command.installation.upsert"
	TypeUpdateInstallation = "services.command.installation.update_status"
	TypeCreateSyncJob      = "services.command.sync_job.create"
)

type ConnectMessage struct {
	Request core.ConnectRequest
}

func (ConnectMessage) Type() string { return TypeConnect }

func (m ConnectMessage) Validate() error {
	if strings.TrimSpace(m.Request.ProviderID) == "" {
		return commandValidationError("provider_id", "provider id is required")
	}
	if err := validateScope(m.Request.Scope); err != nil {
		return err
	}
	return nil
}

type StartReconsentMessage struct {
	Request core.ReconsentRequest
}

func (StartReconsentMessage) Type() string { return TypeStartReconsent }

func (m StartReconsentMessage) Validate() error {
	if strings.TrimSpace(m.Request.ConnectionID) == "" {
		return commandValidationError("connection_id", "connection id is required")
	}
	return nil
}

type CompleteReconsentMessage struct {
	Request core.CompleteAuthRequest
}

func (CompleteReconsentMessage) Type() string { return TypeCompleteReconsent }

func (m CompleteReconsentMessage) Validate() error {
	if strings.TrimSpace(m.Request.ProviderID) == "" {
		return commandValidationError("provider_id", "provider id is required")
	}
	if err := validateScope(m.Request.Scope); err != nil {
		return err
	}
	return nil
}

type CompleteCallbackMessage struct {
	Request core.CompleteAuthRequest
}

func (CompleteCallbackMessage) Type() string { return TypeCompleteCallback }

func (m CompleteCallbackMessage) Validate() error {
	if strings.TrimSpace(m.Request.ProviderID) == "" {
		return commandValidationError("provider_id", "provider id is required")
	}
	if err := validateScope(m.Request.Scope); err != nil {
		return err
	}
	return nil
}

type RefreshMessage struct {
	Request core.RefreshRequest
}

func (RefreshMessage) Type() string { return TypeRefresh }

func (m RefreshMessage) Validate() error {
	if strings.TrimSpace(m.Request.ProviderID) == "" {
		return commandValidationError("provider_id", "provider id is required")
	}
	if strings.TrimSpace(m.Request.ConnectionID) == "" {
		return commandValidationError("connection_id", "connection id is required")
	}
	return nil
}

type RevokeMessage struct {
	ConnectionID string
	Reason       string
}

func (RevokeMessage) Type() string { return TypeRevoke }

func (m RevokeMessage) Validate() error {
	if strings.TrimSpace(m.ConnectionID) == "" {
		return commandValidationError("connection_id", "connection id is required")
	}
	return nil
}

type InvokeCapabilityMessage struct {
	Request core.InvokeCapabilityRequest
}

func (InvokeCapabilityMessage) Type() string { return TypeInvokeCapability }

func (m InvokeCapabilityMessage) Validate() error {
	if strings.TrimSpace(m.Request.ProviderID) == "" {
		return commandValidationError("provider_id", "provider id is required")
	}
	if strings.TrimSpace(m.Request.Capability) == "" {
		return commandValidationError("capability", "capability is required")
	}
	if strings.TrimSpace(m.Request.ConnectionID) == "" {
		if err := validateScope(m.Request.Scope); err != nil {
			return err
		}
	}
	return nil
}

type SubscribeMessage struct {
	Request core.SubscribeRequest
}

func (SubscribeMessage) Type() string { return TypeSubscribe }

func (m SubscribeMessage) Validate() error {
	if strings.TrimSpace(m.Request.ConnectionID) == "" {
		return commandValidationError("connection_id", "connection id is required")
	}
	if strings.TrimSpace(m.Request.ResourceType) == "" {
		return commandValidationError("resource_type", "resource type is required")
	}
	if strings.TrimSpace(m.Request.ResourceID) == "" {
		return commandValidationError("resource_id", "resource id is required")
	}
	if strings.TrimSpace(m.Request.CallbackURL) == "" {
		return commandValidationError("callback_url", "callback url is required")
	}
	return nil
}

type RenewSubscriptionMessage struct {
	Request core.RenewSubscriptionRequest
}

func (RenewSubscriptionMessage) Type() string { return TypeRenewSubscription }

func (m RenewSubscriptionMessage) Validate() error {
	if strings.TrimSpace(m.Request.SubscriptionID) == "" {
		return commandValidationError("subscription_id", "subscription id is required")
	}
	return nil
}

type CancelSubscriptionMessage struct {
	Request core.CancelSubscriptionRequest
}

func (CancelSubscriptionMessage) Type() string { return TypeCancelSubscription }

func (m CancelSubscriptionMessage) Validate() error {
	if strings.TrimSpace(m.Request.SubscriptionID) == "" {
		return commandValidationError("subscription_id", "subscription id is required")
	}
	return nil
}

type AdvanceSyncCursorMessage struct {
	Input core.AdvanceSyncCursorInput
}

func (AdvanceSyncCursorMessage) Type() string { return TypeAdvanceSyncCursor }

func (m AdvanceSyncCursorMessage) Validate() error {
	if strings.TrimSpace(m.Input.ConnectionID) == "" {
		return commandValidationError("connection_id", "connection id is required")
	}
	if strings.TrimSpace(m.Input.ProviderID) == "" {
		return commandValidationError("provider_id", "provider id is required")
	}
	if strings.TrimSpace(m.Input.ResourceType) == "" {
		return commandValidationError("resource_type", "resource type is required")
	}
	if strings.TrimSpace(m.Input.ResourceID) == "" {
		return commandValidationError("resource_id", "resource id is required")
	}
	if strings.TrimSpace(m.Input.Cursor) == "" {
		return commandValidationError("cursor", "cursor is required")
	}
	return nil
}

type UpsertInstallationMessage struct {
	Input core.UpsertInstallationInput
}

func (UpsertInstallationMessage) Type() string { return TypeUpsertInstallation }

func (m UpsertInstallationMessage) Validate() error {
	if strings.TrimSpace(m.Input.ProviderID) == "" {
		return commandValidationError("provider_id", "provider id is required")
	}
	if err := validateScope(m.Input.Scope); err != nil {
		return err
	}
	if strings.TrimSpace(m.Input.InstallType) == "" {
		return commandValidationError("install_type", "install type is required")
	}
	return nil
}

type UpdateInstallationStatusMessage struct {
	InstallationID string
	Status         core.InstallationStatus
	Reason         string
}

func (UpdateInstallationStatusMessage) Type() string { return TypeUpdateInstallation }

func (m UpdateInstallationStatusMessage) Validate() error {
	if strings.TrimSpace(m.InstallationID) == "" {
		return commandValidationError("installation_id", "installation id is required")
	}
	if strings.TrimSpace(string(m.Status)) == "" {
		return commandValidationError("status", "installation status is required")
	}
	return nil
}

type CreateSyncJobMessage struct {
	Request core.CreateSyncJobRequest
}

func (CreateSyncJobMessage) Type() string { return TypeCreateSyncJob }

func (m CreateSyncJobMessage) Validate() error {
	if strings.TrimSpace(m.Request.ProviderID) == "" {
		return commandValidationError("provider_id", "provider id is required")
	}
	scope := core.ScopeRef{
		Type: strings.TrimSpace(strings.ToLower(m.Request.ScopeType)),
		ID:   strings.TrimSpace(m.Request.ScopeID),
	}
	if err := validateScope(scope); err != nil {
		return err
	}
	mode := strings.TrimSpace(strings.ToLower(string(m.Request.Mode)))
	if mode != string(core.SyncJobModeFull) && mode != string(core.SyncJobModeDelta) {
		return commandInvalidInputError("command: sync job mode must be full or delta")
	}
	return nil
}

func validateScope(scope core.ScopeRef) error {
	if err := scope.Validate(); err != nil {
		return commandWrapValidation(err, "command: invalid scope")
	}
	return nil
}
