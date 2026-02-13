package core

import (
	"context"
	"fmt"
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
		ID:              fmt.Sprintf("cred_%d", s.next),
		ConnectionID:    in.ConnectionID,
		Version:         s.next,
		TokenType:       in.TokenType,
		RequestedScopes: append([]string(nil), in.RequestedScopes...),
		GrantedScopes:   append([]string(nil), in.GrantedScopes...),
		Status:          in.Status,
		Refreshable:     in.Refreshable,
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
