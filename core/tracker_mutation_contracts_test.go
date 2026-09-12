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
