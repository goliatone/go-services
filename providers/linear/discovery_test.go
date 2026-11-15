package linear

import (
	"context"
	"strings"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestLinearProjectAndSchemaPagination(t *testing.T) {
	var projectPages, schemaPages int
	provider, closeServer := linearMutationTestProvider(t, true, func(query string, variables map[string]any) string {
		switch {
		case strings.Contains(query, "projects(first:"):
			projectPages++
			if variables["after"] == nil {
				return `{"data":{"team":{"id":"team-1","projects":{"nodes":[{"id":"project-1","name":"One","updatedAt":"2026-09-23T10:00:00Z"}],"pageInfo":{"hasNextPage":true,"endCursor":"project-next"}}}}}`
			}
			if variables["after"] != "project-next" {
				t.Errorf("project cursor = %#v", variables["after"])
			}
			return `{"data":{"team":{"id":"team-1","projects":{"nodes":[{"id":"project-2","name":"Two","updatedAt":"2026-09-23T10:00:00Z"}],"pageInfo":{"hasNextPage":false}}}}}`
		case strings.Contains(query, "states(first:"):
			schemaPages++
			if variables["statesAfter"] == nil {
				return `{"data":{"team":{"id":"team-1","updatedAt":"2026-09-23T10:00:00Z","states":{"nodes":[{"id":"state-1","name":"Todo","type":"unstarted","updatedAt":"2026-09-23T10:00:00Z"}],"pageInfo":{"hasNextPage":true,"endCursor":"state-next"}},"labels":{"nodes":[{"id":"label-1","name":"Bug","updatedAt":"2026-09-23T10:00:00Z"}],"pageInfo":{"hasNextPage":false}}}}}`
			}
			if variables["statesAfter"] != "state-next" {
				t.Errorf("state cursor = %#v", variables["statesAfter"])
			}
			return `{"data":{"team":{"id":"team-1","updatedAt":"2026-09-23T10:00:00Z","states":{"nodes":[{"id":"state-2","name":"Done","type":"completed","updatedAt":"2026-09-23T10:00:00Z"}],"pageInfo":{"hasNextPage":false}},"labels":{"nodes":[{"id":"label-1","name":"Bug","updatedAt":"2026-09-23T10:00:00Z"}],"pageInfo":{"hasNextPage":false}}}}}`
		default:
			t.Errorf("unexpected query %s", query)
			return `{}`
		}
	})
	defer closeServer()
	first, err := provider.DiscoverTrackerScopes(context.Background(), core.TrackerDiscoveryRequest{ConnectionID: "connection-1", ResourceType: "project", ResourceID: "team-1"})
	if err != nil || !first.HasMore || first.NextCursor != "project-next" || len(first.Items) != 1 {
		t.Fatalf("first project page = %#v, err = %v", first, err)
	}
	second, err := provider.DiscoverTrackerScopes(context.Background(), core.TrackerDiscoveryRequest{ConnectionID: "connection-1", ResourceType: "project", ResourceID: "team-1", Cursor: first.NextCursor})
	if err != nil || second.HasMore || len(second.Items) != 1 || second.Items[0].ExternalID != "project-2" {
		t.Fatalf("second project page = %#v, err = %v", second, err)
	}
	schema, err := provider.DiscoverTrackerSchema(context.Background(), core.TrackerDiscoveryRequest{ConnectionID: "connection-1", ResourceID: "team-1"})
	if err != nil {
		t.Fatal(err)
	}
	if projectPages != 4 || schemaPages != 2 || len(schema.Items) != 6 || len(schema.Items[2].Options) != 2 || len(schema.Items[3].Options) != 1 || len(schema.Items[5].Options) != 2 {
		t.Fatalf("pagination: project=%d schema=%d fields=%#v", projectPages, schemaPages, schema.Items)
	}
}

func TestLinearIssueFilterUsesIDAndPreservesCursor(t *testing.T) {
	calls := 0
	provider, closeServer := linearMutationTestProvider(t, false, func(query string, variables map[string]any) string {
		calls++
		if !strings.Contains(query, "query($team:ID!") || !strings.Contains(query, "issues(filter:{team:{id:{eq:$team}}}") || variables["team"] != "team-1" || variables["first"] != float64(10) {
			t.Errorf("invalid issue filter: %s %#v", query, variables)
			return `{"errors":[{"message":"team filter requires ID"}]}`
		}
		if calls == 1 {
			if variables["after"] != nil {
				t.Errorf("first cursor = %#v", variables["after"])
			}
			return `{"data":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"opaque-next"}}}}`
		}
		if calls == 3 {
			if variables["after"] != nil {
				t.Errorf("new scan resumed terminal cursor: %#v", variables["after"])
			}
			return `{"data":{"issues":{"nodes":[{"id":"new-issue","identifier":"TEAM-2","title":"New before old tail","updatedAt":"2026-11-15T22:00:00Z"},{"id":"updated-issue","identifier":"TEAM-1","title":"Updated before old tail","updatedAt":"2026-11-15T21:00:00Z"}],"pageInfo":{"hasNextPage":false,"endCursor":"new-tail"}}}}`
		}
		if variables["after"] != "opaque-next" {
			t.Errorf("next cursor = %#v", variables["after"])
		}
		return `{"data":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":"terminal-cursor"}}}}`
	})
	defer closeServer()
	request := core.TrackerChangesRequest{ConnectionID: "connection-1", ResourceType: "issue", ResourceID: "team-1", Limit: 10}
	first, err := provider.ListTrackerChanges(context.Background(), request)
	if err != nil || !first.HasMore || first.NextCursor != "opaque-next" {
		t.Fatalf("first = %#v, err = %v", first, err)
	}
	request.Cursor = first.NextCursor
	second, err := provider.ListTrackerChanges(context.Background(), request)
	if err != nil || second.HasMore || second.NextCursor != "" || calls != 2 {
		t.Fatalf("second = %#v, calls = %d, err = %v", second, calls, err)
	}
	request.Cursor = second.NextCursor
	third, err := provider.ListTrackerChanges(context.Background(), request)
	if err != nil || third.HasMore || third.NextCursor != "" || len(third.Items) != 2 || calls != 3 {
		t.Fatalf("new scan = %#v, calls = %d, err = %v", third, calls, err)
	}
}
