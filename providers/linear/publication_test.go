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

const publicationScopeJSON = `"organization":{"id":"account"},"team":{"id":"team","organization":{"id":"account"},"autoCloseParentIssues":false,"autoCloseChildIssues":null,"states":{"nodes":[{"id":"todo","name":"Todo","type":"unstarted"},{"id":"done","name":"Done","type":"completed"}],"pageInfo":{"hasNextPage":false}}},"project":{"id":"project","url":"https://linear.app/project/p","teams":{"nodes":[{"id":"team"}],"pageInfo":{"hasNextPage":false}}}`

func TestPublicationJourneyAndBoundaries(t *testing.T) {
	ctx := context.Background()
	scope := core.TrackerPublicationScope{ConnectionID: "connection", AccountID: "account", TeamID: "team", ProjectID: "project"}
	execution := core.TrackerPublicationExecution{ActorID: "actor", IdempotencyKey: "intent", CorrelationID: "corr"}
	milestoneID := "907073da-962e-4fee-9767-297276f0d9e5"
	specID := "9dc316cd-5241-423d-9f52-fc1efc634782"
	childID := "3a0db2bd-a1c2-43e4-a62a-f3d83602f857"
	issues := map[string]map[string]any{}
	milestones := map[string]map[string]any{}
	mutations := 0
	lost, readFailure, wrongScope, badHuman, incomplete, postDrift := false, false, false, false, false, false
	assignmentOverride := ""
	schemaUnavailable := false
	marshal := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	provider, closeServer := linearMutationTestProvider(t, true, func(query string, variables map[string]any) string {
		switch {
		case strings.Contains(query, "organization{id}team"):
			if schemaUnavailable && strings.Contains(query, "states(first:") {
				return `{"errors":[{"message":"state discovery unavailable"}]}`
			}
			data := `{"data":{` + publicationScopeJSON + `}}`
			if wrongScope {
				data = strings.ReplaceAll(data, `"account"`, `"foreign"`)
			}
			return data
		case strings.Contains(query, "members(first:"):
			app := "false"
			if badHuman {
				app = "true"
			}
			return `{"data":{"team":{"id":"team","members":{"nodes":[{"id":"human","name":"Owner","app":` + app + `,"active":true,"isAssignable":true,"organization":{"id":"account"}}],"pageInfo":{"hasNextPage":false}}}}}`
		case strings.Contains(query, "projectMilestoneCreate"):
			mutations++
			input := variables["input"].(map[string]any)
			id := input["id"].(string)
			if _, exists := milestones[id]; exists {
				return `{"errors":[{"message":"duplicate ID"}]}`
			}
			milestones[id] = map[string]any{"id": id, "name": input["name"], "description": input["description"], "updatedAt": "2026-11-14T00:00:00Z", "project": map[string]any{"id": "project", "url": "https://linear.app/project/p"}}
			if lost {
				return `{"errors":[{"message":"uncertain response"}]}`
			}
			return marshal(map[string]any{"data": map[string]any{"projectMilestoneCreate": map[string]any{"success": true, "projectMilestone": milestones[id]}}})
		case strings.Contains(query, "projectMilestones(first:"):
			if incomplete {
				return `{"data":{"project":{"id":"project","projectMilestones":{"nodes":[]}}}}`
			}
			nodes := []any{}
			if variables["after"] == nil {
				for _, m := range milestones {
					nodes = append(nodes, m)
				}
			}
			return marshal(map[string]any{"data": map[string]any{"project": map[string]any{"id": "project", "projectMilestones": map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": variables["after"] == nil, "endCursor": "page-2"}}}}})
		case strings.Contains(query, "projectMilestone(id:"):
			return marshal(map[string]any{"data": map[string]any{"projectMilestone": milestones[variables["id"].(string)]}})
		case strings.Contains(query, "issueCreate"):
			mutations++
			input := variables["input"].(map[string]any)
			id := input["id"].(string)
			if _, exists := issues[id]; exists {
				return `{"errors":[{"message":"duplicate ID"}]}`
			}
			if input["assigneeId"] != "human" || input["projectMilestoneId"] != milestoneID || input["projectId"] != "project" || input["teamId"] != "team" {
				t.Errorf("create escaped intent: %#v", input)
			}
			assignee := any(map[string]any{"id": input["assigneeId"]})
			if assignmentOverride != "" {
				assignee = map[string]any{"id": assignmentOverride}
			}
			issues[id] = map[string]any{"id": id, "identifier": "CTX-1", "url": "https://linear.app/issue/CTX-1", "title": input["title"], "description": input["description"], "state": map[string]any{"id": input["stateId"]}, "updatedAt": "2026-11-14T00:00:00Z", "team": map[string]any{"id": "team"}, "project": map[string]any{"id": "project"}, "projectMilestone": map[string]any{"id": milestoneID}, "assignee": assignee, "parent": nil, "children": map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": false}}}
			if parent, ok := input["parentId"]; ok {
				issues[id]["parent"] = map[string]any{"id": parent}
				issues[parent.(string)]["children"] = map[string]any{"nodes": []any{map[string]any{"id": id}}, "pageInfo": map[string]any{"hasNextPage": false}}
			}
			if lost {
				return `{"errors":[{"message":"uncertain response"}]}`
			}
			return marshal(map[string]any{"data": map[string]any{"issueCreate": map[string]any{"success": true, "issue": map[string]any{"id": id}}}})
		case strings.Contains(query, "issueUpdate"):
			mutations++
			input := variables["input"].(map[string]any)
			id := variables["id"].(string)
			for key, value := range input {
				switch key {
				case "title", "description":
					issues[id][key] = value
				case "stateId":
					issues[id]["state"] = map[string]any{"id": value}
				default:
					t.Errorf("PM field in managed patch: %s", key)
				}
			}
			if postDrift {
				issues[id]["title"] = "Concurrent edit"
			}
			if lost {
				return `{"errors":[{"message":"uncertain response"}]}`
			}
			return marshal(map[string]any{"data": map[string]any{"issueUpdate": map[string]any{"success": true, "issue": map[string]any{"id": id}}}})
		case strings.Contains(query, "issue(id:"):
			if readFailure {
				return `{"errors":[{"message":"unavailable"}]}`
			}
			return marshal(map[string]any{"data": map[string]any{"issue": issues[variables["id"].(string)]}})
		default:
			t.Errorf("unexpected query: %s", query)
			return `{}`
		}
	})
	defer closeServer()
	destination, err := provider.InspectTrackerPublication(ctx, core.TrackerPublicationInspectRequest{Scope: scope, AssigneeID: "human"})
	if err != nil || destination.AssigneeID != "human" || destination.AutoCloseParentIssues == nil || *destination.AutoCloseParentIssues || destination.AutoCloseChildIssues != nil {
		t.Fatalf("inspect: %+v %v", destination, err)
	}
	for _, state := range destination.States {
		if err := state.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	mreq := core.TrackerPublicationMilestoneCreateRequest{Scope: scope, Execution: execution, RequestedID: milestoneID, Name: "Release", Description: "Outcome"}
	result, err := provider.CreateTrackerPublicationMilestone(ctx, mreq)
	if err != nil || result.Status != "confirmed" {
		t.Fatalf("milestone: %+v %v", result, err)
	}
	page, err := provider.ListTrackerPublicationMilestones(ctx, core.TrackerPublicationListRequest{Scope: scope, Limit: 1})
	if err != nil || !page.HasMore || len(page.Items) != 1 {
		t.Fatalf("first page: %+v %v", page, err)
	}
	page, err = provider.ListTrackerPublicationMilestones(ctx, core.TrackerPublicationListRequest{Scope: scope, Cursor: page.NextCursor, Limit: 1})
	if err != nil || page.HasMore || len(page.Items) != 0 {
		t.Fatalf("last page: %+v %v", page, err)
	}
	incomplete = true
	if _, err = provider.ListTrackerPublicationMilestones(ctx, core.TrackerPublicationListRequest{Scope: scope}); err == nil {
		t.Fatal("incomplete page reported absence")
	}
	incomplete = false
	create := core.TrackerPublicationIssueCreateRequest{Scope: scope, Execution: execution, RequestedID: specID, MilestoneID: milestoneID, AssigneeID: "human", Fields: core.TrackerPublicationFields{Title: "Spec", Description: "Initial", StateID: "todo"}}
	for _, boundary := range []string{"scope", "human", "milestone"} {
		before := mutations
		wrongScope = boundary == "scope"
		badHuman = boundary == "human"
		invalid := create
		if boundary == "milestone" {
			invalid.MilestoneID = "foreign"
		}
		if _, err = provider.CreateTrackerPublicationIssue(ctx, invalid); err == nil || mutations != before {
			t.Fatalf("%s boundary permitted write", boundary)
		}
	}
	wrongScope = false
	badHuman = false
	result, err = provider.CreateTrackerPublicationIssue(ctx, create)
	if err != nil || result.Status != "confirmed" || result.AssignmentOverridden {
		t.Fatalf("spec: %+v %v", result, err)
	}
	issues[specID]["assignee"] = map[string]any{"id": "pm-selected"}
	child := create
	child.RequestedID = childID
	child.ParentID = specID
	child.Fields.Title = "Task"
	lost = true
	before := mutations
	result, err = provider.CreateTrackerPublicationIssue(ctx, child)
	if err != nil || result.Status != "unknown" || mutations != before+1 {
		t.Fatalf("lost child: %+v %v", result, err)
	}
	lost = false
	observed, err := provider.ReadTrackerPublicationIssue(ctx, core.TrackerPublicationReadRequest{Scope: scope, ID: childID})
	if err != nil || observed.ParentID != specID || observed.AssigneeID != "human" {
		t.Fatalf("exact recovery: %+v %v", observed, err)
	}
	before = mutations
	result, err = provider.CreateTrackerPublicationIssue(ctx, child)
	if err != nil || result.Status != "unknown" || mutations != before+1 || len(issues) != 2 {
		t.Fatalf("duplicate identity caused retry: %+v %v", result, err)
	}
	observed, err = provider.ReadTrackerPublicationIssue(ctx, core.TrackerPublicationReadRequest{Scope: scope, ID: specID})
	if err != nil || !observed.HasChildren {
		t.Fatalf("remote child missing: %+v %v", observed, err)
	}
	done := "done"
	childUpdate := core.TrackerPublicationIssueUpdateRequest{Target: core.TrackerPublicationReadRequest{Scope: scope, ID: childID}, Execution: execution, ParentID: specID, MilestoneID: milestoneID, Expected: child.Fields, Patch: core.TrackerPublicationPatch{StateID: &done}}
	result, err = provider.UpdateTrackerPublicationIssue(ctx, childUpdate)
	if err != nil || result.Status != "confirmed" || result.Issue.Fields.StateID != "done" || result.Issue.AssigneeID != "human" {
		t.Fatalf("managed native status: %+v %v", result, err)
	}
	schemaUnavailable = true
	if _, err = provider.ReadTrackerPublicationIssue(ctx, core.TrackerPublicationReadRequest{Scope: scope, ID: childID}); err != nil {
		t.Fatalf("exact issue read depends on state discovery: %v", err)
	}
	if _, err = provider.ReadTrackerPublicationMilestone(ctx, core.TrackerPublicationReadRequest{Scope: scope, ID: milestoneID}); err != nil {
		t.Fatalf("exact milestone read depends on state discovery: %v", err)
	}
	if _, err = provider.InspectTrackerPublication(ctx, core.TrackerPublicationInspectRequest{Scope: scope}); err == nil {
		t.Fatal("setup silently accepted unavailable state discovery")
	}
	issues[specID]["assignee"] = nil
	issues[specID]["priority"] = 1
	issues[specID]["dueDate"] = "2026-12-01"
	description := "Progress"
	update := core.TrackerPublicationIssueUpdateRequest{Target: core.TrackerPublicationReadRequest{Scope: scope, ID: specID}, Execution: execution, MilestoneID: milestoneID, Expected: create.Fields, Patch: core.TrackerPublicationPatch{Description: &description}}

	for _, field := range []string{"description", "parent", "assignee"} {
		original := issues[specID][field]
		delete(issues[specID], field)
		before := mutations
		if _, err = provider.UpdateTrackerPublicationIssue(ctx, update); err == nil || mutations != before {
			t.Fatalf("missing %s observation permitted mutation: %v", field, err)
		}
		issues[specID][field] = original
	}
	originalDescription := issues[specID]["description"]
	issues[specID]["description"] = nil
	nullObservation, err := provider.ReadTrackerPublicationIssue(ctx, update.Target)
	if err != nil || nullObservation.Fields.Description != "" || nullObservation.ParentID != "" || nullObservation.AssigneeID != "" {
		t.Fatalf("valid null fields rejected: %+v %v", nullObservation, err)
	}
	issues[specID]["description"] = originalDescription
	result, err = provider.UpdateTrackerPublicationIssue(ctx, update)
	if err != nil || result.Status != "confirmed" || issues[specID]["assignee"] != nil || issues[specID]["priority"] != 1 || issues[specID]["dueDate"] != "2026-12-01" {
		t.Fatalf("managed refresh: %+v %v", result, err)
	}
	before = mutations
	result, err = provider.UpdateTrackerPublicationIssue(ctx, update)
	if err != nil || result.Status != "desired_state_observed" || mutations != before {
		t.Fatalf("repeat convergence: %+v %v", result, err)
	}
	update.Expected = result.Issue.Fields
	description = "More progress"
	lost = true
	before = mutations
	result, err = provider.UpdateTrackerPublicationIssue(ctx, update)
	if err != nil || result.Status != "unknown" || mutations != before+1 {
		t.Fatalf("lost update: %+v %v", result, err)
	}
	lost = false
	before = mutations
	result, err = provider.UpdateTrackerPublicationIssue(ctx, update)
	if err != nil || result.Status != "desired_state_observed" || mutations != before {
		t.Fatalf("lost update convergence: %+v %v", result, err)
	}
	update.Expected = result.Issue.Fields
	description = "Next progress"
	issues[specID]["title"] = "PM edit"
	before = mutations
	result, err = provider.UpdateTrackerPublicationIssue(ctx, update)
	if err != nil || result.Status != "conflict" || mutations != before {
		t.Fatalf("drift: %+v %v", result, err)
	}
	issues[specID]["title"] = update.Expected.Title
	postDrift = true
	result, err = provider.UpdateTrackerPublicationIssue(ctx, update)
	if err != nil || result.Status != "conflict" {
		t.Fatalf("post-write drift: %+v %v", result, err)
	}
	postDrift = false
	readFailure = true
	if _, err = provider.ReadTrackerPublicationIssue(ctx, core.TrackerPublicationReadRequest{Scope: scope, ID: childID}); err == nil {
		t.Fatal("unavailable read became success")
	}
	readFailure = false
	if _, err = provider.ReadTrackerPublicationIssue(ctx, core.TrackerPublicationReadRequest{Scope: scope, ID: "absent"}); err == nil {
		t.Fatal("missing exact ID became success")
	}
	schemaUnavailable = false
	assignmentOverride = "native-rule"
	create.RequestedID = "8f4d4f10-344f-46a6-ae3e-91075e12e289"
	result, err = provider.CreateTrackerPublicationIssue(ctx, create)
	if err != nil || result.Status != "confirmed" || !result.AssignmentOverridden || result.Issue.AssigneeID != "native-rule" {
		t.Fatalf("native assignment override: %+v %v", result, err)
	}
}

func TestPublicationMutationDoesNotRefreshOrReplay(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusTemporaryRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			exchanges, mutations := 0, 0
			client := &http.Client{Transport: linearRoundTrip(func(request *http.Request) (*http.Response, error) {
				code, body := http.StatusOK, ""
				if request.URL.String() == linearOAuthTokenURL {
					exchanges++
					body = `{"access_token":"token","token_type":"Bearer","scope":"read write","expires_in":3600}`
				} else {
					data, _ := io.ReadAll(request.Body)
					if strings.Contains(string(data), "mutation(") {
						mutations++
						code = status
						body = `unconfirmed`
					} else {
						body = `{"data":{` + publicationScopeJSON + `}}`
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
			input := core.TrackerPublicationMilestoneCreateRequest{Scope: core.TrackerPublicationScope{ConnectionID: "connection", AccountID: "account", TeamID: "team", ProjectID: "project"}, Execution: core.TrackerPublicationExecution{ActorID: "actor", IdempotencyKey: "intent", CorrelationID: "corr"}, RequestedID: "907073da-962e-4fee-9767-297276f0d9e5", Name: "Release"}
			result, err := value.(*Provider).CreateTrackerPublicationMilestone(context.Background(), input)
			expectedStatus := "failed"
			if status == http.StatusBadGateway || status == http.StatusTemporaryRedirect {
				expectedStatus = "unknown"
			}
			if err != nil || result.Status != expectedStatus || exchanges != 1 || mutations != 1 {
				t.Fatalf("mutation replayed: result=%+v err=%v exchanges=%d mutations=%d", result, err, exchanges, mutations)
			}
			if status == http.StatusTooManyRequests && result.RetryAfter == 0 {
				t.Fatal("lost retry timing")
			}
		})
	}
}

func TestPublicationWriteGrantRefusesBeforeMutation(t *testing.T) {
	mutations := 0
	provider, closeServer := linearMutationTestProvider(t, false, func(query string, _ map[string]any) string {
		if strings.Contains(query, "mutation(") {
			mutations++
		}
		return `{"data":{` + publicationScopeJSON + `}}`
	})
	defer closeServer()
	input := core.TrackerPublicationMilestoneCreateRequest{Scope: core.TrackerPublicationScope{ConnectionID: "connection", AccountID: "account", TeamID: "team", ProjectID: "project"}, Execution: core.TrackerPublicationExecution{ActorID: "actor", IdempotencyKey: "intent", CorrelationID: "corr"}, RequestedID: "907073da-962e-4fee-9767-297276f0d9e5", Name: "Release"}
	if _, err := provider.CreateTrackerPublicationMilestone(context.Background(), input); err == nil || mutations != 0 {
		t.Fatalf("missing write grant permitted dispatch: %v", err)
	}
}

func TestPublicationCanceledBeforeDispatchIsFailed(t *testing.T) {
	calls := 0
	provider, closeServer := linearMutationTestProvider(t, true, func(string, map[string]any) string { calls++; return `{}` })
	defer closeServer()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output map[string]any
	result := provider.publicationMutation(ctx, core.ActiveCredential{AccessToken: "test-key"}, "https://api.linear.app/graphql", "mutation{noop}", nil, &output)
	if result == nil || result.Status != "failed" || calls != 0 {
		t.Fatalf("known non-dispatch became uncertain: %+v calls=%d", result, calls)
	}
}
