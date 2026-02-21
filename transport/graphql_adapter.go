package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-services/core"
)

const KindGraphQL = "graphql"

type GraphQLAdapter struct {
	Endpoint string
	REST     *RESTAdapter
}

func NewGraphQLAdapter(endpoint string, client HTTPDoer) *GraphQLAdapter {
	return &GraphQLAdapter{
		Endpoint: strings.TrimSpace(endpoint),
		REST:     NewRESTAdapter(client),
	}
}

func (*GraphQLAdapter) Kind() string {
	return KindGraphQL
}

func (a *GraphQLAdapter) Do(ctx context.Context, req core.TransportRequest) (core.TransportResponse, error) {
	if a == nil || a.REST == nil {
		return core.TransportResponse{}, transportError(
			"transport: graphql adapter requires a rest adapter",
			goerrors.CategoryInternal,
			http.StatusInternalServerError,
			map[string]any{"adapter": KindGraphQL},
		)
	}

	endpoint := strings.TrimSpace(req.URL)
	if endpoint == "" {
		endpoint = a.Endpoint
	}
	if endpoint == "" {
		return core.TransportResponse{}, transportError(
			"transport: graphql endpoint is required",
			goerrors.CategoryBadInput,
			http.StatusBadRequest,
			map[string]any{"adapter": KindGraphQL},
		)
	}

	query, ok := readGraphQLQuery(req)
	if !ok {
		return core.TransportResponse{}, transportError(
			"transport: graphql query is required",
			goerrors.CategoryBadInput,
			http.StatusBadRequest,
			map[string]any{"adapter": KindGraphQL, "endpoint": endpoint},
		)
	}
	payload := map[string]any{"query": query}
	if operationName := readGraphQLOperationName(req.Metadata); operationName != "" {
		payload["operationName"] = operationName
	}
	if variables, ok := readGraphQLVariables(req.Metadata); ok {
		payload["variables"] = variables
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return core.TransportResponse{}, transportWrapError(
			err,
			goerrors.CategoryBadInput,
			"transport: marshal graphql payload",
			http.StatusBadRequest,
			map[string]any{"adapter": KindGraphQL, "endpoint": endpoint},
		)
	}

	headers := map[string]string{"Content-Type": "application/json"}
	for key, value := range req.Headers {
		headers[key] = value
	}

	response, err := a.REST.Do(ctx, core.TransportRequest{
		Method:               "POST",
		URL:                  endpoint,
		Headers:              headers,
		Body:                 body,
		Metadata:             req.Metadata,
		Timeout:              req.Timeout,
		MaxResponseBodyBytes: req.MaxResponseBodyBytes,
	})
	if err != nil {
		return core.TransportResponse{}, transportWrapError(
			err,
			goerrors.CategoryExternal,
			"transport: graphql request failed",
			http.StatusBadGateway,
			map[string]any{"adapter": KindGraphQL, "endpoint": endpoint},
		)
	}
	response.Metadata = ensureMetadata(response.Metadata)
	response.Metadata["kind"] = KindGraphQL
	return response, nil
}

func readGraphQLQuery(req core.TransportRequest) (string, bool) {
	if req.Metadata != nil {
		if query := strings.TrimSpace(fmt.Sprint(req.Metadata["query"])); query != "" && query != "<nil>" {
			return query, true
		}
	}
	if len(req.Body) == 0 {
		return "", false
	}
	query := strings.TrimSpace(string(req.Body))
	if query == "" {
		return "", false
	}
	return query, true
}

func readGraphQLOperationName(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	value := strings.TrimSpace(fmt.Sprint(metadata["operation_name"]))
	if value == "" || value == "<nil>" {
		return ""
	}
	return value
}

func readGraphQLVariables(metadata map[string]any) (map[string]any, bool) {
	if len(metadata) == 0 {
		return nil, false
	}
	value, ok := metadata["variables"]
	if !ok || value == nil {
		return nil, false
	}
	if typed, ok := value.(map[string]any); ok {
		if len(typed) == 0 {
			return map[string]any{}, true
		}
		cloned := make(map[string]any, len(typed))
		for key, item := range typed {
			cloned[key] = item
		}
		return cloned, true
	}
	return nil, false
}

func ensureMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return map[string]any{}
	}
	return metadata
}

var _ core.TransportAdapter = (*GraphQLAdapter)(nil)
