package linear

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/goliatone/go-services/core"
	trackerruntime "github.com/goliatone/go-services/providers/tracker"
)

type linearRoundTrip func(*http.Request) (*http.Response, error)

func (f linearRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestLinearOAuthClientCredentialRefreshesAfterUnauthorized(t *testing.T) {
	var exchanges atomic.Int32
	client := &http.Client{Transport: linearRoundTrip(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == linearOAuthTokenURL {
			exchanges.Add(1)
			if err := request.ParseForm(); err != nil || request.PostForm.Get("client_secret") != "secret-1" || request.PostForm.Get("scope") != "read,write" {
				t.Fatalf("invalid token request: %v", err)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"access_token":"token-` + string(rune('0'+exchanges.Load())) + `","token_type":"Bearer","scope":"read write","expires_in":3600}`))}, nil
		}
		if request.URL.String() != defaultGraphQLURL {
			t.Fatalf("unexpected request target: %s", request.URL)
		}
		if request.Header.Get("Authorization") == "Bearer token-1" {
			return &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`unauthorized`))}, nil
		}
		if request.Header.Get("Authorization") != "Bearer token-2" {
			t.Fatalf("wrong GraphQL token: %s", request.Header.Get("Authorization"))
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":{"viewer":{"id":"viewer-1"},"team":{"id":"team-1"}}}`))}, nil
	})}
	providerValue, err := New(Config{TrackerRuntime: trackerruntime.Runtime{HTTPClient: client, Credentials: linearCredentialResolver(func(_ context.Context, id string) (core.ActiveCredential, error) {
		return core.ActiveCredential{ConnectionID: id, Metadata: map[string]any{"auth_type": "oauth_client", "client_id": "client-1", "client_secret": "secret-1"}}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	provider := providerValue.(*Provider)
	capabilities, err := provider.DiscoverTrackerFieldCapabilities(context.Background(), core.TrackerFieldCapabilityRequest{ConnectionID: "connection-1", RepositoryID: "team-1"})
	if err != nil || exchanges.Load() != 2 || len(capabilities) == 0 {
		t.Fatalf("refresh failed: exchanges=%d capabilities=%d err=%v", exchanges.Load(), len(capabilities), err)
	}
	_, err = provider.DiscoverTrackerFieldCapabilities(context.Background(), core.TrackerFieldCapabilityRequest{ConnectionID: "connection-1", RepositoryID: "team-1"})
	if err != nil || exchanges.Load() != 2 {
		t.Fatalf("fresh token was not reused: exchanges=%d err=%v", exchanges.Load(), err)
	}
}
