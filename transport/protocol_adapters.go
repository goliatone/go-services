package transport

import (
	"context"
	"fmt"
	"strings"

	"github.com/goliatone/go-services/core"
)

type ProtocolHTTPAdapter struct {
	kind          string
	defaultMethod string
	defaultHeader map[string]string
	rest          *RESTAdapter
}

func NewSOAPAdapter(client HTTPDoer) *ProtocolHTTPAdapter {
	return newProtocolHTTPAdapter(KindSOAP, client, "POST", map[string]string{
		"Content-Type": "text/xml; charset=utf-8",
	})
}

func NewBulkAdapter(client HTTPDoer) *ProtocolHTTPAdapter {
	return newProtocolHTTPAdapter(KindBulk, client, "POST", map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
	})
}

func NewStreamAdapter(client HTTPDoer) *ProtocolHTTPAdapter {
	return newProtocolHTTPAdapter(KindStream, client, "GET", map[string]string{
		"Accept": "text/event-stream",
	})
}

func NewFileAdapter(client HTTPDoer) *ProtocolHTTPAdapter {
	return newProtocolHTTPAdapter(KindFile, client, "POST", map[string]string{
		"Content-Type": "application/octet-stream",
	})
}

func newProtocolHTTPAdapter(kind string, client HTTPDoer, defaultMethod string, defaultHeaders map[string]string) *ProtocolHTTPAdapter {
	return &ProtocolHTTPAdapter{
		kind:          strings.TrimSpace(strings.ToLower(kind)),
		defaultMethod: strings.TrimSpace(strings.ToUpper(defaultMethod)),
		defaultHeader: cloneHeaders(defaultHeaders),
		rest:          NewRESTAdapter(client),
	}
}

func (a *ProtocolHTTPAdapter) Kind() string {
	if a == nil {
		return ""
	}
	return a.kind
}

func (a *ProtocolHTTPAdapter) Do(ctx context.Context, req core.TransportRequest) (core.TransportResponse, error) {
	if a == nil || a.rest == nil {
		return core.TransportResponse{}, fmt.Errorf("transport: protocol adapter is nil")
	}
	resolved := req
	if strings.TrimSpace(resolved.Method) == "" {
		resolved.Method = a.defaultMethod
	}
	headers := cloneHeaders(a.defaultHeader)
	for key, value := range req.Headers {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		headers[trimmed] = strings.TrimSpace(value)
	}
	resolved.Headers = headers
	response, err := a.rest.Do(ctx, resolved)
	if err != nil {
		return core.TransportResponse{}, err
	}
	response.Metadata = cloneMetadata(response.Metadata)
	response.Metadata["kind"] = a.kind
	response.Metadata["protocol_adapter"] = a.kind
	return response, nil
}

func cloneHeaders(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		out[trimmed] = strings.TrimSpace(value)
	}
	return out
}

func cloneMetadata(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

var _ core.TransportAdapter = (*ProtocolHTTPAdapter)(nil)
