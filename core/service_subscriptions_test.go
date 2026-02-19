package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

var errTestRenewFailed = errors.New("renew failed")

func TestService_SubscriptionLifecycle_PersistsState(t *testing.T) {
	ctx := context.Background()
	connectionStore := newMemoryConnectionStore()
	subscriptionStore := newMemorySubscriptionStore()
	provider := &subscribableTestProvider{
		id: "github",
		subscribeResult: SubscriptionResult{
			ChannelID:            "chan_1",
			RemoteSubscriptionID: "remote_1",
			Metadata: map[string]any{
				"lease": "initial",
			},
		},
		renewResult: SubscriptionResult{
			ChannelID:            "chan_1",
			RemoteSubscriptionID: "remote_renewed",
			Metadata: map[string]any{
				"lease": "renewed",
			},
		},
	}
	registry := NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	svc, err := NewService(Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
		WithSubscriptionStore(subscriptionStore),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	connection, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "user", ID: "usr_1"},
		ExternalAccountID: "acct_1",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	subscription, err := svc.Subscribe(ctx, SubscribeRequest{
		ConnectionID: connection.ID,
		ResourceType: "drive.file",
		ResourceID:   "file_1",
		CallbackURL:  "https://app.example/webhooks/github",
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if subscription.Status != SubscriptionStatusActive {
		t.Fatalf("expected active subscription status, got %q", subscription.Status)
	}
	if subscription.RemoteSubscriptionID != "remote_1" {
		t.Fatalf("expected persisted remote id")
	}

	renewed, err := svc.RenewSubscription(ctx, RenewSubscriptionRequest{
		SubscriptionID: subscription.ID,
	})
	if err != nil {
		t.Fatalf("renew subscription: %v", err)
	}
	if renewed.RemoteSubscriptionID != "remote_renewed" {
		t.Fatalf("expected renewed remote subscription id")
	}
	if renewed.Metadata["lease"] != "renewed" {
		t.Fatalf("expected renewed metadata to persist")
	}

	if err := svc.CancelSubscription(ctx, CancelSubscriptionRequest{
		SubscriptionID: renewed.ID,
		Reason:         "user disconnect",
	}); err != nil {
		t.Fatalf("cancel subscription: %v", err)
	}

	stored, err := subscriptionStore.Get(ctx, renewed.ID)
	if err != nil {
		t.Fatalf("load stored subscription: %v", err)
	}
	if stored.Status != SubscriptionStatusCancelled {
		t.Fatalf("expected cancelled status, got %q", stored.Status)
	}
	if stored.Metadata["status_reason"] != "user disconnect" {
		t.Fatalf("expected cancellation reason metadata")
	}
	if provider.cancelCount != 1 {
		t.Fatalf("expected provider cancel to be called exactly once")
	}
}

func TestService_RenewSubscriptionMarksErroredOnProviderFailure(t *testing.T) {
	ctx := context.Background()
	connectionStore := newMemoryConnectionStore()
	subscriptionStore := newMemorySubscriptionStore()
	provider := &subscribableTestProvider{
		id:       "github",
		renewErr: errTestRenewFailed,
	}
	registry := NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	svc, err := NewService(Config{},
		WithRegistry(registry),
		WithConnectionStore(connectionStore),
		WithSubscriptionStore(subscriptionStore),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	connection, err := connectionStore.Create(ctx, CreateConnectionInput{
		ProviderID:        "github",
		Scope:             ScopeRef{Type: "user", ID: "usr_2"},
		ExternalAccountID: "acct_2",
		Status:            ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	expiresAt := time.Now().UTC().Add(30 * time.Minute)
	subscription, err := subscriptionStore.Upsert(ctx, UpsertSubscriptionInput{
		ConnectionID: connection.ID,
		ProviderID:   "github",
		ResourceType: "drive.file",
		ResourceID:   "file_2",
		ChannelID:    "chan_2",
		CallbackURL:  "https://app.example/webhooks/github",
		Status:       SubscriptionStatusActive,
		ExpiresAt:    &expiresAt,
	})
	if err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	if _, err := svc.RenewSubscription(ctx, RenewSubscriptionRequest{
		SubscriptionID: subscription.ID,
	}); err == nil {
		t.Fatalf("expected renew failure")
	}

	stored, err := subscriptionStore.Get(ctx, subscription.ID)
	if err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if stored.Status != SubscriptionStatusErrored {
		t.Fatalf("expected errored status on failed renewal, got %q", stored.Status)
	}
}

type subscribableTestProvider struct {
	id string

	subscribeResult SubscriptionResult
	renewResult     SubscriptionResult
	renewErr        error
	cancelErr       error
	cancelCount     int
}

func (p *subscribableTestProvider) ID() string {
	return p.id
}

func (p *subscribableTestProvider) AuthKind() AuthKind {
	return AuthKind("oauth2")
}

func (p *subscribableTestProvider) SupportedScopeTypes() []string {
	return []string{"user", "org"}
}

func (p *subscribableTestProvider) Capabilities() []CapabilityDescriptor {
	return []CapabilityDescriptor{}
}

func (p *subscribableTestProvider) BeginAuth(context.Context, BeginAuthRequest) (BeginAuthResponse, error) {
	return BeginAuthResponse{}, nil
}

func (p *subscribableTestProvider) CompleteAuth(context.Context, CompleteAuthRequest) (CompleteAuthResponse, error) {
	return CompleteAuthResponse{}, nil
}

func (p *subscribableTestProvider) Refresh(context.Context, ActiveCredential) (RefreshResult, error) {
	return RefreshResult{}, nil
}

func (p *subscribableTestProvider) Subscribe(context.Context, SubscribeRequest) (SubscriptionResult, error) {
	return p.subscribeResult, nil
}

func (p *subscribableTestProvider) RenewSubscription(context.Context, RenewSubscriptionRequest) (SubscriptionResult, error) {
	if p.renewErr != nil {
		return SubscriptionResult{}, p.renewErr
	}
	return p.renewResult, nil
}

func (p *subscribableTestProvider) CancelSubscription(context.Context, CancelSubscriptionRequest) error {
	p.cancelCount++
	return p.cancelErr
}
