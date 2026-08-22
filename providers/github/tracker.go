package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	trackerruntime "github.com/goliatone/go-services/providers/tracker"
)

const defaultAPIURL = "https://api.github.com"

type Provider struct {
	core.Provider
	runtime trackerruntime.Runtime
}

type repository struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
	Updated  string `json:"updated_at"`
}

type issue struct {
	ID          int64           `json:"id"`
	Number      int             `json:"number"`
	Title       string          `json:"title"`
	Body        string          `json:"body"`
	State       string          `json:"state"`
	StateReason string          `json:"state_reason"`
	HTMLURL     string          `json:"html_url"`
	UpdatedAt   time.Time       `json:"updated_at"`
	User        json.RawMessage `json:"user"`
	Assignees   json.RawMessage `json:"assignees"`
	Labels      json.RawMessage `json:"labels"`
	Milestone   json.RawMessage `json:"milestone"`
	PullRequest json.RawMessage `json:"pull_request"`
}

func (p *Provider) DiscoverTrackerScopes(ctx context.Context, input core.TrackerDiscoveryRequest) (core.TrackerNodePage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerNodePage{}, err
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerNodePage{}, err
	}
	page, err := trackerruntime.Page(input.Cursor)
	if err != nil {
		return core.TrackerNodePage{}, err
	}
	limit := trackerruntime.Limit(input.Limit, 100, 100)
	requestURL := *endpoint
	requestURL.Path += "/user/repos"
	query := requestURL.Query()
	query.Set("affiliation", "owner,collaborator,organization_member")
	query.Set("sort", "updated")
	query.Set("per_page", strconv.Itoa(limit))
	query.Set("page", strconv.Itoa(page))
	requestURL.RawQuery = query.Encode()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	var response []repository
	if err := p.runtime.DoJSONWithCredential(ctx, credential, request, &response); err != nil {
		return core.TrackerNodePage{}, err
	}
	items := make([]core.TrackerNode, 0, len(response))
	for _, repo := range response {
		items = append(items, core.TrackerNode{ExternalID: repo.FullName, Type: "repository", Name: repo.FullName, Capabilities: []string{"issues.read", "webhook"}, NativeRevision: firstNonEmpty(repo.Updated, strconv.FormatInt(repo.ID, 10)), Metadata: map[string]any{"canonical_url": repo.HTMLURL}})
	}
	hasMore := len(response) == limit
	next := ""
	if hasMore {
		next = strconv.Itoa(page + 1)
	}
	return core.TrackerNodePage{Items: items, NextCursor: next, HasMore: hasMore, Revision: trackerruntime.Revision(items)}, nil
}

func (p *Provider) DiscoverTrackerSchema(_ context.Context, input core.TrackerDiscoveryRequest) (core.TrackerSchemaPage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerSchemaPage{}, err
	}
	fields := []core.TrackerField{
		{ID: "title", Name: "Title", Type: "string", Required: true, NativeRevision: "github.issue.v1"},
		{ID: "body", Name: "Body", Type: "markdown", NativeRevision: "github.issue.v1"},
		{ID: "state", Name: "State", Type: "string", Required: true, NativeRevision: "github.issue.v1"},
		{ID: "state_reason", Name: "State reason", Type: "string", NativeRevision: "github.issue.v1"},
		{ID: "labels", Name: "Labels", Type: "list", NativeRevision: "github.issue.v1"},
		{ID: "assignees", Name: "Assignees", Type: "list", NativeRevision: "github.issue.v1"},
		{ID: "milestone", Name: "Milestone", Type: "object", NativeRevision: "github.issue.v1"},
	}
	return core.TrackerSchemaPage{Items: fields, Revision: trackerruntime.Revision(fields)}, nil
}

func (p *Provider) ListTrackerChanges(ctx context.Context, input core.TrackerChangesRequest) (core.TrackerChangePage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerChangePage{}, err
	}
	if input.ResourceType != "repository" {
		return core.TrackerChangePage{}, core.NewTrackerProviderError(core.TrackerErrorSchemaChanged, "GitHub issues require a repository resource", false, 0, nil)
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerChangePage{}, err
	}
	page, err := trackerruntime.Page(input.Cursor)
	if err != nil {
		return core.TrackerChangePage{}, err
	}
	limit := trackerruntime.Limit(input.Limit, 100, 100)
	requestURL := *endpoint
	requestURL.Path += "/repos/" + escapePath(input.ResourceID) + "/issues"
	query := requestURL.Query()
	query.Set("state", "all")
	query.Set("sort", "updated")
	query.Set("direction", "asc")
	query.Set("per_page", strconv.Itoa(limit))
	query.Set("page", strconv.Itoa(page))
	requestURL.RawQuery = query.Encode()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	var response []issue
	if err := p.runtime.DoJSONWithCredential(ctx, credential, request, &response); err != nil {
		return core.TrackerChangePage{}, err
	}
	observedAt := time.Now().UTC()
	items := make([]core.TrackerResource, 0, len(response))
	for _, item := range response {
		if len(item.PullRequest) != 0 && string(item.PullRequest) != "null" {
			continue
		}
		normalized := map[string]any{"title": item.Title, "body": item.Body, "state": item.State, "state_reason": item.StateReason, "user": item.User, "assignees": item.Assignees, "labels": item.Labels, "milestone": item.Milestone}
		native, _ := json.Marshal(item)
		items = append(items, core.TrackerResource{ProviderID: ProviderID, ResourceType: "issue", ExternalID: strconv.Itoa(item.Number), ProviderRevision: item.UpdatedAt.UTC().Format(time.RFC3339Nano), CanonicalURL: item.HTMLURL, ObservedAt: observedAt, NormalizedFields: trackerruntime.RawJSON(normalized), NativeExtension: native, SchemaRevision: "github.issue.v1"})
	}
	hasMore := len(response) == limit
	next := ""
	if hasMore {
		next = strconv.Itoa(page + 1)
	}
	return core.TrackerChangePage{Items: items, NextCursor: next, HasMore: hasMore}, nil
}

func (p *Provider) Subscribe(ctx context.Context, input core.SubscribeRequest) (core.SubscriptionResult, error) {
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.SubscriptionResult{}, err
	}
	requestURL := *endpoint
	requestURL.Path += "/repos/" + escapePath(input.ResourceID) + "/hooks"
	payload := map[string]any{"name": "web", "active": true, "events": []string{"issues"}, "config": map[string]any{"url": input.CallbackURL, "content_type": "json", "insecure_ssl": "0"}}
	request, _ := jsonRequest(ctx, http.MethodPost, requestURL.String(), payload)
	var response struct {
		ID int64 `json:"id"`
	}
	if err := p.runtime.DoJSONWithCredential(ctx, credential, request, &response); err != nil {
		return core.SubscriptionResult{}, err
	}
	id := strconv.FormatInt(response.ID, 10)
	return core.SubscriptionResult{ChannelID: id, RemoteSubscriptionID: id}, nil
}

func (p *Provider) RenewSubscription(_ context.Context, input core.RenewSubscriptionRequest) (core.SubscriptionResult, error) {
	return core.SubscriptionResult{ChannelID: input.RemoteSubscriptionID, RemoteSubscriptionID: input.RemoteSubscriptionID}, nil
}

func (p *Provider) CancelSubscription(ctx context.Context, input core.CancelSubscriptionRequest) error {
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return err
	}
	requestURL := *endpoint
	requestURL.Path += "/repos/" + escapePath(input.ResourceID) + "/hooks/" + url.PathEscape(input.RemoteSubscriptionID)
	request, _ := http.NewRequestWithContext(ctx, http.MethodDelete, requestURL.String(), nil)
	return p.runtime.DoJSONWithCredential(ctx, credential, request, nil)
}

func (p *Provider) resolve(ctx context.Context, connectionID string) (core.ActiveCredential, *url.URL, error) {
	credential, err := p.runtime.ResolveCredential(ctx, connectionID)
	if err != nil {
		return core.ActiveCredential{}, nil, err
	}
	endpoint, err := trackerruntime.Endpoint(credential, "api_url", defaultAPIURL)
	return credential, endpoint, err
}

func escapePath(value string) string {
	parts := strings.Split(strings.Trim(value, "/"), "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return strings.Join(parts, "/")
}

func jsonRequest(ctx context.Context, method, target string, payload any) (*http.Request, error) {
	body := strings.NewReader(string(trackerruntime.RawJSON(payload)))
	request, err := http.NewRequestWithContext(ctx, method, target, body)
	if err == nil {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	return request, err
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "unknown"
}

var _ core.TrackerProvider = (*Provider)(nil)
var _ core.SubscribableProvider = (*Provider)(nil)
