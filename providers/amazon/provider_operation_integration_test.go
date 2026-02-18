package amazon

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestProviderOperationRuntime_UsesMandatorySigV4SignerWithHostAwareRegion(t *testing.T) {
	provider, err := New(Config{
		ClientID:     "client",
		ClientSecret: "secret",
		SigV4:        testSigV4Config(),
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	registry := core.NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	adapter := &amazonRecordingAdapter{response: core.TransportResponse{StatusCode: 200}}
	resolver := &amazonStaticResolver{adapter: adapter}

	svc, err := core.NewService(
		core.Config{},
		core.WithRegistry(registry),
		core.WithTransportResolver(resolver),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := svc.ExecuteProviderOperation(context.Background(), core.ProviderOperationRequest{
		ProviderID:    ProviderID,
		Scope:         core.ScopeRef{Type: "org", ID: "org_1"},
		Operation:     "orders.list",
		TransportKind: "rest",
		TransportRequest: core.TransportRequest{
			Method: "GET",
			URL:    "https://sellingpartnerapi-eu.amazon.com/orders/v0/orders",
		},
		Credential: &core.ActiveCredential{
			AccessToken: "lwa_token",
			Metadata: map[string]any{
				"auth_kind": core.AuthKindAWSSigV4,
			},
		},
		Retry: core.ProviderOperationRetryPolicy{MaxAttempts: 1},
	})
	if err != nil {
		t.Fatalf("execute provider operation: %v", err)
	}

	authHeader := adapter.request.Headers["Authorization"]
	if !strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256 ") {
		t.Fatalf("expected sigv4 authorization header, got %q", authHeader)
	}
	if !strings.Contains(authHeader, "/eu-west-1/") {
		t.Fatalf("expected eu-west-1 credential scope for EU host, got %q", authHeader)
	}
	if got := adapter.request.Headers["X-Amz-Access-Token"]; got != "lwa_token" {
		t.Fatalf("expected x-amz-access-token header, got %q", got)
	}
	if got := fmt.Sprint(result.Meta.Metadata["signed_region"]); got != "eu-west-1" {
		t.Fatalf("expected signed_region metadata eu-west-1, got %q", got)
	}
	if got := fmt.Sprint(result.Meta.Metadata["signed_service"]); got != defaultAWSService {
		t.Fatalf("expected signed_service metadata %q, got %q", defaultAWSService, got)
	}
}

type amazonRecordingAdapter struct {
	request  core.TransportRequest
	response core.TransportResponse
}

func (a *amazonRecordingAdapter) Kind() string {
	return "rest"
}

func (a *amazonRecordingAdapter) Do(_ context.Context, req core.TransportRequest) (core.TransportResponse, error) {
	a.request = req
	return a.response, nil
}

type amazonStaticResolver struct {
	adapter core.TransportAdapter
}

func (r *amazonStaticResolver) Build(kind string, config map[string]any) (core.TransportAdapter, error) {
	_ = config
	if kind != "rest" {
		return nil, fmt.Errorf("unexpected transport kind %q", kind)
	}
	if r == nil || r.adapter == nil {
		return nil, fmt.Errorf("transport resolver not configured")
	}
	return r.adapter, nil
}

var _ core.TransportAdapter = (*amazonRecordingAdapter)(nil)
var _ core.TransportResolver = (*amazonStaticResolver)(nil)
