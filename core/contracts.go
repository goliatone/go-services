package core

import (
	"context"
	"net/http"
	"time"

	glog "github.com/goliatone/go-logger/glog"
)

type CapabilityDeniedBehavior string

const (
	CapabilityDeniedBehaviorBlock   CapabilityDeniedBehavior = "block"
	CapabilityDeniedBehaviorDegrade CapabilityDeniedBehavior = "degrade"
)

type CapabilityDescriptor struct {
	Name           string
	RequiredGrants []string
	OptionalGrants []string
	DeniedBehavior CapabilityDeniedBehavior
}

type BeginAuthRequest struct {
	ProviderID      string
	Scope           ScopeRef
	RedirectURI     string
	State           string
	RequestedGrants []string
	Metadata        map[string]any
}

type BeginAuthResponse struct {
	URL             string
	State           string
	RequestedGrants []string
	Metadata        map[string]any
}

type CompleteAuthRequest struct {
	ProviderID  string
	Scope       ScopeRef
	Code        string
	State       string
	RedirectURI string
	Metadata    map[string]any
}

type ActiveCredential struct {
	ConnectionID    string
	TokenType       string
	AccessToken     string
	RefreshToken    string
	RequestedScopes []string
	GrantedScopes   []string
	ExpiresAt       *time.Time
	Refreshable     bool
	RotatesAt       *time.Time
	Metadata        map[string]any
}

type CompleteAuthResponse struct {
	ExternalAccountID string
	Credential        ActiveCredential
	RequestedGrants   []string
	GrantedGrants     []string
	Metadata          map[string]any
}

type RefreshResult struct {
	Credential    ActiveCredential
	GrantedGrants []string
	Metadata      map[string]any
}

type ConnectionResolutionKind string

const (
	ConnectionResolutionDirect    ConnectionResolutionKind = "direct"
	ConnectionResolutionInherited ConnectionResolutionKind = "inherited"
	ConnectionResolutionNotFound  ConnectionResolutionKind = "not_found"
)

type ConnectionResolution struct {
	Outcome    ConnectionResolutionKind
	Connection Connection
	Parent     *Connection
	Reason     string
}

type CreateConnectionInput struct {
	ProviderID        string
	Scope             ScopeRef
	ExternalAccountID string
	Status            ConnectionStatus
}

type SaveCredentialInput struct {
	ConnectionID      string
	EncryptedPayload  []byte
	TokenType         string
	RequestedScopes   []string
	GrantedScopes     []string
	ExpiresAt         *time.Time
	Refreshable       bool
	RotatesAt         *time.Time
	Status            CredentialStatus
	EncryptionKeyID   string
	EncryptionVersion int
}

type UpsertSubscriptionInput struct {
	ConnectionID         string
	ProviderID           string
	ResourceType         string
	ResourceID           string
	ChannelID            string
	RemoteSubscriptionID string
	CallbackURL          string
	VerificationTokenRef string
	Status               SubscriptionStatus
	ExpiresAt            *time.Time
	Metadata             map[string]any
}

type UpsertSyncCursorInput struct {
	ConnectionID string
	ProviderID   string
	ResourceType string
	ResourceID   string
	Cursor       string
	LastSyncedAt *time.Time
	Status       string
	Metadata     map[string]any
}

type AdvanceSyncCursorInput struct {
	ConnectionID   string
	ProviderID     string
	ResourceType   string
	ResourceID     string
	ExpectedCursor string
	Cursor         string
	LastSyncedAt   *time.Time
	Status         string
	Metadata       map[string]any
}

type SubscribeRequest struct {
	ConnectionID string
	ResourceType string
	ResourceID   string
	CallbackURL  string
	Metadata     map[string]any
}

type RenewSubscriptionRequest struct {
	SubscriptionID string
	Metadata       map[string]any
}

type CancelSubscriptionRequest struct {
	SubscriptionID string
	Reason         string
}

type SubscriptionResult struct {
	ChannelID            string
	RemoteSubscriptionID string
	ExpiresAt            *time.Time
	Metadata             map[string]any
}

type ListChangesRequest struct {
	ConnectionID string
	ResourceType string
	ResourceID   string
	Cursor       string
	Limit        int
}

type ListChangesResult struct {
	Items      []map[string]any
	NextCursor string
	HasMore    bool
	Metadata   map[string]any
}

type AuthBeginRequest struct {
	Scope        ScopeRef
	RedirectURI  string
	State        string
	RequestedRaw []string
	Metadata     map[string]any
}

type AuthBeginResponse struct {
	URL      string
	State    string
	Metadata map[string]any
}

type AuthCompleteRequest struct {
	Scope       ScopeRef
	Code        string
	State       string
	RedirectURI string
	Metadata    map[string]any
}

type AuthCompleteResponse struct {
	Credential ActiveCredential
	Metadata   map[string]any
}

type TransportRequest struct {
	Method      string
	URL         string
	Headers     map[string]string
	Query       map[string]string
	Body        []byte
	Metadata    map[string]any
	Timeout     time.Duration
	Idempotency string
}

type TransportResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
	Metadata   map[string]any
}

type UpsertInstallationInput struct {
	ProviderID  string
	Scope       ScopeRef
	InstallType string
	Status      InstallationStatus
	GrantedAt   *time.Time
	RevokedAt   *time.Time
	Metadata    map[string]any
}

type InboundRequest struct {
	ProviderID string
	Surface    string
	Headers    map[string]string
	Body       []byte
	Metadata   map[string]any
}

type InboundResult struct {
	Accepted   bool
	StatusCode int
	Metadata   map[string]any
}

type RateLimitKey struct {
	ProviderID string
	ScopeType  string
	ScopeID    string
	BucketKey  string
}

type ProviderResponseMeta struct {
	StatusCode int
	Headers    map[string]string
	RetryAfter *time.Duration
	Metadata   map[string]any
}

type BootstrapRequest struct {
	ConnectionID string
	ProviderID   string
	ResourceType string
	ResourceID   string
	Metadata     map[string]any
}

type BackfillRequest struct {
	ConnectionID string
	ProviderID   string
	ResourceType string
	ResourceID   string
	From         *time.Time
	To           *time.Time
	Metadata     map[string]any
}

type ServicesActivityFilter struct {
	ProviderID  string
	ScopeType   string
	ScopeID     string
	Action      string
	Status      ServiceActivityStatus
	From        *time.Time
	To          *time.Time
	Page        int
	PerPage     int
	Connections []string
}

type ServicesActivityPage struct {
	Items      []ServiceActivityEntry
	Page       int
	PerPage    int
	Total      int
	HasNext    bool
	NextCursor string
}

type SaveGrantSnapshotInput struct {
	ConnectionID string
	Version      int
	Requested    []string
	Granted      []string
	CapturedAt   time.Time
	Metadata     map[string]any
}

type GrantSnapshot struct {
	ConnectionID string
	Version      int
	Requested    []string
	Granted      []string
	CapturedAt   time.Time
	Metadata     map[string]any
}

type AppendGrantEventInput struct {
	ConnectionID string
	EventType    string
	Added        []string
	Removed      []string
	OccurredAt   time.Time
	Metadata     map[string]any
}

type PermissionDecision struct {
	Allowed       bool
	Capability    string
	Reason        string
	MissingGrants []string
	Mode          CapabilityDeniedBehavior
}

type Recipient struct {
	Type string
	ID   string
}

type ConnectRequest struct {
	ProviderID      string
	Scope           ScopeRef
	RedirectURI     string
	State           string
	RequestedGrants []string
	Metadata        map[string]any
}

type ReconsentRequest struct {
	ConnectionID    string
	RedirectURI     string
	State           string
	RequestedGrants []string
	Metadata        map[string]any
}

type CallbackCompletion struct {
	Connection Connection
	Credential Credential
}

type RefreshRequest struct {
	ProviderID   string
	ConnectionID string
	Credential   *ActiveCredential
}

type InvokeCapabilityRequest struct {
	ProviderID   string
	Scope        ScopeRef
	Capability   string
	Payload      map[string]any
	ConnectionID string
}

type CapabilityResult struct {
	Allowed    bool
	Mode       CapabilityDeniedBehavior
	Reason     string
	Connection Connection
	Metadata   map[string]any
}

type Provider interface {
	ID() string
	AuthKind() string
	SupportedScopeTypes() []string
	Capabilities() []CapabilityDescriptor

	BeginAuth(ctx context.Context, req BeginAuthRequest) (BeginAuthResponse, error)
	CompleteAuth(ctx context.Context, req CompleteAuthRequest) (CompleteAuthResponse, error)
	Refresh(ctx context.Context, cred ActiveCredential) (RefreshResult, error)
}

type Registry interface {
	Register(provider Provider) error
	Get(providerID string) (Provider, bool)
	List() []Provider
}

type ConnectionStore interface {
	Create(ctx context.Context, in CreateConnectionInput) (Connection, error)
	Get(ctx context.Context, id string) (Connection, error)
	FindByScope(ctx context.Context, providerID string, scope ScopeRef) ([]Connection, error)
	UpdateStatus(ctx context.Context, id string, status string, reason string) error
}

type CredentialStore interface {
	SaveNewVersion(ctx context.Context, in SaveCredentialInput) (Credential, error)
	GetActiveByConnection(ctx context.Context, connectionID string) (Credential, error)
	RevokeActive(ctx context.Context, connectionID string, reason string) error
}

type StoreProvider interface {
	ConnectionStore() ConnectionStore
	CredentialStore() CredentialStore
}

type RepositoryStoreFactory interface {
	BuildStores(persistenceClient any) (StoreProvider, error)
}

type SecretProvider interface {
	Encrypt(ctx context.Context, plaintext []byte) ([]byte, error)
	Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error)
}

type Logger = glog.Logger

type LoggerProvider = glog.LoggerProvider

type FieldsLogger = glog.FieldsLogger

type Signer interface {
	Sign(ctx context.Context, req *http.Request, cred ActiveCredential) error
}

type InheritancePolicy interface {
	ResolveConnection(
		ctx context.Context,
		providerID string,
		requested ScopeRef,
	) (resolved ConnectionResolution, err error)
}

type JobExecutionMessage struct {
	JobID          string
	ScriptPath     string
	Parameters     map[string]any
	IdempotencyKey string
	DedupPolicy    string
}

type JobNackOptions struct {
	Delay      time.Duration
	Requeue    bool
	DeadLetter bool
	Reason     string
}

type JobEnqueuer interface {
	Enqueue(ctx context.Context, msg *JobExecutionMessage) error
}

type JobDelivery interface {
	Message() *JobExecutionMessage
	Ack(ctx context.Context) error
	Nack(ctx context.Context, opts JobNackOptions) error
}

type JobDequeuer interface {
	Dequeue(ctx context.Context) (JobDelivery, error)
}

type JobWorkerHook interface {
	OnStart(ctx context.Context, event JobWorkerEvent)
	OnSuccess(ctx context.Context, event JobWorkerEvent)
	OnFailure(ctx context.Context, event JobWorkerEvent)
	OnRetry(ctx context.Context, event JobWorkerEvent)
}

type JobWorkerEvent struct {
	Message   *JobExecutionMessage
	Attempt   int
	Delay     time.Duration
	Err       error
	StartedAt time.Time
	Duration  time.Duration
}

type WebhookHandler interface {
	Handle(ctx context.Context, req InboundRequest) (InboundResult, error)
}

type WebhookRouter interface {
	Register(providerID string, handler WebhookHandler) error
}

type SubscriptionStore interface {
	Upsert(ctx context.Context, in UpsertSubscriptionInput) (Subscription, error)
	Get(ctx context.Context, id string) (Subscription, error)
	GetByChannelID(ctx context.Context, providerID, channelID string) (Subscription, error)
	ListExpiring(ctx context.Context, before time.Time) ([]Subscription, error)
	UpdateState(ctx context.Context, id string, status string, reason string) error
}

type SyncCursorStore interface {
	Get(ctx context.Context, connectionID string, resourceType string, resourceID string) (SyncCursor, error)
	Upsert(ctx context.Context, in UpsertSyncCursorInput) (SyncCursor, error)
	Advance(ctx context.Context, in AdvanceSyncCursorInput) (SyncCursor, error)
}

type LifecycleHooks interface {
	OnConnected(ctx context.Context, conn Connection) error
	OnDisconnected(ctx context.Context, conn Connection) error
	OnCredentialRotated(ctx context.Context, conn Connection, cred Credential) error
}

type SubscribableProvider interface {
	Subscribe(ctx context.Context, req SubscribeRequest) (SubscriptionResult, error)
	RenewSubscription(ctx context.Context, req RenewSubscriptionRequest) (SubscriptionResult, error)
	CancelSubscription(ctx context.Context, req CancelSubscriptionRequest) error
}

type IncrementalSyncProvider interface {
	ListChanges(ctx context.Context, req ListChangesRequest) (ListChangesResult, error)
}

type AuthStrategy interface {
	Type() string
	Begin(ctx context.Context, req AuthBeginRequest) (AuthBeginResponse, error)
	Complete(ctx context.Context, req AuthCompleteRequest) (AuthCompleteResponse, error)
	Refresh(ctx context.Context, cred ActiveCredential) (RefreshResult, error)
}

type TransportAdapter interface {
	Kind() string
	Do(ctx context.Context, req TransportRequest) (TransportResponse, error)
}

type InstallationStore interface {
	Upsert(ctx context.Context, in UpsertInstallationInput) (Installation, error)
	Get(ctx context.Context, id string) (Installation, error)
	ListByScope(ctx context.Context, providerID string, scope ScopeRef) ([]Installation, error)
	UpdateStatus(ctx context.Context, id string, status string, reason string) error
}

type InboundHandler interface {
	Surface() string
	Handle(ctx context.Context, req InboundRequest) (InboundResult, error)
}

type CommandMessage interface {
	Type() string
}

type CommandDispatcher interface {
	Dispatch(ctx context.Context, msg any) error
}

type RateLimitPolicy interface {
	BeforeCall(ctx context.Context, key RateLimitKey) error
	AfterCall(ctx context.Context, key RateLimitKey, res ProviderResponseMeta) error
}

type BulkSyncOrchestrator interface {
	StartBootstrap(ctx context.Context, req BootstrapRequest) (SyncJob, error)
	StartBackfill(ctx context.Context, req BackfillRequest) (SyncJob, error)
	Resume(ctx context.Context, jobID string) error
}

type ServicesActivitySink interface {
	Record(ctx context.Context, entry ServiceActivityEntry) error
	List(ctx context.Context, filter ServicesActivityFilter) (ServicesActivityPage, error)
}

type ServicesActivityEnricher interface {
	Enrich(ctx context.Context, entry ServiceActivityEntry) (ServiceActivityEntry, error)
}

type LifecycleEventHandler interface {
	Handle(ctx context.Context, event LifecycleEvent) error
}

type LifecycleEventBus interface {
	Publish(ctx context.Context, event LifecycleEvent) error
	Subscribe(handler LifecycleEventHandler)
}

type DispatchStats struct {
	Claimed   int
	Delivered int
	Retried   int
	Failed    int
}

type LifecycleDispatcher interface {
	DispatchPending(ctx context.Context, batchSize int) (DispatchStats, error)
}

type LifecycleHook interface {
	Name() string
	OnEvent(ctx context.Context, event LifecycleEvent) error
}

type ProjectorRegistry interface {
	Register(name string, handler LifecycleEventHandler)
	Handlers() []LifecycleEventHandler
}

type OutboxStore interface {
	Enqueue(ctx context.Context, event LifecycleEvent) error
	ClaimBatch(ctx context.Context, limit int) ([]LifecycleEvent, error)
	Ack(ctx context.Context, eventID string) error
	Retry(ctx context.Context, eventID string, cause error, nextAttemptAt time.Time) error
}

type NotificationProjector interface {
	Handle(ctx context.Context, event LifecycleEvent) error
}

type ActivityProjector interface {
	Handle(ctx context.Context, event LifecycleEvent) error
}

type NotificationDefinitionResolver interface {
	Resolve(ctx context.Context, event LifecycleEvent) (definitionCode string, ok bool, err error)
}

type NotificationRecipientResolver interface {
	Resolve(ctx context.Context, event LifecycleEvent) ([]Recipient, error)
}

type NotificationSendRequest struct {
	DefinitionCode string
	Recipients     []Recipient
	Event          LifecycleEvent
	Metadata       map[string]any
}

type NotificationSender interface {
	Send(ctx context.Context, req NotificationSendRequest) error
}

type NotificationDispatchRecord struct {
	EventID        string
	Projector      string
	DefinitionCode string
	RecipientKey   string
	IdempotencyKey string
	Status         string
	Error          string
	Metadata       map[string]any
}

type NotificationDispatchLedger interface {
	Seen(ctx context.Context, idempotencyKey string) (bool, error)
	Record(ctx context.Context, record NotificationDispatchRecord) error
}

type GrantStore interface {
	SaveSnapshot(ctx context.Context, in SaveGrantSnapshotInput) error
	GetLatestSnapshot(ctx context.Context, connectionID string) (GrantSnapshot, error)
	AppendEvent(ctx context.Context, in AppendGrantEventInput) error
}

type PermissionEvaluator interface {
	EvaluateCapability(
		ctx context.Context,
		connectionID string,
		capability string,
	) (PermissionDecision, error)
}

type GrantAwareProvider interface {
	NormalizeGrantedPermissions(ctx context.Context, raw []string) ([]string, error)
}

type IntegrationService interface {
	Connect(ctx context.Context, req ConnectRequest) (BeginAuthResponse, error)
	StartReconsent(ctx context.Context, req ReconsentRequest) (BeginAuthResponse, error)
	CompleteReconsent(ctx context.Context, req CompleteAuthRequest) (CallbackCompletion, error)
	CompleteCallback(ctx context.Context, req CompleteAuthRequest) (CallbackCompletion, error)
	Refresh(ctx context.Context, req RefreshRequest) (RefreshResult, error)
	Revoke(ctx context.Context, connectionID string, reason string) error
	InvokeCapability(ctx context.Context, req InvokeCapabilityRequest) (CapabilityResult, error)
	SignRequest(ctx context.Context, providerID string, connectionID string, req *http.Request, cred *ActiveCredential) error
	Subscribe(ctx context.Context, req SubscribeRequest) (Subscription, error)
	RenewSubscription(ctx context.Context, req RenewSubscriptionRequest) (Subscription, error)
	CancelSubscription(ctx context.Context, req CancelSubscriptionRequest) error
}
