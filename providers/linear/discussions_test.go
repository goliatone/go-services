package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/goliatone/go-services/core"
)

func discussionNode(body, team string) string {
	return fmt.Sprintf(`{"id":"comment-1","body":%q,"updatedAt":"2026-10-11T12:00:00Z","url":"https://linear.app/team/issue/TEAM-7#comment-1","user":{"id":"user-1","name":"Author"},"issue":{"id":"issue-1","team":{"id":%q}}}`, body, team)
}
func discussionTarget() core.TrackerDiscussionTarget {
	return core.TrackerDiscussionTarget{ProviderID: ProviderID, ConnectionID: "connection-1", ScopeID: "team-1", ResourceType: "issue", ResourceID: "issue-1"}
}

func TestLinearDiscussionScopeAndConditionalReply(t *testing.T) {
	ctx := context.Background()
	body, team := "Question", "team-1"
	mutationMode := "confirmed"
	malformedPage := false
	posts := 0
	provider, closeServer := linearMutationTestProvider(t, true, func(query string, variables map[string]any) string {
		switch {
		case strings.Contains(query, "commentCreate"):
			posts++
			if mutationMode == "unknown" {
				return `{"errors":[{"message":"response uncertain"}]}`
			}
			input := variables["input"].(map[string]any)
			if input["issueId"] != "issue-1" || input["parentId"] != "comment-1" || input["body"] != "Approved" {
				t.Errorf("reply escaped approved input: %#v", input)
			}
			node := strings.Replace(discussionNode("Approved", "team-1"), `"id":"comment-1"`, `"id":"reply-1","parentId":"comment-1"`, 1)
			return `{"data":{"commentCreate":{"success":true,"comment":` + node + `}}}`
		case strings.Contains(query, "comments(first:"):
			if malformedPage {
				return `{"data":{"issue":{"id":"issue-1","team":{"id":"team-1"},"comments":{}}}}`
			}
			if variables["first"] != float64(1) || variables["after"] != "opaque-before" {
				t.Errorf("page variables: %#v", variables)
			}
			return `{"data":{"issue":{"id":"issue-1","team":{"id":"team-1"},"comments":{"nodes":[` + discussionNode(body, team) + `],"pageInfo":{"hasNextPage":true,"endCursor":"opaque-next"}}}}}`
		case strings.Contains(query, "comment(id:"):
			return `{"data":{"comment":` + discussionNode(body, team) + `}}`
		default:
			return `{"data":{"issue":{"id":"issue-1","team":{"id":"team-1"}}}}`
		}
	})
	defer closeServer()
	target := discussionTarget()
	read := core.TrackerDiscussionReadRequest{Target: target, Limit: 1, Cursor: "opaque-before"}
	page, err := provider.ReadTrackerDiscussion(ctx, read)
	if err != nil || len(page.Items) != 1 || page.NextCursor != "opaque-next" {
		t.Fatalf("read: %+v %v", page, err)
	}
	revision := page.Items[0].Revision
	var original discussionComment
	if err = json.Unmarshal([]byte(discussionNode(body, team)), &original); err != nil {
		t.Fatal(err)
	}
	original.User.Name = "Renamed"
	original.URL = "https://linear.app/team/issue/TEAM-7/renamed#comment-1"
	mapped, err := normalizeDiscussionComment(target, original)
	if err != nil || mapped.Revision != revision {
		t.Fatalf("display change altered source: %+v %v", mapped, err)
	}
	caps, err := provider.DiscoverTrackerDiscussionCapabilities(ctx, target)
	if err != nil || !caps.Read.Available || !caps.Reply.Available || caps.Webhook.Available || caps.ReceiptLookup.Available {
		t.Fatalf("caps: %+v %v", caps, err)
	}
	request := core.TrackerDiscussionReplyRequest{Target: target, ParentID: "comment-1", Body: "Approved", ActorID: "reviewer", IdempotencyKey: "effect-1", CorrelationID: "corr", CausationID: "decision"}
	body = "Edited"
	receipt, err := provider.ReplyTrackerDiscussionAtRevision(ctx, request, revision)
	if err != nil || receipt.Status != "failed" || receipt.ErrorCode != core.TrackerErrorConflict || posts != 0 {
		t.Fatalf("stale reply: %+v %v posts=%d", receipt, err, posts)
	}
	body = "Question"
	team = "foreign"
	page, err = provider.ReadTrackerDiscussion(ctx, read)
	if err == nil || len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatalf("foreign page escaped: %+v %v", page, err)
	}
	team = "team-1"
	malformedPage = true
	page, err = provider.ReadTrackerDiscussion(ctx, read)
	if err == nil || len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatalf("incomplete page escaped: %+v %v", page, err)
	}
	malformedPage = false
	receipt, err = provider.ReplyTrackerDiscussionAtRevision(ctx, request, revision)
	if err != nil || receipt.Status != "confirmed" || posts != 1 {
		t.Fatalf("approved reply: %+v %v posts=%d", receipt, err, posts)
	}
	mutationMode = "unknown"
	receipt, err = provider.ReplyTrackerDiscussionAtRevision(ctx, request, revision)
	if err != nil || receipt.Status != "unknown" || posts != 2 {
		t.Fatalf("uncertain reply: %+v %v posts=%d", receipt, err, posts)
	}
}

func TestLinearDiscussionReplyUsesCommentGrant(t *testing.T) {
	posts := 0
	provider, closeServer := linearMutationTestProvider(t, false, func(query string, _ map[string]any) string {
		if strings.Contains(query, "commentCreate") {
			posts++
			return `{"errors":[{"message":"uncertain"}]}`
		}
		return `{"data":{"issue":{"id":"issue-1","team":{"id":"team-1"}}}}`
	})
	defer closeServer()
	target := discussionTarget()
	caps, err := provider.DiscoverTrackerDiscussionCapabilities(t.Context(), target)
	if err != nil || caps.Reply.Available || !caps.Read.Available {
		t.Fatalf("unproven grant: %+v %v", caps, err)
	}
	request := core.TrackerDiscussionReplyRequest{Target: target, Body: "Approved", ActorID: "reviewer", IdempotencyKey: "effect-1", CorrelationID: "corr", CausationID: "decision"}
	receipt, err := provider.ReplyTrackerDiscussion(t.Context(), request)
	if err != nil || receipt.Status != "failed" || posts != 0 {
		t.Fatalf("denied reply: %+v %v posts=%d", receipt, err, posts)
	}
	resolver := provider.runtime.Credentials
	provider.runtime.Credentials = linearCredentialResolver(func(ctx context.Context, id string) (core.ActiveCredential, error) {
		credential, err := resolver.ResolveTrackerCredential(ctx, id)
		credential.GrantedScopes = []string{"read", "comments:create"}
		return credential, err
	})
	caps, err = provider.DiscoverTrackerDiscussionCapabilities(t.Context(), target)
	if err != nil || !caps.Reply.Available {
		t.Fatalf("comment-only grant: %+v %v", caps, err)
	}
	receipt, err = provider.ReplyTrackerDiscussion(t.Context(), request)
	if err != nil || receipt.Status != "unknown" || posts != 1 {
		t.Fatalf("comment-only reply: %+v %v posts=%d", receipt, err, posts)
	}
}
