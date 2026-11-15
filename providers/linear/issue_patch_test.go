package linear

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/goliatone/go-services/core"
	trackerruntime "github.com/goliatone/go-services/providers/tracker"
)

const patchIssueID = "9dc316cd-5241-423d-9f52-fc1efc634782"
const patchTodoID = "70f21609-e163-4e34-bdd1-e19044a89194"
const patchDoneID = "90a2a8b3-7103-45ef-9ff1-481f098dbdc3"

func issuePatchTestData() map[string]any {
	return map[string]any{
		"organization": map[string]any{"id": "account"},
		"team":         map[string]any{"id": "team", "organization": map[string]any{"id": "account"}, "archivedAt": nil, "autoCloseParentIssues": false, "autoCloseChildIssues": false, "states": map[string]any{"nodes": []any{map[string]any{"id": patchTodoID, "name": "Todo", "type": "unstarted", "team": map[string]any{"id": "team"}, "archivedAt": nil}, map[string]any{"id": patchDoneID, "name": "Done", "type": "completed", "team": map[string]any{"id": "team"}, "archivedAt": nil}}, "pageInfo": map[string]any{"hasNextPage": false}}},
		"issue":        map[string]any{"id": patchIssueID, "identifier": "COL-42", "url": "https://linear.app/team/issue/COL-42", "title": "Before", "description": "Body", "updatedAt": "2026-11-15T00:00:00Z", "archivedAt": nil, "team": map[string]any{"id": "team"}, "state": map[string]any{"id": patchTodoID}, "project": nil, "projectMilestone": nil, "parent": nil},
	}
}

func issuePatchJSON(data any) string {
	b, _ := json.Marshal(map[string]any{"data": data})
	return string(b)
}
func issuePatchTestRequest() core.TrackerIssuePatchRequest {
	return core.TrackerIssuePatchRequest{Target: core.TrackerIssuePatchTarget{ConnectionID: "connection", AccountID: "account", TeamID: "team", IssueID: patchIssueID}, Field: "title", ExpectedRevision: "2026-11-15T00:00:00Z", ExpectedValue: json.RawMessage(`"Before"`), Value: json.RawMessage(`"After"`), Execution: core.TrackerPublicationExecution{ActorID: "owner", IdempotencyKey: "effect", CorrelationID: "correlation", CausationID: "cause"}}
}

func TestIssuePatchJourneyAndReadOnlyRecovery(t *testing.T) {
	data := issuePatchTestData()
	issue := data["issue"].(map[string]any)
	mutations, stateReads := 0, 0
	mode := "confirmed"
	provider, closeServer := linearMutationTestProvider(t, true, func(query string, vars map[string]any) string {
		if strings.Contains(query, "mutation(") {
			mutations++
			patch := vars["input"].(map[string]any)
			if len(patch) != 1 || vars["id"] != patchIssueID {
				t.Errorf("escaped one-field intent: %+v", vars)
			}
			for key, value := range patch {
				if key != "title" && key != "description" && key != "stateId" {
					t.Errorf("unexpected field: %s", key)
				}
				if key == "stateId" {
					issue["state"] = map[string]any{"id": value}
				} else {
					issue[key] = value
				}
			}
			issue["updatedAt"] = "2026-11-15T01:00:00Z"
			if mode == "lost" {
				return `{"errors":[{"message":"response lost"}]}`
			}
			if mode == "mismatch" {
				issue["title"] = "Concurrent edit"
			}
			if mode == "missing-readback" {
				delete(issue, "description")
			}
			return issuePatchJSON(map[string]any{"issueUpdate": map[string]any{"success": true, "issue": map[string]any{"id": patchIssueID}}})
		}
		if strings.Contains(query, "states(first:") {
			stateReads++
		}
		return issuePatchJSON(data)
	})
	defer closeServer()
	ctx := context.Background()
	request := issuePatchTestRequest()
	inspect := core.TrackerIssuePatchInspectRequest{Target: request.Target, Field: request.Field}
	inspection, err := provider.InspectTrackerIssuePatch(ctx, inspect)
	if err != nil || inspection.Observation.ProjectID == nil || *inspection.Observation.ProjectID != "" || mutations != 0 {
		t.Fatalf("ordinary issue inspection: %+v %v", inspection, err)
	}
	result, err := provider.PatchTrackerIssue(ctx, request)
	if err != nil || result.Status != core.TrackerIssuePatchConfirmed || result.ValidateFor(request) != nil || mutations != 1 {
		t.Fatalf("patch: %+v %v writes=%d", result, err, mutations)
	}
	result, err = provider.PatchTrackerIssue(ctx, request)
	if err != nil || result.Status != core.TrackerIssuePatchDesiredStateObserved || result.Receipt != nil || mutations != 1 {
		t.Fatalf("repeat: %+v %v writes=%d", result, err, mutations)
	}
	stale := request
	stale.Value = json.RawMessage(`"Another"`)
	result, err = provider.PatchTrackerIssue(ctx, stale)
	if err != nil || result.Status != core.TrackerIssuePatchConflict || mutations != 1 {
		t.Fatalf("stale: %+v %v", result, err)
	}
	request.Field = "description"
	request.ExpectedRevision = issue["updatedAt"].(string)
	request.ExpectedValue = json.RawMessage(`"Body"`)
	request.Value = json.RawMessage(`""`)
	result, err = provider.PatchTrackerIssue(ctx, request)
	if err != nil || result.Status != core.TrackerIssuePatchConfirmed || issue["description"] != "" || mutations != 2 || stateReads != 0 {
		t.Fatalf("clear without state discovery: %+v %v", result, err)
	}
	request = issuePatchTestRequest()
	request.ExpectedRevision = issue["updatedAt"].(string)
	request.ExpectedValue = json.RawMessage(`"After"`)
	request.Value = json.RawMessage(`"Lost reply"`)
	mode = "lost"
	result, err = provider.PatchTrackerIssue(ctx, request)
	if err != nil || result.Status != core.TrackerIssuePatchUnknown || result.Receipt != nil || mutations != 3 {
		t.Fatalf("unknown: %+v %v", result, err)
	}
	inspection, err = provider.InspectTrackerIssuePatch(ctx, inspect)
	if err != nil || inspection.Observation.Title != "Lost reply" || mutations != 3 {
		t.Fatalf("read recovery retried mutation: %+v %v", inspection, err)
	}
	request.ExpectedValue = json.RawMessage(`"Lost reply"`)
	request.Value = json.RawMessage(`"Requested"`)
	mode = "mismatch"
	result, err = provider.PatchTrackerIssue(ctx, request)
	if err != nil || result.Status != core.TrackerIssuePatchUnknown || result.Observation == nil || result.Observation.Title != "Concurrent edit" || result.ValidateFor(request) != nil || mutations != 4 {
		t.Fatalf("post-dispatch conflict: %+v %v", result, err)
	}
	request.ExpectedValue = json.RawMessage(`"Concurrent edit"`)
	mode = "missing-readback"
	result, err = provider.PatchTrackerIssue(ctx, request)
	if err != nil || result.Status != core.TrackerIssuePatchUnknown || result.Observation != nil || mutations != 5 {
		t.Fatalf("missing readback: %+v %v", result, err)
	}
}

func TestIssuePatchScopeAndObservationRefusals(t *testing.T) {
	for _, scenario := range []string{"account", "issue-team", "issue-id", "team-archived", "issue-archived", "missing-archive", "missing-description", "missing-project", "wrong-url", "no-grant"} {
		t.Run(scenario, func(t *testing.T) {
			data := issuePatchTestData()
			issue, team := data["issue"].(map[string]any), data["team"].(map[string]any)
			switch scenario {
			case "account":
				data["organization"] = map[string]any{"id": "foreign"}
			case "issue-team":
				issue["team"] = map[string]any{"id": "foreign"}
			case "issue-id":
				issue["id"] = patchDoneID
			case "team-archived":
				team["archivedAt"] = "2026-11-15T00:00:00Z"
			case "issue-archived":
				issue["archivedAt"] = "2026-11-15T00:00:00Z"
			case "missing-archive":
				delete(issue, "archivedAt")
			case "missing-description":
				delete(issue, "description")
			case "missing-project":
				delete(issue, "project")
			case "wrong-url":
				issue["url"] = "https://other.example/issue"
			}
			writes := 0
			provider, closeServer := linearMutationTestProvider(t, scenario != "no-grant", func(query string, _ map[string]any) string {
				if strings.Contains(query, "mutation(") {
					writes++
				}
				return issuePatchJSON(data)
			})
			defer closeServer()
			if _, err := provider.PatchTrackerIssue(context.Background(), issuePatchTestRequest()); err == nil || writes != 0 {
				t.Fatalf("scope/evidence guard failed: err=%v writes=%d", err, writes)
			}
		})
	}
}

func TestIssuePatchStateSchemaAndAutomationGuards(t *testing.T) {
	for _, scenario := range []string{"safe", "parent-enabled", "child-enabled", "unknown", "schema-changed", "incomplete", "foreign-option", "duplicate", "text-without-schema"} {
		t.Run(scenario, func(t *testing.T) {
			data := issuePatchTestData()
			team, issue := data["team"].(map[string]any), data["issue"].(map[string]any)
			states := team["states"].(map[string]any)
			switch scenario {
			case "parent-enabled":
				team["autoCloseParentIssues"] = true
			case "child-enabled":
				team["autoCloseChildIssues"] = true
			case "unknown":
				team["autoCloseChildIssues"] = nil
			case "incomplete":
				states["pageInfo"] = map[string]any{"hasNextPage": true, "endCursor": "next"}
			case "foreign-option":
				states["nodes"].([]any)[1].(map[string]any)["team"] = map[string]any{"id": "foreign"}
			case "duplicate":
				states["nodes"].([]any)[1].(map[string]any)["type"] = "duplicate"
			case "text-without-schema":
				delete(team, "states")
				issue["description"] = nil
			}
			writes := 0
			provider, closeServer := linearMutationTestProvider(t, true, func(query string, vars map[string]any) string {
				if strings.Contains(query, "mutation(") {
					writes++
					patch := vars["input"].(map[string]any)
					if len(patch) != 1 {
						t.Error("multiple fields patched")
					}
					if value, ok := patch["stateId"]; ok {
						issue["state"] = map[string]any{"id": value}
					} else {
						issue["title"] = patch["title"]
					}
					issue["updatedAt"] = "2026-11-15T01:00:00Z"
					return issuePatchJSON(map[string]any{"issueUpdate": map[string]any{"success": true, "issue": map[string]any{"id": patchIssueID}}})
				}
				return issuePatchJSON(data)
			})
			defer closeServer()
			request := issuePatchTestRequest()
			if scenario != "text-without-schema" {
				request.Field = "state"
				request.ExpectedValue, _ = json.Marshal(patchTodoID)
				request.Value, _ = json.Marshal(patchDoneID)
				inspection, err := provider.InspectTrackerIssuePatch(context.Background(), core.TrackerIssuePatchInspectRequest{Target: request.Target, Field: "state"})
				if err != nil {
					t.Fatal(err)
				}
				request.ExpectedSchemaRevision = "unavailable"
				if inspection.StateSchema != nil {
					request.ExpectedSchemaRevision = inspection.StateSchema.Revision
				}
				if scenario == "schema-changed" {
					team["autoCloseParentIssues"] = true
				}
			}
			result, err := provider.PatchTrackerIssue(context.Background(), request)
			if scenario == "safe" || scenario == "text-without-schema" {
				if err != nil || result.Status != core.TrackerIssuePatchConfirmed || writes != 1 {
					t.Fatalf("allowed patch: %+v %v writes=%d", result, err, writes)
				}
			} else if writes != 0 || err == nil && result.Status != core.TrackerIssuePatchConflict {
				t.Fatalf("guard failed: %+v %v writes=%d", result, err, writes)
			}
		})
	}
}

func TestIssuePatchMutationDoesNotRefreshOrReplay(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusTemporaryRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			exchanges, writes := 0, 0
			client := &http.Client{Transport: linearRoundTrip(func(request *http.Request) (*http.Response, error) {
				code, body := http.StatusOK, ""
				if request.URL.String() == linearOAuthTokenURL {
					exchanges++
					body = `{"access_token":"token","token_type":"Bearer","scope":"read write","expires_in":3600}`
				} else {
					payload, _ := io.ReadAll(request.Body)
					if strings.Contains(string(payload), "mutation(") {
						writes++
						code = status
						body = "unconfirmed"
					} else {
						body = issuePatchJSON(issuePatchTestData())
					}
				}
				return &http.Response{StatusCode: code, Header: http.Header{"Retry-After": []string{"2"}, "Location": []string{"https://api.linear.app/graphql"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			value, err := New(Config{TrackerRuntime: trackerruntime.Runtime{HTTPClient: client, Credentials: linearCredentialResolver(func(_ context.Context, id string) (core.ActiveCredential, error) {
				return core.ActiveCredential{ConnectionID: id, Metadata: map[string]any{"auth_type": "oauth_client", "client_id": "client", "client_secret": "secret"}}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := value.(*Provider).PatchTrackerIssue(context.Background(), issuePatchTestRequest())
			want := core.TrackerIssuePatchFailed
			if status == http.StatusBadGateway || status == http.StatusTemporaryRedirect {
				want = core.TrackerIssuePatchUnknown
			}
			if err != nil || result.Status != want || writes != 1 || exchanges != 1 {
				t.Fatalf("replayed: %+v %v writes=%d exchanges=%d", result, err, writes, exchanges)
			}
			if status == http.StatusTooManyRequests && result.RetryAfter == 0 {
				t.Fatal("lost rate-limit timing")
			}
		})
	}
}
