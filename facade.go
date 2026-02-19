package services

import (
	"fmt"

	servicescommand "github.com/goliatone/go-services/command"
	servicesquery "github.com/goliatone/go-services/query"
)

type CommandQueryService interface {
	servicescommand.MutatingService
	servicesquery.SyncCursorReader
	servicesquery.InstallationReader
}

type Commands struct {
	Connect            *servicescommand.ConnectCommand
	StartReconsent     *servicescommand.StartReconsentCommand
	CompleteReconsent  *servicescommand.CompleteReconsentCommand
	CompleteCallback   *servicescommand.CompleteCallbackCommand
	Refresh            *servicescommand.RefreshCommand
	Revoke             *servicescommand.RevokeCommand
	InvokeCapability   *servicescommand.InvokeCapabilityCommand
	Subscribe          *servicescommand.SubscribeCommand
	RenewSubscription  *servicescommand.RenewSubscriptionCommand
	CancelSubscription *servicescommand.CancelSubscriptionCommand
	AdvanceSyncCursor  *servicescommand.AdvanceSyncCursorCommand
	UpsertInstallation *servicescommand.UpsertInstallationCommand
	UpdateInstallation *servicescommand.UpdateInstallationStatusCommand
	CreateSyncJob      *servicescommand.CreateSyncJobCommand
}

type Queries struct {
	LoadSyncCursor       *servicesquery.LoadSyncCursorQuery
	ListServicesActivity *servicesquery.ListServicesActivityQuery
	GetInstallation      *servicesquery.GetInstallationQuery
	ListInstallations    *servicesquery.ListInstallationsQuery
	GetSyncJob           *servicesquery.GetSyncJobQuery
}

type Facade struct {
	service  CommandQueryService
	commands Commands
	queries  Queries
}

type FacadeOption func(*facadeOptions)

type facadeOptions struct {
	activityReader servicesquery.ServicesActivityReader
}

func WithActivityReader(reader servicesquery.ServicesActivityReader) FacadeOption {
	return func(options *facadeOptions) {
		options.activityReader = reader
	}
}

func NewFacade(service CommandQueryService, opts ...FacadeOption) (*Facade, error) {
	if service == nil {
		return nil, fmt.Errorf("services: command/query service is required")
	}
	cfg := facadeOptions{}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&cfg)
	}

	reader := cfg.activityReader
	if reader == nil {
		return nil, fmt.Errorf("services: activity reader is required; use WithActivityReader(...)")
	}

	facade := &Facade{service: service}
	var createSyncJobCommand *servicescommand.CreateSyncJobCommand
	if syncJobService, ok := service.(servicescommand.SyncJobMutatingService); ok {
		createSyncJobCommand = servicescommand.NewCreateSyncJobCommand(syncJobService)
	}
	var getSyncJobQuery *servicesquery.GetSyncJobQuery
	if syncJobReader, ok := service.(servicesquery.SyncJobReader); ok {
		getSyncJobQuery = servicesquery.NewGetSyncJobQuery(syncJobReader)
	}
	facade.commands = Commands{
		Connect:            servicescommand.NewConnectCommand(service),
		StartReconsent:     servicescommand.NewStartReconsentCommand(service),
		CompleteReconsent:  servicescommand.NewCompleteReconsentCommand(service),
		CompleteCallback:   servicescommand.NewCompleteCallbackCommand(service),
		Refresh:            servicescommand.NewRefreshCommand(service),
		Revoke:             servicescommand.NewRevokeCommand(service),
		InvokeCapability:   servicescommand.NewInvokeCapabilityCommand(service),
		Subscribe:          servicescommand.NewSubscribeCommand(service),
		RenewSubscription:  servicescommand.NewRenewSubscriptionCommand(service),
		CancelSubscription: servicescommand.NewCancelSubscriptionCommand(service),
		AdvanceSyncCursor:  servicescommand.NewAdvanceSyncCursorCommand(service),
		UpsertInstallation: servicescommand.NewUpsertInstallationCommand(service),
		UpdateInstallation: servicescommand.NewUpdateInstallationStatusCommand(service),
		CreateSyncJob:      createSyncJobCommand,
	}
	facade.queries = Queries{
		LoadSyncCursor:       servicesquery.NewLoadSyncCursorQuery(service),
		ListServicesActivity: servicesquery.NewListServicesActivityQuery(reader),
		GetInstallation:      servicesquery.NewGetInstallationQuery(service),
		ListInstallations:    servicesquery.NewListInstallationsQuery(service),
		GetSyncJob:           getSyncJobQuery,
	}

	return facade, nil
}

func (f *Facade) Commands() Commands {
	if f == nil {
		return Commands{}
	}
	return f.commands
}

func (f *Facade) Queries() Queries {
	if f == nil {
		return Queries{}
	}
	return f.queries
}

func (f *Facade) Service() CommandQueryService {
	if f == nil {
		return nil
	}
	return f.service
}
