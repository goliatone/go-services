package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

const KindREST = "rest"

const defaultRESTClientTimeout = 30 * time.Second

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type RESTAdapter struct {
	Client         HTTPDoer
	DefaultHeaders map[string]string
}

func NewRESTAdapter(client HTTPDoer) *RESTAdapter {
	if client == nil {
		client = &http.Client{Timeout: defaultRESTClientTimeout}
	}
	return &RESTAdapter{
		Client:         client,
		DefaultHeaders: map[string]string{},
	}
}

func (*RESTAdapter) Kind() string {
	return KindREST
}

func (a *RESTAdapter) Do(ctx context.Context, req core.TransportRequest) (core.TransportResponse, error) {
	if a == nil || a.Client == nil {
		return core.TransportResponse{}, fmt.Errorf("transport: rest adapter requires an http client")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	method := strings.TrimSpace(strings.ToUpper(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	parsedURL, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil {
		return core.TransportResponse{}, err
	}
	if parsedURL.String() == "" {
		return core.TransportResponse{}, fmt.Errorf("transport: request url is required")
	}

	query := parsedURL.Query()
	for key, value := range req.Query {
		if strings.TrimSpace(key) == "" {
			continue
		}
		query.Set(strings.TrimSpace(key), strings.TrimSpace(value))
	}
	parsedURL.RawQuery = query.Encode()

	requestCtx := ctx
	cancel := func() {}
	if req.Timeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, req.Timeout)
	}
	defer cancel()

	httpReq, err := http.NewRequestWithContext(requestCtx, method, parsedURL.String(), bytes.NewReader(req.Body))
	if err != nil {
		return core.TransportResponse{}, err
	}
	for key, value := range a.DefaultHeaders {
		if strings.TrimSpace(key) == "" {
			continue
		}
		httpReq.Header.Set(strings.TrimSpace(key), strings.TrimSpace(value))
	}
	for key, value := range req.Headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		httpReq.Header.Set(strings.TrimSpace(key), strings.TrimSpace(value))
	}

	startedAt := time.Now().UTC()
	httpRes, err := a.Client.Do(httpReq)
	if err != nil {
		return core.TransportResponse{}, err
	}
	defer httpRes.Body.Close()

	body, err := io.ReadAll(httpRes.Body)
	if err != nil {
		return core.TransportResponse{}, err
	}

	return core.TransportResponse{
		StatusCode: httpRes.StatusCode,
		Headers:    flattenHeaders(httpRes.Header),
		Body:       body,
		Metadata: map[string]any{
			"duration_ms": time.Since(startedAt).Milliseconds(),
			"kind":        KindREST,
		},
	}, nil
}

func flattenHeaders(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}
	flat := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) == 0 {
			flat[key] = ""
			continue
		}
		flat[key] = strings.Join(values, ",")
	}
	return flat
}

var _ core.TransportAdapter = (*RESTAdapter)(nil)
