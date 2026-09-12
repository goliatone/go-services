package tracker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

type credentialResolverFunc func(context.Context, string) (core.ActiveCredential, error)

func (f credentialResolverFunc) ResolveTrackerCredential(ctx context.Context, id string) (core.ActiveCredential, error) {
	return f(ctx, id)
}

func TestRuntimeBoundsAndAuthenticatesProviderRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret-token" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)
	runtime := Runtime{Credentials: credentialResolverFunc(func(context.Context, string) (core.ActiveCredential, error) {
		return core.ActiveCredential{AccessToken: "secret-token"}, nil
	}), HTTPClient: server.Client()}
	request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	var output map[string]bool
	if err := runtime.DoJSON(context.Background(), "connection-1", request, &output); err != nil {
		t.Fatal(err)
	}
	if !output["ok"] {
		t.Fatalf("output = %#v", output)
	}
}

func TestRuntimeMapsRateLimitWithoutCredentialDisclosure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Retry-After", "2")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`slow down`))
	}))
	t.Cleanup(server.Close)
	runtime := Runtime{Credentials: credentialResolverFunc(func(context.Context, string) (core.ActiveCredential, error) {
		return core.ActiveCredential{AccessToken: "must-not-leak"}, nil
	}), HTTPClient: server.Client()}
	request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	err := runtime.DoJSON(context.Background(), "connection-1", request, nil)
	providerErr, ok := err.(*core.TrackerProviderError)
	if !ok || providerErr.Code != core.TrackerErrorRateLimited || providerErr.RetryAfter != 2*time.Second {
		t.Fatalf("error = %#v", err)
	}
	if err != nil && strings.Contains(err.Error(), "must-not-leak") {
		t.Fatal("credential leaked in error")
	}
}

func TestRuntimeMapsGitHubFailureCategories(t *testing.T) {
	tests := []struct {
		status int
		header http.Header
		code   string
	}{
		{http.StatusForbidden, http.Header{}, core.TrackerErrorPermission},
		{http.StatusForbidden, http.Header{"X-Ratelimit-Remaining": []string{"0"}}, core.TrackerErrorRateLimited},
		{http.StatusNotFound, http.Header{}, core.TrackerErrorNotFound},
		{http.StatusUnprocessableEntity, http.Header{}, core.TrackerErrorValidation},
		{http.StatusPreconditionFailed, http.Header{}, core.TrackerErrorConflict},
	}
	for _, test := range tests {
		response := &http.Response{StatusCode: test.status, Header: test.header}
		err := responseError(response, []byte(`{"message":"safe"}`))
		providerErr, ok := err.(*core.TrackerProviderError)
		if !ok || providerErr.Code != test.code {
			t.Fatalf("status %d: %#v", test.status, err)
		}
	}
}

func TestRuntimeReturnsSafeResponseMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("X-GitHub-Request-Id", "request-1")
		writer.Header().Set("ETag", `"etag-1"`)
		writer.Header().Set("X-RateLimit-Remaining", "12")
		writer.Header().Set("X-RateLimit-Reset", "1789257600")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	runtime := Runtime{Credentials: credentialResolverFunc(func(context.Context, string) (core.ActiveCredential, error) {
		return core.ActiveCredential{AccessToken: "secret"}, nil
	}), HTTPClient: server.Client()}
	request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	metadata, err := runtime.DoJSONWithCredentialMetadata(context.Background(), core.ActiveCredential{AccessToken: "secret"}, request, nil)
	if err != nil || metadata.RequestID != "request-1" || metadata.ETag != `"etag-1"` || metadata.RateLimitRemaining != 12 || metadata.RateLimitReset.IsZero() {
		t.Fatalf("metadata=%#v err=%v", metadata, err)
	}
}

func TestRuntimeRedactsCredentialMaterialEchoedByProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`token=secret-access refresh=secret-refresh signing=secret-signing`))
	}))
	defer server.Close()
	credential := core.ActiveCredential{AccessToken: "secret-access", RefreshToken: "secret-refresh", Metadata: map[string]any{"webhook_secret": "secret-signing"}}
	runtime := Runtime{HTTPClient: server.Client()}
	request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	_, err := runtime.DoJSONWithCredentialMetadata(context.Background(), credential, request, nil)
	if err == nil || strings.Contains(err.Error(), "secret-access") || strings.Contains(err.Error(), "secret-refresh") || strings.Contains(err.Error(), "secret-signing") {
		t.Fatalf("unredacted error: %v", err)
	}
}

func TestEndpointRejectsUnsafeConnectionConfiguration(t *testing.T) {
	unsafe := []string{"http://jira.example", "https://user:pass@jira.example", "https://jira.example?redirect=http://localhost"}
	for _, value := range unsafe {
		_, err := Endpoint(core.ActiveCredential{Metadata: map[string]any{"base_url": value}}, "base_url", "https://safe.example")
		if err == nil {
			t.Fatalf("unsafe endpoint %q was accepted", value)
		}
	}
	endpoint, err := Endpoint(core.ActiveCredential{Metadata: map[string]any{"base_url": "https://jira.example/root/"}}, "base_url", "https://safe.example")
	if err != nil || endpoint.String() != "https://jira.example/root" {
		t.Fatalf("endpoint = %v, err = %v", endpoint, err)
	}
}
