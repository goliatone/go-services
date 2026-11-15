package githubprojects

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/tracker"
)

func TestProjectStatusWriteGuards(t *testing.T) {
	for _, scenario := range []string{"success", "schema", "permission", "grant", "revision", "project", "draft", "pull_request", "invalid_option", "unknown"} {
		t.Run(scenario, func(t *testing.T) {
			writes := 0
			permission := true
			project, kind, revision := "P1", "ISSUE", "2026-11-14T10:00:00Z"
			fieldName := "Done"
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Query     string
					Variables map[string]any
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					return
				}
				if strings.Contains(payload.Query, "viewerCanUpdate") {
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"node": map[string]any{"id": "P1", "viewerCanUpdate": permission, "field": map[string]any{"id": "F1", "name": "Status", "dataType": "SINGLE_SELECT", "options": []map[string]string{{"id": "O1", "name": fieldName}}}}}})
					return
				}
				item := map[string]any{"id": "I1", "type": kind, "updatedAt": revision, "project": map[string]string{"id": project}, "content": map[string]string{"id": "ISSUE1", "url": "https://github.com/acme/test/issues/1", "updatedAt": "2026-11-14T09:00:00Z"}, "fieldValueByName": map[string]any{"optionId": "O1", "field": map[string]string{"id": "F1"}}}
				if strings.HasPrefix(payload.Query, "mutation") {
					writes++
					input := payload.Variables["input"].(map[string]any)
					if input["fieldId"] != "F1" || input["projectId"] != "P1" || input["itemId"] != "I1" || input["value"].(map[string]any)["singleSelectOptionId"] != "O1" {
						t.Errorf("wrong mutation: %#v", input)
					}
					if scenario == "unknown" {
						_, _ = w.Write([]byte(`{"errors":[{"message":"uncertain"}]}`))
						return
					}
					item["updatedAt"] = "2026-11-14T10:01:00Z"
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"updateProjectV2ItemFieldValue": map[string]any{"projectV2Item": item}}})
				} else {
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"node": item}})
				}
			}))
			defer server.Close()
			credential := &discoveryCredentialResolver{value: core.ActiveCredential{AccessToken: "test", GrantedScopes: []string{"project"}, Metadata: map[string]any{"graphql_url": server.URL}}}
			provider := newDiscoveryProvider(t, tracker.Runtime{Credentials: credential, HTTPClient: server.Client()}).(*Provider)
			schema, err := provider.DiscoverTrackerProjectStatus(context.Background(), core.TrackerDiscoveryRequest{ConnectionID: "c1", ResourceID: "P1"})
			if err != nil {
				t.Fatal(err)
			}
			input := core.TrackerIssueUpdateRequest{ConnectionID: "c1", RepositoryID: "P1", IssueID: "I1", Field: "F1", Value: json.RawMessage(`"O1"`), ExpectedRevision: revision, ActorID: "actor", CorrelationID: "correlation", IdempotencyKey: "key"}
			wantCode := ""
			switch scenario {
			case "schema":
				fieldName = "Accepted"
				wantCode = core.TrackerErrorSchemaChanged
			case "permission":
				permission = false
				wantCode = core.TrackerErrorPermission
			case "grant":
				credential.value.GrantedScopes = []string{"read:project"}
				wantCode = core.TrackerErrorPermission
			case "revision":
				revision = "2026-11-14T11:00:00Z"
				wantCode = core.TrackerErrorConflict
			case "project":
				project = "P2"
				wantCode = core.TrackerErrorValidation
			case "draft":
				kind = "DRAFT_ISSUE"
				wantCode = core.TrackerErrorValidation
			case "pull_request":
				kind = "PULL_REQUEST"
				wantCode = core.TrackerErrorValidation
			case "invalid_option":
				input.Value = json.RawMessage(`"O2"`)
				wantCode = core.TrackerErrorValidation
			}
			receipt, err := provider.UpdateTrackerProjectStatus(context.Background(), input, schema.Revision)
			if wantCode != "" {
				var refused *core.TrackerProviderError
				if !errors.As(err, &refused) || refused.Code != wantCode || writes != 0 {
					t.Fatalf("writes=%d receipt=%+v err=%v", writes, receipt, err)
				}
				return
			}
			if err != nil || writes != 1 {
				t.Fatalf("writes=%d receipt=%+v err=%v", writes, receipt, err)
			}
			if scenario == "unknown" {
				if receipt.Status != "unknown" {
					t.Fatal(receipt)
				}
				return
			}
			if receipt.Validate() != nil || receipt.Status != "succeeded" || receipt.ProviderRevision != "2026-11-14T10:01:00Z" {
				t.Fatal(receipt)
			}
		})
	}
}
