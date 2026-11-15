package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func issuePatchRequest() TrackerIssuePatchRequest {
	return TrackerIssuePatchRequest{
		Target: TrackerIssuePatchTarget{ConnectionID: "connection", AccountID: "account", TeamID: "team", IssueID: "9dc316cd-5241-423d-9f52-fc1efc634782"},
		Field:  "title", ExpectedRevision: "before", ExpectedValue: json.RawMessage(`"Before"`), Value: json.RawMessage(`"After"`),
		Execution: TrackerPublicationExecution{ActorID: "actor", IdempotencyKey: "intent", CorrelationID: "correlation", CausationID: "cause"},
	}
}

func issuePatchObservation() TrackerIssuePatchObservation {
	description, archived := "", false
	return TrackerIssuePatchObservation{Target: issuePatchRequest().Target, DisplayID: "COL-42", CanonicalURL: "https://linear.app/team/issue/COL-42", Revision: "after", ObservedAt: time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC), Title: "After", Description: &description, StateID: "70f21609-e163-4e34-bdd1-e19044a89194", Archived: &archived}
}

func TestIssuePatchPayloadBoundary(t *testing.T) {
	request := issuePatchRequest()
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*TrackerIssuePatchRequest){
		"missing account":          func(r *TrackerIssuePatchRequest) { r.Target.AccountID = "" },
		"display issue ID":         func(r *TrackerIssuePatchRequest) { r.Target.IssueID = "COL-42" },
		"nil UUID":                 func(r *TrackerIssuePatchRequest) { r.Target.IssueID = "00000000-0000-0000-0000-000000000000" },
		"missing actor":            func(r *TrackerIssuePatchRequest) { r.Execution.ActorID = "" },
		"missing effect key":       func(r *TrackerIssuePatchRequest) { r.Execution.IdempotencyKey = "" },
		"missing baseline":         func(r *TrackerIssuePatchRequest) { r.ExpectedValue = nil },
		"missing revision":         func(r *TrackerIssuePatchRequest) { r.ExpectedRevision = "" },
		"unsupported PM field":     func(r *TrackerIssuePatchRequest) { r.Field = "assignees" },
		"null description":         func(r *TrackerIssuePatchRequest) { r.Field = "description"; r.Value = json.RawMessage(`null`) },
		"object instead of string": func(r *TrackerIssuePatchRequest) { r.Value = json.RawMessage(`{"title":"After"}`) },
		"blank title":              func(r *TrackerIssuePatchRequest) { r.Value = json.RawMessage(`"  "`) },
		"oversized title": func(r *TrackerIssuePatchRequest) {
			r.Value, _ = json.Marshal(strings.Repeat("x", TrackerIssuePatchMaxTitleBytes+1))
		},
		"invalid UTF8": func(r *TrackerIssuePatchRequest) { r.Value = json.RawMessage{'"', 0xff, '"'} },
		"state label": func(r *TrackerIssuePatchRequest) {
			r.Field = "state"
			r.ExpectedSchemaRevision = "schema"
			r.Value = json.RawMessage(`"Done"`)
		},
		"irrelevant schema": func(r *TrackerIssuePatchRequest) { r.ExpectedSchemaRevision = "schema" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := request
			mutate(&invalid)
			if invalid.Validate() == nil {
				t.Fatal("accepted invalid patch")
			}
		})
	}
	request.Field = "description"
	request.Value = json.RawMessage(`""`)
	if err := request.Validate(); err != nil {
		t.Fatalf("explicit clear: %v", err)
	}
	request.Value, _ = json.Marshal(strings.Repeat("x", TrackerIssuePatchMaxDescriptionBytes))
	if err := request.Validate(); err != nil {
		t.Fatalf("bounded description: %v", err)
	}
	request.Value, _ = json.Marshal(strings.Repeat("x", TrackerIssuePatchMaxDescriptionBytes+1))
	if request.Validate() == nil {
		t.Fatal("accepted oversized description")
	}
	request.Field = "state"
	request.Value, _ = json.Marshal(issuePatchObservation().StateID)
	request.ExpectedValue = request.Value
	if request.Validate() == nil {
		t.Fatal("accepted state without schema revision")
	}
	request.ExpectedSchemaRevision = "schema"
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestIssuePatchInspectionScopeAndPresence(t *testing.T) {
	request := TrackerIssuePatchInspectRequest{Target: issuePatchRequest().Target, Field: "title"}
	inspection := TrackerIssuePatchInspection{Observation: issuePatchObservation(), Capability: TrackerFieldCapability{Field: "title", Operation: "patch", Granted: true, Revision: "cap"}}
	if err := inspection.ValidateFor(request); err != nil {
		t.Fatalf("text inspection without schema or project: %v", err)
	}
	missing := inspection
	missing.Observation.Description = nil
	if missing.ValidateFor(request) == nil {
		t.Fatal("missing description became an empty description")
	}
	wrong := inspection
	wrong.Observation.Target.AccountID = "another-account"
	if wrong.ValidateFor(request) == nil {
		t.Fatal("accepted cross-account observation")
	}
	wrong = inspection
	wrong.Capability.Operation = "update"
	if wrong.ValidateFor(request) == nil {
		t.Fatal("accepted strict-update capability as optimistic patch")
	}
	archived := true
	wrong = inspection
	wrong.Observation.Archived = &archived
	if wrong.ValidateFor(request) == nil {
		t.Fatal("accepted patch grant for an archived issue")
	}
	request.Field, inspection.Capability.Field = "state", "state"
	if inspection.ValidateFor(request) == nil {
		t.Fatal("accepted state grant without schema")
	}
	inspection.Capability.Granted, inspection.Capability.Reason = false, "schema_unavailable"
	if err := inspection.ValidateFor(request); err != nil {
		t.Fatalf("denied inspection lost its reason: %v", err)
	}
	inspection.Capability.Granted, inspection.Capability.Reason = true, ""
	inspection.StateSchema = &TrackerIssuePatchStateSchema{Revision: "schema", Complete: true, States: []TrackerNode{{ExternalID: inspection.Observation.StateID, Type: "started", Name: "In progress", ParentID: request.Target.TeamID, NativeRevision: "option"}}}
	if err := inspection.ValidateFor(request); err != nil {
		t.Fatalf("unknown automation must remain representable: %v", err)
	}
	inspection.StateSchema.Complete = false
	if inspection.ValidateFor(request) == nil {
		t.Fatal("incomplete schema allowed state patch grant")
	}
	inspection.StateSchema.Complete = true
	inspection.StateSchema.States[0].ParentID = "another-team"
	if inspection.ValidateFor(request) == nil {
		t.Fatal("accepted foreign-team state")
	}
}

func TestIssuePatchResultEvidenceBoundary(t *testing.T) {
	request, observation := issuePatchRequest(), issuePatchObservation()
	receipt := TrackerMutationReceipt{ProviderID: "linear", RepositoryID: request.Target.TeamID, ExternalID: request.Target.IssueID, DisplayID: observation.DisplayID, CanonicalURL: observation.CanonicalURL, ProviderRevision: observation.Revision, Status: TrackerIssuePatchConfirmed, CorrelationID: request.Execution.CorrelationID, CausationID: request.Execution.CausationID}
	confirmed := TrackerIssuePatchResult{Status: TrackerIssuePatchConfirmed, Observation: &observation, Receipt: &receipt}
	if err := confirmed.ValidateFor(request); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{TrackerIssuePatchDesiredStateObserved, TrackerIssuePatchConflict, TrackerIssuePatchFailed, TrackerIssuePatchUnknown} {
		result := TrackerIssuePatchResult{Status: status, ErrorCode: "reason"}
		if status == TrackerIssuePatchDesiredStateObserved {
			result.Observation = &observation
			result.ErrorCode = ""
		}
		if status == TrackerIssuePatchConflict {
			result.Observation = &observation
		}
		if err := result.ValidateFor(request); err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		result.Receipt = &receipt
		if result.ValidateFor(request) == nil {
			t.Fatalf("%s attributed an unconfirmed mutation", status)
		}
	}
	withoutReceipt := confirmed
	withoutReceipt.Receipt = nil
	if withoutReceipt.ValidateFor(request) == nil {
		t.Fatal("confirmed without receipt")
	}
	receipt.CorrelationID = "another-command"
	if confirmed.ValidateFor(request) == nil {
		t.Fatal("accepted unrelated receipt")
	}
	receipt.CorrelationID = request.Execution.CorrelationID
	receipt.ProviderRevision = "different-observation"
	if confirmed.ValidateFor(request) == nil {
		t.Fatal("accepted receipt/observation drift")
	}
	receipt.ProviderRevision = observation.Revision
	observation.Title = "Concurrent edit"
	if confirmed.ValidateFor(request) == nil {
		t.Fatal("mismatched read-back claimed success")
	}
	unknown := TrackerIssuePatchResult{Status: TrackerIssuePatchUnknown, ErrorCode: "readback_mismatch", Observation: &observation}
	if err := unknown.ValidateFor(request); err != nil {
		t.Fatal(err)
	}
	observation.Target.TeamID = "other-team"
	if unknown.ValidateFor(request) == nil {
		t.Fatal("unknown result retained cross-scope evidence")
	}
}
