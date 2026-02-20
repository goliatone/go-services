package embedded

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestSessionTokenExchangeClient_SendsExpectedFormBodyAndHeaders(t *testing.T) {
	var receivedContentType string
	var receivedAccept string
	var receivedForm map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = strings.TrimSpace(r.Header.Get("Content-Type"))
		receivedAccept = strings.TrimSpace(r.Header.Get("Accept"))
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		receivedForm = map[string]string{
			"grant_type":           r.Form.Get("grant_type"),
			"subject_token":        r.Form.Get("subject_token"),
			"subject_token_type":   r.Form.Get("subject_token_type"),
			"requested_token_type": r.Form.Get("requested_token_type"),
			"client_id":            r.Form.Get("client_id"),
			"client_secret":        r.Form.Get("client_secret"),
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "offline_token_1",
			"token_type":   "bearer",
			"scope":        "read_products,read_orders",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	client := NewSessionTokenExchangeClient(ExchangeClientConfig{
		ClientID:     "client_id",
		ClientSecret: "client_secret",
		BuildTokenURL: func(_ string) (string, error) {
			return server.URL, nil
		},
		Now: func() time.Time {
			return time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	token, err := client.ExchangeSessionToken(context.Background(), ExchangeSessionTokenRequest{
		ShopDomain:   "merchant.myshopify.com",
		SessionToken: "session_jwt",
	})
	if err != nil {
		t.Fatalf("exchange session token: %v", err)
	}

	if receivedContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("unexpected content type: %q", receivedContentType)
	}
	if receivedAccept != "application/json" {
		t.Fatalf("unexpected accept header: %q", receivedAccept)
	}
	if receivedForm["grant_type"] != tokenExchangeGrantType {
		t.Fatalf("unexpected grant_type: %q", receivedForm["grant_type"])
	}
	if receivedForm["subject_token"] != "session_jwt" {
		t.Fatalf("unexpected subject_token: %q", receivedForm["subject_token"])
	}
	if receivedForm["subject_token_type"] != subjectTokenTypeIDToken {
		t.Fatalf("unexpected subject_token_type: %q", receivedForm["subject_token_type"])
	}
	if receivedForm["requested_token_type"] != requestedTypeOfflineURN {
		t.Fatalf("expected offline requested_token_type, got %q", receivedForm["requested_token_type"])
	}
	if token.AccessToken != "offline_token_1" {
		t.Fatalf("unexpected exchanged access token: %q", token.AccessToken)
	}
}

func TestSessionTokenExchangeClient_OfflineAndOnlineRequestedTokenTypeBehavior(t *testing.T) {
	var requestedTokenTypes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		requestedTokenTypes = append(requestedTokenTypes, r.Form.Get("requested_token_type"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token",
			"token_type":   "bearer",
		})
	}))
	defer server.Close()

	client := NewSessionTokenExchangeClient(ExchangeClientConfig{
		ClientID:     "client_id",
		ClientSecret: "client_secret",
		BuildTokenURL: func(_ string) (string, error) {
			return server.URL, nil
		},
	})

	if _, err := client.ExchangeSessionToken(context.Background(), ExchangeSessionTokenRequest{
		ShopDomain:   "merchant.myshopify.com",
		SessionToken: "session_jwt_1",
	}); err != nil {
		t.Fatalf("offline exchange: %v", err)
	}

	if _, err := client.ExchangeSessionToken(context.Background(), ExchangeSessionTokenRequest{
		ShopDomain:         "merchant.myshopify.com",
		SessionToken:       "session_jwt_2",
		RequestedTokenType: core.EmbeddedRequestedTokenTypeOnline,
	}); err != nil {
		t.Fatalf("online exchange: %v", err)
	}

	if len(requestedTokenTypes) != 2 {
		t.Fatalf("expected two requests, got %d", len(requestedTokenTypes))
	}
	if requestedTokenTypes[0] != requestedTypeOfflineURN {
		t.Fatalf("expected offline token type on default request, got %q", requestedTokenTypes[0])
	}
	if requestedTokenTypes[1] != requestedTypeOnlineURN {
		t.Fatalf("expected online token type on explicit online request, got %q", requestedTokenTypes[1])
	}
}

func TestSessionTokenExchangeClient_InvalidRequestedTokenTypeRejected(t *testing.T) {
	client := NewSessionTokenExchangeClient(ExchangeClientConfig{
		ClientID:     "client_id",
		ClientSecret: "client_secret",
		BuildTokenURL: func(_ string) (string, error) {
			return "https://merchant.myshopify.com/admin/oauth/access_token", nil
		},
	})

	_, err := client.ExchangeSessionToken(context.Background(), ExchangeSessionTokenRequest{
		ShopDomain:         "merchant.myshopify.com",
		SessionToken:       "session_jwt_1",
		RequestedTokenType: core.EmbeddedRequestedTokenType("custom"),
	})
	if err == nil {
		t.Fatalf("expected invalid requested token type error")
	}
	if !errors.Is(err, ErrInvalidRequestedTokenType) {
		t.Fatalf("expected ErrInvalidRequestedTokenType, got %v", err)
	}
}

func TestSessionTokenExchangeClient_MetadataDoesNotLeakAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "sensitive_token",
			"token_type":   "bearer",
			"scope":        "read_products",
		})
	}))
	defer server.Close()

	client := NewSessionTokenExchangeClient(ExchangeClientConfig{
		ClientID:     "client_id",
		ClientSecret: "client_secret",
		BuildTokenURL: func(_ string) (string, error) {
			return server.URL, nil
		},
	})
	token, err := client.ExchangeSessionToken(context.Background(), ExchangeSessionTokenRequest{
		ShopDomain:   "merchant.myshopify.com",
		SessionToken: "session_jwt_1",
	})
	if err != nil {
		t.Fatalf("exchange session token: %v", err)
	}

	if _, ok := token.Metadata["access_token"]; ok {
		t.Fatalf("access_token must not be copied into metadata")
	}
	if got := token.AccessToken; got != "sensitive_token" {
		t.Fatalf("expected access token to remain in token field, got %q", got)
	}
}

func TestSessionTokenExchangeClient_MapsErrorPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_subject_token",
			"error_description": "token is invalid",
		})
	}))
	defer server.Close()

	client := NewSessionTokenExchangeClient(ExchangeClientConfig{
		ClientID:     "client_id",
		ClientSecret: "client_secret",
		BuildTokenURL: func(_ string) (string, error) {
			return server.URL, nil
		},
	})

	_, err := client.ExchangeSessionToken(context.Background(), ExchangeSessionTokenRequest{
		ShopDomain:   "merchant.myshopify.com",
		SessionToken: "session_jwt",
	})
	if err == nil {
		t.Fatalf("expected exchange error")
	}

	var exchangeErr *ExchangeError
	if !errors.As(err, &exchangeErr) {
		t.Fatalf("expected ExchangeError, got %T", err)
	}
	if exchangeErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", exchangeErr.StatusCode)
	}
	if exchangeErr.ErrorCode != "invalid_subject_token" {
		t.Fatalf("expected invalid_subject_token code, got %q", exchangeErr.ErrorCode)
	}
}
