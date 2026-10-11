package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

const discussionFixture = `{"id":81,"body":"request","html_url":"https://github.com/owner/repo/issues/42#issuecomment-81","updated_at":"2026-10-11T01:00:00Z","user":{"id":7,"login":"author","type":"User"}}`

func TestGitHubDiscussionReadReplyAndGrantRefusal(t *testing.T) {
	for _, grant := range []bool{true, false} {
		name := "granted"
		if !grant {
			name = "unproven"
		}
		t.Run(name, func(t *testing.T) {
			posts := 0
			provider, closeServer := githubMutationProvider(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/owner/repo":
					if grant {
						w.Header().Set("X-OAuth-Scopes", "repo")
					}
					_, _ = w.Write([]byte(`{"id":1,"full_name":"owner/repo","has_issues":true,"permissions":{"pull":true}}`))
				case "/repos/owner/repo/issues/42":
					_, _ = w.Write([]byte(`{"id":9001,"number":42}`))
				case "/repos/owner/repo/issues/42/comments":
					if r.Method == http.MethodPost {
						posts++
						var body map[string]string
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["body"] != "approved" || len(body) != 1 {
							t.Errorf("reply changed: %+v, %v", body, err)
						}
						_, _ = w.Write([]byte(strings.Replace(discussionFixture, `"body":"request"`, `"body":"approved"`, 1)))
						return
					}
					if r.URL.Query().Get("per_page") != "1" || r.URL.Query().Get("page") != "1" {
						t.Errorf("unbounded query: %s", r.URL.RawQuery)
					}
					w.Header().Set("Link", `<https://api.github.com/repos/owner/repo/issues/42/comments?page=2>; rel="next"`)
					_, _ = w.Write([]byte("[" + discussionFixture + "]"))
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			})
			defer closeServer()
			target := core.TrackerDiscussionTarget{ProviderID: ProviderID, ConnectionID: "connection-1", ScopeID: "owner/repo", ResourceType: "issue", ResourceID: "9001:42"}
			caps, err := provider.DiscoverTrackerDiscussionCapabilities(context.Background(), target)
			if err != nil || !caps.Read.Available || caps.Reply.Available != grant || caps.ReceiptLookup.Available {
				t.Fatalf("capabilities: %+v, %v", caps, err)
			}
			page, err := provider.ReadTrackerDiscussion(context.Background(), core.TrackerDiscussionReadRequest{Target: target, Limit: 1})
			if err != nil || len(page.Items) != 1 || !page.HasMore || page.NextCursor != "2" || page.Items[0].Author.ID != "7" {
				t.Fatalf("page: %+v, %v", page, err)
			}
			receipt, err := provider.ReplyTrackerDiscussion(context.Background(), core.TrackerDiscussionReplyRequest{Target: target, Body: "approved", ActorID: "reviewer", IdempotencyKey: "effect", CorrelationID: "corr", CausationID: "decision"})
			if grant {
				if err != nil || receipt.Status != "confirmed" || receipt.IdempotencyKey != "effect" || posts != 1 {
					t.Fatalf("reply: %+v, posts=%d, %v", receipt, posts, err)
				}
			} else if err != nil || receipt.Status != "failed" || receipt.ErrorCode != core.TrackerErrorPermission || posts != 0 {
				t.Fatalf("unproven reply: %+v, posts=%d, %v", receipt, posts, err)
			}
		})
	}
}

func TestGitHubDiscussionRefusesPRAndForeignComment(t *testing.T) {
	for _, test := range []struct{ name, issue, comment string }{
		{"pr", `{"id":9001,"number":42,"pull_request":{"url":"https://github.com/owner/repo/pull/42"}}`, discussionFixture},
		{"foreign", `{"id":9001,"number":42}`, `{"id":81,"body":"request","html_url":"https://github.com/owner/foreign/issues/42#issuecomment-81","updated_at":"2026-10-11T01:00:00Z","user":{"id":7}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider, closeServer := githubMutationProvider(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/owner/repo":
					_, _ = w.Write([]byte(`{"id":1,"full_name":"owner/repo","has_issues":true,"permissions":{"pull":true}}`))
				case "/repos/owner/repo/issues/42":
					_, _ = w.Write([]byte(test.issue))
				default:
					_, _ = w.Write([]byte("[" + test.comment + "]"))
				}
			})
			defer closeServer()
			target := core.TrackerDiscussionTarget{ProviderID: ProviderID, ConnectionID: "connection-1", ScopeID: "owner/repo", ResourceType: "issue", ResourceID: "9001:42"}
			page, err := provider.ReadTrackerDiscussion(context.Background(), core.TrackerDiscussionReadRequest{Target: target, Limit: 1})
			if err == nil || len(page.Items) != 0 || page.NextCursor != "" {
				t.Fatalf("foreign page: %+v, %v", page, err)
			}
		})
	}
}

func TestGitHubDiscussionHintsAreSignedAndTargetBound(t *testing.T) {
	provider := &Provider{}
	target := core.TrackerDiscussionTarget{ProviderID: ProviderID, ConnectionID: "connection-1", ScopeID: "owner/repo", ResourceType: "issue", ResourceID: "9001:42"}
	body := []byte(`{"action":"deleted","repository":{"full_name":"owner/repo"},"issue":{"id":9001,"number":42},"comment":` + discussionFixture + `}`)
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write(body)
	request := core.TrackerWebhookVerificationRequest{Secret: []byte("secret"), Body: body, Signature: "sha256=" + hex.EncodeToString(mac.Sum(nil)), DeliveryID: "delivery", Event: "issue_comment"}
	hint, err := provider.VerifyTrackerDiscussionWebhook(context.Background(), target, request)
	if err != nil || hint.Action != "remove" || hint.MessageID != "81" {
		t.Fatalf("hint: %+v, %v", hint, err)
	}
	var comment discussionComment
	if err = json.Unmarshal([]byte(discussionFixture), &comment); err != nil {
		t.Fatal(err)
	}
	if hint.Revision == discussionRevision(comment, false) {
		t.Fatal("deletion did not change source revision")
	}
	foreign := target
	foreign.ScopeID = "owner/foreign"
	if _, err = provider.VerifyTrackerDiscussionWebhook(context.Background(), foreign, request); err == nil {
		t.Fatal("signed foreign hint accepted")
	}
	request.Body = []byte(strings.Replace(strings.Replace(string(body), `"body":"request"`, `"body":""`, 1), `"user":{"id":7,"login":"author","type":"User"}`, `"user":null`, 1))
	mac = hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write(request.Body)
	request.Signature = "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if _, err = provider.VerifyTrackerDiscussionWebhook(context.Background(), target, request); err != nil {
		t.Fatalf("minimal deletion: %v", err)
	}
	request.Body = []byte("invalid-json")
	if _, err = provider.VerifyTrackerDiscussionWebhook(context.Background(), target, request); err == nil {
		t.Fatal("invalid signature accepted")
	}
}

func TestGitHubDiscussionRevisionAndConditionalReply(t *testing.T) {
	var original discussionComment
	if err := json.Unmarshal([]byte(discussionFixture), &original); err != nil {
		t.Fatal(err)
	}
	target := core.TrackerDiscussionTarget{ProviderID: ProviderID, ConnectionID: "connection-1", ScopeID: "owner/repo", ResourceType: "issue", ResourceID: "9001:42"}
	message, err := discussionMessage(target, discussionIdentity{ID: 9001, Number: 42}, original)
	if err != nil {
		t.Fatal(err)
	}
	renamed := original
	renamed.User.Login = "renamed"
	renamed.User.HTMLURL = "https://github.com/renamed"
	if discussionRevision(renamed, false) != message.Revision {
		t.Fatal("profile-only mutation changed revision")
	}
	edited := renamed
	edited.Body = "edited request"
	if discussionRevision(edited, false) == message.Revision || discussionRevision(original, true) == message.Revision {
		t.Fatal("real edit/deletion lost revision")
	}
	current := renamed
	missing := false
	posts := 0
	provider, closeServer := githubMutationProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo":
			w.Header().Set("X-OAuth-Scopes", "repo")
			_, _ = w.Write([]byte(`{"id":1,"full_name":"owner/repo","has_issues":true,"permissions":{"pull":true}}`))
		case "/repos/owner/repo/issues/42":
			_, _ = w.Write([]byte(`{"id":9001,"number":42}`))
		case "/repos/owner/repo/issues/comments/81":
			if missing {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(current)
		case "/repos/owner/repo/issues/42/comments":
			if r.Method != http.MethodPost {
				t.Errorf("unexpected method: %s", r.Method)
				w.WriteHeader(405)
				return
			}
			posts++
			reply := original
			reply.ID = 82
			reply.Body = "approved"
			reply.HTMLURL = "https://github.com/owner/repo/issues/42#issuecomment-82"
			_ = json.NewEncoder(w).Encode(reply)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	defer closeServer()
	request := core.TrackerDiscussionReplyRequest{Target: target, ParentID: "81", Body: "approved", ActorID: "reviewer", IdempotencyKey: "effect", CorrelationID: "corr", CausationID: "decision"}
	if err := provider.ValidateTrackerDiscussionReply(t.Context(), target, "81", message.Revision); err != nil {
		t.Fatal(err)
	}
	current = edited
	err = provider.ValidateTrackerDiscussionReply(t.Context(), target, "81", message.Revision)
	var providerErr *core.TrackerProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != core.TrackerErrorConflict {
		t.Fatalf("stale preflight: %v", err)
	}
	receipt, err := provider.ReplyTrackerDiscussionAtRevision(t.Context(), request, message.Revision)
	if err != nil || receipt.Status != "failed" || receipt.ErrorCode != "source_revision_changed" || posts != 0 {
		t.Fatalf("edited parent posted: %+v, %v, posts=%d", receipt, err, posts)
	}
	missing = true
	receipt, err = provider.ReplyTrackerDiscussionAtRevision(t.Context(), request, message.Revision)
	if err != nil || receipt.Status != "failed" || posts != 0 {
		t.Fatalf("deleted parent posted: %+v, %v, posts=%d", receipt, err, posts)
	}
	missing = false
	current = renamed
	if receipt, err = provider.ReplyTrackerDiscussion(t.Context(), request); err != nil || receipt.Status != "failed" || receipt.ErrorCode != core.TrackerErrorValidation || posts != 0 {
		t.Fatalf("unguarded parent accepted: %v, posts=%d", err, posts)
	}
	receipt, err = provider.ReplyTrackerDiscussionAtRevision(t.Context(), request, message.Revision)
	if err != nil || receipt.Status != "confirmed" || posts != 1 {
		t.Fatalf("current parent refused: %+v, %v, posts=%d", receipt, err, posts)
	}
}

func TestGitHubDiscussionPreflightFailuresAreKnownUnsent(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		grant  bool
		code   string
	}{
		{"rate_limit", http.StatusTooManyRequests, true, core.TrackerErrorRateLimited},
		{"revoked", http.StatusUnauthorized, true, core.TrackerErrorCredentialRevoked},
		{"grant_lost", http.StatusOK, false, core.TrackerErrorPermission},
	} {
		t.Run(test.name, func(t *testing.T) {
			posts := 0
			provider, closeServer := githubMutationProvider(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts++
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				if r.URL.Path == "/repos/owner/repo" {
					if test.status != http.StatusOK {
						w.Header().Set("Retry-After", "60")
						w.WriteHeader(test.status)
						return
					}
					if test.grant {
						w.Header().Set("X-OAuth-Scopes", "repo")
					}
					_, _ = w.Write([]byte(`{"id":1,"full_name":"owner/repo","has_issues":true,"permissions":{"pull":true}}`))
					return
				}
				_, _ = w.Write([]byte(`{"id":9001,"number":42}`))
			})
			defer closeServer()
			target := core.TrackerDiscussionTarget{ProviderID: ProviderID, ConnectionID: "connection-1", ScopeID: "owner/repo", ResourceType: "issue", ResourceID: "9001:42"}
			receipt, err := provider.ReplyTrackerDiscussion(t.Context(), core.TrackerDiscussionReplyRequest{Target: target, Body: "approved", ActorID: "reviewer", IdempotencyKey: "effect", CorrelationID: "corr", CausationID: "decision"})
			if err != nil || receipt.Status != "failed" || receipt.ErrorCode != test.code || posts != 0 {
				t.Fatalf("unsent: %+v, %v, posts=%d", receipt, err, posts)
			}
			if test.status == http.StatusTooManyRequests && !receipt.RateLimit.ResetAt.After(time.Now().UTC()) {
				t.Fatalf("lost retry deadline: %+v", receipt)
			}
		})
	}
}

func TestGitHubDiscussionPostFailureRemainsAmbiguous(t *testing.T) {
	posts := 0
	provider, closeServer := githubMutationProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.URL.Path == "/repos/owner/repo" {
			w.Header().Set("X-OAuth-Scopes", "repo")
			_, _ = w.Write([]byte(`{"id":1,"full_name":"owner/repo","has_issues":true,"permissions":{"pull":true}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":9001,"number":42}`))
	})
	defer closeServer()
	target := core.TrackerDiscussionTarget{ProviderID: ProviderID, ConnectionID: "connection-1", ScopeID: "owner/repo", ResourceType: "issue", ResourceID: "9001:42"}
	receipt, err := provider.ReplyTrackerDiscussion(t.Context(), core.TrackerDiscussionReplyRequest{Target: target, Body: "approved", ActorID: "reviewer", IdempotencyKey: "effect", CorrelationID: "corr", CausationID: "decision"})
	if err == nil || receipt.Status == "failed" || posts != 1 {
		t.Fatalf("possibly posted reply was classified unsent: %+v, %v, posts=%d", receipt, err, posts)
	}
}
