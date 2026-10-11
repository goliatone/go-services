package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/goliatone/go-services/core"
	trackerruntime "github.com/goliatone/go-services/providers/tracker"
)

type linearCredentialResolver func(context.Context, string) (core.ActiveCredential, error)

func (r linearCredentialResolver) ResolveTrackerCredential(ctx context.Context, id string) (core.ActiveCredential, error) {
	return r(ctx, id)
}

func linearMutationTestProvider(t *testing.T, granted bool, handle func(string, map[string]any) string) (*Provider, func()) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "test-key" {
			t.Errorf("authorization header was not set")
		}
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(handle(body.Query, body.Variables)))
	}))
	grants := []string(nil)
	if granted {
		grants = []string{"write"}
	}
	provider, err := New(Config{TrackerRuntime: trackerruntime.Runtime{HTTPClient: server.Client(), Credentials: linearCredentialResolver(func(_ context.Context, id string) (core.ActiveCredential, error) {
		return core.ActiveCredential{ConnectionID: id, AccessToken: "test-key", GrantedScopes: grants, Metadata: map[string]any{"graphql_url": server.URL}}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	return provider.(*Provider), server.Close
}

func TestLinearIssueCreateAndUpdateUseTeamIdentityAndRevision(t *testing.T) {
	var mutations atomic.Int32
	provider, closeServer := linearMutationTestProvider(t, true, func(query string, variables map[string]any) string {
		switch {
		case strings.Contains(query, "viewer{id}"):
			return `{"data":{"viewer":{"id":"user-1"},"team":{"id":"team-1"}}}`
		case strings.Contains(query, "states(first:"):
			return `{"data":{"team":{"id":"team-1","updatedAt":"2026-09-23T10:00:00Z","states":{"nodes":[{"id":"state-1","name":"Todo","updatedAt":"2026-09-23T10:00:00Z"}],"pageInfo":{"hasNextPage":false}},"labels":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`
		case strings.Contains(query, "projects(first:"):
			return `{"data":{"team":{"id":"team-1","projects":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`
		case strings.Contains(query, "issueCreate"):
			mutations.Add(1)
			input := variables["input"].(map[string]any)
			if input["teamId"] != "team-1" || input["title"] != "A fix" || input["stateId"] != "state-1" {
				t.Errorf("create input = %#v", input)
			}
			return `{"data":{"issueCreate":{"success":true,"issue":{"id":"issue-1","identifier":"TEAM-42","url":"https://linear.app/team/issue/TEAM-42","updatedAt":"2026-09-23T10:00:00Z","team":{"id":"team-1"}}}}}`
		case strings.Contains(query, "issue(id:"):
			return `{"data":{"issue":{"id":"issue-1","identifier":"TEAM-42","url":"https://linear.app/team/issue/TEAM-42","updatedAt":"2026-09-23T10:00:00Z","team":{"id":"team-1"}}}}`
		case strings.Contains(query, "issueUpdate"):
			mutations.Add(1)
			input := variables["input"].(map[string]any)
			if variables["id"] != "issue-1" || input["stateId"] != "state-2" {
				t.Errorf("update variables = %#v", variables)
			}
			return `{"data":{"issueUpdate":{"success":true,"issue":{"id":"issue-1","identifier":"TEAM-42","url":"https://linear.app/team/issue/TEAM-42","updatedAt":"2026-09-23T10:01:00Z","team":{"id":"team-1"}}}}}`
		default:
			t.Errorf("unexpected query %s", query)
			return `{}`
		}
	})
	defer closeServer()
	created, err := provider.CreateTrackerIssue(context.Background(), core.TrackerIssueCreateRequest{ConnectionID: "connection-1", RepositoryID: "team-1", Fields: map[string]json.RawMessage{"title": json.RawMessage(`"A fix"`), "state": json.RawMessage(`"state-1"`)}, IdempotencyKey: "create-1", ActorID: "owner", CorrelationID: "corr-1"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ExternalID != "issue-1" || created.DisplayID != "TEAM-42" || created.ProviderRevision != "2026-09-23T10:00:00Z" || mutations.Load() != 1 {
		t.Fatalf("create receipt = %#v, mutations = %d", created, mutations.Load())
	}
	_, err = provider.UpdateTrackerIssue(context.Background(), core.TrackerIssueUpdateRequest{ConnectionID: "connection-1", RepositoryID: "team-1", IssueID: "issue-1", Field: "state", Value: json.RawMessage(`"state-2"`), ExpectedRevision: created.ProviderRevision, IdempotencyKey: "update-1", ActorID: "owner", CorrelationID: "corr-2"})
	if err == nil || mutations.Load() != 1 {
		t.Fatalf("unguarded update was sent: err=%v mutations=%d", err, mutations.Load())
	}
	_, err = provider.UpdateTrackerIssue(context.Background(), core.TrackerIssueUpdateRequest{ConnectionID: "connection-1", RepositoryID: "team-1", IssueID: "issue-1", Field: "state", Value: json.RawMessage(`"state-2"`), ExpectedRevision: "stale", IdempotencyKey: "update-2", ActorID: "owner", CorrelationID: "corr-3"})
	if err == nil || mutations.Load() != 1 {
		t.Fatalf("stale update was sent: err=%v mutations=%d", err, mutations.Load())
	}
}

func TestLinearWriteRequiresProvenGrant(t *testing.T) {
	var calls atomic.Int32
	provider, closeServer := linearMutationTestProvider(t, false, func(_ string, _ map[string]any) string {
		calls.Add(1)
		return `{"data":{"viewer":{"id":"user-1"},"team":{"id":"team-1"}}}`
	})
	defer closeServer()
	caps, err := provider.DiscoverTrackerFieldCapabilities(context.Background(), core.TrackerFieldCapabilityRequest{ConnectionID: "connection-1", RepositoryID: "team-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, cap := range caps {
		if cap.Operation != "read" && cap.Granted {
			t.Fatalf("unproven write capability: %#v", cap)
		}
	}
	_, err = provider.CreateTrackerIssue(context.Background(), core.TrackerIssueCreateRequest{ConnectionID: "connection-1", RepositoryID: "team-1", Fields: map[string]json.RawMessage{"title": json.RawMessage(`"A fix"`)}, IdempotencyKey: "create-1", ActorID: "owner", CorrelationID: "corr-1"})
	if err == nil || calls.Load() != 1 {
		t.Fatalf("write grant denial failed: err=%v calls=%d", err, calls.Load())
	}
}

func TestLinearCreateRejectsProjectOutsideSelectedTeam(t *testing.T) {
	var creates atomic.Int32
	provider, closeServer := linearMutationTestProvider(t, true, func(query string, _ map[string]any) string {
		switch {
		case strings.Contains(query, "viewer{id}"):
			return `{"data":{"viewer":{"id":"user-1"},"team":{"id":"team-1"}}}`
		case strings.Contains(query, "states(first:"):
			return `{"data":{"team":{"id":"team-1","updatedAt":"2026-09-23T10:00:00Z","states":{"nodes":[],"pageInfo":{"hasNextPage":false}},"labels":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`
		case strings.Contains(query, "projects(first:"):
			return `{"data":{"team":{"id":"team-1","projects":{"nodes":[{"id":"project-1","name":"Delivery","updatedAt":"2026-09-23T10:00:00Z"}],"pageInfo":{"hasNextPage":false}}}}}`
		case strings.Contains(query, "issueCreate"):
			creates.Add(1)
			return `{}`
		default:
			return `{}`
		}
	})
	defer closeServer()
	_, err := provider.CreateTrackerIssue(context.Background(), core.TrackerIssueCreateRequest{ConnectionID: "connection-1", RepositoryID: "team-1", Fields: map[string]json.RawMessage{"title": json.RawMessage(`"Fix"`), "project": json.RawMessage(`"project-2"`)}, IdempotencyKey: "create-1", ActorID: "owner", CorrelationID: "corr-1"})
	if err == nil || creates.Load() != 0 {
		t.Fatalf("cross-team project was written: err=%v creates=%d", err, creates.Load())
	}
}

func TestLinearMutationRejectsUnsupportedFields(t *testing.T) {
	for _, fields := range []map[string]json.RawMessage{
		{"title": json.RawMessage(`""`)},
		{"assignees": json.RawMessage(`["one","two"]`)},
		{"priority": json.RawMessage(`2`)},
	} {
		if _, err := linearIssueInput(fields); err == nil {
			t.Fatal(fmt.Sprintf("accepted unsupported fields: %#v", fields))
		}
	}
}
