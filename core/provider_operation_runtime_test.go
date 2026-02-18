package core

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	goerrors "github.com/goliatone/go-errors"
)

func TestService_ExecuteProviderOperation_SignsAndAppliesRateLimitHooks(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	adapter := &recordingTransportAdapter{
		kind: "rest",
		responses: []TransportResponse{
			{StatusCode: 200, Headers: map[string]string{"X-Trace": "1"}, Body: []byte(`{"ok":true}`)},
		},
	}
	resolver := &staticTransportResolver{adapter: adapter}
	policy := &recordingRateLimitPolicy{}

	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithTransportResolver(resolver),
		WithRateLimitPolicy(policy),
		WithSigner(BearerTokenSigner{}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := svc.ExecuteProviderOperation(context.Background(), ProviderOperationRequest{
		ProviderID:    "github",
		Scope:         ScopeRef{Type: "org", ID: "org_1"},
		Operation:     "issues.list",
		BucketKey:     "issues",
		TransportKind: "rest",
		TransportRequest: TransportRequest{
			Method: "GET",
			URL:    "https://api.example.test/issues",
		},
		Credential: &ActiveCredential{AccessToken: "token_123"},
		Retry: ProviderOperationRetryPolicy{
			MaxAttempts: 1,
		},
	})
	if err != nil {
		t.Fatalf("execute provider operation: %v", err)
	}
	if result.Attempts != 1 || result.Retried {
		t.Fatalf("unexpected attempt metadata: %#v", result)
	}
	if result.AuthStrategy == "" {
		t.Fatalf("expected resolved auth strategy metadata")
	}
	if len(adapter.requests) != 1 {
		t.Fatalf("expected one adapter invocation, got %d", len(adapter.requests))
	}
	if got := adapter.requests[0].Headers["Authorization"]; got != "Bearer token_123" {
		t.Fatalf("expected bearer signature header, got %q", got)
	}
	if adapter.requests[0].Headers["Idempotency-Key"] == "" {
		t.Fatalf("expected idempotency key header to be set")
	}
	if adapter.requests[0].Idempotency != result.Idempotency {
		t.Fatalf("expected result idempotency to match request idempotency")
	}
	if len(policy.beforeCalls) != 1 || len(policy.afterCalls) != 1 {
		t.Fatalf("expected rate-limit before/after calls, got before=%d after=%d", len(policy.beforeCalls), len(policy.afterCalls))
	}
}

func TestService_ExecuteProviderOperation_RetryMiddlewarePreservesIdempotency(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	adapter := &recordingTransportAdapter{
		kind: "rest",
		responses: []TransportResponse{
			{StatusCode: 429, Headers: map[string]string{"Retry-After": "3"}},
			{StatusCode: 200, Headers: map[string]string{}},
		},
	}
	resolver := &staticTransportResolver{adapter: adapter}
	var delays []time.Duration

	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithTransportResolver(resolver),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := svc.ExecuteProviderOperation(context.Background(), ProviderOperationRequest{
		ProviderID:    "github",
		Scope:         ScopeRef{Type: "org", ID: "org_1"},
		TransportKind: "rest",
		TransportRequest: TransportRequest{
			Method: "POST",
			URL:    "https://api.example.test/issues",
			Body:   []byte(`{"title":"x"}`),
		},
		Credential: &ActiveCredential{AccessToken: "token_123"},
		Retry: ProviderOperationRetryPolicy{
			MaxAttempts: 2,
			Sleep: func(_ context.Context, delay time.Duration) error {
				delays = append(delays, delay)
				return nil
			},
		},
	})
	if err != nil {
		t.Fatalf("execute provider operation with retry: %v", err)
	}
	if result.Attempts != 2 || !result.Retried {
		t.Fatalf("expected retried result metadata, got %#v", result)
	}
	if len(delays) != 1 || delays[0] != 3*time.Second {
		t.Fatalf("expected retry-after delay of 3s, got %+v", delays)
	}
	if len(adapter.requests) != 2 {
		t.Fatalf("expected two adapter invocations, got %d", len(adapter.requests))
	}
	if adapter.requests[0].Idempotency == "" || adapter.requests[1].Idempotency == "" {
		t.Fatalf("expected idempotency key on both attempts")
	}
	if adapter.requests[0].Idempotency != adapter.requests[1].Idempotency {
		t.Fatalf("expected same idempotency key across attempts")
	}
}

func TestGenerateIdempotencyKey_CanonicalizesAndIncludesQueryParameters(t *testing.T) {
	providerID := "github"
	connectionID := "conn_1"
	operation := "issues.list"

	fromQueryMap := generateIdempotencyKey(providerID, connectionID, operation, TransportRequest{
		Method: "GET",
		URL:    "https://api.example.test/issues",
		Query: map[string]string{
			"q":    "search",
			"page": "1",
		},
	})
	fromRawURL := generateIdempotencyKey(providerID, connectionID, operation, TransportRequest{
		Method: "GET",
		URL:    "https://api.example.test/issues?page=1&q=search",
	})
	if fromQueryMap != fromRawURL {
		t.Fatalf("expected canonical URL+query to produce identical idempotency keys")
	}

	differentQuery := generateIdempotencyKey(providerID, connectionID, operation, TransportRequest{
		Method: "GET",
		URL:    "https://api.example.test/issues",
		Query: map[string]string{
			"q":    "different",
			"page": "1",
		},
	})
	if fromQueryMap == differentQuery {
		t.Fatalf("expected query value changes to change idempotency key")
	}
}

func TestService_ExecuteProviderOperation_ReturnsTypedFailureAfterRetries(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	adapter := &recordingTransportAdapter{
		kind: "rest",
		responses: []TransportResponse{
			{StatusCode: 503},
			{StatusCode: 503},
		},
	}
	resolver := &staticTransportResolver{adapter: adapter}

	svc, err := NewService(Config{}, WithRegistry(registry), WithTransportResolver(resolver))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.ExecuteProviderOperation(context.Background(), ProviderOperationRequest{
		ProviderID:    "github",
		Scope:         ScopeRef{Type: "org", ID: "org_1"},
		TransportKind: "rest",
		TransportRequest: TransportRequest{
			Method: "GET",
			URL:    "https://api.example.test/issues",
		},
		Retry: ProviderOperationRetryPolicy{
			MaxAttempts: 2,
			Sleep:       func(context.Context, time.Duration) error { return nil },
		},
	})
	if err == nil {
		t.Fatalf("expected provider operation failure")
	}

	var rich *goerrors.Error
	if !goerrors.As(err, &rich) {
		t.Fatalf("expected go-errors envelope, got %T", err)
	}
	if rich.TextCode != ServiceErrorProviderOperationFailed {
		t.Fatalf("expected %s, got %s", ServiceErrorProviderOperationFailed, rich.TextCode)
	}

	var opErr *ProviderOperationError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected provider operation error in chain")
	}
	if opErr.StatusCode != 503 {
		t.Fatalf("expected status 503, got %d", opErr.StatusCode)
	}
}

func TestService_ExecuteProviderOperation_MapsRateLimitDeterministically(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	adapter := &recordingTransportAdapter{
		kind:      "rest",
		responses: []TransportResponse{{StatusCode: 200}},
	}
	policy := &recordingRateLimitPolicy{beforeErr: fmt.Errorf("throttled by upstream")}
	resolver := &staticTransportResolver{adapter: adapter}

	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithTransportResolver(resolver),
		WithRateLimitPolicy(policy),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.ExecuteProviderOperation(context.Background(), ProviderOperationRequest{
		ProviderID:    "github",
		Scope:         ScopeRef{Type: "org", ID: "org_1"},
		TransportKind: "rest",
		TransportRequest: TransportRequest{
			Method: "GET",
			URL:    "https://api.example.test/issues",
		},
		Retry: ProviderOperationRetryPolicy{MaxAttempts: 1},
	})
	if err == nil {
		t.Fatalf("expected throttled error")
	}
	var rich *goerrors.Error
	if !goerrors.As(err, &rich) {
		t.Fatalf("expected go-errors envelope, got %T", err)
	}
	if rich.TextCode != ServiceErrorRateLimited {
		t.Fatalf("expected %s, got %s", ServiceErrorRateLimited, rich.TextCode)
	}
}

func TestService_ExecuteProviderOperation_NormalizesSigV4SigningMetadata(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "amazon"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	adapter := &recordingTransportAdapter{
		kind: "rest",
		responses: []TransportResponse{
			{
				StatusCode: 200,
				Headers: map[string]string{
					"Date": "Wed, 18 Feb 2026 12:05:00 GMT",
				},
			},
		},
	}
	resolver := &staticTransportResolver{adapter: adapter}
	signer := AWSSigV4Signer{
		Now: func() time.Time {
			return time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
		},
	}

	svc, err := NewService(
		Config{},
		WithRegistry(registry),
		WithTransportResolver(resolver),
		WithSigner(signer),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := svc.ExecuteProviderOperation(context.Background(), ProviderOperationRequest{
		ProviderID:    "amazon",
		Scope:         ScopeRef{Type: "org", ID: "org_1"},
		TransportKind: "rest",
		TransportRequest: TransportRequest{
			Method: "GET",
			URL:    "https://sellingpartnerapi-na.amazon.com/orders/v0/orders",
		},
		Credential: &ActiveCredential{
			AccessToken: "lwa_token",
			Metadata: map[string]any{
				"auth_kind":               AuthKindAWSSigV4,
				"aws_access_key_id":       "AKIAEXAMPLE",
				"aws_secret_access_key":   "secret_value",
				"aws_region":              "us-east-1",
				"aws_service":             "execute-api",
				"aws_signing_mode":        "header",
				"aws_access_token_header": "x-amz-access-token",
			},
		},
		Retry: ProviderOperationRetryPolicy{MaxAttempts: 1},
	})
	if err != nil {
		t.Fatalf("execute provider operation: %v", err)
	}
	if got := fmt.Sprint(result.Meta.Metadata["signing_profile"]); got != AuthKindAWSSigV4 {
		t.Fatalf("expected signing_profile %q, got %q", AuthKindAWSSigV4, got)
	}
	if got := fmt.Sprint(result.Meta.Metadata["signed_region"]); got != "us-east-1" {
		t.Fatalf("expected signed_region metadata, got %q", got)
	}
	if got := fmt.Sprint(result.Meta.Metadata["signing_mode"]); got != "header" {
		t.Fatalf("expected signing_mode metadata, got %q", got)
	}
	if _, ok := result.Meta.Metadata["clock_skew_hint_seconds"]; !ok {
		t.Fatalf("expected clock skew hint metadata")
	}
}

type staticTransportResolver struct {
	adapter TransportAdapter
}

func (r *staticTransportResolver) Build(string, map[string]any) (TransportAdapter, error) {
	if r == nil || r.adapter == nil {
		return nil, fmt.Errorf("transport resolver not configured")
	}
	return r.adapter, nil
}

type recordingTransportAdapter struct {
	kind      string
	responses []TransportResponse
	errs      []error
	requests  []TransportRequest
}

func (a *recordingTransportAdapter) Kind() string {
	if a == nil {
		return ""
	}
	return a.kind
}

func (a *recordingTransportAdapter) Do(_ context.Context, req TransportRequest) (TransportResponse, error) {
	if a == nil {
		return TransportResponse{}, fmt.Errorf("adapter is nil")
	}
	a.requests = append(a.requests, cloneTransportRequest(req))
	index := len(a.requests) - 1
	if index < len(a.errs) && a.errs[index] != nil {
		return TransportResponse{}, a.errs[index]
	}
	if index < len(a.responses) {
		return a.responses[index], nil
	}
	if len(a.responses) > 0 {
		return a.responses[len(a.responses)-1], nil
	}
	return TransportResponse{StatusCode: 200}, nil
}

type recordingRateLimitPolicy struct {
	beforeErr   error
	afterErr    error
	beforeCalls []RateLimitKey
	afterCalls  []ProviderResponseMeta
}

func (p *recordingRateLimitPolicy) BeforeCall(_ context.Context, key RateLimitKey) error {
	if p != nil {
		p.beforeCalls = append(p.beforeCalls, key)
	}
	if p == nil {
		return nil
	}
	return p.beforeErr
}

func (p *recordingRateLimitPolicy) AfterCall(_ context.Context, _ RateLimitKey, res ProviderResponseMeta) error {
	if p != nil {
		p.afterCalls = append(p.afterCalls, res)
	}
	if p == nil {
		return nil
	}
	return p.afterErr
}
