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
	goerrors "github.com/goliatone/go-errors"
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

	method := strings.TrimSpace(strings.ToUpper(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	parsedURL, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil {
		return core.TransportResponse{}, transportWrapError(
			err,
			goerrors.CategoryBadInput,
			"transport: invalid request url",
			http.StatusBadRequest,
			map[string]any{"adapter": KindREST, "url": strings.TrimSpace(req.URL)},
		)
	}
	if parsedURL.String() == "" {
		return core.TransportResponse{}, transportError(
			"transport: request url is required",
			goerrors.CategoryBadInput,
			http.StatusBadRequest,
			map[string]any{"adapter": KindREST},
		)
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
		return core.TransportResponse{}, transportWrapError(
			err,
			goerrors.CategoryBadInput,
			"transport: create http request",
			http.StatusBadRequest,
			map[string]any{"adapter": KindREST, "method": method, "url": parsedURL.String()},
		)
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
		return core.TransportResponse{}, transportWrapError(
			err,
			goerrors.CategoryExternal,
			"transport: execute http request",
			http.StatusBadGateway,
			map[string]any{"adapter": KindREST, "method": method, "url": parsedURL.String()},
		)
	}
	defer httpRes.Body.Close()

	maxBodyBytes := resolveResponseBodyLimit(req.MaxResponseBodyBytes, a.MaxResponseBodyBytes)
	body, err := io.ReadAll(io.LimitReader(httpRes.Body, maxBodyBytes+1))
	if err != nil {
		return core.TransportResponse{}, transportWrapError(
			err,
			goerrors.CategoryExternal,
			"transport: read response body",
			http.StatusBadGateway,
			map[string]any{"adapter": KindREST, "status_code": httpRes.StatusCode},
		)
	}
	if int64(len(body)) > maxBodyBytes {
		return core.TransportResponse{}, transportError(
			fmt.Sprintf("transport: response body exceeds limit of %d bytes", maxBodyBytes),
			goerrors.CategoryExternal,
			http.StatusBadGateway,
			map[string]any{
				"adapter":          KindREST,
				"status_code":      httpRes.StatusCode,
				"response_limit_b": maxBodyBytes,
			},
		)
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
