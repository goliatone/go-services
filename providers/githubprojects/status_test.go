package githubprojects

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/tracker"
)

func TestProjectStatusWriteGuards(t *testing.T) {
	for _, scenario := range []string{"success", "schema", "permission", "grant", "revision", "project", "draft", "pull_request", "invalid_option", "unknown", "invalid_url", "auth_before_dispatch", "cancel_before_dispatch", "legacy_revision", "response_wrong_url", "response_old_revision", "mutation_forbidden", "mutation_rate_limit", "transport_unknown"} {
		t.Run(scenario, func(t *testing.T) {
			testCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
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
				if scenario == "invalid_url" {
					item["content"].(map[string]string)["url"] = "https://"
				}
				if strings.HasPrefix(payload.Query, "mutation") {
					if scenario == "mutation_forbidden" {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					if scenario == "mutation_rate_limit" {
						w.Header().Set("Retry-After", "2")
						w.WriteHeader(http.StatusTooManyRequests)
						return
					}
					writes++
					if scenario == "transport_unknown" {
						w.WriteHeader(http.StatusBadGateway)
						return
					}
					input := payload.Variables["input"].(map[string]any)
					if input["fieldId"] != "F1" || input["projectId"] != "P1" || input["itemId"] != "I1" || input["value"].(map[string]any)["singleSelectOptionId"] != "O1" {
						t.Errorf("wrong mutation: %#v", input)
					}
					if scenario == "unknown" {
						_, _ = w.Write([]byte(`{"errors":[{"message":"uncertain"}]}`))
						return
					}
					item["updatedAt"] = "2026-11-14T10:01:00Z"
					if scenario == "response_wrong_url" {
						item["content"].(map[string]string)["url"] = "https://github.com/acme/test/issues/99"
					}
					if scenario == "response_old_revision" {
						item["updatedAt"] = "2026-11-14T09:00:00Z"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"updateProjectV2ItemFieldValue": map[string]any{"projectV2Item": item}}})
				} else {
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"node": item}})
				}
			}))
			defer server.Close()
			credential := &discoveryCredentialResolver{value: core.ActiveCredential{AccessToken: "test", GrantedScopes: []string{"project"}, Metadata: map[string]any{"graphql_url": server.URL}}}
			runtime := tracker.Runtime{Credentials: credential, HTTPClient: server.Client()}
			if scenario == "auth_before_dispatch" || scenario == "cancel_before_dispatch" {
				runtime.Authenticate = func(ctx context.Context, req *http.Request, c core.ActiveCredential) error {
					raw, _ := req.GetBody()
					defer raw.Close()
					body, _ := io.ReadAll(raw)
					if strings.Contains(string(body), "mutation(") {
						if scenario == "cancel_before_dispatch" {
							cancel()
						} else {
							return errors.New("test auth failure")
						}
					}
					return tracker.BearerAuthenticator(ctx, req, c)
				}
			}
			provider := newDiscoveryProvider(t, runtime).(*Provider)
			schema, err := provider.DiscoverTrackerProjectStatus(context.Background(), core.TrackerDiscoveryRequest{ConnectionID: "c1", ResourceID: "P1"})
			if err != nil {
				t.Fatal(err)
			}
			input := core.TrackerIssueUpdateRequest{ConnectionID: "c1", RepositoryID: "P1", IssueID: "I1", Field: "F1", Value: json.RawMessage(`"O1"`), ExpectedRevision: projectItemRevision("I1", time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC), "ISSUE1", time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)), ActorID: "actor", CorrelationID: "correlation", IdempotencyKey: "key"}
			wantCode := ""
			switch scenario {
			case "mutation_forbidden":
				wantCode = core.TrackerErrorPermission
			case "mutation_rate_limit":
				wantCode = core.TrackerErrorRateLimited
			case "legacy_revision":
				input.ExpectedRevision = revision
				wantCode = core.TrackerErrorConflict
			case "cancel_before_dispatch":
				wantCode = core.TrackerErrorUnavailable
			case "invalid_url":
				wantCode = core.TrackerErrorValidation
			case "auth_before_dispatch":
				wantCode = core.TrackerErrorCredentialRevoked
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
			beforeResolves := credential.calls.Load()
			receipt, err := provider.UpdateTrackerProjectStatus(testCtx, input, schema.Revision)
			if credential.calls.Load() != beforeResolves+1 {
				t.Errorf("write resolved credentials %d times", credential.calls.Load()-beforeResolves)
			}
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
			if scenario == "unknown" || scenario == "response_wrong_url" || scenario == "response_old_revision" || scenario == "transport_unknown" {
				if receipt.Status != "unknown" || receipt.Validate() != nil {
					t.Fatal(receipt)
				}
				return
			}
			if receipt.Validate() != nil || receipt.Status != "succeeded" || receipt.ProviderRevision != projectItemRevision("I1", time.Date(2026, 11, 14, 10, 1, 0, 0, time.UTC), "ISSUE1", time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)) {
				t.Fatal(receipt)
			}
		})
	}
}

func TestProjectStatusRevisionDoesNotHideOlderTimestampChanges(t *testing.T) {
	item := statusItem{ID: "item", UpdatedAt: time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)}
	item.Content.ID = "issue"
	item.Content.UpdatedAt = item.UpdatedAt.Add(time.Hour)
	before := item.revision()
	item.UpdatedAt = item.UpdatedAt.Add(time.Minute)
	if item.revision() == before {
		t.Fatal("newer issue timestamp hid Project item change")
	}
	item.UpdatedAt = item.Content.UpdatedAt.Add(time.Hour)
	before = item.revision()
	item.Content.UpdatedAt = item.Content.UpdatedAt.Add(time.Minute)
	if item.revision() == before {
		t.Fatal("newer item timestamp hid issue change")
	}
}
