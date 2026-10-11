package core

import (
	"strings"
	"testing"
	"time"
)

func TestTrackerDiscussionScopeAndReceipts(t *testing.T) {
	target := TrackerDiscussionTarget{ProviderID: "github", ConnectionID: "connection", ScopeID: "owner/repo", ResourceType: "issue", ResourceID: "issue-42"}
	message := TrackerDiscussionMessage{ProviderID: "github", Target: target, MessageID: "comment-1", Revision: "v1", CanonicalURL: "https://github.com/owner/repo/issues/42#issuecomment-1", Author: TrackerDiscussionAuthor{ID: "author"}, Body: "A request", ObservedAt: time.Now().UTC()}
	page := TrackerDiscussionPage{Target: target, Items: []TrackerDiscussionMessage{message}}
	if err := page.Validate(); err != nil {
		t.Fatal(err)
	}
	page.Items[0].Target.ConnectionID = "other-connection"
	if err := page.Validate(); err == nil {
		t.Fatal("cross-connection page accepted")
	}
	page.Items = nil
	page.HasMore = true
	if err := page.Validate(); err == nil {
		t.Fatal("continuation without cursor accepted")
	}
	page.NextCursor = "opaque"
	if err := page.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"project", "draft", "pull_request"} {
		unsupported := target
		unsupported.ResourceType = kind
		if err := unsupported.Validate(); err == nil {
			t.Fatalf("unsupported target %q accepted", kind)
		}
	}
	message.Tombstone = true
	if err := message.Validate(); err == nil {
		t.Fatal("tombstone retained content")
	}
	message.Body, message.Author = "", TrackerDiscussionAuthor{}
	if err := message.Validate(); err != nil {
		t.Fatal(err)
	}
	receipt := TrackerDiscussionReceipt{ProviderID: "github", Target: target, IdempotencyKey: "operation-1", Status: "unknown", ErrorCode: "outcome_unknown"}
	if err := receipt.Validate(); err != nil {
		t.Fatal(err)
	}
	receipt.Status, receipt.ErrorCode = "confirmed", ""
	if err := receipt.Validate(); err == nil {
		t.Fatal("confirmed receipt without evidence accepted")
	}
	receipt.MessageID, receipt.Revision, receipt.CanonicalURL = "comment-1", "v1", message.CanonicalURL
	if err := receipt.Validate(); err != nil {
		t.Fatal(err)
	}
	receipt.CanonicalURL = "https://token@github.com/owner/repo/issues/42"
	if err := receipt.Validate(); err == nil {
		t.Fatal("credential-bearing URL accepted")
	}
}

func TestTrackerDiscussionReplyIdentityAndIndependentCapabilities(t *testing.T) {
	target := TrackerDiscussionTarget{ProviderID: "github", ConnectionID: "connection", ScopeID: "scope", ResourceType: "issue", ResourceID: "issue"}
	request := TrackerDiscussionReplyRequest{Target: target, Body: "Approved reply", ActorID: "actor", IdempotencyKey: "operation", CorrelationID: "correlation", CausationID: "decision"}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	request.CausationID = ""
	if err := request.Validate(); err == nil {
		t.Fatal("reply without causation accepted")
	}
	request.CausationID, request.Body = "decision", strings.Repeat("x", TrackerDiscussionMaxBodyBytes+1)
	if err := request.Validate(); err == nil {
		t.Fatal("unbounded reply accepted")
	}
	unavailable := TrackerDiscussionCapability{Reason: "unsupported"}
	capabilities := TrackerDiscussionCapabilities{Revision: "grants-v1", Read: TrackerDiscussionCapability{Available: true}, Reply: unavailable, Webhook: unavailable, ReceiptLookup: unavailable}
	if err := capabilities.Validate(); err != nil {
		t.Fatal(err)
	}
	capabilities.ReceiptLookup = TrackerDiscussionCapability{}
	if err := capabilities.Validate(); err == nil {
		t.Fatal("unsupported receipt lookup without reason accepted")
	}
}

func TestTrackerDiscussionContentAndProviderBoundary(t *testing.T) {
	target := TrackerDiscussionTarget{ProviderID: "github", ConnectionID: "c", ScopeID: "s", ResourceType: "issue", ResourceID: "i"}
	message := TrackerDiscussionMessage{ProviderID: "github", Target: target, MessageID: "m", Revision: "r", CanonicalURL: "https://github.com/o/r/issues/1", Author: TrackerDiscussionAuthor{ID: "a"}, ObservedAt: time.Now()}
	if err := message.Validate(); err == nil {
		t.Fatal("empty live message accepted")
	}
	message.Tombstone = true
	if err := message.Validate(); err != nil {
		t.Fatal(err)
	}
	message.Tombstone, message.Body = false, "request"
	request := TrackerDiscussionReadRequest{Target: target, Limit: 1}
	page := TrackerDiscussionPage{Target: target, Items: []TrackerDiscussionMessage{message}}
	if err := page.ValidateFor(request); err != nil {
		t.Fatal(err)
	}
	other := message
	other.ProviderID, other.MessageID = "jira", "m2"
	page.Items = append(page.Items, other)
	if err := page.Validate(); err == nil {
		t.Fatal("mixed provider page accepted")
	}
	other.ProviderID = "github"
	page.Items[1] = other
	if err := page.ValidateFor(request); err == nil {
		t.Fatal("requested limit exceeded")
	}
	page.Items = nil
	page.Target.ProviderID = "jira"
	if err := page.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := page.ValidateFor(request); err == nil {
		t.Fatal("foreign provider empty page accepted")
	}
	page.Target = target
	page.Target.ConnectionID = "other"
	if err := page.ValidateFor(request); err == nil {
		t.Fatal("foreign connection empty page accepted")
	}
	receipt := TrackerDiscussionReceipt{ProviderID: "jira", Target: target, IdempotencyKey: "op", Status: "unknown", ErrorCode: "outcome_unknown"}
	if err := receipt.Validate(); err == nil {
		t.Fatal("foreign provider receipt accepted")
	}
	hint := TrackerDiscussionWebhookHint{ProviderID: "jira", Target: target, DeliveryID: "d", MessageID: "m", Revision: "r", Action: "create"}
	if err := hint.Validate(); err == nil {
		t.Fatal("foreign provider webhook hint accepted")
	}
	receipt.ProviderID, receipt.Target.ProviderID = "jira", "jira"
	if err := receipt.Validate(); err != nil {
		t.Fatal(err)
	}
	lookup := TrackerDiscussionReceiptRequest{Target: target, IdempotencyKey: "op"}
	if err := receipt.ValidateFor(lookup); err == nil {
		t.Fatal("foreign self-consistent receipt accepted")
	}
	receipt.ProviderID, receipt.Target = "github", target
	if err := receipt.ValidateFor(lookup); err != nil {
		t.Fatal(err)
	}
	receipt.IdempotencyKey = "other-operation"
	if err := receipt.ValidateFor(lookup); err == nil {
		t.Fatal("foreign operation receipt accepted")
	}
	hint.Target.ProviderID = "jira"
	if err := hint.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := hint.ValidateFor(target); err == nil {
		t.Fatal("foreign self-consistent webhook hint accepted")
	}
}
