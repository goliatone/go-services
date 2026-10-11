package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/tracker"
)

type discussionCredentialResolver func(context.Context, string) (core.ActiveCredential, error)

func (f discussionCredentialResolver) ResolveTrackerCredential(ctx context.Context, id string) (core.ActiveCredential, error) {
	return f(ctx, id)
}

func TestJiraDiscussionScopePermissionsAndConditionalReply(t *testing.T) {
	body, project, mode := "Question", "PRJ", "confirmed"
	replyGranted, readGranted := true, true
	posts, parentStatus, permissionStatus := 0, 200, 200
	visibility := &discussionVisibility{Type: "role", Identifier: "10002", Value: "Administrators"}
	pageMode := "valid"
	internalComment := false
	tokenType := "Basic"
	var grantedScopes []string
	var site string
	comment := func(id, text string, restriction *discussionVisibility) map[string]any {
		return map[string]any{"id": id, "self": site + "/rest/api/3/issue/10001/comment/" + id, "updated": "2026-10-11T12:00:00.000+0000", "author": map[string]any{"accountId": "account-1", "displayName": "Author", "accountType": "app"}, "body": approvedDiscussionBody(text), "visibility": restriction, "jsdPublic": !internalComment}
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tokenType == "Bearer" {
			if r.Header.Get("Authorization") != "Bearer token" {
				t.Error("wrong bearer auth")
			}
		} else if email, token, ok := r.BasicAuth(); !ok || email != "integration@example.test" || token != "token" {
			t.Error("wrong basic auth")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rest/api/3/issue/10001":
			if r.URL.Query().Get("fields") != "project" {
				t.Errorf("unbounded issue fields")
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "10001", "key": "PRJ-7", "fields": map[string]any{"project": map[string]any{"id": "10000", "key": project}}})
		case r.URL.Path == "/rest/api/3/mypermissions":
			if r.URL.Query().Get("issueId") != "10001" || r.URL.Query().Get("permissions") != "BROWSE_PROJECTS,ADD_COMMENTS" {
				t.Errorf("permissions escaped issue scope: %s", r.URL)
			}
			if permissionStatus != 200 {
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(permissionStatus)
				w.Write([]byte(`{"errorMessages":["denied"]}`))
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"permissions": map[string]any{"BROWSE_PROJECTS": map[string]any{"havePermission": readGranted}, "ADD_COMMENTS": map[string]any{"havePermission": replyGranted}}})
		case strings.HasSuffix(r.URL.Path, "/comment/20001"):
			if parentStatus != 200 {
				w.WriteHeader(parentStatus)
				return
			}
			json.NewEncoder(w).Encode(comment("20001", body, visibility))
		case strings.HasSuffix(r.URL.Path, "/comment") && r.Method == http.MethodGet:
			offset := 0
			if r.URL.Query().Get("startAt") == "1" {
				offset = 1
			}
			if r.URL.Query().Get("maxResults") != "1" || r.URL.Query().Get("orderBy") != "created" {
				t.Errorf("unbounded/unordered page %s", r.URL)
			}
			value := comment(fmt.Sprint(20001+offset), body, visibility)
			if pageMode == "foreign" {
				value["self"] = "https://foreign.example/rest/api/3/issue/10001/comment/20001"
			}
			page := map[string]any{"startAt": offset, "maxResults": 1, "total": 2, "comments": []any{value}}
			if pageMode == "missing" {
				delete(page, "comments")
			}
			if pageMode == "oversize" {
				page["comments"] = []any{value, value}
			}
			if pageMode == "stalled" {
				page["comments"] = []any{}
			}
			json.NewEncoder(w).Encode(page)
		case strings.HasSuffix(r.URL.Path, "/comment") && r.Method == http.MethodPost:
			posts++
			var input struct {
				Body       adfNode
				Visibility *discussionVisibility
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			approved, err := discussionBody(tracker.RawJSON(input.Body))
			expectedVisibility, _ := visibilityIdentity(visibility)
			actualVisibility, _ := visibilityIdentity(input.Visibility)
			if err != nil || approved != "Approved\n\nreply\n" || actualVisibility != expectedVisibility {
				t.Errorf("changed approved body/audience: %+v %v", input, err)
			}
			if mode == "unknown" {
				w.WriteHeader(503)
				return
			}
			if mode == "mismatch" {
				approved = "Changed by server"
			}
			result := comment("20002", approved, input.Visibility)
			if mode == "widened" {
				result["visibility"] = nil
			}
			if mode == "malformed" {
				w.Write([]byte(`{`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(result)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	site = server.URL
	resolver := discussionCredentialResolver(func(_ context.Context, id string) (core.ActiveCredential, error) {
		if id != "connection" {
			return core.ActiveCredential{}, errors.New("unknown connection")
		}
		return core.ActiveCredential{TokenType: tokenType, GrantedScopes: grantedScopes, AccessToken: "token", Metadata: map[string]any{"base_url": site, "email": "integration@example.test"}}, nil
	})
	raw, err := New(Config{TrackerRuntime: tracker.Runtime{Credentials: resolver, HTTPClient: server.Client(), Authenticate: func(ctx context.Context, request *http.Request, credential core.ActiveCredential) error {
		if mode == "auth" && request.Method == http.MethodPost {
			return errors.New("local authentication refusal")
		}
		return authenticate(ctx, request, credential)
	}}})
	if err != nil {
		t.Fatal(err)
	}
	provider := raw.(*Provider)
	target := core.TrackerDiscussionTarget{ProviderID: ProviderID, ConnectionID: "connection", ScopeID: "PRJ", ResourceType: "issue", ResourceID: "10001"}
	input := core.TrackerDiscussionReadRequest{Target: target, Limit: 1}
	page, err := provider.ReadTrackerDiscussion(t.Context(), input)
	if err != nil || len(page.Items) != 1 || !page.HasMore || page.NextCursor == "" || !page.Items[0].Author.Bot || page.Items[0].ParentID != "" {
		t.Fatalf("read: %+v %v", page, err)
	}
	revision := page.Items[0].Revision
	internalComment = true
	internalPage, internalErr := provider.ReadTrackerDiscussion(t.Context(), input)
	if internalErr != nil {
		t.Fatal(internalErr)
	}
	internalRequest := core.TrackerDiscussionReplyRequest{Target: target, ParentID: "20001", Body: "approved", ActorID: "reviewer", IdempotencyKey: "internal", CorrelationID: "correlation", CausationID: "decision"}
	internalReceipt, internalErr := provider.ReplyTrackerDiscussionAtRevision(t.Context(), internalRequest, internalPage.Items[0].Revision)
	if internalErr != nil || internalReceipt.Status != "failed" || internalReceipt.ErrorCode != core.TrackerErrorUnavailable || posts != 0 {
		t.Fatalf("internal comment exposed: %+v %v", internalReceipt, internalErr)
	}
	internalComment = false
	input.Cursor = page.NextCursor
	last, err := provider.ReadTrackerDiscussion(t.Context(), input)
	if err != nil || len(last.Items) != 1 || last.HasMore || last.NextCursor != "" {
		t.Fatalf("last page: %+v %v", last, err)
	}
	input.Target.ConnectionID = "foreign"
	if _, err = provider.ReadTrackerDiscussion(t.Context(), input); err == nil {
		t.Fatal("cursor crossed connection")
	}
	input.Target = target
	input.Cursor = ""
	for _, bad := range []string{"foreign", "missing", "oversize", "stalled"} {
		pageMode = bad
		refused, err := provider.ReadTrackerDiscussion(t.Context(), input)
		if err == nil || len(refused.Items) != 0 || refused.NextCursor != "" {
			t.Fatalf("%s page escaped: %+v %v", bad, refused, err)
		}
	}
	pageMode = "valid"
	caps, err := provider.DiscoverTrackerDiscussionCapabilities(t.Context(), target)
	if err != nil || !caps.Read.Available || !caps.Reply.Available || caps.Webhook.Available || caps.ReceiptLookup.Available {
		t.Fatalf("caps: %+v %v", caps, err)
	}
	request := core.TrackerDiscussionReplyRequest{Target: target, ParentID: "20001", Body: "Approved\n\nreply\n", ActorID: "reviewer", IdempotencyKey: "effect", CorrelationID: "correlation", CausationID: "decision"}
	refuse := func(code string) {
		t.Helper()
		previous := posts
		receipt, err := provider.ReplyTrackerDiscussionAtRevision(t.Context(), request, revision)
		if err != nil || receipt.Status != "failed" || receipt.ErrorCode != code || posts != previous {
			t.Fatalf("refusal %s: %+v %v posts=%d", code, receipt, err, posts)
		}
	}
	fullGranular := []string{"read:issue-meta:jira", "read:issue-security-level:jira", "read:issue.vote:jira", "read:issue.changelog:jira", "read:avatar:jira", "read:issue:jira", "read:status:jira", "read:user:jira", "read:field-configuration:jira", "read:permission:jira", "read:comment:jira", "read:comment.property:jira", "read:group:jira", "read:project:jira", "read:project-role:jira", "write:comment:jira"}
	for _, test := range []struct {
		tokenType   string
		scopes      []string
		read, reply bool
	}{
		{"Basic", nil, true, true},
		{"Basic", []string{"issues.read", "webhooks.manage"}, true, true},
		{"Bearer", nil, false, false},
		{"Bearer", []string{"read:jira-work"}, true, false},
		{"Basic", []string{"read:comment:jira"}, false, false},
		{"Bearer", []string{"read:jira-work", "write:jira-work"}, true, true},
		{"Bearer", []string{"read:comment:jira", "write:comment:jira"}, false, false},
		{"Bearer", []string{"write:issue:jira"}, false, false},
		{"Bearer", fullGranular, true, true},
	} {
		tokenType, grantedScopes = test.tokenType, test.scopes
		nativeCaps, nativeErr := provider.DiscoverTrackerDiscussionCapabilities(t.Context(), target)
		if nativeErr != nil || nativeCaps.Read.Available != test.read || nativeCaps.Reply.Available != test.reply {
			t.Fatalf("native scope capabilities %s %v: %+v %v", tokenType, grantedScopes, nativeCaps, nativeErr)
		}
		if !test.reply {
			refuse(core.TrackerErrorPermission)
		} else if err := provider.ValidateTrackerDiscussionReply(t.Context(), target, "20001", revision); err != nil {
			t.Fatal(err)
		}
	}
	tokenType = "Bearer"
	for _, missing := range fullGranular {
		grantedScopes = nil
		for _, grant := range fullGranular {
			if grant != missing {
				grantedScopes = append(grantedScopes, grant)
			}
		}
		partial, err := provider.DiscoverTrackerDiscussionCapabilities(t.Context(), target)
		if err != nil || partial.Reply.Available || partial.Read.Available != (missing == "write:comment:jira") {
			t.Fatalf("missing %s: %+v %v", missing, partial, err)
		}
		refuse(core.TrackerErrorPermission)
	}
	tokenType, grantedScopes = "Basic", nil

	replyGranted = false
	caps, err = provider.DiscoverTrackerDiscussionCapabilities(t.Context(), target)
	if err != nil || !caps.Read.Available || caps.Reply.Available {
		t.Fatalf("read-only: %+v %v", caps, err)
	}
	refuse(core.TrackerErrorPermission)
	replyGranted = true
	readGranted = false
	refuse(core.TrackerErrorPermission)
	readGranted = true
	body = "Edited"
	refuse("source_revision_changed")
	body = "Question"
	visibility = &discussionVisibility{Type: "role", Identifier: "10003", Value: "Different role"}
	refuse("source_revision_changed")
	visibility = &discussionVisibility{Type: "role", Identifier: "10002", Value: "Administrators"}
	parentStatus = 404
	refuse("source_revision_changed")
	parentStatus = 200
	project = "OTHER"
	refuse("source_revision_changed")
	project = "PRJ"
	permissionStatus = 429
	receipt, err := provider.ReplyTrackerDiscussionAtRevision(t.Context(), request, revision)
	if err != nil || receipt.Status != "failed" || receipt.ErrorCode != core.TrackerErrorRateLimited || receipt.RateLimit.ResetAt.IsZero() || posts != 0 {
		t.Fatalf("backoff: %+v %v", receipt, err)
	}
	permissionStatus = 200
	mode = "auth"
	refuse(core.TrackerErrorCredentialRevoked)
	mode = "confirmed"
	if err = provider.ValidateTrackerDiscussionReply(t.Context(), target, "20001", revision); err != nil {
		t.Fatal(err)
	}
	receipt, err = provider.ReplyTrackerDiscussionAtRevision(t.Context(), request, revision)
	if err != nil || receipt.Status != "confirmed" || receipt.MessageID != "20002" || posts != 1 {
		t.Fatalf("reply: %+v %v posts=%d", receipt, err, posts)
	}
	for _, uncertain := range []string{"unknown", "mismatch", "widened", "malformed"} {
		mode = uncertain
		previous := posts
		receipt, err = provider.ReplyTrackerDiscussionAtRevision(t.Context(), request, revision)
		if err != nil || receipt.Status != "unknown" || posts != previous+1 {
			t.Fatalf("%s send: %+v %v posts=%d", uncertain, receipt, err, posts)
		}
	}
	mode, tokenType, grantedScopes = "confirmed", "Bearer", fullGranular
	previous := posts
	receipt, err = provider.ReplyTrackerDiscussionAtRevision(t.Context(), request, revision)
	if err != nil || receipt.Status != "confirmed" || posts != previous+1 {
		t.Fatalf("complete granular reply: %+v %v posts=%d", receipt, err, posts)
	}
	provider.runtime.Credentials = discussionCredentialResolver(func(context.Context, string) (core.ActiveCredential, error) {
		return core.ActiveCredential{}, errors.New("revoked")
	})
	refuse(core.TrackerErrorCredentialRevoked)
}

func TestJiraDiscussionADFAndStableIdentity(t *testing.T) {
	for _, body := range []string{"Approved", "\nleading\n\ntrailing\n", "a\r\nb", "é 😀"} {
		normalized, err := discussionBody(tracker.RawJSON(approvedDiscussionBody(body)))
		if err != nil || normalized != body {
			t.Fatalf("round trip %q: %q %v", body, normalized, err)
		}
	}
	raw := json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"See ","marks":[{"type":"strong"}]},{"type":"text","text":"docs","marks":[{"type":"link","attrs":{"href":"https://example.test"}}]}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"account-1","text":"@Author"}},{"type":"hardBreak"},{"type":"emoji","attrs":{"shortName":":smile:"}}]}]}]}]}`)
	body, err := discussionBody(raw)
	if err != nil || body != "See docs (https://example.test)\n@Author\n:smile:" {
		t.Fatalf("ADF: %q %v", body, err)
	}
	for _, invalid := range []string{`{}`, `"plain text"`, `{"type":"doc","version":1,"content":[{"type":"mediaSingle","content":[{"type":"media"}]}]}`} {
		if _, err := discussionBody(json.RawMessage(invalid)); err == nil {
			t.Fatalf("unsupported document accepted %s", invalid)
		}
	}
}

func TestJiraDiscussionRevisionIgnoresDisplayAndJSONOrdering(t *testing.T) {
	endpoint, _ := url.Parse("https://tenant.atlassian.net")
	current := discussionContext{endpoint: endpoint, issue: discussionIssue{ID: "10001", Key: "PRJ-7"}}
	target := core.TrackerDiscussionTarget{ProviderID: ProviderID, ConnectionID: "c", ScopeID: "PRJ", ResourceType: "issue", ResourceID: "10001"}
	comment := discussionComment{ID: "20001", Self: "https://tenant.atlassian.net/rest/api/3/issue/10001/comment/20001", Updated: "2026-10-11T12:00:00Z", Body: json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Question"}]}]}`)}
	comment.Author.AccountID = "author"
	original, err := normalizeDiscussionComment(target, current, comment)
	if err != nil {
		t.Fatal(err)
	}
	comment.Author.DisplayName = "Renamed"
	comment.Body = json.RawMessage(`{"content":[{"content":[{"text":"Question","type":"text"}],"type":"paragraph"}],"version":1,"type":"doc"}`)
	current.issue.Key = "RENAMED-7"
	repeated, err := normalizeDiscussionComment(target, current, comment)
	if err != nil || repeated.Revision != original.Revision || repeated.CanonicalURL == original.CanonicalURL {
		t.Fatalf("display/ordering changed revision: %+v %v", repeated, err)
	}
	comment.Author.AccountType = "app"
	changed, err := normalizeDiscussionComment(target, current, comment)
	if err != nil || changed.Revision == original.Revision {
		t.Fatalf("author authority not versioned: %+v %v", changed, err)
	}
}
