package workday

import (
	"context"
	"strings"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestNew_UsesServiceAccountJWTAuthAndCapabilities(t *testing.T) {
	providerRaw, err := New(Config{
		Issuer:           "svc-account@example.iam.gserviceaccount.com",
		Audience:         "https://api.workday.test/token",
		SigningKey:       "secret-signing-key",
		SigningAlgorithm: "HS256",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	provider, ok := providerRaw.(*Provider)
	if !ok {
		t.Fatalf("expected *Provider")
	}
	if provider.AuthKind() != core.AuthKindServiceAccountJWT {
		t.Fatalf("expected auth kind %q, got %q", core.AuthKindServiceAccountJWT, provider.AuthKind())
	}
	if provider.AuthStrategy() == nil {
		t.Fatalf("expected auth strategy")
	}
	if len(provider.Capabilities()) == 0 {
		t.Fatalf("expected capabilities")
	}
}

func TestProvider_CompleteAuthReturnsJWTLikeAccessToken(t *testing.T) {
	providerRaw, err := New(Config{
		Issuer:           "svc-account@example.iam.gserviceaccount.com",
		Audience:         "https://api.workday.test/token",
		SigningKey:       "secret-signing-key",
		SigningAlgorithm: "HS256",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	provider := providerRaw.(*Provider)

	complete, err := provider.CompleteAuth(context.Background(), core.CompleteAuthRequest{
		Scope: core.ScopeRef{Type: "org", ID: "org_1"},
		Metadata: map[string]any{
			"subject": "tenant-admin@example.com",
		},
	})
	if err != nil {
		t.Fatalf("complete auth: %v", err)
	}
	parts := strings.Split(strings.TrimSpace(complete.Credential.AccessToken), ".")
	if len(parts) != 3 {
		t.Fatalf("expected jwt-like token format, got %q", complete.Credential.AccessToken)
	}
}

func TestProvider_ResolveCapabilityOperation_UsesProtocolKinds(t *testing.T) {
	providerRaw, err := New(Config{
		Issuer:           "svc-account@example.iam.gserviceaccount.com",
		Audience:         "https://api.workday.test/token",
		SigningKey:       "secret-signing-key",
		SigningAlgorithm: "HS256",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	provider := providerRaw.(*Provider)

	reportDegraded, err := provider.ResolveCapabilityOperation(context.Background(), core.CapabilityOperationResolveRequest{
		ProviderID: "workday",
		Scope:      core.ScopeRef{Type: "org", ID: "org_1"},
		Connection: core.Connection{ID: "conn_1"},
		Capability: "hr.reports.export",
		Decision: core.CapabilityResult{
			Allowed: true,
			Mode:    core.CapabilityDeniedBehaviorDegrade,
			Metadata: map[string]any{
				"missing_grants": []string{GrantReportExport},
			},
		},
	})
	if err != nil {
		t.Fatalf("resolve degraded report operation: %v", err)
	}
	if reportDegraded.TransportKind != "stream" {
		t.Fatalf("expected degraded report transport kind stream, got %q", reportDegraded.TransportKind)
	}

	reportFull, err := provider.ResolveCapabilityOperation(context.Background(), core.CapabilityOperationResolveRequest{
		ProviderID: "workday",
		Scope:      core.ScopeRef{Type: "org", ID: "org_1"},
		Connection: core.Connection{ID: "conn_1"},
		Capability: "hr.reports.export",
		Decision: core.CapabilityResult{
			Allowed: true,
			Mode:    core.CapabilityDeniedBehaviorBlock,
		},
	})
	if err != nil {
		t.Fatalf("resolve full report operation: %v", err)
	}
	if reportFull.TransportKind != "file" {
		t.Fatalf("expected full report transport kind file, got %q", reportFull.TransportKind)
	}

	compDegraded, err := provider.ResolveCapabilityOperation(context.Background(), core.CapabilityOperationResolveRequest{
		ProviderID: "workday",
		Scope:      core.ScopeRef{Type: "org", ID: "org_1"},
		Connection: core.Connection{ID: "conn_1"},
		Capability: "hr.compensation.read",
		Decision: core.CapabilityResult{
			Allowed: true,
			Mode:    core.CapabilityDeniedBehaviorDegrade,
			Metadata: map[string]any{
				"missing_grants": []string{GrantCompRead},
			},
		},
	})
	if err != nil {
		t.Fatalf("resolve degraded compensation operation: %v", err)
	}
	if compDegraded.TransportKind != "soap" {
		t.Fatalf("expected degraded compensation transport kind soap, got %q", compDegraded.TransportKind)
	}
}
