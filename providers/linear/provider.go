package linear

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/tracker"
)

const (
	ProviderID        = "linear"
	defaultGraphQLURL = "https://api.linear.app/graphql"
)

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
	return &Provider{StaticProvider: tracker.StaticProvider{ProviderID: ProviderID, Kind: core.AuthKindAPIKey, ScopeTypes: []string{"workspace"}, Grants: []core.CapabilityDescriptor{{Name: "issues.read", DeniedBehavior: core.CapabilityDeniedBehaviorBlock}, {Name: "webhooks.manage", DeniedBehavior: core.CapabilityDeniedBehaviorDegrade}}}, runtime: runtime}, nil
}

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

func (p *Provider) DiscoverTrackerScopes(ctx context.Context, input core.TrackerDiscoveryRequest) (core.TrackerNodePage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerNodePage{}, err
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerNodePage{}, err
	}
	query := `query($first:Int!,$after:String){teams(first:$first,after:$after){nodes{id key name updatedAt}pageInfo{hasNextPage endCursor}}}`
	var response tracker.GraphQLResponse[struct {
		Teams struct {
			Nodes []struct {
				ID, Key, Name string
				UpdatedAt     time.Time
			} `json:"nodes"`
			PageInfo pageInfo `json:"pageInfo"`
		} `json:"teams"`
	}]
	if err := p.runtime.DoGraphQL(ctx, credential, endpoint, query, map[string]any{"first": tracker.Limit(input.Limit, 50, 100), "after": nullable(input.Cursor)}, &response); err != nil {
		return core.TrackerNodePage{}, err
	}
	if err := graphqlErrors(response.Errors); err != nil {
		return core.TrackerNodePage{}, err
	}
	items := make([]core.TrackerNode, 0, len(response.Data.Teams.Nodes))
	for _, team := range response.Data.Teams.Nodes {
		items = append(items, core.TrackerNode{ExternalID: team.ID, Type: "team", Name: team.Name, Capabilities: []string{"issues.read", "schema.read", "webhook"}, NativeRevision: team.UpdatedAt.UTC().Format(time.RFC3339Nano), Metadata: map[string]any{"key": team.Key}})
	}
	page := response.Data.Teams.PageInfo
	return core.TrackerNodePage{Items: items, NextCursor: page.EndCursor, HasMore: page.HasNextPage, Revision: tracker.Revision(items)}, nil
}

func (p *Provider) DiscoverTrackerSchema(ctx context.Context, input core.TrackerDiscoveryRequest) (core.TrackerSchemaPage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerSchemaPage{}, err
	}
	if strings.TrimSpace(input.ResourceID) == "" {
		return core.TrackerSchemaPage{}, core.NewTrackerProviderError(core.TrackerErrorSchemaChanged, "Linear team id is required for schema discovery", false, 0, nil)
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerSchemaPage{}, err
	}
	query := `query($id:String!){team(id:$id){updatedAt states{nodes{id name type color updatedAt}}labels{nodes{id name color updatedAt}}}}`
	var response tracker.GraphQLResponse[struct {
		Team struct {
			UpdatedAt time.Time
			States    struct {
				Nodes []struct {
					ID, Name, Type, Color string
					UpdatedAt             time.Time
				} `json:"nodes"`
			}
			Labels struct {
				Nodes []struct {
					ID, Name, Color string
					UpdatedAt       time.Time
				} `json:"nodes"`
			}
		} `json:"team"`
	}]
	if err := p.runtime.DoGraphQL(ctx, credential, endpoint, query, map[string]any{"id": input.ResourceID}, &response); err != nil {
		return core.TrackerSchemaPage{}, err
	}
	if err := graphqlErrors(response.Errors); err != nil {
		return core.TrackerSchemaPage{}, err
	}
	revision := response.Data.Team.UpdatedAt.UTC().Format(time.RFC3339Nano)
	stateOptions := make([]core.TrackerNode, 0, len(response.Data.Team.States.Nodes))
	for _, state := range response.Data.Team.States.Nodes {
		stateOptions = append(stateOptions, core.TrackerNode{ExternalID: state.ID, Type: "workflow_state", Name: state.Name, NativeRevision: state.UpdatedAt.UTC().Format(time.RFC3339Nano), Metadata: map[string]any{"category": state.Type, "color": state.Color}})
	}
	labelOptions := make([]core.TrackerNode, 0, len(response.Data.Team.Labels.Nodes))
	for _, label := range response.Data.Team.Labels.Nodes {
		labelOptions = append(labelOptions, core.TrackerNode{ExternalID: label.ID, Type: "label", Name: label.Name, NativeRevision: label.UpdatedAt.UTC().Format(time.RFC3339Nano), Metadata: map[string]any{"color": label.Color}})
	}
	fields := []core.TrackerField{{ID: "title", Name: "Title", Type: "string", Required: true, NativeRevision: revision}, {ID: "description", Name: "Description", Type: "markdown", NativeRevision: revision}, {ID: "state", Name: "State", Type: "workflow_state", Required: true, Options: stateOptions, NativeRevision: revision}, {ID: "labels", Name: "Labels", Type: "list", Options: labelOptions, NativeRevision: revision}, {ID: "priority", Name: "Priority", Type: "number", NativeRevision: revision}, {ID: "assignee", Name: "Assignee", Type: "user", NativeRevision: revision}}
	return core.TrackerSchemaPage{Items: fields, Revision: tracker.Revision(fields)}, nil
}

func (p *Provider) ListTrackerChanges(ctx context.Context, input core.TrackerChangesRequest) (core.TrackerChangePage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerChangePage{}, err
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerChangePage{}, err
	}
	query := `query($team:String!,$first:Int!,$after:String){issues(filter:{team:{id:{eq:$team}}},first:$first,after:$after,orderBy:updatedAt){nodes{id identifier title description priority url createdAt updatedAt archivedAt state{id name type}assignee{id name email}labels{nodes{id name color}}team{id key name}}pageInfo{hasNextPage endCursor}}}`
	var response tracker.GraphQLResponse[struct {
		Issues struct {
			Nodes    []json.RawMessage `json:"nodes"`
			PageInfo pageInfo          `json:"pageInfo"`
		} `json:"issues"`
	}]
	if err := p.runtime.DoGraphQL(ctx, credential, endpoint, query, map[string]any{"team": input.ResourceID, "first": tracker.Limit(input.Limit, 50, 100), "after": nullable(input.Cursor)}, &response); err != nil {
		return core.TrackerChangePage{}, err
	}
	if err := graphqlErrors(response.Errors); err != nil {
		return core.TrackerChangePage{}, err
	}
	observedAt := time.Now().UTC()
	items := make([]core.TrackerResource, 0, len(response.Data.Issues.Nodes))
	for _, raw := range response.Data.Issues.Nodes {
		var issue struct {
			ID, Identifier, Title, Description, URL string
			Priority                                int
			UpdatedAt                               time.Time
			State                                   json.RawMessage
			Assignee                                json.RawMessage
			Labels                                  json.RawMessage
			Team                                    json.RawMessage
			ArchivedAt                              *time.Time
		}
		if err := json.Unmarshal(raw, &issue); err != nil {
			return core.TrackerChangePage{}, core.NewTrackerProviderError(core.TrackerErrorExternal, "Linear issue payload is invalid", false, 0, err)
		}
		normalized := map[string]any{"identifier": issue.Identifier, "title": issue.Title, "description": issue.Description, "priority": issue.Priority, "state": issue.State, "assignee": issue.Assignee, "labels": issue.Labels, "team": issue.Team, "archived_at": issue.ArchivedAt}
		items = append(items, core.TrackerResource{ProviderID: ProviderID, ResourceType: "issue", ExternalID: issue.ID, ProviderRevision: issue.UpdatedAt.UTC().Format(time.RFC3339Nano), CanonicalURL: issue.URL, ObservedAt: observedAt, NormalizedFields: tracker.RawJSON(normalized), NativeExtension: raw, SchemaRevision: "linear.issue.v1"})
	}
	page := response.Data.Issues.PageInfo
	return core.TrackerChangePage{Items: items, NextCursor: page.EndCursor, HasMore: page.HasNextPage}, nil
}

func (p *Provider) Subscribe(ctx context.Context, input core.SubscribeRequest) (core.SubscriptionResult, error) {
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.SubscriptionResult{}, err
	}
	query := `mutation($input:WebhookCreateInput!){webhookCreate(input:$input){success webhook{id enabled}}}`
	var response tracker.GraphQLResponse[struct {
		WebhookCreate struct {
			Success bool
			Webhook struct {
				ID      string
				Enabled bool
			}
		} `json:"webhookCreate"`
	}]
	variables := map[string]any{"input": map[string]any{"teamId": input.ResourceID, "url": input.CallbackURL, "resourceTypes": []string{"Issue"}}}
	if err := p.runtime.DoGraphQL(ctx, credential, endpoint, query, variables, &response); err != nil {
		return core.SubscriptionResult{}, err
	}
	if err := graphqlErrors(response.Errors); err != nil {
		return core.SubscriptionResult{}, err
	}
	if !response.Data.WebhookCreate.Success || response.Data.WebhookCreate.Webhook.ID == "" {
		return core.SubscriptionResult{}, core.NewTrackerProviderError(core.TrackerErrorExternal, "Linear webhook creation failed", false, 0, nil)
	}
	id := response.Data.WebhookCreate.Webhook.ID
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
	query := `mutation($id:String!){webhookDelete(id:$id){success}}`
	var response tracker.GraphQLResponse[struct {
		WebhookDelete struct{ Success bool } `json:"webhookDelete"`
	}]
	if err := p.runtime.DoGraphQL(ctx, credential, endpoint, query, map[string]any{"id": input.RemoteSubscriptionID}, &response); err != nil {
		return err
	}
	if err := graphqlErrors(response.Errors); err != nil {
		return err
	}
	if !response.Data.WebhookDelete.Success {
		return core.NewTrackerProviderError(core.TrackerErrorExternal, "Linear webhook deletion failed", false, 0, nil)
	}
	return nil
}

func (p *Provider) resolve(ctx context.Context, connectionID string) (core.ActiveCredential, string, error) {
	credential, err := p.runtime.ResolveCredential(ctx, connectionID)
	if err != nil {
		return core.ActiveCredential{}, "", err
	}
	endpoint, err := tracker.Endpoint(credential, "graphql_url", defaultGraphQLURL)
	if err != nil {
		return core.ActiveCredential{}, "", err
	}
	return credential, endpoint.String(), nil
}

func authenticate(_ context.Context, request *http.Request, credential core.ActiveCredential) error {
	token := strings.TrimSpace(credential.AccessToken)
	if token == "" {
		return core.NewTrackerProviderError(core.TrackerErrorCredentialRevoked, "Linear access token is empty", false, 0, nil)
	}
	if strings.EqualFold(strings.TrimSpace(credential.TokenType), "bearer") {
		token = "Bearer " + token
	}
	request.Header.Set("Authorization", token)
	return nil
}

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func graphqlErrors(input []struct {
	Message string `json:"message"`
}) error {
	messages := make([]string, 0, len(input))
	for _, item := range input {
		messages = append(messages, item.Message)
	}
	return tracker.GraphQLError(messages)
}

var _ core.TrackerProvider = (*Provider)(nil)
var _ core.SubscribableProvider = (*Provider)(nil)
