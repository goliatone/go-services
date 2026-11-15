package github

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

func TestDeliveryEvidenceHeadBindingAndReadOnly(t *testing.T) {
	head := strings.Repeat("a", 40)
	checksHead := head
	canonicalURL := "https://github.com/Owner/Repo/pull/7"
	calls := 0
	provider, closeServer := githubMutationProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Query     string
			Variables map[string]any
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Method != "POST" || r.URL.Path != "/graphql" || !strings.HasPrefix(body.Query, "query(") || body.Variables["number"] != float64(7) {
			t.Fatalf("unexpected request %s %s %#v", r.Method, r.URL.Path, body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"id": "PR_7", "url": canonicalURL, "number": 7, "headRefOid": head, "merged": true, "mergeCommit": map[string]string{"oid": strings.Repeat("c", 40)}, "statusCheckRollup": map[string]any{"state": "SUCCESS", "commit": map[string]string{"oid": checksHead}}}}}})
	})
	defer closeServer()
	input := core.TrackerDiscoveryRequest{ConnectionID: "connection-1", ResourceType: "pull_request", ResourceID: "owner/repo/pull/7"}
	first, err := provider.ReadTrackerDeliveryEvidence(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.ReadTrackerDeliveryEvidence(context.Background(), input)
	if err != nil || first.ProviderRevision != second.ProviderRevision {
		t.Fatalf("refresh changed semantic revision: %v", err)
	}
	head = strings.Repeat("b", 40)
	if _, err := provider.ReadTrackerDeliveryEvidence(context.Background(), input); err == nil {
		t.Fatal("accepted checks for old head")
	}
	checksHead = head
	next, err := provider.ReadTrackerDeliveryEvidence(context.Background(), input)
	if err != nil || next.ProviderRevision == first.ProviderRevision {
		t.Fatalf("head revision missing: %v", err)
	}
	canonicalURL = "https://github.com/other/repo/pull/7"
	if result, err := provider.ReadTrackerDeliveryEvidence(context.Background(), input); err == nil || result.ExternalID != "" {
		t.Fatalf("different repository accepted: %+v %v", result, err)
	}
	input.ResourceID = "../repo/pull/7"
	if _, err := provider.ReadTrackerDeliveryEvidence(context.Background(), input); err == nil || calls != 5 {
		t.Fatalf("bad identity sent to provider: calls=%d err=%v", calls, err)
	}
}

func TestDeliveryEvidenceErrorsAndInputBounds(t *testing.T) {
	for _, tt := range []struct {
		name, kind, retryHeader string
		reset                   bool
		wantCode                string
		wantRetry               bool
	}{
		{"primary limit", "RATE_LIMITED", "", true, core.TrackerErrorRateLimited, true},
		{"secondary limit", "RATE_LIMITED", "120", false, core.TrackerErrorRateLimited, true},
		{"limit without timing", "RATE_LIMITED", "", false, core.TrackerErrorRateLimited, true},
		{"permission", "FORBIDDEN", "", false, core.TrackerErrorPermission, false},
		{"missing", "NOT_FOUND", "", false, core.TrackerErrorNotFound, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			provider, closeServer := githubMutationProvider(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if tt.retryHeader != "" {
					w.Header().Set("Retry-After", tt.retryHeader)
				}
				if tt.reset {
					w.Header().Set("X-RateLimit-Remaining", "0")
					w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(3*time.Minute).Unix(), 10))
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]string{"type": tt.kind, "message": "must-not-leak"}}, "data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"id": "PR_7", "url": "https://github.com/owner/repo/pull/7", "number": 7, "headRefOid": strings.Repeat("a", 40)}}}})
			})
			defer closeServer()
			input := core.TrackerDiscoveryRequest{ConnectionID: "connection-1", ResourceType: "pull_request", ResourceID: "owner/repo/pull/7"}
			result, err := provider.ReadTrackerDeliveryEvidence(t.Context(), input)
			pe, ok := err.(*core.TrackerProviderError)
			if !ok || pe.Code != tt.wantCode || pe.Retryable != tt.wantRetry || result.ExternalID != "" || strings.Contains(err.Error(), "must-not-leak") {
				t.Fatalf("partial result or wrong safe error: %+v %#v", result, err)
			}
			if tt.wantRetry && pe.RetryAfter < time.Minute {
				t.Fatalf("missing backoff: %v", pe.RetryAfter)
			}
			if tt.retryHeader != "" && pe.RetryAfter != 120*time.Second {
				t.Fatalf("lost Retry-After: %v", pe.RetryAfter)
			}
			if tt.reset && pe.RetryAfter < 2*time.Minute {
				t.Fatalf("lost reset: %v", pe.RetryAfter)
			}
			input.ResourceID = "owner/repo/pull/2147483648"
			_, err = provider.ReadTrackerDeliveryEvidence(t.Context(), input)
			pe, ok = err.(*core.TrackerProviderError)
			if !ok || pe.Code != core.TrackerErrorValidation || calls != 1 {
				t.Fatalf("invalid GraphQL number dispatched: %v calls=%d", err, calls)
			}
		})
	}
}
