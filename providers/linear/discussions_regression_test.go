package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/goliatone/go-services/core"
	trackerruntime "github.com/goliatone/go-services/providers/tracker"
)

func TestLinearDiscussionBotAttribution(t *testing.T) {
	node := strings.Replace(discussionNode("Approved", "team-1"), `"user":{"id":"user-1","name":"Author"}`, `"user":null,"botActor":{"id":"app-1","name":"App"}`, 1)
	posts := 0
	provider, closeServer := linearMutationTestProvider(t, true, func(query string, _ map[string]any) string {
		if !strings.Contains(query, "botActor{id name}") && (strings.Contains(query, "comments(first:") || strings.Contains(query, "commentCreate")) {
			t.Error("bot attribution omitted from query")
		}
		if strings.Contains(query, "commentCreate") {
			posts++
			return `{"data":{"commentCreate":{"success":true,"comment":` + node + `}}}`
		}
		if strings.Contains(query, "comments(first:") {
			return `{"data":{"issue":{"id":"issue-1","team":{"id":"team-1"},"comments":{"nodes":[` + node + `],"pageInfo":{"hasNextPage":false}}}}}`
		}
		return `{"data":{"issue":{"id":"issue-1","team":{"id":"team-1"}}}}`
	})
	defer closeServer()
	page, err := provider.ReadTrackerDiscussion(t.Context(), core.TrackerDiscussionReadRequest{Target: discussionTarget(), Limit: 1})
	if err != nil || len(page.Items) != 1 || !page.Items[0].Author.Bot || page.Items[0].Author.ID != "bot:app-1" {
		t.Fatalf("bot read: %+v %v", page, err)
	}
	receipt, err := provider.ReplyTrackerDiscussion(t.Context(), core.TrackerDiscussionReplyRequest{Target: discussionTarget(), Body: "Approved", ActorID: "reviewer", IdempotencyKey: "effect", CorrelationID: "corr", CausationID: "decision"})
	if err != nil || receipt.Status != "confirmed" || posts != 1 {
		t.Fatalf("bot confirmation: %+v %v posts=%d", receipt, err, posts)
	}
	var item discussionComment
	if err = json.Unmarshal([]byte(strings.Replace(node, `"id":"app-1"`, `"id":null`, 1)), &item); err != nil {
		t.Fatal(err)
	}
	if _, err = normalizeDiscussionComment(discussionTarget(), item); err == nil {
		t.Fatal("invented identity for anonymous bot")
	}
	item.User = &struct{ ID, Name string }{ID: "app-user", Name: "App"}
	mapped, err := normalizeDiscussionComment(discussionTarget(), item)
	if err != nil || !mapped.Author.Bot || mapped.Author.ID != "app-user" {
		t.Fatalf("associated bot user lost: %+v %v", mapped, err)
	}
}

func TestLinearDiscussionOAuthScopes(t *testing.T) {
	for _, scope := range []string{"read", "read comments:create"} {
		t.Run(scope, func(t *testing.T) {
			requested := strings.Fields(scope)
			posts, exchanges := 0, 0
			client := &http.Client{Transport: linearRoundTrip(func(request *http.Request) (*http.Response, error) {
				body := `{"data":{"issue":{"id":"issue-1","team":{"id":"team-1"}},"viewer":{"id":"app-1"},"team":{"id":"team-1"}}}`
				if request.URL.String() == linearOAuthTokenURL {
					exchanges++
					if err := request.ParseForm(); err != nil {
						t.Fatal(err)
					}
					if request.PostForm.Get("scope") != strings.Join(requested, ",") {
						t.Fatal("configured scopes were widened")
					}
					body = fmt.Sprintf(`{"access_token":"token","token_type":"Bearer","scope":%q,"expires_in":3600}`, scope)
				} else {
					var input struct{ Query string }
					if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
						t.Fatal(err)
					}
					if strings.Contains(input.Query, "commentCreate") {
						posts++
						body = `{"data":{"commentCreate":{"success":true,"comment":` + discussionNode("Approved", "team-1") + `}}}`
					}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			value, err := New(Config{TrackerRuntime: trackerruntime.Runtime{HTTPClient: client, Credentials: linearCredentialResolver(func(_ context.Context, id string) (core.ActiveCredential, error) {
				return core.ActiveCredential{ConnectionID: id, RequestedScopes: requested, Metadata: map[string]any{"auth_type": "oauth_client", "client_id": "client", "client_secret": "secret"}}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			provider := value.(*Provider)
			caps, err := provider.DiscoverTrackerDiscussionCapabilities(t.Context(), discussionTarget())
			wantReply := strings.Contains(scope, "comments:create")
			if err != nil || !caps.Read.Available || caps.Reply.Available != wantReply {
				t.Fatalf("capabilities: %+v %v", caps, err)
			}
			fields, err := provider.DiscoverTrackerFieldCapabilities(t.Context(), core.TrackerFieldCapabilityRequest{ConnectionID: "connection-1", RepositoryID: "team-1"})
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range fields {
				if field.Operation != "read" && field.Granted {
					t.Fatal("comment scope authorized issue write")
				}
			}
			_, err = provider.CreateTrackerIssue(t.Context(), core.TrackerIssueCreateRequest{ConnectionID: "connection-1", RepositoryID: "team-1", Fields: map[string]json.RawMessage{"title": json.RawMessage(`"Title"`)}, ActorID: "reviewer", IdempotencyKey: "issue", CorrelationID: "corr"})
			if err == nil {
				t.Fatal("issue creation accepted without write")
			}
			receipt, err := provider.ReplyTrackerDiscussion(t.Context(), core.TrackerDiscussionReplyRequest{Target: discussionTarget(), Body: "Approved", ActorID: "reviewer", IdempotencyKey: "effect", CorrelationID: "corr", CausationID: "decision"})
			wantStatus, wantPosts := "failed", 0
			if wantReply {
				wantStatus, wantPosts = "confirmed", 1
			}
			if err != nil || receipt.Status != wantStatus || posts != wantPosts || exchanges != 1 {
				t.Fatalf("reply: %+v %v posts=%d exchanges=%d", receipt, err, posts, exchanges)
			}
		})
	}
}

func TestLinearDiscussionRefreshBeforeMutation(t *testing.T) {
	for _, scenario := range []string{"issue", "parent", "parent-grant-lost", "unknown"} {
		t.Run(scenario, func(t *testing.T) {
			exchanges, posts := 0, 0
			client := &http.Client{Transport: linearRoundTrip(func(request *http.Request) (*http.Response, error) {
				response := func(status int, body string) *http.Response {
					return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
				}
				if request.URL.String() == linearOAuthTokenURL {
					exchanges++
					scope := "read comments:create"
					if scenario == "parent-grant-lost" && exchanges > 1 {
						scope = "read"
					}
					return response(200, fmt.Sprintf(`{"access_token":"token-%d","token_type":"Bearer","scope":%q,"expires_in":3600}`, exchanges, scope)), nil
				}
				var input struct{ Query string }
				if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(input.Query, "commentCreate") {
					posts++
					if request.Header.Get("Authorization") != "Bearer token-2" {
						t.Fatal("mutation used obsolete credential")
					}
					if scenario == "unknown" {
						return response(401, "unauthorized"), nil
					}
					return response(200, `{"data":{"commentCreate":{"success":true,"comment":`+strings.Replace(discussionNode("Approved", "team-1"), `"id":"comment-1"`, `"id":"reply-1","parentId":"comment-1"`, 1)+`}}}`), nil
				}
				isParent := strings.Contains(input.Query, "comment(id:")
				if request.Header.Get("Authorization") == "Bearer token-1" && (scenario == "issue" || isParent) {
					return response(401, "unauthorized"), nil
				}
				if isParent {
					return response(200, `{"data":{"comment":`+discussionNode("Question", "team-1")+`}}`), nil
				}
				return response(200, `{"data":{"issue":{"id":"issue-1","team":{"id":"team-1"}}}}`), nil
			})}
			value, err := New(Config{TrackerRuntime: trackerruntime.Runtime{HTTPClient: client, Credentials: linearCredentialResolver(func(_ context.Context, id string) (core.ActiveCredential, error) {
				return core.ActiveCredential{ConnectionID: id, RequestedScopes: []string{"read", "comments:create"}, Metadata: map[string]any{"auth_type": "oauth_client", "client_id": "client", "client_secret": "secret"}}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			var item discussionComment
			if err = json.Unmarshal([]byte(discussionNode("Question", "team-1")), &item); err != nil {
				t.Fatal(err)
			}
			parent, err := normalizeDiscussionComment(discussionTarget(), item)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := value.(*Provider).ReplyTrackerDiscussionAtRevision(t.Context(), core.TrackerDiscussionReplyRequest{Target: discussionTarget(), ParentID: parent.MessageID, Body: "Approved", ActorID: "reviewer", IdempotencyKey: "effect", CorrelationID: "corr", CausationID: "decision"}, parent.Revision)
			wantStatus, wantPosts := "confirmed", 1
			if scenario == "parent-grant-lost" {
				wantStatus, wantPosts = "failed", 0
			}
			if scenario == "unknown" {
				wantStatus = "unknown"
			}
			if err != nil || receipt.Status != wantStatus || posts != wantPosts || exchanges != 2 {
				t.Fatalf("refresh reply: %+v %v posts=%d exchanges=%d", receipt, err, posts, exchanges)
			}
		})
	}
}

func TestLinearOAuthCacheTracksRequestedScopes(t *testing.T) {
	exchanges := 0
	requested := []string{"read", "write"}
	client := &http.Client{Transport: linearRoundTrip(func(request *http.Request) (*http.Response, error) {
		body := `{"data":{"issue":{"id":"issue-1","team":{"id":"team-1"}}}}`
		if request.URL.String() == linearOAuthTokenURL {
			exchanges++
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if request.PostForm.Get("scope") != strings.Join(requested, ",") {
				t.Fatal("token request differs from configured scopes")
			}
			body = fmt.Sprintf(`{"access_token":"token-%d","token_type":"Bearer","scope":%q,"expires_in":3600}`, exchanges, strings.Join(requested, " "))
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	value, err := New(Config{TrackerRuntime: trackerruntime.Runtime{HTTPClient: client, Credentials: linearCredentialResolver(func(_ context.Context, id string) (core.ActiveCredential, error) {
		return core.ActiveCredential{ConnectionID: id, RequestedScopes: requested, Metadata: map[string]any{"auth_type": "oauth_client", "client_id": "client", "client_secret": "secret"}}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	provider := value.(*Provider)
	caps, err := provider.DiscoverTrackerDiscussionCapabilities(t.Context(), discussionTarget())
	if err != nil || !caps.Reply.Available {
		t.Fatalf("initial grant: %+v %v", caps, err)
	}
	requested = []string{"read"}
	caps, err = provider.DiscoverTrackerDiscussionCapabilities(t.Context(), discussionTarget())
	if err != nil || caps.Reply.Available || exchanges != 2 {
		t.Fatalf("stale cached grant: %+v %v exchanges=%d", caps, err, exchanges)
	}
	_, err = provider.DiscoverTrackerDiscussionCapabilities(t.Context(), discussionTarget())
	if err != nil || exchanges != 2 {
		t.Fatalf("fresh cache: %v exchanges=%d", err, exchanges)
	}
}
