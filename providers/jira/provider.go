package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/tracker"
)

const ProviderID = "jira"

type Config struct {
	TrackerRuntime tracker.Runtime
}

type Provider struct {
	tracker.StaticProvider
	runtime tracker.Runtime
}

func New(cfg Config) (core.Provider, error) {
	runtime := cfg.TrackerRuntime
	if runtime.Authenticate == nil {
		runtime.Authenticate = authenticate
	}
	return &Provider{StaticProvider: tracker.StaticProvider{ProviderID: ProviderID, Kind: core.AuthKindAPIKey, ScopeTypes: []string{"site"}, Grants: []core.CapabilityDescriptor{{Name: "issues.read", DeniedBehavior: core.CapabilityDeniedBehaviorBlock}, {Name: "webhooks.manage", DeniedBehavior: core.CapabilityDeniedBehaviorDegrade}}}, runtime: runtime}, nil
}

func (p *Provider) DiscoverTrackerScopes(ctx context.Context, input core.TrackerDiscoveryRequest) (core.TrackerNodePage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerNodePage{}, err
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerNodePage{}, err
	}
	offset, err := tracker.Offset(input.Cursor)
	if err != nil {
		return core.TrackerNodePage{}, err
	}
	limit := tracker.Limit(input.Limit, 50, 100)
	target := endpointPath(endpoint, "/rest/api/3/project/search")
	query := target.Query()
	query.Set("startAt", strconv.Itoa(offset))
	query.Set("maxResults", strconv.Itoa(limit))
	query.Set("orderBy", "key")
	target.RawQuery = query.Encode()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	var response struct {
		Values []struct {
			ID, Key, Name  string
			ProjectTypeKey string `json:"projectTypeKey"`
		} `json:"values"`
		StartAt, MaxResults, Total int
		IsLast                     bool `json:"isLast"`
	}
	if err := p.runtime.DoJSONWithCredential(ctx, credential, request, &response); err != nil {
		return core.TrackerNodePage{}, err
	}
	items := make([]core.TrackerNode, 0, len(response.Values))
	for _, project := range response.Values {
		items = append(items, core.TrackerNode{ExternalID: project.Key, Type: "project", Name: project.Name, Capabilities: []string{"issues.read", "schema.read", "webhook"}, NativeRevision: tracker.Revision(project), Metadata: map[string]any{"id": project.ID, "project_type": project.ProjectTypeKey}})
	}
	hasMore := !response.IsLast && offset+len(response.Values) < response.Total
	next := ""
	if hasMore {
		next = strconv.Itoa(offset + len(response.Values))
	}
	return core.TrackerNodePage{Items: items, NextCursor: next, HasMore: hasMore, Revision: tracker.Revision(items)}, nil
}

func (p *Provider) DiscoverTrackerSchema(ctx context.Context, input core.TrackerDiscoveryRequest) (core.TrackerSchemaPage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerSchemaPage{}, err
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerSchemaPage{}, err
	}
	offset, err := tracker.Offset(input.Cursor)
	if err != nil {
		return core.TrackerSchemaPage{}, err
	}
	limit := tracker.Limit(input.Limit, 50, 100)
	target := endpointPath(endpoint, "/rest/api/3/field/search")
	query := target.Query()
	query.Set("startAt", strconv.Itoa(offset))
	query.Set("maxResults", strconv.Itoa(limit))
	target.RawQuery = query.Encode()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	var response struct {
		Values []struct {
			ID, Name string
			Required bool
			Schema   struct{ Type, Items, System, Custom string }
		} `json:"values"`
		Total, StartAt, MaxResults int
		IsLast                     bool `json:"isLast"`
	}
	if err := p.runtime.DoJSONWithCredential(ctx, credential, request, &response); err != nil {
		return core.TrackerSchemaPage{}, err
	}
	items := make([]core.TrackerField, 0, len(response.Values))
	for _, field := range response.Values {
		fieldType := field.Schema.Type
		if field.Schema.Items != "" {
			fieldType += "<" + field.Schema.Items + ">"
		}
		items = append(items, core.TrackerField{ID: field.ID, Name: field.Name, Type: fieldType, Required: field.Required, NativeRevision: tracker.Revision(field), Metadata: map[string]any{"system": field.Schema.System, "custom": field.Schema.Custom}})
	}
	hasMore := !response.IsLast && offset+len(response.Values) < response.Total
	next := ""
	if hasMore {
		next = strconv.Itoa(offset + len(response.Values))
	}
	return core.TrackerSchemaPage{Items: items, NextCursor: next, HasMore: hasMore, Revision: tracker.Revision(items)}, nil
}

func (p *Provider) ListTrackerChanges(ctx context.Context, input core.TrackerChangesRequest) (core.TrackerChangePage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerChangePage{}, err
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerChangePage{}, err
	}
	target := endpointPath(endpoint, "/rest/api/3/search/jql")
	payload := map[string]any{"jql": fmt.Sprintf("project = %q ORDER BY updated ASC", input.ResourceID), "maxResults": tracker.Limit(input.Limit, 50, 100), "fields": []string{"*all"}}
	if strings.TrimSpace(input.Cursor) != "" {
		payload["nextPageToken"] = input.Cursor
	}
	request, _ := jsonRequest(ctx, http.MethodPost, target.String(), payload)
	var response struct {
		Issues        []json.RawMessage `json:"issues"`
		NextPageToken string            `json:"nextPageToken"`
		IsLast        bool              `json:"isLast"`
	}
	if err := p.runtime.DoJSONWithCredential(ctx, credential, request, &response); err != nil {
		return core.TrackerChangePage{}, err
	}
	observedAt := time.Now().UTC()
	items := make([]core.TrackerResource, 0, len(response.Issues))
	for _, raw := range response.Issues {
		var issue struct {
			ID, Key string
			Fields  struct {
				Summary     string
				Description json.RawMessage
				Status      json.RawMessage
				Assignee    json.RawMessage
				Reporter    json.RawMessage
				Labels      json.RawMessage
				Priority    json.RawMessage
				IssueType   json.RawMessage `json:"issuetype"`
				Updated     string
			}
		}
		if err := json.Unmarshal(raw, &issue); err != nil {
			return core.TrackerChangePage{}, core.NewTrackerProviderError(core.TrackerErrorExternal, "Jira issue payload is invalid", false, 0, err)
		}
		normalized := map[string]any{"key": issue.Key, "title": issue.Fields.Summary, "description": issue.Fields.Description, "status": issue.Fields.Status, "assignee": issue.Fields.Assignee, "reporter": issue.Fields.Reporter, "labels": issue.Fields.Labels, "priority": issue.Fields.Priority, "issue_type": issue.Fields.IssueType}
		items = append(items, core.TrackerResource{ProviderID: ProviderID, ResourceType: "issue", ExternalID: issue.ID, ProviderRevision: firstNonEmpty(issue.Fields.Updated, tracker.Revision(raw)), CanonicalURL: endpointPath(endpoint, "/browse/"+url.PathEscape(issue.Key)).String(), ObservedAt: observedAt, NormalizedFields: tracker.RawJSON(normalized), NativeExtension: raw, SchemaRevision: "jira.issue.v1"})
	}
	hasMore := !response.IsLast && response.NextPageToken != ""
	return core.TrackerChangePage{Items: items, NextCursor: response.NextPageToken, HasMore: hasMore}, nil
}

func (p *Provider) Subscribe(ctx context.Context, input core.SubscribeRequest) (core.SubscriptionResult, error) {
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.SubscriptionResult{}, err
	}
	target := endpointPath(endpoint, "/rest/api/3/webhook")
	payload := map[string]any{"url": input.CallbackURL, "webhooks": []map[string]any{{"events": []string{"jira:issue_created", "jira:issue_updated", "jira:issue_deleted"}, "jqlFilter": fmt.Sprintf("project = %q", input.ResourceID)}}}
	request, _ := jsonRequest(ctx, http.MethodPost, target.String(), payload)
	var response struct {
		WebhookRegistrationResult []struct {
			CreatedWebhookID int64 `json:"createdWebhookId"`
		} `json:"webhookRegistrationResult"`
	}
	if err := p.runtime.DoJSONWithCredential(ctx, credential, request, &response); err != nil {
		return core.SubscriptionResult{}, err
	}
	if len(response.WebhookRegistrationResult) == 0 || response.WebhookRegistrationResult[0].CreatedWebhookID == 0 {
		return core.SubscriptionResult{}, core.NewTrackerProviderError(core.TrackerErrorExternal, "Jira webhook creation failed", false, 0, nil)
	}
	id := strconv.FormatInt(response.WebhookRegistrationResult[0].CreatedWebhookID, 10)
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
	id, err := strconv.ParseInt(input.RemoteSubscriptionID, 10, 64)
	if err != nil {
		return core.NewTrackerProviderError(core.TrackerErrorExternal, "Jira webhook id is invalid", false, 0, err)
	}
	target := endpointPath(endpoint, "/rest/api/3/webhook")
	request, _ := jsonRequest(ctx, http.MethodDelete, target.String(), map[string]any{"webhookIds": []int64{id}})
	return p.runtime.DoJSONWithCredential(ctx, credential, request, nil)
}

func (p *Provider) resolve(ctx context.Context, connectionID string) (core.ActiveCredential, *url.URL, error) {
	credential, err := p.runtime.ResolveCredential(ctx, connectionID)
	if err != nil {
		return core.ActiveCredential{}, nil, err
	}
	configured, _ := credential.Metadata["base_url"].(string)
	if strings.TrimSpace(configured) == "" {
		return core.ActiveCredential{}, nil, core.NewTrackerProviderError(core.TrackerErrorCredentialRevoked, "Jira base_url credential metadata is required", false, 0, nil)
	}
	endpoint, err := tracker.Endpoint(credential, "base_url", configured)
	return credential, endpoint, err
}

func authenticate(_ context.Context, request *http.Request, credential core.ActiveCredential) error {
	token := strings.TrimSpace(credential.AccessToken)
	if token == "" {
		return core.NewTrackerProviderError(core.TrackerErrorCredentialRevoked, "Jira access token is empty", false, 0, nil)
	}
	if strings.EqualFold(credential.TokenType, "bearer") {
		request.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
	email, _ := credential.Metadata["email"].(string)
	if strings.TrimSpace(email) == "" {
		return core.NewTrackerProviderError(core.TrackerErrorCredentialRevoked, "Jira email credential metadata is required for API token authentication", false, 0, nil)
	}
	request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(email)+":"+token)))
	return nil
}

func jsonRequest(ctx context.Context, method, target string, payload any) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, target, strings.NewReader(string(tracker.RawJSON(payload))))
	if err == nil {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json")
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

func endpointPath(endpoint *url.URL, suffix string) *url.URL {
	target := *endpoint
	target.Path = strings.TrimRight(endpoint.Path, "/") + "/" + strings.TrimLeft(suffix, "/")
	return &target
}

var _ core.TrackerProvider = (*Provider)(nil)
var _ core.SubscribableProvider = (*Provider)(nil)
