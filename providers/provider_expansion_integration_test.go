package providers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/amazon"
	"github.com/goliatone/go-services/providers/google/shopping"
	"github.com/goliatone/go-services/providers/pinterest"
	"github.com/goliatone/go-services/providers/tiktok"
)

func TestProviderExpansion_ConnectionLifecycleRoundtrip(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access_token_1",
				"refresh_token": "refresh_token_1",
				"token_type":    "bearer",
				"expires_in":    3600,
				"scope":         "user.info.basic video.list video.insights user_accounts:read boards:read pins:read https://www.googleapis.com/auth/content sellingpartnerapi::catalog sellingpartnerapi::inventory sellingpartnerapi::orders",
			})
		case "refresh_token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access_token_2",
				"refresh_token": "refresh_token_2",
				"token_type":    "bearer",
				"expires_in":    3600,
				"scope":         "user.info.basic boards:read https://www.googleapis.com/auth/content sellingpartnerapi::orders",
			})
		default:
			http.Error(w, "unsupported grant type", http.StatusBadRequest)
		}
	}))
	defer tokenServer.Close()

	cases := []struct {
		name              string
		build             func() (core.Provider, error)
		scope             core.ScopeRef
		externalAccountID string
	}{
		{
			name: "tiktok",
			build: func() (core.Provider, error) {
				return tiktok.New(tiktok.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					AuthURL:      tiktok.AuthURL,
					TokenURL:     tokenServer.URL,
				})
			},
			scope:             core.ScopeRef{Type: "org", ID: "org_1"},
			externalAccountID: "tt_account_1",
		},
		{
			name: "pinterest",
			build: func() (core.Provider, error) {
				return pinterest.New(pinterest.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					AuthURL:      pinterest.AuthURL,
					TokenURL:     tokenServer.URL,
				})
			},
			scope:             core.ScopeRef{Type: "org", ID: "org_1"},
			externalAccountID: "pin_account_1",
		},
		{
			name: "google_shopping",
			build: func() (core.Provider, error) {
				return shopping.New(shopping.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					AuthURL:      shopping.AuthURL,
					TokenURL:     tokenServer.URL,
				})
			},
			scope:             core.ScopeRef{Type: "org", ID: "org_1"},
			externalAccountID: "merchant_account_1",
		},
		{
			name: "amazon",
			build: func() (core.Provider, error) {
				return amazon.New(amazon.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					AuthURL:      amazon.AuthURL,
					TokenURL:     tokenServer.URL,
					SigV4: amazon.SigV4Config{
						AccessKeyID:     "AKIA_TEST",
						SecretAccessKey: "secret_key",
						Region:          "us-east-1",
						Service:         "execute-api",
					},
				})
			},
			scope:             core.ScopeRef{Type: "org", ID: "org_1"},
			externalAccountID: "amz_seller_1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider, err := tc.build()
			if err != nil {
				t.Fatalf("new provider: %v", err)
			}

			complete, err := provider.CompleteAuth(context.Background(), core.CompleteAuthRequest{
				Scope:    tc.scope,
				Code:     "code_1",
				State:    "state_1",
				Metadata: map[string]any{"external_account_id": tc.externalAccountID},
			})
			if err != nil {
				t.Fatalf("complete auth: %v", err)
			}
			if complete.ExternalAccountID != tc.externalAccountID {
				t.Fatalf("expected external account id %q, got %q", tc.externalAccountID, complete.ExternalAccountID)
			}
			if complete.Credential.AccessToken != "access_token_1" {
				t.Fatalf("expected access token from callback")
			}

			refresh, err := provider.Refresh(context.Background(), complete.Credential)
			if err != nil {
				t.Fatalf("refresh credential: %v", err)
			}
			if refresh.Credential.AccessToken != "access_token_2" {
				t.Fatalf("expected refreshed access token, got %q", refresh.Credential.AccessToken)
			}
		})
	}
}

func TestProviderExpansion_GrantEnforcementIntegration(t *testing.T) {
	cases := []struct {
		name          string
		build         func() (core.Provider, error)
		capability    string
		blockedGrants []string
		allowedGrants []string
	}{
		{
			name: "tiktok",
			build: func() (core.Provider, error) {
				return tiktok.New(tiktok.Config{ClientID: "client", ClientSecret: "secret"})
			},
			capability:    "analytics.read",
			blockedGrants: []string{tiktok.GrantVideoList},
			allowedGrants: []string{tiktok.GrantVideoInsights},
		},
		{
			name: "pinterest",
			build: func() (core.Provider, error) {
				return pinterest.New(pinterest.Config{ClientID: "client", ClientSecret: "secret"})
			},
			capability:    "boards.read",
			blockedGrants: []string{pinterest.GrantPinsRead},
			allowedGrants: []string{pinterest.GrantBoardsRead},
		},
		{
			name: "google_shopping",
			build: func() (core.Provider, error) {
				return shopping.New(shopping.Config{ClientID: "client", ClientSecret: "secret"})
			},
			capability:    "orders.read",
			blockedGrants: []string{},
			allowedGrants: []string{shopping.GrantContent},
		},
		{
			name: "amazon",
			build: func() (core.Provider, error) {
				return amazon.New(amazon.Config{
					ClientID:     "client",
					ClientSecret: "secret",
					SigV4: amazon.SigV4Config{
						AccessKeyID:     "AKIA_TEST",
						SecretAccessKey: "secret_key",
						Region:          "us-east-1",
						Service:         "execute-api",
					},
				})
			},
			capability:    "orders.read",
			blockedGrants: []string{amazon.GrantCatalogRead},
			allowedGrants: []string{amazon.GrantOrdersRead},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider, err := tc.build()
			if err != nil {
				t.Fatalf("new provider: %v", err)
			}

			registry := core.NewProviderRegistry()
			if err := registry.Register(provider); err != nil {
				t.Fatalf("register provider: %v", err)
			}

			connection := core.Connection{
				ID:         "conn_" + strings.ReplaceAll(provider.ID(), "_", ""),
				ProviderID: provider.ID(),
				ScopeType:  "org",
				ScopeID:    "org_1",
				Status:     core.ConnectionStatusActive,
			}
			connectionStore := &providerConnectionStoreStub{connection: connection}
			grantStore := &providerGrantStoreStub{snapshot: core.GrantSnapshot{
				ConnectionID: connection.ID,
				Version:      1,
				Requested:    append([]string(nil), tc.allowedGrants...),
				Granted:      append([]string(nil), tc.blockedGrants...),
				CapturedAt:   time.Now().UTC(),
			}}

			svc, err := core.NewService(
				core.Config{},
				core.WithRegistry(registry),
				core.WithConnectionStore(connectionStore),
				core.WithGrantStore(grantStore),
			)
			if err != nil {
				t.Fatalf("new service: %v", err)
			}

			blocked, err := svc.InvokeCapability(context.Background(), core.InvokeCapabilityRequest{
				ProviderID:   provider.ID(),
				ConnectionID: connection.ID,
				Capability:   tc.capability,
			})
			if err != nil {
				t.Fatalf("invoke blocked capability: %v", err)
			}
			if blocked.Allowed {
				t.Fatalf("expected %s to be blocked when required grants are missing", tc.capability)
			}

			grantStore.snapshot.Granted = append([]string(nil), tc.allowedGrants...)
			allowed, err := svc.InvokeCapability(context.Background(), core.InvokeCapabilityRequest{
				ProviderID:   provider.ID(),
				ConnectionID: connection.ID,
				Capability:   tc.capability,
			})
			if err != nil {
				t.Fatalf("invoke allowed capability: %v", err)
			}
			if !allowed.Allowed {
				t.Fatalf("expected %s to be allowed once required grants are present", tc.capability)
			}
		})
	}
}

type providerConnectionStoreStub struct {
	connection core.Connection
}

func (s *providerConnectionStoreStub) Create(_ context.Context, _ core.CreateConnectionInput) (core.Connection, error) {
	return s.connection, nil
}

func (s *providerConnectionStoreStub) Get(_ context.Context, id string) (core.Connection, error) {
	if id != s.connection.ID {
		return core.Connection{}, fmt.Errorf("connection %q not found", id)
	}
	return s.connection, nil
}

func (s *providerConnectionStoreStub) FindByScope(
	_ context.Context,
	providerID string,
	scope core.ScopeRef,
) ([]core.Connection, error) {
	if providerID == s.connection.ProviderID &&
		scope.Type == s.connection.ScopeType &&
		scope.ID == s.connection.ScopeID {
		return []core.Connection{s.connection}, nil
	}
	return []core.Connection{}, nil
}

func (s *providerConnectionStoreStub) FindByScopeAndExternalAccount(
	_ context.Context,
	providerID string,
	scope core.ScopeRef,
	externalAccountID string,
) (core.Connection, bool, error) {
	if providerID == s.connection.ProviderID &&
		scope.Type == s.connection.ScopeType &&
		scope.ID == s.connection.ScopeID &&
		externalAccountID == s.connection.ExternalAccountID {
		return s.connection, true, nil
	}
	return core.Connection{}, false, nil
}

func (s *providerConnectionStoreStub) UpdateStatus(
	_ context.Context,
	id string,
	status string,
	reason string,
) error {
	if id != s.connection.ID {
		return fmt.Errorf("connection %q not found", id)
	}
	_ = reason
	s.connection.Status = core.ConnectionStatus(status)
	return nil
}

type providerGrantStoreStub struct {
	snapshot core.GrantSnapshot
}

func (s *providerGrantStoreStub) SaveSnapshot(_ context.Context, in core.SaveGrantSnapshotInput) error {
	s.snapshot = core.GrantSnapshot{
		ConnectionID: in.ConnectionID,
		Version:      in.Version,
		Requested:    append([]string(nil), in.Requested...),
		Granted:      append([]string(nil), in.Granted...),
		CapturedAt:   in.CapturedAt,
		Metadata:     copyMetadata(in.Metadata),
	}
	return nil
}

func (s *providerGrantStoreStub) GetLatestSnapshot(_ context.Context, connectionID string) (core.GrantSnapshot, bool, error) {
	if s.snapshot.ConnectionID != connectionID {
		return core.GrantSnapshot{}, false, nil
	}
	return s.snapshot, true, nil
}

func (s *providerGrantStoreStub) AppendEvent(context.Context, core.AppendGrantEventInput) error {
	return nil
}

func copyMetadata(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

var _ core.ConnectionStore = (*providerConnectionStoreStub)(nil)
var _ core.GrantStore = (*providerGrantStoreStub)(nil)
