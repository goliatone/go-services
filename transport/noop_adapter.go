package transport

import (
	"context"
	"fmt"
	"strings"

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
		return core.TransportResponse{}, fmt.Errorf("transport: adapter is nil")
	}
	if a.reason != "" {
		return core.TransportResponse{}, fmt.Errorf(
			"transport: %s adapter is not configured: %s",
			a.kind,
			a.reason,
		)
	}
	return core.TransportResponse{}, fmt.Errorf(
		"transport: %s adapter is not configured",
		a.kind,
	)
}

var _ core.TransportAdapter = (*UnsupportedAdapter)(nil)
