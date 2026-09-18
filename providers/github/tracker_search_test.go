package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestRepositorySearchFindsLateInventoryWithinBudget(t *testing.T) {
	var calls atomic.Int32
	provider, closeServer := githubMutationProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		q := r.URL.Query()
		if r.URL.Path != "/user/repos" || q.Get("affiliation") != "owner,collaborator,organization_member" || q.Get("sort") != "full_name" || q.Get("direction") != "asc" || q.Get("per_page") != "100" || r.Header.Get("Authorization") != "Bearer must-not-leak" {
			t.Errorf("unexpected inventory request: %s", r.URL)
		}
		page, _ := strconv.Atoi(q.Get("page"))
		repos := []repository{}
		if page <= 3 {
			for n := 0; n < 100; n++ {
				repos = append(repos, repository{ID: int64(page*100 + n), FullName: fmt.Sprintf("a-owner/repo-%d-%03d", page, n)})
			}
		} else if page == 4 {
			for n, owner := range []string{"target-owner", "target-collaborator", "target-organization"} {
				repo := repository{ID: int64(1000 + n), FullName: owner + "/project", Private: true, Archived: n == 2, HasIssues: true}
				repo.Owner.Login = owner
				repo.Permissions.Pull = true
				repo.Permissions.Push = true
				repos = append(repos, repo)
			}
		} else {
			t.Errorf("unexpected page %d", page)
		}
		_ = json.NewEncoder(w).Encode(repos)
	})
	defer closeServer()
	input := core.TrackerDiscoveryRequest{ConnectionID: "connection-1", Search: " TaRgEt "}
	page, err := provider.DiscoverTrackerScopes(context.Background(), input)
	if err != nil || len(page.Items) != 0 || !page.HasMore || page.NextCursor == "" || calls.Load() != 3 {
		t.Fatalf("first scan=%+v calls=%d err=%v", page, calls.Load(), err)
	}
	if err := page.Validate(); err != nil {
		t.Fatal(err)
	}
	input.Cursor, input.Search = page.NextCursor, "target"
	page, err = provider.DiscoverTrackerScopes(context.Background(), input)
	if err != nil || len(page.Items) != 3 || page.HasMore || page.NextCursor != "" || calls.Load() != 4 {
		t.Fatalf("last scan=%+v calls=%d err=%v", page, calls.Load(), err)
	}
	if page.Items[0].Metadata["visibility"] != "private" || page.Items[0].Metadata["repository_id"] != "1000" || !slices.Contains(page.Items[0].Capabilities, "issues.write") || slices.Contains(page.Items[2].Capabilities, "issues.write") {
		t.Fatalf("lost node projection: %+v", page.Items)
	}
	input.Search, input.Cursor = "absent", ""
	page, err = provider.DiscoverTrackerScopes(context.Background(), input)
	if err != nil || !page.HasMore {
		t.Fatalf("no-match intermediate: %+v %v", page, err)
	}
	input.Cursor = page.NextCursor
	page, err = provider.DiscoverTrackerScopes(context.Background(), input)
	if err != nil || page.HasMore || len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatalf("exhausted: %+v %v", page, err)
	}
}

func TestRepositorySearchResumesWithinPageWithoutDroppingMatches(t *testing.T) {
	var calls atomic.Int32
	provider, closeServer := githubMutationProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		repos := []repository{}
		for n := (page - 1) * 100; n < min(page*100, 103); n++ {
			repos = append(repos, repository{ID: int64(n + 1), FullName: fmt.Sprintf("owner/match-%03d", n)})
		}
		_ = json.NewEncoder(w).Encode(repos)
	})
	defer closeServer()
	for _, limit := range []int{0, 37, 100, 500} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			input := core.TrackerDiscoveryRequest{ConnectionID: "connection-1", Search: "match", Limit: limit}
			ids := []string{}
			expected := limit
			if expected == 0 {
				expected = 50
			}
			if expected > 100 {
				expected = 100
			}
			expiry := int64(0)
			for iteration := 0; iteration < 10; iteration++ {
				before := calls.Load()
				page, err := provider.DiscoverTrackerScopes(context.Background(), input)
				if err != nil {
					t.Fatal(err)
				}
				if len(page.Items) > expected || calls.Load()-before > 3 {
					t.Fatalf("unbounded search: %+v", page)
				}
				for _, item := range page.Items {
					ids = append(ids, item.ExternalID)
				}
				if !page.HasMore {
					break
				}
				body, _, _ := strings.Cut(page.NextCursor, ".")
				data, _ := base64.RawURLEncoding.DecodeString(body)
				var state repositorySearchCursor
				if json.Unmarshal(data, &state) != nil || strings.Contains(string(data), "must-not-leak") || strings.Contains(string(data), "api_url") {
					t.Fatal("invalid or unsafe cursor payload")
				}
				if expiry != 0 && state.Expires != expiry {
					t.Fatal("continuation extended traversal expiry")
				}
				expiry = state.Expires
				input.Cursor = page.NextCursor
			}
			if len(ids) != 103 {
				t.Fatalf("matched %d records", len(ids))
			}
			for n, id := range ids {
				if id != fmt.Sprintf("owner/match-%03d", n) {
					t.Fatalf("omitted/repeated match at %d: %s", n, id)
				}
			}
		})
	}
}

func TestRepositorySearchCursorRejectsChangedBindings(t *testing.T) {
	var calls atomic.Int32
	provider, closeServer := githubMutationProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`[{"id":1,"full_name":"owner/match-one"},{"id":2,"full_name":"owner/match-two"}]`))
	})
	defer closeServer()
	ctx := context.Background()
	credential, err := provider.runtime.ResolveCredential(ctx, "connection-1")
	if err != nil {
		t.Fatal(err)
	}
	credential.GrantedScopes = []string{"repo", "read:user"}
	active := credential
	provider.runtime.Credentials = githubCredentialResolver(func(context.Context, string) (core.ActiveCredential, error) { return active, nil })
	input := core.TrackerDiscoveryRequest{ConnectionID: "connection-1", Search: "match", Limit: 1}
	page, err := provider.DiscoverTrackerScopes(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Cursor = page.NextCursor
	cases := []struct {
		name   string
		mutate func(*core.TrackerDiscoveryRequest)
	}{
		{"query", func(q *core.TrackerDiscoveryRequest) { q.Search = "different" }},
		{"connection", func(q *core.TrackerDiscoveryRequest) { q.ConnectionID = "other" }},
		{"limit", func(q *core.TrackerDiscoveryRequest) { q.Limit = 2 }},
		{"credential", func(*core.TrackerDiscoveryRequest) { active.AccessToken = "rotated-private-token" }},
		{"token-type", func(*core.TrackerDiscoveryRequest) { active.TokenType = "changed" }},
		{"grants", func(*core.TrackerDiscoveryRequest) { active.GrantedScopes = []string{"repo"} }},
		{"endpoint", func(*core.TrackerDiscoveryRequest) {
			active.Metadata = map[string]any{"api_url": "https://other.example.test"}
		}},
		{"numeric", func(q *core.TrackerDiscoveryRequest) { q.Cursor = "2" }},
		{"oversized", func(q *core.TrackerDiscoveryRequest) { q.Cursor = strings.Repeat("a", 4097) }},
		{"tampered", func(q *core.TrackerDiscoveryRequest) { q.Cursor = "A" + q.Cursor[1:] }},
		{"signature", func(q *core.TrackerDiscoveryRequest) {
			body, _, _ := strings.Cut(q.Cursor, ".")
			q.Cursor = body + ".AA"
		}},
		{"cleared-search", func(q *core.TrackerDiscoveryRequest) { q.Search = "" }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			active = credential
			request := input
			tt.mutate(&request)
			before := calls.Load()
			result, err := provider.DiscoverTrackerScopes(ctx, request)
			var providerErr *core.TrackerProviderError
			if !errors.As(err, &providerErr) || providerErr.Code != core.TrackerErrorCursorInvalid || result.NextCursor != "" || len(result.Items) != 0 || calls.Load() != before {
				t.Fatalf("accepted changed cursor: %+v %v", result, err)
			}
			if strings.Contains(err.Error(), "must-not-leak") || strings.Contains(err.Error(), "rotated-private-token") {
				t.Fatal("credential leaked")
			}
		})
	}
	active = credential
	active.GrantedScopes = []string{"read:user", "repo"}
	input.Search = " MATCH "
	page, err = provider.DiscoverTrackerScopes(ctx, input)
	if err != nil || len(page.Items) != 1 || page.Items[0].ExternalID != "owner/match-two" {
		t.Fatalf("equivalent binding failed: %+v %v", page, err)
	}
	restarted, err := New(Config{ClientID: "test-client", TrackerRuntime: provider.runtime})
	if err != nil {
		t.Fatal(err)
	}
	_, err = restarted.(core.TrackerProvider).DiscoverTrackerScopes(ctx, input)
	var providerErr *core.TrackerProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != core.TrackerErrorCursorInvalid {
		t.Fatalf("restart accepted cursor: %v", err)
	}
}

func TestRepositorySearchRejectsExpiredAndInvalidPositions(t *testing.T) {
	var calls atomic.Int32
	provider, closeServer := githubMutationProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`[{"id":1,"full_name":"owner/match"}]`))
	})
	defer closeServer()
	ctx := context.Background()
	credential, endpoint, err := provider.resolve(ctx, "connection-1")
	if err != nil {
		t.Fatal(err)
	}
	binding := repositorySearchBinding{Connection: "connection-1", Search: "match", Limit: 1, Endpoint: endpoint.String(), AccessToken: credential.AccessToken}
	for _, state := range []repositorySearchCursor{
		{Version: 1, Expires: time.Now().Unix() - 1, Page: 1},
		{Version: 2, Expires: time.Now().Add(time.Minute).Unix(), Page: 1},
		{Version: 1, Expires: time.Now().Add(time.Hour).Unix(), Page: 1},
		{Version: 1, Expires: time.Now().Add(time.Minute).Unix(), Page: 0},
		{Version: 1, Expires: time.Now().Add(time.Minute).Unix(), Page: 2147483648},
		{Version: 1, Expires: time.Now().Add(time.Minute).Unix(), Page: 1, Offset: -1},
		{Version: 1, Expires: time.Now().Add(time.Minute).Unix(), Page: 1, Offset: 100},
	} {
		before := calls.Load()
		_, err := provider.DiscoverTrackerScopes(ctx, core.TrackerDiscoveryRequest{ConnectionID: "connection-1", Search: "match", Limit: 1, Cursor: provider.encodeSearchCursor(state, binding)})
		var providerErr *core.TrackerProviderError
		if !errors.As(err, &providerErr) || providerErr.Code != core.TrackerErrorCursorInvalid || calls.Load() != before {
			t.Fatalf("accepted invalid state %+v: %v", state, err)
		}
	}
	state := repositorySearchCursor{Version: 1, Expires: time.Now().Add(time.Minute).Unix(), Page: 1, Offset: 2}
	_, err = provider.DiscoverTrackerScopes(ctx, core.TrackerDiscoveryRequest{ConnectionID: "connection-1", Search: "match", Limit: 1, Cursor: provider.encodeSearchCursor(state, binding)})
	var providerErr *core.TrackerProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != core.TrackerErrorCursorInvalid {
		t.Fatalf("accepted missing offset: %v", err)
	}
}

func TestRepositorySearchFailureDoesNotAdvanceAndRetryRecovers(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var calls atomic.Int32
	provider, closeServer := githubMutationProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("page") == "2" {
			if fail.Load() {
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte("must-not-leak"))
				return
			}
			_, _ = w.Write([]byte(`[{"id":101,"full_name":"owner/match-last"}]`))
			return
		}
		repos := []repository{{ID: 1, FullName: "owner/match-first"}}
		for n := 1; n < 100; n++ {
			repos = append(repos, repository{ID: int64(n + 1), FullName: fmt.Sprintf("owner/other-%03d", n)})
		}
		_ = json.NewEncoder(w).Encode(repos)
	})
	defer closeServer()
	input := core.TrackerDiscoveryRequest{ConnectionID: "connection-1", Search: "match"}
	page, err := provider.DiscoverTrackerScopes(context.Background(), input)
	var providerErr *core.TrackerProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != core.TrackerErrorRateLimited || providerErr.RetryAfter != 2*time.Second || len(page.Items) != 0 || page.HasMore || page.NextCursor != "" || strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("unsafe failed scan: %+v %v", page, err)
	}
	fail.Store(false)
	page, err = provider.DiscoverTrackerScopes(context.Background(), input)
	if err != nil || len(page.Items) != 2 || page.HasMore || calls.Load() != 4 {
		t.Fatalf("retry skipped matches: %+v %v", page, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := calls.Load()
	page, err = provider.DiscoverTrackerScopes(ctx, input)
	if !errors.Is(err, context.Canceled) || len(page.Items) != 0 || calls.Load() != before {
		t.Fatalf("canceled search: %+v %v", page, err)
	}
}

func TestRepositorySearchLiteralAndBlankBrowseCompatibility(t *testing.T) {
	queries := make(chan url.Values, 3)
	provider, closeServer := githubMutationProvider(t, func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.Query()
		_, _ = w.Write([]byte(`[{"id":1,"full_name":"owner/a_b"},{"id":2,"full_name":"owner/axb"}]`))
	})
	defer closeServer()
	page, err := provider.DiscoverTrackerScopes(context.Background(), core.TrackerDiscoveryRequest{ConnectionID: "connection-1", Search: " A_B "})
	if err != nil || len(page.Items) != 1 || page.Items[0].ExternalID != "owner/a_b" || page.HasMore {
		t.Fatalf("literal search: %+v %v", page, err)
	}
	<-queries
	page, err = provider.DiscoverTrackerScopes(context.Background(), core.TrackerDiscoveryRequest{ConnectionID: "connection-1", Search: " ", Limit: 2, Cursor: "2"})
	q := <-queries
	if err != nil || len(page.Items) != 2 || !page.HasMore || page.NextCursor != "3" || q.Get("sort") != "updated" || q.Get("per_page") != "2" || q.Get("page") != "2" {
		t.Fatalf("browse changed: %+v %v %v", page, q, err)
	}
}
