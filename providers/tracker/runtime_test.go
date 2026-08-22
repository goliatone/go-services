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
