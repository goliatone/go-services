package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type testProvider struct {
	id           string
	capabilities []CapabilityDescriptor
}

func (p testProvider) ID() string { return p.id }

func (p testProvider) AuthKind() string { return "oauth2" }

func (p testProvider) SupportedScopeTypes() []string { return []string{"user", "org"} }

func (p testProvider) Capabilities() []CapabilityDescriptor {
	return append([]CapabilityDescriptor(nil), p.capabilities...)
}

func (p testProvider) BeginAuth(context.Context, BeginAuthRequest) (BeginAuthResponse, error) {
	return BeginAuthResponse{URL: "https://example.com/auth", State: "state"}, nil
}

func (p testProvider) CompleteAuth(context.Context, CompleteAuthRequest) (CompleteAuthResponse, error) {
	now := time.Now().UTC().Add(10 * time.Minute)
	return CompleteAuthResponse{
		ExternalAccountID: "acct_1",
		Credential: ActiveCredential{
			TokenType:       "bearer",
			RequestedScopes: []string{"repo:read"},
			GrantedScopes:   []string{"repo:read"},
			Refreshable:     true,
			ExpiresAt:       &now,
		},
	}, nil
}

func (p testProvider) Refresh(context.Context, ActiveCredential) (RefreshResult, error) {
	now := time.Now().UTC().Add(1 * time.Hour)
	return RefreshResult{Credential: ActiveCredential{TokenType: "bearer", ExpiresAt: &now, Refreshable: true}}, nil
}

type memoryConnectionStore struct {
	mu      sync.Mutex
	next    int
	byID    map[string]Connection
	byScope map[string][]string
}

func newMemoryConnectionStore() *memoryConnectionStore {
	return &memoryConnectionStore{
		byID:    map[string]Connection{},
		byScope: map[string][]string{},
	}
}

func (s *memoryConnectionStore) Create(_ context.Context, in CreateConnectionInput) (Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	id := fmt.Sprintf("conn_%d", s.next)
	connection := Connection{
		ID:                id,
		ProviderID:        in.ProviderID,
		ScopeType:         in.Scope.Type,
		ScopeID:           in.Scope.ID,
		ExternalAccountID: in.ExternalAccountID,
		Status:            in.Status,
	}
	s.byID[id] = connection
	key := scopeKey(in.ProviderID, in.Scope)
	s.byScope[key] = append(s.byScope[key], id)
	return connection, nil
}

func (s *memoryConnectionStore) Get(_ context.Context, id string) (Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conn, ok := s.byID[id]
	if !ok {
		return Connection{}, fmt.Errorf("missing connection")
	}
	return conn, nil
}

func (s *memoryConnectionStore) FindByScope(_ context.Context, providerID string, scope ScopeRef) ([]Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.byScope[scopeKey(providerID, scope)]
	out := make([]Connection, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.byID[id])
	}
	return out, nil
}

func (s *memoryConnectionStore) UpdateStatus(_ context.Context, id string, status string, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	conn, ok := s.byID[id]
	if !ok {
		return fmt.Errorf("missing connection")
	}
	conn.Status = ConnectionStatus(status)
	conn.LastError = reason
	s.byID[id] = conn
	return nil
}

type memoryCredentialStore struct {
	mu      sync.Mutex
	current map[string]Credential
	next    int
}

func newMemoryCredentialStore() *memoryCredentialStore {
	return &memoryCredentialStore{current: map[string]Credential{}}
}

func (s *memoryCredentialStore) SaveNewVersion(_ context.Context, in SaveCredentialInput) (Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	credential := Credential{
		ID:               fmt.Sprintf("cred_%d", s.next),
		ConnectionID:     in.ConnectionID,
		Version:          s.next,
		EncryptedPayload: append([]byte(nil), in.EncryptedPayload...),
		TokenType:        in.TokenType,
		RequestedScopes:  append([]string(nil), in.RequestedScopes...),
		GrantedScopes:    append([]string(nil), in.GrantedScopes...),
		Status:           in.Status,
		Refreshable:      in.Refreshable,
	}
	if in.ExpiresAt != nil {
		credential.ExpiresAt = *in.ExpiresAt
	}
	if in.RotatesAt != nil {
		credential.RotatesAt = *in.RotatesAt
	}
	s.current[in.ConnectionID] = credential
	return credential, nil
}

func (s *memoryCredentialStore) GetActiveByConnection(_ context.Context, connectionID string) (Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	credential, ok := s.current[connectionID]
	if !ok {
		return Credential{}, fmt.Errorf("missing credential")
	}
	return credential, nil
}

func (s *memoryCredentialStore) RevokeActive(_ context.Context, connectionID string, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	credential, ok := s.current[connectionID]
	if !ok {
		return nil
	}
	credential.Status = CredentialStatusRevoked
	s.current[connectionID] = credential
	return nil
}

type memoryGrantStore struct {
	mu        sync.Mutex
	snapshots map[string][]GrantSnapshot
	events    map[string][]AppendGrantEventInput
}

func newMemoryGrantStore() *memoryGrantStore {
	return &memoryGrantStore{
		snapshots: map[string][]GrantSnapshot{},
		events:    map[string][]AppendGrantEventInput{},
	}
}

func (s *memoryGrantStore) SaveSnapshot(_ context.Context, in SaveGrantSnapshotInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := GrantSnapshot{
		ConnectionID: in.ConnectionID,
		Version:      in.Version,
		Requested:    append([]string(nil), in.Requested...),
		Granted:      append([]string(nil), in.Granted...),
		CapturedAt:   in.CapturedAt,
		Metadata:     copyAnyMap(in.Metadata),
	}
	s.snapshots[in.ConnectionID] = append(s.snapshots[in.ConnectionID], record)
	return nil
}

func (s *memoryGrantStore) GetLatestSnapshot(_ context.Context, connectionID string) (GrantSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.snapshots[connectionID]
	if len(items) == 0 {
		return GrantSnapshot{}, fmt.Errorf("missing grant snapshot")
	}
	return items[len(items)-1], nil
}

func (s *memoryGrantStore) AppendEvent(_ context.Context, in AppendGrantEventInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := AppendGrantEventInput{
		ConnectionID: in.ConnectionID,
		EventType:    in.EventType,
		Added:        append([]string(nil), in.Added...),
		Removed:      append([]string(nil), in.Removed...),
		OccurredAt:   in.OccurredAt,
		Metadata:     copyAnyMap(in.Metadata),
	}
	s.events[in.ConnectionID] = append(s.events[in.ConnectionID], record)
	return nil
}

func (s *memoryGrantStore) Snapshots(connectionID string) []GrantSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.snapshots[connectionID]
	out := make([]GrantSnapshot, 0, len(items))
	for _, item := range items {
		out = append(out, GrantSnapshot{
			ConnectionID: item.ConnectionID,
			Version:      item.Version,
			Requested:    append([]string(nil), item.Requested...),
			Granted:      append([]string(nil), item.Granted...),
			CapturedAt:   item.CapturedAt,
			Metadata:     copyAnyMap(item.Metadata),
		})
	}
	return out
}

func (s *memoryGrantStore) Events(connectionID string) []AppendGrantEventInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.events[connectionID]
	out := make([]AppendGrantEventInput, 0, len(items))
	for _, item := range items {
		out = append(out, AppendGrantEventInput{
			ConnectionID: item.ConnectionID,
			EventType:    item.EventType,
			Added:        append([]string(nil), item.Added...),
			Removed:      append([]string(nil), item.Removed...),
			OccurredAt:   item.OccurredAt,
			Metadata:     copyAnyMap(item.Metadata),
		})
	}
	return out
}

type memorySubscriptionStore struct {
	mu     sync.Mutex
	next   int
	byID   map[string]Subscription
	byPair map[string]string
}

func newMemorySubscriptionStore() *memorySubscriptionStore {
	return &memorySubscriptionStore{
		byID:   map[string]Subscription{},
		byPair: map[string]string{},
	}
}

func (s *memorySubscriptionStore) Upsert(_ context.Context, in UpsertSubscriptionInput) (Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := in.ProviderID + ":" + in.ChannelID
	id := s.byPair[key]
	if id == "" {
		s.next++
		id = fmt.Sprintf("sub_%d", s.next)
		s.byPair[key] = id
	}

	record := s.byID[id]
	record.ID = id
	record.ConnectionID = in.ConnectionID
	record.ProviderID = in.ProviderID
	record.ResourceType = in.ResourceType
	record.ResourceID = in.ResourceID
	record.ChannelID = in.ChannelID
	record.RemoteSubscriptionID = in.RemoteSubscriptionID
	record.CallbackURL = in.CallbackURL
	record.VerificationTokenRef = in.VerificationTokenRef
	record.Status = in.Status
	record.Metadata = copyAnyMap(in.Metadata)
	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	if in.ExpiresAt != nil {
		record.ExpiresAt = *in.ExpiresAt
	} else {
		record.ExpiresAt = time.Time{}
	}
	s.byID[id] = record
	return record, nil
}

func (s *memorySubscriptionStore) Get(_ context.Context, id string) (Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.byID[id]
	if !ok {
		return Subscription{}, fmt.Errorf("missing subscription")
	}
	return record, nil
}

func (s *memorySubscriptionStore) GetByChannelID(_ context.Context, providerID, channelID string) (Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.byPair[providerID+":"+channelID]
	if id == "" {
		return Subscription{}, fmt.Errorf("missing subscription")
	}
	return s.byID[id], nil
}

func (s *memorySubscriptionStore) ListExpiring(_ context.Context, before time.Time) ([]Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Subscription{}
	for _, record := range s.byID {
		if record.Status != SubscriptionStatusActive || record.ExpiresAt.IsZero() {
			continue
		}
		if record.ExpiresAt.Before(before) || record.ExpiresAt.Equal(before) {
			out = append(out, record)
		}
	}
	return out, nil
}

func (s *memorySubscriptionStore) UpdateState(_ context.Context, id string, status string, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.byID[id]
	if !ok {
		return fmt.Errorf("missing subscription")
	}
	record.Status = SubscriptionStatus(status)
	record.Metadata = copyAnyMap(record.Metadata)
	if strings.TrimSpace(reason) != "" {
		record.Metadata["status_reason"] = strings.TrimSpace(reason)
	}
	record.UpdatedAt = time.Now().UTC()
	s.byID[id] = record
	return nil
}

type memorySyncCursorStore struct {
	mu      sync.Mutex
	next    int
	records map[string]SyncCursor
}

func newMemorySyncCursorStore() *memorySyncCursorStore {
	return &memorySyncCursorStore{
		records: map[string]SyncCursor{},
	}
}

func (s *memorySyncCursorStore) Get(_ context.Context, connectionID string, resourceType string, resourceID string) (SyncCursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.records {
		if record.ConnectionID != connectionID {
			continue
		}
		if record.ResourceType != resourceType || record.ResourceID != resourceID {
			continue
		}
		return record, nil
	}
	return SyncCursor{}, fmt.Errorf("missing cursor")
}

func (s *memorySyncCursorStore) Upsert(_ context.Context, in UpsertSyncCursorInput) (SyncCursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := syncCursorKey(in.ConnectionID, in.ProviderID, in.ResourceType, in.ResourceID)
	record := s.records[key]
	if record.ID == "" {
		s.next++
		record.ID = fmt.Sprintf("cursor_%d", s.next)
		record.CreatedAt = time.Now().UTC()
	}
	record.ConnectionID = in.ConnectionID
	record.ProviderID = in.ProviderID
	record.ResourceType = in.ResourceType
	record.ResourceID = in.ResourceID
	record.Cursor = in.Cursor
	record.Status = in.Status
	record.Metadata = copyAnyMap(in.Metadata)
	record.UpdatedAt = time.Now().UTC()
	if in.LastSyncedAt != nil {
		record.LastSyncedAt = *in.LastSyncedAt
	}
	s.records[key] = record
	return record, nil
}

func (s *memorySyncCursorStore) Advance(_ context.Context, in AdvanceSyncCursorInput) (SyncCursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := syncCursorKey(in.ConnectionID, in.ProviderID, in.ResourceType, in.ResourceID)
	record, ok := s.records[key]
	if !ok {
		if strings.TrimSpace(in.ExpectedCursor) != "" {
			return SyncCursor{}, ErrSyncCursorConflict
		}
		s.next++
		record = SyncCursor{
			ID:           fmt.Sprintf("cursor_%d", s.next),
			ConnectionID: in.ConnectionID,
			ProviderID:   in.ProviderID,
			ResourceType: in.ResourceType,
			ResourceID:   in.ResourceID,
			CreatedAt:    time.Now().UTC(),
		}
	}
	if strings.TrimSpace(in.ExpectedCursor) != "" && in.ExpectedCursor != record.Cursor {
		return SyncCursor{}, ErrSyncCursorConflict
	}
	record.Cursor = in.Cursor
	record.Status = in.Status
	record.Metadata = copyAnyMap(in.Metadata)
	record.UpdatedAt = time.Now().UTC()
	if in.LastSyncedAt != nil {
		record.LastSyncedAt = *in.LastSyncedAt
	}
	s.records[key] = record
	return record, nil
}

func syncCursorKey(connectionID string, providerID string, resourceType string, resourceID string) string {
	return connectionID + ":" + providerID + ":" + resourceType + ":" + resourceID
}

func scopeKey(providerID string, scope ScopeRef) string {
	return providerID + ":" + scope.Type + ":" + scope.ID
}

type stubLogger struct{}

func (stubLogger) Trace(string, ...any) {}
func (stubLogger) Debug(string, ...any) {}
func (stubLogger) Info(string, ...any)  {}
func (stubLogger) Warn(string, ...any)  {}
func (stubLogger) Error(string, ...any) {}
func (stubLogger) Fatal(string, ...any) {}
func (s stubLogger) WithContext(context.Context) Logger {
	return s
}

type stubLoggerProvider struct {
	logger Logger
}

func (s stubLoggerProvider) GetLogger(string) Logger {
	return s.logger
}

type mapRawLoader struct {
	values map[string]any
}

func (l mapRawLoader) LoadRaw(context.Context) (map[string]any, error) {
	if len(l.values) == 0 {
		return map[string]any{}, nil
	}
	out := make(map[string]any, len(l.values))
	for key, value := range l.values {
		out[key] = value
	}
	return out, nil
}

type staticInheritancePolicy struct {
	resolution ConnectionResolution
	err        error
}

func (s staticInheritancePolicy) ResolveConnection(context.Context, string, ScopeRef) (ConnectionResolution, error) {
	return s.resolution, s.err
}
