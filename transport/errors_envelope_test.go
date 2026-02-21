package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-services/core"
)

func TestRESTAdapter_ResponseLimitReturnsRichError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("12345"))
	}))
	defer server.Close()

	adapter := NewRESTAdapter(server.Client())
	adapter.MaxResponseBodyBytes = 4

	_, err := adapter.Do(context.Background(), core.TransportRequest{Method: http.MethodGet, URL: server.URL})
	if err == nil {
		t.Fatalf("expected response body limit error")
	}

	var rich *goerrors.Error
	if !goerrors.As(err, &rich) {
		t.Fatalf("expected go-errors envelope, got %T", err)
	}
	if rich.Category != goerrors.CategoryExternal {
		t.Fatalf("expected external category, got %q", rich.Category)
	}
	if rich.TextCode != core.ServiceErrorExternalFailure {
		t.Fatalf("expected %q text code, got %q", core.ServiceErrorExternalFailure, rich.TextCode)
	}
	if rich.Code != http.StatusBadGateway {
		t.Fatalf("expected %d code, got %d", http.StatusBadGateway, rich.Code)
	}
}

func TestProtocolAdapter_NilReturnsRichError(t *testing.T) {
	var adapter *ProtocolHTTPAdapter
	_, err := adapter.Do(context.Background(), core.TransportRequest{})
	if err == nil {
		t.Fatalf("expected protocol adapter nil error")
	}

	var rich *goerrors.Error
	if !goerrors.As(err, &rich) {
		t.Fatalf("expected go-errors envelope, got %T", err)
	}
	if rich.Category != goerrors.CategoryInternal {
		t.Fatalf("expected internal category, got %q", rich.Category)
	}
	if rich.TextCode != core.ServiceErrorInternal {
		t.Fatalf("expected %q text code, got %q", core.ServiceErrorInternal, rich.TextCode)
	}
	if rich.Code != http.StatusInternalServerError {
		t.Fatalf("expected %d code, got %d", http.StatusInternalServerError, rich.Code)
	}
}
