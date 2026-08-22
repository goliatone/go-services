package providers_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/devkit"
	"github.com/goliatone/go-services/providers/github"
	"github.com/goliatone/go-services/providers/githubprojects"
	"github.com/goliatone/go-services/providers/jira"
	"github.com/goliatone/go-services/providers/linear"
	trackerruntime "github.com/goliatone/go-services/providers/tracker"
)

type trackerCredentialResolver struct{ credential core.ActiveCredential }

func (r trackerCredentialResolver) ResolveTrackerCredential(context.Context, string) (core.ActiveCredential, error) {
	return r.credential, nil
}

func TestTrackerProviderPacksConformToTypedObservationContract(t *testing.T) {
	t.Run("github issues", func(t *testing.T) {
		server := newTrackerTLSServer(t, func(request *http.Request, _ string) string {
			switch request.URL.Path {
			case "/user/repos":
				return `[ {"id":1,"full_name":"acme/widgets","html_url":"https://github.com/acme/widgets","updated_at":"2026-08-21T10:00:00Z"} ]`
			case "/repos/acme/widgets/issues":
				return `[ {"id":2,"number":7,"title":"Fix it","body":"Details","state":"open","html_url":"https://github.com/acme/widgets/issues/7","updated_at":"2026-08-21T10:01:00Z","user":{},"assignees":[],"labels":[],"milestone":null} ]`
			default:
				t.Fatalf("unexpected GitHub path %s", request.URL.Path)
				return ""
			}
		})
		provider, err := github.New(github.Config{ClientID: "client", ClientSecret: "secret", TrackerRuntime: runtimeFor(server, map[string]any{"api_url": server.URL})})
		if err != nil {
			t.Fatal(err)
		}
		assertTrackerConformance(t, provider, "repository", "acme/widgets")
	})

	t.Run("github projects v2", func(t *testing.T) {
		server := newTrackerTLSServer(t, func(_ *http.Request, body string) string {
			switch {
			case strings.Contains(body, "projectsV2"):
				return `{"data":{"viewer":{"projectsV2":{"nodes":[{"id":"PVT_1","number":1,"title":"Roadmap","url":"https://github.com/users/acme/projects/1","updatedAt":"2026-08-21T10:00:00Z"}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`
			case strings.Contains(body, "fields(first"):
				return `{"data":{"node":{"updatedAt":"2026-08-21T10:00:00Z","fields":{"nodes":[{"id":"PVTF_1","name":"Status","dataType":"SINGLE_SELECT","options":[{"id":"ready","name":"Ready"}]}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`
			case strings.Contains(body, "items(first"):
				return `{"data":{"node":{"items":{"nodes":[{"id":"PVTI_1","updatedAt":"2026-08-21T10:01:00Z","type":"ISSUE","content":{"id":"I_1","number":7,"title":"Fix it","body":"Details","state":"OPEN","url":"https://github.com/acme/widgets/issues/7","updatedAt":"2026-08-21T10:01:00Z"},"fieldValues":{"nodes":[]}}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`
			default:
				t.Fatalf("unexpected GitHub GraphQL query %s", body)
				return ""
			}
		})
		provider, err := githubprojects.New(githubprojects.Config{ClientID: "client", ClientSecret: "secret", TrackerRuntime: runtimeFor(server, map[string]any{"graphql_url": server.URL})})
		if err != nil {
			t.Fatal(err)
		}
		assertTrackerConformance(t, provider, "project_v2", "PVT_1")
	})

	t.Run("linear", func(t *testing.T) {
		server := newTrackerTLSServer(t, func(_ *http.Request, body string) string {
			switch {
			case strings.Contains(body, "teams(first"):
				return `{"data":{"teams":{"nodes":[{"id":"team-1","key":"ENG","name":"Engineering","updatedAt":"2026-08-21T10:00:00Z"}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`
			case strings.Contains(body, "team(id"):
				return `{"data":{"team":{"updatedAt":"2026-08-21T10:00:00Z","states":{"nodes":[{"id":"state-1","name":"Todo","type":"unstarted","color":"#fff","updatedAt":"2026-08-21T10:00:00Z"}]},"labels":{"nodes":[]}}}}`
			case strings.Contains(body, "issues(filter"):
				return `{"data":{"issues":{"nodes":[{"id":"issue-1","identifier":"ENG-1","title":"Fix it","description":"Details","priority":2,"url":"https://linear.app/acme/issue/ENG-1","updatedAt":"2026-08-21T10:01:00Z","state":{"id":"state-1","name":"Todo","type":"unstarted"},"assignee":null,"labels":{"nodes":[]},"team":{"id":"team-1","key":"ENG","name":"Engineering"}}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`
			default:
				t.Fatalf("unexpected Linear GraphQL query %s", body)
				return ""
			}
		})
		provider, err := linear.New(linear.Config{TrackerRuntime: runtimeFor(server, map[string]any{"graphql_url": server.URL})})
		if err != nil {
			t.Fatal(err)
		}
		assertTrackerConformance(t, provider, "team", "team-1")
	})

	t.Run("jira", func(t *testing.T) {
		server := newTrackerTLSServer(t, func(request *http.Request, _ string) string {
			switch request.URL.Path {
			case "/rest/api/3/project/search":
				return `{"values":[{"id":"10000","key":"ENG","name":"Engineering","projectTypeKey":"software"}],"startAt":0,"maxResults":10,"total":1,"isLast":true}`
			case "/rest/api/3/field/search":
				return `{"values":[{"id":"summary","name":"Summary","required":true,"schema":{"type":"string","system":"summary"}}],"startAt":0,"maxResults":10,"total":1,"isLast":true}`
			case "/rest/api/3/search/jql":
				return `{"issues":[{"id":"10001","key":"ENG-1","fields":{"summary":"Fix it","description":{},"status":{"name":"Todo"},"assignee":null,"reporter":null,"labels":[],"priority":{"name":"Medium"},"issuetype":{"name":"Task"},"updated":"2026-08-21T10:01:00Z"}}],"isLast":true}`
			default:
				t.Fatalf("unexpected Jira path %s", request.URL.Path)
				return ""
			}
		})
		provider, err := jira.New(jira.Config{TrackerRuntime: runtimeFor(server, map[string]any{"base_url": server.URL, "email": "owner@example.test"})})
		if err != nil {
			t.Fatal(err)
		}
		assertTrackerConformance(t, provider, "project", "ENG")
	})
}

func newTrackerTLSServer(t *testing.T, response func(*http.Request, string) string) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(response(request, string(body))))
	}))
	t.Cleanup(server.Close)
	return server
}

func runtimeFor(server *httptest.Server, metadata map[string]any) trackerruntime.Runtime {
	return trackerruntime.Runtime{Credentials: trackerCredentialResolver{credential: core.ActiveCredential{TokenType: "Bearer", AccessToken: "test-token", Metadata: metadata}}, HTTPClient: server.Client()}
}

func assertTrackerConformance(t *testing.T, provider core.Provider, resourceType, resourceID string) {
	t.Helper()
	trackerProvider, ok := provider.(core.TrackerProvider)
	if !ok {
		t.Fatalf("%s does not implement core.TrackerProvider", provider.ID())
	}
	if err := devkit.ValidateTrackerProviderConformance(context.Background(), trackerProvider, "connection-1", resourceType, resourceID); err != nil {
		t.Fatal(err)
	}
}
