package transport

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-services/core"
)

const (
	KindSOAP   = "soap"
	KindBulk   = "bulk"
	KindStream = "stream"
	KindFile   = "file"
)

type UnsupportedAdapter struct {
	kind   string
	reason string
}

func NewUnsupportedAdapter(kind string, reason string) *UnsupportedAdapter {
	return &UnsupportedAdapter{
		kind:   strings.TrimSpace(strings.ToLower(kind)),
		reason: strings.TrimSpace(reason),
	}
}

func (a *UnsupportedAdapter) Kind() string {
	if a == nil {
		return ""
	}
	return a.kind
}

func (a *UnsupportedAdapter) Do(context.Context, core.TransportRequest) (core.TransportResponse, error) {
	if a == nil {
		return core.TransportResponse{}, transportError(
			"transport: adapter is nil",
			goerrors.CategoryInternal,
			http.StatusInternalServerError,
			map[string]any{"adapter": "unsupported"},
		)
	}
	if a.reason != "" {
		return core.TransportResponse{}, transportError(
			fmt.Sprintf("transport: %s adapter is not configured: %s", a.kind, a.reason),
			goerrors.CategoryOperation,
			http.StatusNotImplemented,
			map[string]any{"adapter": a.kind, "reason": a.reason},
		)
	}
	return core.TransportResponse{}, transportError(
		fmt.Sprintf("transport: %s adapter is not configured", a.kind),
		goerrors.CategoryOperation,
		http.StatusNotImplemented,
		map[string]any{"adapter": a.kind},
	)
}

var _ core.TransportAdapter = (*UnsupportedAdapter)(nil)
