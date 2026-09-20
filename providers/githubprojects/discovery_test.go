package githubprojects

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/tracker"
)

type discoveryCredentialResolver struct {
	calls atomic.Int32
	value core.ActiveCredential
}

func (r *discoveryCredentialResolver) ResolveTrackerCredential(context.Context, string) (core.ActiveCredential, error) {
	r.calls.Add(1)
	return r.value, nil
}

func TestDiscoverTrackerScopesEnumeratesViewerAndOrganizationProjects(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		body, _ := io.ReadAll(request.Body)
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(payload.Query, "minPermissionLevel:READ") || !strings.Contains(payload.Query, "field:NUMBER,direction:ASC") {
			t.Fatalf("query does not use stable readable Project ordering: %s", payload.Query)
		}
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(payload.Query, "viewer{id login projectsV2"):
			after, _ := payload.Variables["after"].(string)
			if after == "" {
				_, _ = writer.Write([]byte(`{"data":{"viewer":{"id":"U_1","login":"alice","projectsV2":{"nodes":[{"id":"P_USER_1","number":1,"title":"Personal one","url":"https://github.com/users/alice/projects/1","updatedAt":"2026-09-20T10:00:00Z","owner":{"__typename":"User","id":"U_1","login":"alice"}}],"pageInfo":{"hasNextPage":true,"endCursor":"USER_1"}}}}}`))
				return
			}
			if after != "USER_1" {
				t.Fatalf("unexpected viewer cursor %q", after)
			}
			_, _ = writer.Write([]byte(`{"data":{"viewer":{"id":"U_1","login":"alice","projectsV2":{"nodes":[{"id":"P_USER_2","number":2,"title":"Personal two","url":"https://github.com/users/alice/projects/2","updatedAt":"2026-09-20T10:01:00Z","owner":{"__typename":"User","id":"U_1","login":"alice"}}],"pageInfo":{"hasNextPage":false,"endCursor":"USER_2"}}}}}`))
		case strings.Contains(payload.Query, "organizations(first:1"):
			after, _ := payload.Variables["ownerAfter"].(string)
			switch after {
			case "":
				_, _ = writer.Write([]byte(`{"data":{"viewer":{"organizations":{"nodes":[{"id":"O_ALPHA","login":"alpha","projectsV2":{"nodes":[{"id":"P_ALPHA_1","number":1,"title":"Alpha one","url":"https://github.com/orgs/alpha/projects/1","updatedAt":"2026-09-20T11:00:00Z","owner":{"__typename":"Organization","id":"O_ALPHA","login":"alpha"}}],"pageInfo":{"hasNextPage":true,"endCursor":"ALPHA_1"}}}],"pageInfo":{"hasNextPage":true,"endCursor":"ORG_ALPHA"}}}}}`))
			case "ORG_ALPHA":
				_, _ = writer.Write([]byte(`{"data":{"viewer":{"organizations":{"nodes":[{"id":"O_EMPTY","login":"empty","projectsV2":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}}],"pageInfo":{"hasNextPage":true,"endCursor":"ORG_EMPTY"}}}}}`))
			case "ORG_EMPTY":
				_, _ = writer.Write([]byte(`{"data":{"viewer":{"organizations":{"nodes":[{"id":"O_OMEGA","login":"omega","projectsV2":{"nodes":[{"id":"P_OMEGA_1","number":1,"title":"Omega one","url":"https://github.com/orgs/omega/projects/1","updatedAt":"2026-09-20T12:00:00Z","owner":{"__typename":"Organization","id":"O_OMEGA","login":"omega"}}],"pageInfo":{"hasNextPage":false,"endCursor":"OMEGA_1"}}}],"pageInfo":{"hasNextPage":false,"endCursor":"ORG_OMEGA"}}}}}`))
			default:
				t.Fatalf("unexpected organization cursor %q", after)
			}
		case strings.Contains(payload.Query, "node(id:$owner)"):
			if payload.Variables["owner"] != "O_ALPHA" || payload.Variables["after"] != "ALPHA_1" {
				t.Fatalf("unexpected organization Project variables: %#v", payload.Variables)
			}
			_, _ = writer.Write([]byte(`{"data":{"node":{"id":"O_ALPHA","login":"alpha","projectsV2":{"nodes":[{"id":"P_ALPHA_2","number":2,"title":"Alpha two","url":"https://github.com/orgs/alpha/projects/2","updatedAt":"2026-09-20T11:01:00Z","owner":{"__typename":"Organization","id":"O_ALPHA","login":"alpha"}}],"pageInfo":{"hasNextPage":false,"endCursor":"ALPHA_2"}}}}}`))
		default:
			t.Fatalf("unexpected query: %s", payload.Query)
		}
	}))
	defer server.Close()

	resolver := &discoveryCredentialResolver{value: core.ActiveCredential{TokenType: "Bearer", AccessToken: "test-token", Metadata: map[string]any{"graphql_url": server.URL}}}
	runtime := tracker.Runtime{Credentials: resolver, HTTPClient: server.Client()}
	provider := newDiscoveryProvider(t, runtime)
	input := core.TrackerDiscoveryRequest{ConnectionID: "connection-1", Limit: 1}

	var ids []string
	owners := map[string][]string{}
	for pageNumber := range 10 {
		page, err := provider.DiscoverTrackerScopes(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		if err := page.Validate(); err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			ids = append(ids, item.ExternalID)
			owners[item.ExternalID] = []string{item.ParentID, item.Metadata["owner"].(string), item.Metadata["owner_type"].(string)}
		}
		if !page.HasMore {
			break
		}
		if page.NextCursor == "" {
			t.Fatal("continuation is missing")
		}
		input.Cursor = page.NextCursor
		if pageNumber == 2 {
			provider = newDiscoveryProvider(t, runtime)
		}
	}

	want := []string{"P_USER_1", "P_USER_2", "P_ALPHA_1", "P_ALPHA_2", "P_OMEGA_1"}
	if !slices.Equal(ids, want) {
		t.Fatalf("Project inventory mismatch: got %v want %v", ids, want)
	}
	if got := owners["P_USER_1"]; !slices.Equal(got, []string{"U_1", "alice", "user"}) {
		t.Fatalf("personal owner metadata mismatch: %v", got)
	}
	if got := owners["P_ALPHA_2"]; !slices.Equal(got, []string{"O_ALPHA", "alpha", "organization"}) {
		t.Fatalf("organization owner metadata mismatch: %v", got)
	}
	if requests.Load() != 6 || resolver.calls.Load() != 6 {
		t.Fatalf("unexpected request counts: http=%d credentials=%d", requests.Load(), resolver.calls.Load())
	}
}

func TestDiscoverTrackerScopesRejectsInvalidCursorBeforeCredentialOrHTTPIO(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	resolver := &discoveryCredentialResolver{value: core.ActiveCredential{AccessToken: "must-not-leak", Metadata: map[string]any{"graphql_url": server.URL}}}
	provider := newDiscoveryProvider(t, tracker.Runtime{Credentials: resolver, HTTPClient: server.Client()})

	cases := map[string]string{
		"malformed": "not-base64!",
		"oversized": strings.Repeat("a", maxDiscoveryCursorSize+1),
		"wrong binding": encodeDiscoveryCursor(discoveryCursor{
			Version: discoveryCursorVersion, Binding: discoveryBinding("other-connection"), Phase: discoveryPhaseViewer,
		}),
		"impossible state": encodeDiscoveryCursor(discoveryCursor{
			Version: discoveryCursorVersion, Binding: discoveryBinding("connection-1"), Phase: discoveryPhaseOrgs, ProjectAfter: "PROJECT",
		}),
	}
	for name, cursor := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := provider.DiscoverTrackerScopes(context.Background(), core.TrackerDiscoveryRequest{ConnectionID: "connection-1", Cursor: cursor})
			var providerErr *core.TrackerProviderError
			if !errors.As(err, &providerErr) || providerErr.Code != core.TrackerErrorCursorInvalid {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Contains(err.Error(), "must-not-leak") {
				t.Fatal("credential leaked")
			}
		})
	}
	if resolver.calls.Load() != 0 || requests.Load() != 0 {
		t.Fatalf("invalid cursor performed I/O: credentials=%d http=%d", resolver.calls.Load(), requests.Load())
	}
}

func TestDiscoverTrackerScopesRefusesPartialGraphQLData(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":{"viewer":{"id":"U_1","login":"alice","projectsV2":{"nodes":[{"id":"P_1","number":1,"title":"Partial","url":"https://github.com/users/alice/projects/1","updatedAt":"2026-09-20T10:00:00Z"}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}},"errors":[{"message":"organization access denied"}]}`))
	}))
	defer server.Close()
	resolver := &discoveryCredentialResolver{value: core.ActiveCredential{AccessToken: "test-token", Metadata: map[string]any{"graphql_url": server.URL}}}
	provider := newDiscoveryProvider(t, tracker.Runtime{Credentials: resolver, HTTPClient: server.Client()})
	page, err := provider.DiscoverTrackerScopes(context.Background(), core.TrackerDiscoveryRequest{ConnectionID: "connection-1"})
	if err == nil || len(page.Items) != 0 || page.HasMore || page.NextCursor != "" {
		t.Fatalf("partial GraphQL response was accepted: %+v %v", page, err)
	}
}

func newDiscoveryProvider(t *testing.T, runtime tracker.Runtime) core.TrackerProvider {
	t.Helper()
	provider, err := New(Config{ClientID: "client", TrackerRuntime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	return provider.(core.TrackerProvider)
}
