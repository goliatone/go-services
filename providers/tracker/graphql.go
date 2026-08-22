package tracker

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/goliatone/go-services/core"
)

type GraphQLResponse[T any] struct {
	Data   T `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (r Runtime) DoGraphQL(ctx context.Context, credential core.ActiveCredential, endpoint, query string, variables map[string]any, output any) error {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return core.NewTrackerProviderError(core.TrackerErrorExternal, "GraphQL request cannot be encoded", false, 0, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return core.NewTrackerProviderError(core.TrackerErrorExternal, "GraphQL request cannot be created", false, 0, err)
	}
	request.Header.Set("Content-Type", "application/json")
	return r.DoJSONWithCredential(ctx, credential, request, output)
}

func GraphQLError(messages []string) error {
	if len(messages) == 0 {
		return nil
	}
	return core.NewTrackerProviderError(core.TrackerErrorExternal, strings.Join(messages, "; "), false, 0, nil)
}
