package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/goliatone/go-services/core"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestThreadedReplyBindsReceiptAndDoesNotRetryUnknown(t *testing.T) {
	posts := 0
	mode := "valid"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth.test" {
			w.Header().Set("X-OAuth-Scopes", "chat:write,channels:history")
			fmt.Fprint(w, `{"ok":true,"team_id":"T","user_id":"UB","bot_id":"B"}`)
			return
		}
		posts++
		switch mode {
		case "valid":
			fmt.Fprint(w, `{"ok":true,"channel":"C","ts":"2.000001","message":{"thread_ts":"1.000001"}}`)
		case "wrong":
			fmt.Fprint(w, `{"ok":true,"channel":"OTHER","ts":"2.000001","message":{"thread_ts":"1.000001"}}`)
		default:
			w.WriteHeader(502)
		}
	}))
	defer server.Close()
	provider, err := New(Config{BaseURL: server.URL, Credentials: CredentialResolverFunc(func(context.Context, string) (core.ActiveCredential, error) {
		return core.ActiveCredential{AccessToken: "fixture-token"}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	request := MessageRequest{OperationID: "effect", ConnectionID: "stored", TeamID: "T", ChannelID: "C", ThreadID: "1.000001", Body: "Approved reply"}
	result, err := provider.Send(context.Background(), request)
	if err != nil || result.State != Sent || result.ValidateFor(request) != nil {
		t.Fatalf("reply: %+v %v", result, err)
	}
	mode = "wrong"
	result, err = provider.Send(context.Background(), request)
	if err != nil || result.State != Unknown || result.External != nil {
		t.Fatalf("wrong destination: %+v %v", result, err)
	}
	mode = "unknown"
	before := posts
	result, err = provider.Send(context.Background(), request)
	if err != nil || result.State != Unknown || posts != before+1 {
		t.Fatalf("uncertain retry: %+v %v posts=%d", result, err, posts)
	}
}
func TestSignedEventsPreserveEditAndRefuseChangedBody(t *testing.T) {
	now := time.Unix(1700000000, 0)
	provider, err := New(Config{Now: func() time.Time { return now }, SigningSecrets: SigningSecretResolverFunc(func(context.Context, string) ([]byte, error) { return []byte("fixture-secret"), nil })})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"type":"event_callback","team_id":"T","event_id":"E","event":{"type":"message","subtype":"message_changed","channel":"C","event_ts":"1700000000.000001","message":{"ts":"1.000001","thread_ts":"1.000000","text":"edited","user":"U","edited":{"ts":"1700000000.000002"}}}}`)
	timestamp := fmt.Sprint(now.Unix())
	mac := hmac.New(sha256.New, []byte("fixture-secret"))
	mac.Write([]byte("v0:" + timestamp + ":"))
	mac.Write(body)
	input := HTTPRequest{ConnectionID: "stored", Body: body, Timestamp: timestamp, Signature: "v0=" + hex.EncodeToString(mac.Sum(nil))}
	event, err := provider.Verify(context.Background(), input)
	if err != nil || event.Body != "edited" || event.ThreadID != "1.000000" || event.Revision != "1700000000.000002" {
		t.Fatalf("edit: %+v %v", event, err)
	}
	input.Body = []byte(strings.Replace(string(body), "edited", "altered", 1))
	if _, err = provider.Verify(context.Background(), input); err == nil {
		t.Fatal("changed signed body accepted")
	}
}

func TestRootUnknownDeliveryReconcilesChannelHistory(t *testing.T) {
	posts := 0
	histories := 0
	request := MessageRequest{OperationID: "root-effect", ConnectionID: "stored", TeamID: "T", ChannelID: "C", Body: "Approved alert"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth.test":
			w.Header().Set("X-OAuth-Scopes", "chat:write,channels:history")
			fmt.Fprint(w, `{"ok":true,"team_id":"T","user_id":"UB","bot_id":"B"}`)
		case "/chat.postMessage":
			posts++
			w.WriteHeader(502)
		case "/conversations.history":
			histories++
			args := r.URL.Query()
			if r.Method != http.MethodGet || args.Get("channel") != "C" || args.Has("ts") || args.Get("limit") != "100" {
				t.Errorf("root history request: %s %+v", r.Method, args)
			}
			fmt.Fprintf(w, `{"ok":true,"messages":[{"ts":"2.000001","client_msg_id":%q}]}`, clientMessageID(request.OperationID))
		default:
			t.Errorf("unexpected method: %s", r.URL.Path)
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	provider, err := New(Config{BaseURL: server.URL, Credentials: CredentialResolverFunc(func(context.Context, string) (core.ActiveCredential, error) {
		return core.ActiveCredential{AccessToken: "fixture"}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	sent, err := provider.Send(t.Context(), request)
	if err != nil || sent.State != Unknown || posts != 1 {
		t.Fatalf("send: %+v %v", sent, err)
	}
	confirmed, err := provider.Reconcile(t.Context(), request)
	if err != nil || confirmed.State != Sent || confirmed.ValidateFor(request) != nil || histories != 1 || posts != 1 {
		t.Fatalf("reconcile: %+v %v posts=%d histories=%d", confirmed, err, posts, histories)
	}
}
