package core

import (
	"context"
	"testing"
	"time"
)

func TestResolveCredentialTokenState(t *testing.T) {
	now := time.Date(2026, 2, 16, 12, 0, 0, 0, time.UTC)
	expiresSoon := now.Add(2 * time.Minute)
	expiresLater := now.Add(2 * time.Hour)

	cases := []struct {
		name       string
		credential ActiveCredential
		expired    bool
		soon       bool
		auto       bool
	}{
		{
			name: "missing_expiry",
			credential: ActiveCredential{
				AccessToken:  "access",
				RefreshToken: "refresh",
				Refreshable:  true,
			},
			expired: false,
			soon:    false,
			auto:    true,
		},
		{
			name: "expired",
			credential: ActiveCredential{
				AccessToken:  "access",
				RefreshToken: "refresh",
				Refreshable:  true,
				ExpiresAt:    ptrTime(now.Add(-1 * time.Minute)),
			},
			expired: true,
			soon:    false,
			auto:    true,
		},
		{
			name: "expiring_soon",
			credential: ActiveCredential{
				AccessToken:  "access",
				RefreshToken: "refresh",
				Refreshable:  true,
				ExpiresAt:    &expiresSoon,
			},
			expired: false,
			soon:    true,
			auto:    true,
		},
		{
			name: "healthy_ttl_not_auto_refreshable",
			credential: ActiveCredential{
				AccessToken: "access",
				ExpiresAt:   &expiresLater,
			},
			expired: false,
			soon:    false,
			auto:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := ResolveCredentialTokenState(now, tc.credential, DefaultCredentialExpiringSoonWindow)
			if state.IsExpired != tc.expired || state.IsExpiringSoon != tc.soon {
				t.Fatalf("expected expired=%t soon=%t, got expired=%t soon=%t", tc.expired, tc.soon, state.IsExpired, state.IsExpiringSoon)
			}
			if state.CanAutoRefresh != tc.auto {
				t.Fatalf("expected can_auto_refresh=%t, got %t", tc.auto, state.CanAutoRefresh)
			}
		})
	}
}

func TestShouldRefreshCredential(t *testing.T) {
	now := time.Date(2026, 2, 16, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		state  CredentialTokenState
		window time.Duration
		want   bool
	}{
		{
			name: "no_auto_refresh",
			state: CredentialTokenState{
				CanAutoRefresh: false,
			},
			window: DefaultCredentialRefreshLeadWindow,
			want:   false,
		},
		{
			name: "missing_access_token_refreshable",
			state: CredentialTokenState{
				CanAutoRefresh: true,
				HasAccessToken: false,
			},
			window: DefaultCredentialRefreshLeadWindow,
			want:   true,
		},
		{
			name: "outside_lead_window",
			state: CredentialTokenState{
				CanAutoRefresh: true,
				HasAccessToken: true,
				ExpiresAt:      ptrTime(now.Add(30 * time.Minute)),
			},
			window: DefaultCredentialRefreshLeadWindow,
			want:   false,
		},
		{
			name: "inside_lead_window",
			state: CredentialTokenState{
				CanAutoRefresh: true,
				HasAccessToken: true,
				ExpiresAt:      ptrTime(now.Add(2 * time.Minute)),
			},
			window: DefaultCredentialRefreshLeadWindow,
			want:   true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldRefreshCredential(now, tc.state, tc.window); got != tc.want {
				t.Fatalf("expected %t, got %t", tc.want, got)
			}
		})
	}
}

func TestServiceEnsureCredentialFresh(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	provider := &freshnessProvider{
		testProvider: testProvider{id: "github"},
		refreshResult: RefreshResult{
			Credential: ActiveCredential{
				TokenType:       "bearer",
				AccessToken:     "access-refreshed",
				RefreshToken:    "refresh-1",
				RequestedScopes: []string{"repo:read"},
				GrantedScopes:   []string{"repo:read"},
				Refreshable:     true,
				ExpiresAt:       ptrTime(now.Add(2 * time.Hour)),
			},
		},
	}
	registry := NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	connectionStore := newMemoryConnectionStore()
	connection, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "user", ID: "u1"},
		ExternalAccountID: "acct-1",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	credentialStore := newMemoryCredentialStore()
	if err := saveTestActiveCredential(ctx, credentialStore, connection.ID, ActiveCredential{
		ConnectionID:    connection.ID,
		TokenType:       "bearer",
		AccessToken:     "access-1",
		RefreshToken:    "refresh-1",
		RequestedScopes: []string{"repo:read"},
		GrantedScopes:   []string{"repo:read"},
		Refreshable:     true,
		ExpiresAt:       ptrTime(now.Add(1 * time.Minute)),
	}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
		WithCredentialStore(credentialStore),
		WithSecretProvider(testSecretProvider{}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := svc.EnsureCredentialFresh(ctx, EnsureCredentialFreshRequest{
		ProviderID:   "github",
		ConnectionID: connection.ID,
	})
	if err != nil {
		t.Fatalf("ensure credential fresh: %v", err)
	}
	if !result.RefreshAttempted || !result.Refreshed {
		t.Fatalf("expected refresh attempt and success, got %+v", result)
	}
	if result.Credential.AccessToken != "access-refreshed" {
		t.Fatalf("expected refreshed access token, got %q", result.Credential.AccessToken)
	}
	if provider.refreshCalls != 1 {
		t.Fatalf("expected one refresh call, got %d", provider.refreshCalls)
	}
}

type freshnessProvider struct {
	testProvider
	refreshResult RefreshResult
	refreshErr    error
	refreshCalls  int
}

func (p *freshnessProvider) Refresh(_ context.Context, _ ActiveCredential) (RefreshResult, error) {
	p.refreshCalls++
	if p.refreshErr != nil {
		return RefreshResult{}, p.refreshErr
	}
	return p.refreshResult, nil
}

func saveTestActiveCredential(ctx context.Context, store *memoryCredentialStore, connectionID string, active ActiveCredential) error {
	codec := JSONCredentialCodec{}
	payload, err := codec.Encode(active)
	if err != nil {
		return err
	}
	encrypted, err := testSecretProvider{}.Encrypt(ctx, payload)
	if err != nil {
		return err
	}
	_, err = store.SaveNewVersion(ctx, SaveCredentialInput{
		ConnectionID:     connectionID,
		EncryptedPayload: encrypted,
		PayloadFormat:    codec.Format(),
		PayloadVersion:   codec.Version(),
		TokenType:        active.TokenType,
		RequestedScopes:  append([]string(nil), active.RequestedScopes...),
		GrantedScopes:    append([]string(nil), active.GrantedScopes...),
		ExpiresAt:        active.ExpiresAt,
		Refreshable:      active.Refreshable,
		RotatesAt:        active.RotatesAt,
		Status:           CredentialStatusActive,
	})
	return err
}

func ptrTime(value time.Time) *time.Time {
	utc := value.UTC()
	return &utc
}
