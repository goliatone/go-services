package devkit

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/goliatone/go-services/core"
)

type TransportScript struct {
	Response core.TransportResponse
	Err      error
}

type FakeTransportAdapter struct {
	mu       sync.Mutex
	kind     string
	scripts  []TransportScript
	requests []core.TransportRequest
}

func NewFakeTransportAdapter(kind string, scripts ...TransportScript) *FakeTransportAdapter {
	return &FakeTransportAdapter{
		kind:    strings.TrimSpace(strings.ToLower(kind)),
		scripts: append([]TransportScript(nil), scripts...),
	}
}

func (a *FakeTransportAdapter) Kind() string {
	if a == nil {
		return ""
	}
	return a.kind
}

func (a *FakeTransportAdapter) Do(_ context.Context, req core.TransportRequest) (core.TransportResponse, error) {
	if a == nil {
		return core.TransportResponse{}, fmt.Errorf("devkit: fake transport adapter is nil")
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	a.requests = append(a.requests, cloneTransportRequest(req))
	index := len(a.requests) - 1
	if index < len(a.scripts) {
		script := a.scripts[index]
		return cloneTransportResponse(script.Response), script.Err
	}
	if len(a.scripts) > 0 {
		last := a.scripts[len(a.scripts)-1]
		return cloneTransportResponse(last.Response), last.Err
	}
	return core.TransportResponse{
		StatusCode: 200,
		Headers:    map[string]string{},
		Metadata:   map[string]any{"kind": a.kind},
	}, nil
}

func (a *FakeTransportAdapter) Requests() []core.TransportRequest {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	out := make([]core.TransportRequest, 0, len(a.requests))
	for _, item := range a.requests {
		out = append(out, cloneTransportRequest(item))
	}
	return out
}

func cloneTransportRequest(in core.TransportRequest) core.TransportRequest {
	out := core.TransportRequest{
		Method:               in.Method,
		URL:                  in.URL,
		Headers:              map[string]string{},
		Query:                map[string]string{},
		Body:                 append([]byte(nil), in.Body...),
		Metadata:             map[string]any{},
		Timeout:              in.Timeout,
		MaxResponseBodyBytes: in.MaxResponseBodyBytes,
		Idempotency:          in.Idempotency,
	}
	for key, value := range in.Headers {
		out.Headers[key] = value
	}
	for key, value := range in.Query {
		out.Query[key] = value
	}
	for key, value := range in.Metadata {
		out.Metadata[key] = value
	}
	return out
}

func cloneTransportResponse(in core.TransportResponse) core.TransportResponse {
	out := core.TransportResponse{
		StatusCode: in.StatusCode,
		Headers:    map[string]string{},
		Body:       append([]byte(nil), in.Body...),
		Metadata:   map[string]any{},
	}
	for key, value := range in.Headers {
		out.Headers[key] = value
	}
	for key, value := range in.Metadata {
		out.Metadata[key] = value
	}
	return out
}

var _ core.TransportAdapter = (*FakeTransportAdapter)(nil)
