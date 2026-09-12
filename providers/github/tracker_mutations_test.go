package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/devkit"
	trackerruntime "github.com/goliatone/go-services/providers/tracker"
)

type githubCredentialResolver func(context.Context, string) (core.ActiveCredential, error)

func (r githubCredentialResolver) ResolveTrackerCredential(ctx context.Context, id string) (core.ActiveCredential, error) {
	return r(ctx, id)
}

func TestGitHubFieldCapabilitiesReflectRepositoryPermissions(t *testing.T) {
	provider, closeServer := githubMutationProvider(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/owner/repo" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		writer.Header().Set("X-OAuth-Scopes", "repo")
		_, _ = writer.Write([]byte(`{"id":7,"full_name":"owner/repo","has_issues":true,"archived":false,"updated_at":"repo-r1","permissions":{"pull":true,"push":true}}`))
	})
	defer closeServer()
	capabilities, err := provider.DiscoverTrackerFieldCapabilities(context.Background(), core.TrackerFieldCapabilityRequest{ConnectionID: "connection-1", RepositoryID: "owner/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(capabilities) != len(githubIssueFields)*3 {
		t.Fatalf("capabilities = %#v", capabilities)
	}
	for _, capability := range capabilities {
		if err := capability.Validate(); err != nil {
			t.Fatalf("invalid capability %#v: %v", capability, err)
		}
		if capability.Field == "state" && capability.Operation == "create" && capability.Granted {
			t.Fatal("state create capability must remain denied")
		}
	}
}

func TestGitHubFieldWritesFailClosedBeforeMutation(t *testing.T) {
	for _, test := range []struct {
		name string
		repo string
	}{
		{"triage_metadata", `{"id":7,"full_name":"owner/repo","has_issues":true,"permissions":{"pull":true,"triage":true}}`},
		{"unproven_token", `{"id":7,"full_name":"owner/repo","has_issues":true,"permissions":{"push":true}}`},
		{"read_only", `{"id":7,"full_name":"owner/repo","has_issues":true,"permissions":{"pull":true}}`},
		{"archived", `{"id":7,"full_name":"owner/repo","has_issues":true,"archived":true,"permissions":{"push":true}}`},
		{"issues_disabled", `{"id":7,"full_name":"owner/repo","has_issues":false,"permissions":{"push":true}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var writes atomic.Int32
			provider, closeServer := githubMutationProvider(t, func(writer http.ResponseWriter, request *http.Request) {
				if test.name != "unproven_token" {
					writer.Header().Set("X-OAuth-Scopes", "repo")
				}
				if request.Method != http.MethodGet || request.URL.Path != "/repos/owner/repo" {
					writes.Add(1)
					t.Errorf("unexpected mutation %s %s", request.Method, request.URL.Path)
				}
				_, _ = writer.Write([]byte(test.repo))
			})
			defer closeServer()
			caps, err := provider.DiscoverTrackerFieldCapabilities(context.Background(), core.TrackerFieldCapabilityRequest{ConnectionID: "connection-1", RepositoryID: "owner/repo"})
			if err != nil {
				t.Fatal(err)
			}
			for _, cap := range caps {
				if cap.Field == "labels" && cap.Operation != "read" && cap.Granted {
					t.Fatalf("labels write granted: %#v", cap)
				}
				if test.name == "triage_metadata" && cap.Field == "title" && cap.Operation == "update" && !cap.Granted {
					t.Fatal("triage title update denied")
				}
			}
			_, err = provider.CreateTrackerIssue(context.Background(), core.TrackerIssueCreateRequest{ConnectionID: "connection-1", RepositoryID: "owner/repo", Fields: map[string]json.RawMessage{"title": json.RawMessage(`"Title"`), "labels": json.RawMessage(`["bug"]`)}, IdempotencyKey: "create-1", ActorID: "owner", CorrelationID: "corr-create"})
			if err == nil {
				t.Fatal("denied create succeeded")
			}
			_, err = provider.UpdateTrackerIssue(context.Background(), core.TrackerIssueUpdateRequest{ConnectionID: "connection-1", RepositoryID: "owner/repo", IssueID: "9001", IssueNumber: 42, Field: "labels", Value: json.RawMessage(`["bug"]`), ExpectedRevision: "revision-1", IdempotencyKey: "update-1", ActorID: "owner", CorrelationID: "corr-update"})
			if err == nil || writes.Load() != 0 {
				t.Fatalf("denied update: writes=%d err=%v", writes.Load(), err)
			}
		})
	}
}

func TestGitHubCreateIssueReturnsImmutableRedactedReceipt(t *testing.T) {
	provider, closeServer := githubMutationProvider(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == "/repos/owner/repo" {
			writer.Header().Set("X-OAuth-Scopes", "repo")
			_, _ = writer.Write([]byte(`{"id":7,"full_name":"owner/repo","has_issues":true,"permissions":{"push":true}}`))
			return
		}
		if request.Method != http.MethodPost || request.URL.Path != "/repos/owner/repo/issues" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer must-not-leak" || request.Header.Get("X-Idempotency-Key") != "publish-1" {
			t.Fatalf("headers = %#v", request.Header)
		}
		body, _ := io.ReadAll(request.Body)
		if !strings.Contains(string(body), `"title":"Publish me"`) || !strings.Contains(string(body), `"body":"Body"`) {
			t.Fatalf("body = %s", body)
		}
		writer.Header().Set("X-GitHub-Request-Id", "request-1")
		writer.Header().Set("ETag", `"etag-1"`)
		writer.Header().Set("X-RateLimit-Remaining", "4999")
		writer.Header().Set("X-RateLimit-Reset", "1789257600")
		_, _ = writer.Write([]byte(`{"id":9001,"number":42,"title":"Publish me","body":"Body","state":"open","html_url":"https://github.com/owner/repo/issues/42","updated_at":"2026-09-12T21:00:00Z"}`))
	})
	defer closeServer()
	receipt, err := provider.CreateTrackerIssue(context.Background(), core.TrackerIssueCreateRequest{ConnectionID: "connection-1", RepositoryID: "owner/repo", Fields: map[string]json.RawMessage{"title": json.RawMessage(`"Publish me"`), "description": json.RawMessage(`"Body"`)}, IdempotencyKey: "publish-1", ActorID: "owner", CorrelationID: "corr-1"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ExternalID != "9001" || receipt.DisplayID != "42" || receipt.ProviderRevision != "2026-09-12T21:00:00Z" || receipt.RequestID != "request-1" || receipt.RateLimit.Remaining != 4999 || receipt.Metadata["etag"] != `"etag-1"` {
		t.Fatalf("receipt = %#v", receipt)
	}
	encoded, _ := json.Marshal(receipt)
	if receipt.CorrelationID != "corr-1" {
		t.Fatalf("receipt correlation = %q", receipt.CorrelationID)
	}
	if strings.Contains(string(encoded), "must-not-leak") || strings.Contains(string(encoded), "publish-1") {
		t.Fatalf("receipt leaked request secrets: %s", encoded)
	}
}

func TestGitHubUpdateChecksCurrentRevisionBeforeMutation(t *testing.T) {
	var patchCalls atomic.Int32
	provider, closeServer := githubMutationProvider(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == "/repos/owner/repo" {
			writer.Header().Set("X-OAuth-Scopes", "repo")
			_, _ = writer.Write([]byte(`{"id":7,"full_name":"owner/repo","has_issues":true,"permissions":{"push":true}}`))
			return
		}
		if request.URL.Path != "/repos/owner/repo/issues/42" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		switch request.Method {
		case http.MethodGet:
			_, _ = writer.Write([]byte(`{"id":9001,"number":42,"title":"Before","state":"open","html_url":"https://github.com/owner/repo/issues/42","updated_at":"2026-09-12T21:00:00Z"}`))
		case http.MethodPatch:
			patchCalls.Add(1)
			body, _ := io.ReadAll(request.Body)
			if string(body) != `{"title":"After"}` {
				t.Fatalf("body = %s", body)
			}
			_, _ = writer.Write([]byte(`{"id":9001,"number":42,"title":"After","state":"open","html_url":"https://github.com/owner/repo/issues/42","updated_at":"2026-09-12T21:01:00Z"}`))
		default:
			t.Fatalf("method = %s", request.Method)
		}
	})
	defer closeServer()
	base := core.TrackerIssueUpdateRequest{ConnectionID: "connection-1", RepositoryID: "owner/repo", IssueID: "9001", IssueNumber: 42, Field: "title", Value: json.RawMessage(`"After"`), ExpectedRevision: "2026-09-12T21:00:00Z", IdempotencyKey: "update-1", ActorID: "owner", CorrelationID: "corr-1"}
	receipt, err := provider.UpdateTrackerIssue(context.Background(), base)
	if err != nil || receipt.ProviderRevision != "2026-09-12T21:01:00Z" || patchCalls.Load() != 1 {
		t.Fatalf("update: %#v patches=%d err=%v", receipt, patchCalls.Load(), err)
	}
	base.ExpectedRevision = "stale"
	if _, err := provider.UpdateTrackerIssue(context.Background(), base); err == nil || patchCalls.Load() != 1 {
		t.Fatalf("stale revision mutated provider: patches=%d err=%v", patchCalls.Load(), err)
	}
}

func TestGitHubUpdatePayloadsCoverSupportedFields(t *testing.T) {
	tests := map[string]struct {
		value json.RawMessage
		key   string
	}{
		"title": {json.RawMessage(`"Title"`), "title"}, "description": {json.RawMessage(`"Body"`), "body"}, "state": {json.RawMessage(`"closed"`), "state"}, "labels": {json.RawMessage(`["bug"]`), "labels"}, "assignees": {json.RawMessage(`["octocat"]`), "assignees"}, "milestone": {json.RawMessage(`7`), "milestone"},
	}
	for field, test := range tests {
		payload, err := githubUpdateIssuePayload(field, test.value)
		if err != nil || len(payload) != 1 || payload[test.key] == nil {
			t.Fatalf("%s payload=%#v err=%v", field, payload, err)
		}
	}
}

func TestGitHubMutationProviderConformance(t *testing.T) {
	provider, closeServer := githubMutationProvider(t, func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/owner/repo":
			writer.Header().Set("X-OAuth-Scopes", "repo")
			_, _ = writer.Write([]byte(`{"id":7,"full_name":"owner/repo","has_issues":true,"updated_at":"repo-r1","permissions":{"pull":true,"push":true}}`))
		case request.Method == http.MethodPost && request.URL.Path == "/repos/owner/repo/issues":
			_, _ = writer.Write([]byte(`{"id":9001,"number":42,"title":"Create","state":"open","html_url":"https://github.com/owner/repo/issues/42","updated_at":"2026-09-12T21:00:00Z"}`))
		case request.Method == http.MethodGet && request.URL.Path == "/repos/owner/repo/issues/42":
			_, _ = writer.Write([]byte(`{"id":9001,"number":42,"title":"Create","state":"open","html_url":"https://github.com/owner/repo/issues/42","updated_at":"2026-09-12T21:00:00Z"}`))
		case request.Method == http.MethodPatch && request.URL.Path == "/repos/owner/repo/issues/42":
			_, _ = writer.Write([]byte(`{"id":9001,"number":42,"title":"Updated","state":"open","html_url":"https://github.com/owner/repo/issues/42","updated_at":"2026-09-12T21:01:00Z"}`))
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	})
	defer closeServer()
	fixture := devkit.TrackerMutationConformanceFixture{
		Capability: core.TrackerFieldCapabilityRequest{ConnectionID: "connection-1", RepositoryID: "owner/repo"},
		Create:     core.TrackerIssueCreateRequest{ConnectionID: "connection-1", RepositoryID: "owner/repo", Fields: map[string]json.RawMessage{"title": json.RawMessage(`"Create"`)}, IdempotencyKey: "create-1", ActorID: "owner", CorrelationID: "corr-create"},
		Update:     core.TrackerIssueUpdateRequest{ConnectionID: "connection-1", RepositoryID: "owner/repo", IssueNumber: 42, Field: "title", Value: json.RawMessage(`"Updated"`), IdempotencyKey: "update-1", ActorID: "owner", CorrelationID: "corr-update"},
	}
	if err := devkit.ValidateTrackerMutationProviderConformance(context.Background(), provider, fixture); err != nil {
		t.Fatal(err)
	}
}

func githubMutationProvider(t *testing.T, handler http.HandlerFunc) (*Provider, func()) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	runtime := trackerruntime.Runtime{Credentials: githubCredentialResolver(func(context.Context, string) (core.ActiveCredential, error) {
		return core.ActiveCredential{AccessToken: "must-not-leak", Metadata: map[string]any{"api_url": server.URL}}, nil
	}), HTTPClient: server.Client()}
	value, err := New(Config{ClientID: "test-client", TrackerRuntime: runtime})
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	provider, ok := value.(*Provider)
	if !ok {
		server.Close()
		t.Fatalf("provider type = %T", value)
	}
	return provider, server.Close
}
