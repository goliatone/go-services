package core

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTrackerMutationContractsValidate(t *testing.T) {
	create := TrackerIssueCreateRequest{ConnectionID: "connection", RepositoryID: "owner/repo", Fields: map[string]json.RawMessage{"title": json.RawMessage(`"Title"`)}, IdempotencyKey: "key", ActorID: "actor", CorrelationID: "correlation"}
	if err := create.Validate(); err != nil {
		t.Fatal(err)
	}
	update := TrackerIssueUpdateRequest{ConnectionID: "connection", RepositoryID: "owner/repo", IssueID: "9001", IssueNumber: 42, Field: "title", Value: json.RawMessage(`"After"`), ExpectedRevision: "r1", IdempotencyKey: "key", ActorID: "actor", CorrelationID: "correlation"}
	if err := update.Validate(); err != nil {
		t.Fatal(err)
	}
	update.IssueNumber = 0
	if err := update.Validate(); err != nil {
		t.Fatal(err)
	}
	update.IssueNumber = -1
	if err := update.Validate(); err == nil {
		t.Fatal("negative issue number accepted")
	}
	receipt := TrackerMutationReceipt{ProviderID: "github", RepositoryID: "owner/repo", ExternalID: "9001", DisplayID: "42", CanonicalURL: "https://github.com/owner/repo/issues/42", ProviderRevision: "r2", Status: "succeeded", RateLimit: TrackerRateLimit{Remaining: 10, ResetAt: time.Now().UTC()}}
	if err := receipt.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTrackerMutationContractsRejectIncompleteInputs(t *testing.T) {
	if err := (TrackerFieldCapabilityRequest{}).Validate(); err == nil {
		t.Fatal("empty capability request accepted")
	}
	if err := (TrackerIssueCreateRequest{}).Validate(); err == nil {
		t.Fatal("empty create request accepted")
	}
	if err := (TrackerIssueUpdateRequest{}).Validate(); err == nil {
		t.Fatal("empty update request accepted")
	}
	if err := (TrackerMutationReceipt{}).Validate(); err == nil {
		t.Fatal("empty receipt accepted")
	}
}

func TestTrackerMutationUnknownReceiptNeedsIdentityButNotCommittedRevision(t *testing.T) {
	receipt := TrackerMutationReceipt{ProviderID: "github_projects_v2", RepositoryID: "project", ExternalID: "item", DisplayID: "item", CanonicalURL: "https://github.com/acme/test/issues/1", Status: "unknown"}
	if err := receipt.Validate(); err != nil {
		t.Fatal(err)
	}
	receipt.Status = "succeeded"
	if receipt.Validate() == nil {
		t.Fatal("confirmed receipt without committed revision accepted")
	}
	receipt.Status = "unknown"
	receipt.ExternalID = ""
	if receipt.Validate() == nil {
		t.Fatal("unknown receipt without identity accepted")
	}
}
