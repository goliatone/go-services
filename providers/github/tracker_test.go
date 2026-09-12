package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestGitHubWebhookPayloadRequiresAndForwardsSigningSecret(t *testing.T) {
	if _, err := githubWebhookPayload(core.SubscribeRequest{CallbackURL: "https://hooks.example.test/github"}); err == nil {
		t.Fatal("webhook payload accepted a missing signing secret")
	}
	payload, err := githubWebhookPayload(core.SubscribeRequest{CallbackURL: "https://hooks.example.test/github", Metadata: map[string]any{"webhook_secret": "server-held-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	config, ok := payload["config"].(map[string]any)
	if !ok || config["secret"] != "server-held-secret" || config["url"] != "https://hooks.example.test/github" {
		t.Fatalf("unexpected GitHub hook configuration: %#v", payload)
	}
}

func TestGitHubWebhookVerificationUsesHMACSHA256(t *testing.T) {
	secret, body := []byte("server-held-secret"), []byte(`{"action":"opened"}`)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	provider := &Provider{}
	request := core.TrackerWebhookVerificationRequest{Secret: secret, Body: body, Signature: signature, DeliveryID: "delivery-1", Event: "issues"}
	if err := provider.VerifyTrackerWebhook(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	request.Signature = "sha256=bad"
	if err := provider.VerifyTrackerWebhook(context.Background(), request); err == nil {
		t.Fatal("invalid signature accepted")
	}
}

func TestGitHubSubscribeRepairsEquivalentHookWithoutDuplicate(t *testing.T) {
	var posts, patches atomic.Int32
	provider, closeServer := githubMutationProvider(t, func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodGet:
			_, _ = writer.Write([]byte(`[{"id":77,"active":true,"events":["issues"],"config":{"url":"https://hooks.example.test/github/"}}]`))
		case http.MethodPatch:
			patches.Add(1)
			var payload map[string]any
			_ = json.NewDecoder(request.Body).Decode(&payload)
			config, _ := payload["config"].(map[string]any)
			if config["secret"] != "server-held-secret" || request.URL.Path != "/repos/owner/repo/hooks/77" {
				t.Fatalf("repair request: path=%s payload=%#v", request.URL.Path, payload)
			}
			_, _ = writer.Write([]byte(`{"id":77}`))
		case http.MethodPost:
			posts.Add(1)
		default:
			t.Fatalf("unexpected method %s", request.Method)
		}
	})
	defer closeServer()
	result, err := provider.Subscribe(context.Background(), core.SubscribeRequest{ConnectionID: "connection-1", ResourceType: "repository", ResourceID: "owner/repo", CallbackURL: "https://hooks.example.test/github", Metadata: map[string]any{"webhook_secret": "server-held-secret"}})
	if err != nil || result.RemoteSubscriptionID != "77" || posts.Load() != 0 || patches.Load() != 1 {
		t.Fatalf("result=%#v posts=%d patches=%d err=%v", result, posts.Load(), patches.Load(), err)
	}
}
