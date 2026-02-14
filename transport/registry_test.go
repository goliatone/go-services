package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-services/core"
)

type staticAdapter struct {
	kind string
}

func (a staticAdapter) Kind() string { return a.kind }

func (a staticAdapter) Do(context.Context, core.TransportRequest) (core.TransportResponse, error) {
	return core.TransportResponse{StatusCode: 200}, nil
}

func TestRegistry_RegisterGetAndListDeterministic(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(staticAdapter{kind: "graphql"}); err != nil {
		t.Fatalf("register graphql adapter: %v", err)
	}
	if err := registry.Register(staticAdapter{kind: "rest"}); err != nil {
		t.Fatalf("register rest adapter: %v", err)
	}

	if _, ok := registry.Get("rest"); !ok {
		t.Fatalf("expected rest adapter to be registered")
	}

	listed := registry.List()
	if len(listed) != 2 {
		t.Fatalf("expected 2 adapters, got %d", len(listed))
	}
	if listed[0].Kind() != "graphql" || listed[1].Kind() != "rest" {
		t.Fatalf("expected deterministic sorted order, got %q and %q", listed[0].Kind(), listed[1].Kind())
	}

	if err := registry.Register(staticAdapter{kind: "rest"}); err == nil {
		t.Fatalf("expected duplicate registration error")
	}
}

func TestRegistry_RegisterFactoryBuildsCustomAdapter(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterFactory("custom", func(config map[string]any) (core.TransportAdapter, error) {
		kind := strings.TrimSpace(fmt.Sprint(config["kind"]))
		if kind == "" {
			kind = "custom"
		}
		return staticAdapter{kind: kind}, nil
	}); err != nil {
		t.Fatalf("register adapter factory: %v", err)
	}

	adapter, err := registry.Build("custom", map[string]any{"kind": "bulk"})
	if err != nil {
		t.Fatalf("build adapter from factory: %v", err)
	}
	if adapter.Kind() != "bulk" {
		t.Fatalf("expected bulk adapter from factory, got %q", adapter.Kind())
	}
}

func TestRESTAdapter_DoSendsMethodHeadersAndQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST method, got %s", r.Method)
		}
		if got := r.URL.Query().Get("q"); got != "search" {
			t.Fatalf("expected query value, got %q", got)
		}
		if got := r.Header.Get("X-Test"); got != "value" {
			t.Fatalf("expected header value, got %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if string(body) != "payload" {
			t.Fatalf("expected request body payload")
		}
		w.Header().Set("X-Server", "ok")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("done"))
	}))
	defer server.Close()

	adapter := NewRESTAdapter(server.Client())
	result, err := adapter.Do(context.Background(), core.TransportRequest{
		Method: "POST",
		URL:    server.URL,
		Query:  map[string]string{"q": "search"},
		Headers: map[string]string{
			"X-Test": "value",
		},
		Body:    []byte("payload"),
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("perform rest request: %v", err)
	}
	if result.StatusCode != http.StatusAccepted {
		t.Fatalf("expected accepted status, got %d", result.StatusCode)
	}
	if string(result.Body) != "done" {
		t.Fatalf("unexpected response body: %q", string(result.Body))
	}
	if result.Headers["X-Server"] != "ok" {
		t.Fatalf("expected response header")
	}
}

func TestNewRESTAdapter_DefaultClientTimeout(t *testing.T) {
	adapter := NewRESTAdapter(nil)
	httpClient, ok := adapter.Client.(*http.Client)
	if !ok {
		t.Fatalf("expected default http client implementation")
	}
	if httpClient.Timeout != defaultRESTClientTimeout {
		t.Fatalf("expected default timeout %s, got %s", defaultRESTClientTimeout, httpClient.Timeout)
	}
	if adapter.MaxResponseBodyBytes != defaultRESTResponseBodyLimit {
		t.Fatalf("expected default response body limit %d, got %d", defaultRESTResponseBodyLimit, adapter.MaxResponseBodyBytes)
	}
}

func TestRESTAdapter_DoFailsOnResponseBodyOverLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("12345"))
	}))
	defer server.Close()

	adapter := NewRESTAdapter(server.Client())
	adapter.MaxResponseBodyBytes = 4

	_, err := adapter.Do(context.Background(), core.TransportRequest{
		Method: "GET",
		URL:    server.URL,
	})
	if err == nil {
		t.Fatalf("expected response body limit error")
	}
	if !strings.Contains(err.Error(), "response body exceeds limit") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRESTAdapter_RequestBodyLimitOverridesAdapterLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("12345"))
	}))
	defer server.Close()

	adapter := NewRESTAdapter(server.Client())
	adapter.MaxResponseBodyBytes = 1024

	_, err := adapter.Do(context.Background(), core.TransportRequest{
		Method:               "GET",
		URL:                  server.URL,
		MaxResponseBodyBytes: 4,
	})
	if err == nil {
		t.Fatalf("expected response body limit error")
	}
	if !strings.Contains(err.Error(), "response body exceeds limit of 4 bytes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGraphQLAdapter_UsesMetadataQueryAndVariables(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST method, got %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("expected json content type, got %q", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode graphql payload: %v", err)
		}
		if payload["query"] != "query Ping { ping }" {
			t.Fatalf("unexpected graphql query %v", payload["query"])
		}
		vars, ok := payload["variables"].(map[string]any)
		if !ok {
			t.Fatalf("expected variables in graphql payload")
		}
		if vars["id"] != "123" {
			t.Fatalf("expected variables id=123")
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"ping":"pong"}}`))
	}))
	defer server.Close()

	adapter := NewGraphQLAdapter(server.URL, server.Client())
	result, err := adapter.Do(context.Background(), core.TransportRequest{
		Metadata: map[string]any{
			"query":     "query Ping { ping }",
			"variables": map[string]any{"id": "123"},
		},
	})
	if err != nil {
		t.Fatalf("perform graphql request: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", result.StatusCode)
	}
	if !strings.Contains(string(result.Body), "pong") {
		t.Fatalf("unexpected graphql response body: %q", string(result.Body))
	}
	if result.Metadata["kind"] != KindGraphQL {
		t.Fatalf("expected graphql metadata kind")
	}
}

func TestGraphQLAdapter_ForwardsResponseBodyLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("12345"))
	}))
	defer server.Close()

	adapter := NewGraphQLAdapter(server.URL, server.Client())
	_, err := adapter.Do(context.Background(), core.TransportRequest{
		Metadata: map[string]any{
			"query": "query Ping { ping }",
		},
		MaxResponseBodyBytes: 4,
	})
	if err == nil {
		t.Fatalf("expected response body limit error")
	}
	if !strings.Contains(err.Error(), "response body exceeds limit of 4 bytes") {
		t.Fatalf("unexpected error: %v", err)
	}
}
