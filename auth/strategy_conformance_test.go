package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestStrategyConformanceByMode(t *testing.T) {
	now := time.Date(2026, 2, 13, 18, 0, 0, 0, time.UTC)
	privateKeyPEM := generateTestRSAPrivateKeyPEM(t)
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "cc_conformance_token",
			"token_type":   "bearer",
			"expires_in":   1800,
			"scope":        "read",
		})
	}))
	defer tokenServer.Close()

	tests := []struct {
		name         string
		expectedType string
		strategy     core.AuthStrategy
		completeReq  core.AuthCompleteRequest
	}{
		{
			name:         "api_key",
			expectedType: core.AuthKindAPIKey,
			strategy:     NewAPIKeyStrategy(APIKeyStrategyConfig{}),
			completeReq: core.AuthCompleteRequest{
				Scope:    core.ScopeRef{Type: "user", ID: "u1"},
				Metadata: map[string]any{"api_key": "k1"},
			},
		},
		{
			name:         "pat",
			expectedType: core.AuthKindPAT,
			strategy:     NewPATStrategy(APIKeyStrategyConfig{}),
			completeReq: core.AuthCompleteRequest{
				Scope:    core.ScopeRef{Type: "user", ID: "u2"},
				Metadata: map[string]any{"pat": "pat_1"},
			},
		},
		{
			name:         "hmac",
			expectedType: core.AuthKindHMAC,
			strategy:     NewHMACStrategy(HMACStrategyConfig{}),
			completeReq: core.AuthCompleteRequest{
				Scope:    core.ScopeRef{Type: "org", ID: "o1"},
				Metadata: map[string]any{"hmac_secret": "secret_1"},
			},
		},
		{
			name:         "service_account_jwt",
			expectedType: core.AuthKindServiceAccountJWT,
			strategy: NewServiceAccountJWTStrategy(ServiceAccountJWTStrategyConfig{
				Issuer:           "svc@example.com",
				Subject:          "svc@example.com",
				Audience:         "https://oauth2.googleapis.com/token",
				SigningAlgorithm: "RS256",
				SigningKey:       privateKeyPEM,
				Now: func() time.Time {
					return now
				},
			}),
			completeReq: core.AuthCompleteRequest{
				Scope: core.ScopeRef{Type: "org", ID: "o2"},
			},
		},
		{
			name:         "oauth2_client_credentials",
			expectedType: core.AuthKindOAuth2ClientCredential,
			strategy: NewOAuth2ClientCredentialsStrategy(OAuth2ClientCredentialsStrategyConfig{
				ClientID:     "client_1",
				ClientSecret: "secret_1",
				TokenURL:     tokenServer.URL,
				Now: func() time.Time {
					return now
				},
			}),
			completeReq: core.AuthCompleteRequest{
				Scope: core.ScopeRef{Type: "org", ID: "o3"},
			},
		},
		{
			name:         "basic",
			expectedType: core.AuthKindBasic,
			strategy: NewBasicStrategy(BasicStrategyConfig{
				Username: "user_1",
				Password: "pass_1",
			}),
			completeReq: core.AuthCompleteRequest{
				Scope: core.ScopeRef{Type: "user", ID: "u3"},
			},
		},
		{
			name:         "mtls",
			expectedType: core.AuthKindMTLS,
			strategy: NewMTLSStrategy(MTLSStrategyConfig{
				CertRef: "cert://a",
				KeyRef:  "key://a",
			}),
			completeReq: core.AuthCompleteRequest{
				Scope: core.ScopeRef{Type: "org", ID: "o4"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			if got := tc.strategy.Type(); got != tc.expectedType {
				t.Fatalf("expected strategy type %q, got %q", tc.expectedType, got)
			}

			begin, err := tc.strategy.Begin(ctx, core.AuthBeginRequest{
				Scope:        tc.completeReq.Scope,
				RequestedRaw: []string{"read"},
			})
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			if begin.Metadata["auth_kind"] == nil {
				t.Fatalf("expected auth_kind metadata")
			}

			complete, err := tc.strategy.Complete(ctx, tc.completeReq)
			if err != nil {
				t.Fatalf("complete: %v", err)
			}
			if complete.Credential.TokenType == "" {
				t.Fatalf("expected token type in completed credential")
			}

			if _, err := tc.strategy.Refresh(ctx, complete.Credential); err != nil {
				t.Fatalf("refresh: %v", err)
			}
		})
	}
}
