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

	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-services/core"
)

const KindREST = "rest"

const defaultRESTClientTimeout = 30 * time.Second
const defaultRESTResponseBodyLimit int64 = 10 << 20 // 10 MiB

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type RESTAdapter struct {
	Client               HTTPDoer
	DefaultHeaders       map[string]string
	MaxResponseBodyBytes int64
}

func NewRESTAdapter(client HTTPDoer) *RESTAdapter {
	if client == nil {
		client = &http.Client{Timeout: defaultRESTClientTimeout}
	}
	return &RESTAdapter{
		Client:               client,
		DefaultHeaders:       map[string]string{},
		MaxResponseBodyBytes: defaultRESTResponseBodyLimit,
	}
}

func (*RESTAdapter) Kind() string {
	return KindREST
}

func (a *RESTAdapter) Do(ctx context.Context, req core.TransportRequest) (core.TransportResponse, error) {
	if a == nil || a.Client == nil {
		return core.TransportResponse{}, transportError(
			"transport: rest adapter requires an http client",
			goerrors.CategoryInternal,
			http.StatusInternalServerError,
			map[string]any{"adapter": KindREST},
		)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	httpReq, cancel, err := a.buildRequest(ctx, req)
	if err != nil {
		return core.TransportResponse{}, err
	}
	defer cancel()

	startedAt := time.Now().UTC()
	httpRes, err := a.Client.Do(httpReq)
	if err != nil {
		return core.TransportResponse{}, transportWrapError(
			err,
			goerrors.CategoryExternal,
			"transport: execute http request",
			http.StatusBadGateway,
			map[string]any{"adapter": KindREST, "method": httpReq.Method, "url": httpReq.URL.String()},
		)
	}
	defer func() { _ = httpRes.Body.Close() }()

	body, err := readBoundedResponseBody(httpRes, resolveResponseBodyLimit(req.MaxResponseBodyBytes, a.MaxResponseBodyBytes))
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

func (a *RESTAdapter) buildRequest(
	ctx context.Context,
	req core.TransportRequest,
) (*http.Request, context.CancelFunc, error) {
	method := strings.TrimSpace(strings.ToUpper(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	parsedURL, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil {
		return nil, nil, transportWrapError(
			err, goerrors.CategoryBadInput, "transport: invalid request url", http.StatusBadRequest,
			map[string]any{"adapter": KindREST, "url": strings.TrimSpace(req.URL)},
		)
	}
	if parsedURL.String() == "" {
		return nil, nil, transportError(
			"transport: request url is required", goerrors.CategoryBadInput, http.StatusBadRequest,
			map[string]any{"adapter": KindREST},
		)
	}
	applyQuery(parsedURL, req.Query)
	requestCtx, cancel := requestContext(ctx, req.Timeout)
	httpReq, err := http.NewRequestWithContext(requestCtx, method, parsedURL.String(), bytes.NewReader(req.Body))
	if err != nil {
		cancel()
		return nil, nil, transportWrapError(
			err, goerrors.CategoryBadInput, "transport: create http request", http.StatusBadRequest,
			map[string]any{"adapter": KindREST, "method": method, "url": parsedURL.String()},
		)
	}
	applyHeaders(httpReq.Header, a.DefaultHeaders)
	applyHeaders(httpReq.Header, req.Headers)
	return httpReq, cancel, nil
}

func applyQuery(parsedURL *url.URL, values map[string]string) {
	query := parsedURL.Query()
	for key, value := range values {
		if strings.TrimSpace(key) != "" {
			query.Set(strings.TrimSpace(key), strings.TrimSpace(value))
		}
	}
	parsedURL.RawQuery = query.Encode()
}

func requestContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return ctx, func() {}
}

func applyHeaders(header http.Header, values map[string]string) {
	for key, value := range values {
		if strings.TrimSpace(key) != "" {
			header.Set(strings.TrimSpace(key), strings.TrimSpace(value))
		}
	}
}

func readBoundedResponseBody(response *http.Response, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, transportWrapError(
			err, goerrors.CategoryExternal, "transport: read response body", http.StatusBadGateway,
			map[string]any{"adapter": KindREST, "status_code": response.StatusCode},
		)
	}
	if int64(len(body)) > limit {
		return nil, transportError(
			fmt.Sprintf("transport: response body exceeds limit of %d bytes", limit),
			goerrors.CategoryExternal,
			http.StatusBadGateway,
			map[string]any{"adapter": KindREST, "status_code": response.StatusCode, "response_limit_b": limit},
		)
	}
	return body, nil
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

func resolveResponseBodyLimit(requestLimit int64, adapterLimit int64) int64 {
	if requestLimit > 0 {
		return requestLimit
	}
	if adapterLimit > 0 {
		return adapterLimit
	}
	return defaultRESTResponseBodyLimit
}

var _ core.TransportAdapter = (*RESTAdapter)(nil)
