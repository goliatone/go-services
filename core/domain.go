package core

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidScopeType                    = errors.New("core: invalid scope type")
	ErrInvalidConnectionStatusTransition   = errors.New("core: invalid connection status transition")
	ErrInvalidCredentialStatusTransition   = errors.New("core: invalid credential status transition")
	ErrInvalidInstallationStatusTransition = errors.New("core: invalid installation status transition")
	ErrInvalidSubscriptionStatusTransition = errors.New("core: invalid subscription status transition")
	ErrInvalidSyncJobStatusTransition      = errors.New("core: invalid sync job status transition")
	ErrInvalidSyncJobMode                  = errors.New("core: invalid sync job mode")
	ErrInvalidSyncJobScope                 = errors.New("core: invalid sync job scope")
	ErrSyncJobNotFound                     = errors.New("core: sync job not found")
)

type ScopeType string

const (
	ScopeTypeUser ScopeType = "user"
	ScopeTypeOrg  ScopeType = "org"
)

type ScopeRef struct {
	Type string
	ID   string
}

func (s ScopeRef) Validate() error {
	t := strings.TrimSpace(strings.ToLower(s.Type))
	if t != string(ScopeTypeUser) && t != string(ScopeTypeOrg) {
		return fmt.Errorf("%w: %q", ErrInvalidScopeType, s.Type)
	}
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("%w: empty id", ErrInvalidScopeType)
	}
	return nil
}

type ConnectionStatus string

const (
	ConnectionStatusActive         ConnectionStatus = "active"
	ConnectionStatusDisconnected   ConnectionStatus = "disconnected"
	ConnectionStatusErrored        ConnectionStatus = "errored"
	ConnectionStatusPendingReauth  ConnectionStatus = "pending_reauth"
	ConnectionStatusNeedsReconsent ConnectionStatus = "needs_reconsent"
)

type Connection struct {
	ID                string
	ProviderID        string
	ScopeType         string
	ScopeID           string
	ExternalAccountID string
	Status            ConnectionStatus
	InheritsFrom      string
	LastError         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (c *Connection) TransitionTo(status ConnectionStatus, reason string, now time.Time) error {
	if c == nil {
		return nil
	}
	if c.Status == status {
		c.UpdatedAt = now
		if strings.TrimSpace(reason) != "" {
			c.LastError = strings.TrimSpace(reason)
		}
		return nil
	}
	if !connectionTransitionAllowed(c.Status, status) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidConnectionStatusTransition, c.Status, status)
	}
	c.Status = status
	c.UpdatedAt = now
	if strings.TrimSpace(reason) != "" {
		c.LastError = strings.TrimSpace(reason)
	}
	if status == ConnectionStatusActive {
		c.LastError = ""
	}
	return nil
}

func connectionTransitionAllowed(current, next ConnectionStatus) bool {
	allowed := map[ConnectionStatus]map[ConnectionStatus]struct{}{
		ConnectionStatusActive: {
			ConnectionStatusDisconnected:   {},
			ConnectionStatusErrored:        {},
			ConnectionStatusPendingReauth:  {},
			ConnectionStatusNeedsReconsent: {},
		},
		ConnectionStatusDisconnected: {
			ConnectionStatusActive: {},
		},
		ConnectionStatusErrored: {
			ConnectionStatusActive:        {},
			ConnectionStatusPendingReauth: {},
			ConnectionStatusDisconnected:  {},
		},
		ConnectionStatusPendingReauth: {
			ConnectionStatusActive:       {},
			ConnectionStatusDisconnected: {},
		},
		ConnectionStatusNeedsReconsent: {
			ConnectionStatusActive:        {},
			ConnectionStatusDisconnected:  {},
			ConnectionStatusPendingReauth: {},
		},
	}
	_, ok := allowed[current][next]
	return ok
}

type CredentialStatus string

const (
	CredentialStatusActive  CredentialStatus = "active"
	CredentialStatusRevoked CredentialStatus = "revoked"
	CredentialStatusExpired CredentialStatus = "expired"
)

type Credential struct {
	ID               string
	ConnectionID     string
	Version          int
	EncryptedPayload []byte
	PayloadFormat    string
	PayloadVersion   int
	TokenType        string
	RequestedScopes  []string
	GrantedScopes    []string
	ExpiresAt        time.Time
	Refreshable      bool
	RotatesAt        time.Time
	Status           CredentialStatus
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (c *Credential) TransitionTo(status CredentialStatus, now time.Time) error {
	if c == nil {
		return nil
	}
	if c.Status == status {
		c.UpdatedAt = now
		return nil
	}
	if !credentialTransitionAllowed(c.Status, status) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidCredentialStatusTransition, c.Status, status)
	}
	c.Status = status
	c.UpdatedAt = now
	return nil
}

func credentialTransitionAllowed(current, next CredentialStatus) bool {
	allowed := map[CredentialStatus]map[CredentialStatus]struct{}{
		CredentialStatusActive: {
			CredentialStatusRevoked: {},
			CredentialStatusExpired: {},
		},
		CredentialStatusExpired: {
			CredentialStatusActive:  {},
			CredentialStatusRevoked: {},
		},
		CredentialStatusRevoked: {},
	}
	_, ok := allowed[current][next]
	return ok
}

type SubscriptionStatus string

const (
	SubscriptionStatusActive    SubscriptionStatus = "active"
	SubscriptionStatusExpired   SubscriptionStatus = "expired"
	SubscriptionStatusCancelled SubscriptionStatus = "cancelled"
	SubscriptionStatusErrored   SubscriptionStatus = "errored"
)

type Subscription struct {
	ID                   string
	ConnectionID         string
	ProviderID           string
	ResourceType         string
	ResourceID           string
	ChannelID            string
	RemoteSubscriptionID string
	CallbackURL          string
	VerificationTokenRef string
	Status               SubscriptionStatus
	ExpiresAt            time.Time
	Metadata             map[string]any
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (s *Subscription) TransitionTo(status SubscriptionStatus, now time.Time) error {
	if s == nil {
		return nil
	}
	if s.Status == status {
		s.UpdatedAt = now
		return nil
	}
	if !subscriptionTransitionAllowed(s.Status, status) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidSubscriptionStatusTransition, s.Status, status)
	}
	s.Status = status
	s.UpdatedAt = now
	return nil
}

func subscriptionTransitionAllowed(current, next SubscriptionStatus) bool {
	allowed := map[SubscriptionStatus]map[SubscriptionStatus]struct{}{
		SubscriptionStatusActive: {
			SubscriptionStatusExpired:   {},
			SubscriptionStatusCancelled: {},
			SubscriptionStatusErrored:   {},
		},
		SubscriptionStatusExpired: {
			SubscriptionStatusActive:    {},
			SubscriptionStatusCancelled: {},
		},
		SubscriptionStatusErrored: {
			SubscriptionStatusActive:    {},
			SubscriptionStatusCancelled: {},
		},
		SubscriptionStatusCancelled: {},
	}
	_, ok := allowed[current][next]
	return ok
}

type SyncCursor struct {
	ID           string
	ConnectionID string
	ProviderID   string
	ResourceType string
	ResourceID   string
	Cursor       string
	LastSyncedAt time.Time
	Status       string
	Metadata     map[string]any
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type InstallationStatus string

const (
	InstallationStatusActive         InstallationStatus = "active"
	InstallationStatusSuspended      InstallationStatus = "suspended"
	InstallationStatusUninstalled    InstallationStatus = "uninstalled"
	InstallationStatusNeedsReconsent InstallationStatus = "needs_reconsent"
)

type Installation struct {
	ID          string
	ProviderID  string
	ScopeType   string
	ScopeID     string
	InstallType string
	Status      InstallationStatus
	GrantedAt   *time.Time
	RevokedAt   *time.Time
	Metadata    map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (i *Installation) TransitionTo(status InstallationStatus, now time.Time) error {
	if i == nil {
		return nil
	}
	if i.Status == status {
		i.UpdatedAt = now
		return nil
	}
	if !installationTransitionAllowed(i.Status, status) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidInstallationStatusTransition, i.Status, status)
	}
	i.Status = status
	i.UpdatedAt = now
	return nil
}

func installationTransitionAllowed(current, next InstallationStatus) bool {
	allowed := map[InstallationStatus]map[InstallationStatus]struct{}{
		InstallationStatusActive: {
			InstallationStatusSuspended:      {},
			InstallationStatusUninstalled:    {},
			InstallationStatusNeedsReconsent: {},
		},
		InstallationStatusSuspended: {
			InstallationStatusActive:      {},
			InstallationStatusUninstalled: {},
		},
		InstallationStatusNeedsReconsent: {
			InstallationStatusActive:      {},
			InstallationStatusUninstalled: {},
		},
		InstallationStatusUninstalled: {},
	}
	_, ok := allowed[current][next]
	return ok
}

type SyncJobStatus string

const (
	SyncJobStatusQueued    SyncJobStatus = "queued"
	SyncJobStatusRunning   SyncJobStatus = "running"
	SyncJobStatusSucceeded SyncJobStatus = "succeeded"
	SyncJobStatusFailed    SyncJobStatus = "failed"
)

type SyncJobMode string

const (
	SyncJobModeFull        SyncJobMode = "full"
	SyncJobModeDelta       SyncJobMode = "delta"
	SyncJobModeBootstrap   SyncJobMode = "bootstrap"
	SyncJobModeIncremental SyncJobMode = "incremental"
	SyncJobModeBackfill    SyncJobMode = "backfill"
)

type SyncJob struct {
	ID            string
	ConnectionID  string
	ProviderID    string
	Mode          SyncJobMode
	Checkpoint    string
	Status        SyncJobStatus
	Attempts      int
	NextAttemptAt *time.Time
	Metadata      map[string]any
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (j *SyncJob) TransitionTo(status SyncJobStatus, now time.Time) error {
	if j == nil {
		return nil
	}
	if j.Status == status {
		j.UpdatedAt = now
		return nil
	}
	if !syncJobTransitionAllowed(j.Status, status) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidSyncJobStatusTransition, j.Status, status)
	}
	j.Status = status
	j.UpdatedAt = now
	return nil
}

func syncJobTransitionAllowed(current, next SyncJobStatus) bool {
	allowed := map[SyncJobStatus]map[SyncJobStatus]struct{}{
		SyncJobStatusQueued: {
			SyncJobStatusRunning: {},
			SyncJobStatusFailed:  {},
		},
		SyncJobStatusRunning: {
			SyncJobStatusSucceeded: {},
			SyncJobStatusFailed:    {},
		},
		SyncJobStatusFailed: {
			SyncJobStatusQueued:  {},
			SyncJobStatusRunning: {},
		},
		SyncJobStatusSucceeded: {},
	}
	_, ok := allowed[current][next]
	return ok
}

type ServiceActivityStatus string

const (
	ServiceActivityStatusOK    ServiceActivityStatus = "ok"
	ServiceActivityStatusWarn  ServiceActivityStatus = "warn"
	ServiceActivityStatusError ServiceActivityStatus = "error"
)

type ServiceActivityEntry struct {
	ID        string
	Actor     string
	Action    string
	Object    string
	Channel   string
	Status    ServiceActivityStatus
	Metadata  map[string]any
	CreatedAt time.Time
}

type LifecycleEvent struct {
	ID           string
	Name         string
	ProviderID   string
	ScopeType    string
	ScopeID      string
	ConnectionID string
	Source       string
	OccurredAt   time.Time
	Payload      map[string]any
	Metadata     map[string]any
}
