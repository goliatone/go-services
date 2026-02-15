package services

import (
	"fmt"
	"reflect"

	servicescommand "github.com/goliatone/go-services/command"
	"github.com/goliatone/go-services/core"
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
}

type Queries struct {
	LoadSyncCursor       *servicesquery.LoadSyncCursorQuery
	ListServicesActivity *servicesquery.ListServicesActivityQuery
	GetInstallation      *servicesquery.GetInstallationQuery
	ListInstallations    *servicesquery.ListInstallationsQuery
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
		reader = resolveActivityReader(service)
	}

	facade := &Facade{service: service}
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
	}
	facade.queries = Queries{
		LoadSyncCursor:       servicesquery.NewLoadSyncCursorQuery(service),
		ListServicesActivity: servicesquery.NewListServicesActivityQuery(reader),
		GetInstallation:      servicesquery.NewGetInstallationQuery(service),
		ListInstallations:    servicesquery.NewListInstallationsQuery(service),
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

func resolveActivityReader(service CommandQueryService) servicesquery.ServicesActivityReader {
	if service == nil {
		return nil
	}
	if reader, ok := service.(servicesquery.ServicesActivityReader); ok {
		return reader
	}
	provider, ok := service.(interface {
		Dependencies() core.ServiceDependencies
	})
	if !ok {
		return nil
	}
	deps := provider.Dependencies()
	if deps.RepositoryFactory == nil {
		return nil
	}

	factoryValue := reflect.ValueOf(deps.RepositoryFactory)
	if !factoryValue.IsValid() {
		return nil
	}
	if factoryValue.Kind() == reflect.Ptr && factoryValue.IsNil() {
		return nil
	}
	method := factoryValue.MethodByName("ActivityStore")
	if !method.IsValid() || method.Type().NumIn() != 0 || method.Type().NumOut() != 1 {
		return nil
	}

	results, ok := safeReflectCall(method)
	if !ok {
		return nil
	}
	if len(results) != 1 {
		return nil
	}
	candidate := results[0]
	if !candidate.IsValid() {
		return nil
	}
	if candidate.Kind() == reflect.Ptr && candidate.IsNil() {
		return nil
	}
	reader, ok := candidate.Interface().(servicesquery.ServicesActivityReader)
	if !ok {
		return nil
	}
	return reader
}

func safeReflectCall(method reflect.Value) (_ []reflect.Value, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return method.Call(nil), true
}
