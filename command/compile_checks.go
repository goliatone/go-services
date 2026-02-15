package command

import gocmd "github.com/goliatone/go-command"

var (
	_ gocmd.Commander[ConnectMessage]                  = (*ConnectCommand)(nil)
	_ gocmd.Commander[StartReconsentMessage]           = (*StartReconsentCommand)(nil)
	_ gocmd.Commander[CompleteReconsentMessage]        = (*CompleteReconsentCommand)(nil)
	_ gocmd.Commander[CompleteCallbackMessage]         = (*CompleteCallbackCommand)(nil)
	_ gocmd.Commander[RefreshMessage]                  = (*RefreshCommand)(nil)
	_ gocmd.Commander[RevokeMessage]                   = (*RevokeCommand)(nil)
	_ gocmd.Commander[InvokeCapabilityMessage]         = (*InvokeCapabilityCommand)(nil)
	_ gocmd.Commander[SubscribeMessage]                = (*SubscribeCommand)(nil)
	_ gocmd.Commander[RenewSubscriptionMessage]        = (*RenewSubscriptionCommand)(nil)
	_ gocmd.Commander[CancelSubscriptionMessage]       = (*CancelSubscriptionCommand)(nil)
	_ gocmd.Commander[AdvanceSyncCursorMessage]        = (*AdvanceSyncCursorCommand)(nil)
	_ gocmd.Commander[UpsertInstallationMessage]       = (*UpsertInstallationCommand)(nil)
	_ gocmd.Commander[UpdateInstallationStatusMessage] = (*UpdateInstallationStatusCommand)(nil)
)
