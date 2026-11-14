package core

import "testing"

func TestPublicationPayloadBoundary(t *testing.T) {
	scope := TrackerPublicationScope{ConnectionID: "c", AccountID: "a", TeamID: "t", ProjectID: "p"}
	execution := TrackerPublicationExecution{ActorID: "actor", IdempotencyKey: "intent", CorrelationID: "correlation"}
	request := TrackerPublicationIssueCreateRequest{Scope: scope, Execution: execution, RequestedID: "9dc316cd-5241-423d-9f52-fc1efc634782", MilestoneID: "m", AssigneeID: "human", Fields: TrackerPublicationFields{Title: "Spec", StateID: "todo"}}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", "name", "9dc316cd-5241-523d-9f52-fc1efc634782"} {
		invalid := request
		invalid.RequestedID = id
		if invalid.Validate() == nil {
			t.Fatalf("accepted invalid create id %q", id)
		}
	}
	request.AssigneeID = ""
	if request.Validate() == nil {
		t.Fatal("accepted missing human default")
	}
	empty := ""
	update := TrackerPublicationIssueUpdateRequest{Target: TrackerPublicationReadRequest{Scope: scope, ID: "issue"}, Execution: execution, MilestoneID: "m", Expected: request.Fields, Patch: TrackerPublicationPatch{Description: &empty}}
	if err := update.Validate(); err != nil {
		t.Fatal(err)
	}
	update.Patch.Title = &empty
	if update.Validate() == nil {
		t.Fatal("accepted blank managed title")
	}
}
