package services_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	services "github.com/goliatone/go-services"
	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/devkit"
	"github.com/goliatone/go-services/ratelimit"
)

func TestDownstreamComposition_UsesExecutionPrimitiveWithoutOwningRuntimeInternals(t *testing.T) {
	registry := core.NewProviderRegistry()
	if err := registry.Register(downstreamProvider{id: "github"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	adapter := devkit.NewFakeTransportAdapter("rest",
		devkit.TransportScript{
			Response: core.TransportResponse{
				StatusCode: 429,
				Headers:    map[string]string{"Retry-After": "2"},
				Body:       []byte(`{"error":"throttled"}`),
			},
		},
		devkit.TransportScript{
			Response: core.TransportResponse{
				StatusCode: 200,
				Headers:    map[string]string{},
				Body:       []byte(`{"items":[{"id":"o_1"}]}`),
			},
		},
	)

	now := time.Unix(1_700_000_000, 0).UTC()
	rateStore := ratelimit.NewMemoryStateStore()
	policy := ratelimit.NewAdaptivePolicy(rateStore)
	policy.Now = func() time.Time { return now }

	svc, err := services.NewService(
		services.Config{},
		services.WithRegistry(registry),
		services.WithTransportResolver(staticTransportResolver{adapter: adapter}),
		services.WithRateLimitPolicy(policy),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	domain := downstreamOrdersDomain{runtime: svc}
	var delays []time.Duration
	result, err := domain.FetchOrders(
		context.Background(),
		core.ScopeRef{Type: "org", ID: "org_1"},
		core.ActiveCredential{AccessToken: "token_abc", TokenType: "bearer"},
		func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			now = now.Add(delay)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("fetch orders through runtime primitive: %v", err)
	}
	if result.Response.StatusCode != 200 {
		t.Fatalf("expected final status 200, got %d", result.Response.StatusCode)
	}
	if result.Attempts != 2 || !result.Retried {
		t.Fatalf("expected retried runtime result metadata, got %#v", result)
	}
	if len(delays) != 1 || delays[0] != 2*time.Second {
		t.Fatalf("expected retry-after delay propagation, got %+v", delays)
	}

	requests := adapter.Requests()
	if len(requests) != 2 {
		t.Fatalf("expected two transport calls, got %d", len(requests))
	}
	if requests[0].Headers["Authorization"] != "Bearer token_abc" {
		t.Fatalf("expected signer-managed authorization header on downstream operation")
	}
	if requests[0].Idempotency == "" || requests[1].Idempotency == "" {
		t.Fatalf("expected idempotency key on each attempt")
	}
	if requests[0].Idempotency != requests[1].Idempotency {
		t.Fatalf("expected stable idempotency key across retries")
	}

	state, err := rateStore.Get(context.Background(), core.RateLimitKey{
		ProviderID: "github",
		ScopeType:  "org",
		ScopeID:    "org_1",
		BucketKey:  "orders_api",
	})
	if err != nil {
		t.Fatalf("load persisted rate-limit state: %v", err)
	}
	if state.Attempts != 0 || state.ThrottledUntil != nil {
		t.Fatalf("expected rate-limit state reset after successful retry, got %#v", state)
	}
}

type downstreamRuntime interface {
	ExecuteProviderOperation(
		ctx context.Context,
		req core.ProviderOperationRequest,
	) (core.ProviderOperationResult, error)
}

type downstreamOrdersDomain struct {
	runtime downstreamRuntime
}

func (d downstreamOrdersDomain) FetchOrders(
	ctx context.Context,
	scope core.ScopeRef,
	credential core.ActiveCredential,
	sleepFn func(ctx context.Context, delay time.Duration) error,
) (core.ProviderOperationResult, error) {
	if d.runtime == nil {
		return core.ProviderOperationResult{}, fmt.Errorf("runtime is required")
	}
	return d.runtime.ExecuteProviderOperation(ctx, core.ProviderOperationRequest{
		ProviderID:    "github",
		Scope:         scope,
		Operation:     "orders.sync",
		BucketKey:     "orders_api",
		TransportKind: "rest",
		TransportRequest: core.TransportRequest{
			Method: "GET",
			URL:    "https://api.example.test/orders",
		},
		Credential: &credential,
		Retry: core.ProviderOperationRetryPolicy{
			MaxAttempts: 2,
			Sleep:       sleepFn,
		},
	})
}

type staticTransportResolver struct {
	adapter core.TransportAdapter
}

func (r staticTransportResolver) Build(string, map[string]any) (core.TransportAdapter, error) {
	if r.adapter == nil {
		return nil, fmt.Errorf("transport adapter is required")
	}
	return r.adapter, nil
}

type downstreamProvider struct {
	id string
}

func (p downstreamProvider) ID() string { return p.id }

func (downstreamProvider) AuthKind() core.AuthKind { return core.AuthKindOAuth2AuthCode }

func (downstreamProvider) SupportedScopeTypes() []string { return []string{"user", "org"} }

func (downstreamProvider) Capabilities() []core.CapabilityDescriptor { return nil }

func (downstreamProvider) BeginAuth(context.Context, core.BeginAuthRequest) (core.BeginAuthResponse, error) {
	return core.BeginAuthResponse{}, nil
}

func (downstreamProvider) CompleteAuth(context.Context, core.CompleteAuthRequest) (core.CompleteAuthResponse, error) {
	return core.CompleteAuthResponse{}, nil
}

func (downstreamProvider) Refresh(context.Context, core.ActiveCredential) (core.RefreshResult, error) {
	return core.RefreshResult{}, nil
}
