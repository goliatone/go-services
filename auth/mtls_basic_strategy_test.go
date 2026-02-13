package auth

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestBasicStrategy_Complete(t *testing.T) {
	strategy := NewBasicStrategy(BasicStrategyConfig{
		Username: "user_1",
		Password: "pass_1",
	})

	result, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "user", ID: "u1"},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	expected := base64.StdEncoding.EncodeToString([]byte("user_1:pass_1"))
	if result.Credential.AccessToken != expected {
		t.Fatalf("unexpected encoded basic token")
	}
	if result.Credential.TokenType != core.AuthKindBasic {
		t.Fatalf("expected basic token type")
	}
}

func TestBasicStrategy_CompleteRequiresCredentials(t *testing.T) {
	strategy := NewBasicStrategy(BasicStrategyConfig{})
	_, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "user", ID: "u2"},
	})
	if err == nil {
		t.Fatalf("expected missing credentials error")
	}
}

func TestMTLSStrategy_Complete(t *testing.T) {
	strategy := NewMTLSStrategy(MTLSStrategyConfig{
		CertRef: "cert://id",
		KeyRef:  "key://id",
	})

	result, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "org", ID: "o1"},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if result.Credential.TokenType != core.AuthKindMTLS {
		t.Fatalf("expected mtls token type")
	}
	if result.Credential.Metadata["cert_ref"] != "cert://id" {
		t.Fatalf("expected cert ref metadata")
	}
}

func TestMTLSStrategy_CompleteRequiresCertAndKeyRef(t *testing.T) {
	strategy := NewMTLSStrategy(MTLSStrategyConfig{})
	_, err := strategy.Complete(context.Background(), core.AuthCompleteRequest{
		Scope: core.ScopeRef{Type: "org", ID: "o2"},
	})
	if err == nil {
		t.Fatalf("expected mtls missing cert/key error")
	}
}

