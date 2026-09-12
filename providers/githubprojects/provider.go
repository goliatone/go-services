package githubprojects

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/identity"
	"github.com/goliatone/go-services/providers"
	"github.com/goliatone/go-services/providers/tracker"
)

const (
	ProviderID        = "github_projects_v2"
	defaultGraphQLURL = "https://api.github.com/graphql"
)

type Config struct {
	ClientID       string
	ClientSecret   string
	AuthURL        string
	TokenURL       string
	DefaultScopes  []string
	TrackerRuntime tracker.Runtime
}

type Provider struct {
	core.Provider
	runtime tracker.Runtime
}

func New(cfg Config) (core.Provider, error) {
	if cfg.AuthURL == "" {
		cfg.AuthURL = "https://github.com/login/oauth/authorize"
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = "https://github.com/login/oauth/access_token"
	}
	if len(cfg.DefaultScopes) == 0 {
		cfg.DefaultScopes = []string{"repo", "read:project"}
	}
	base, err := providers.NewOAuth2Provider(providers.OAuth2Config{ID: ProviderID, AuthURL: cfg.AuthURL, TokenURL: cfg.TokenURL, ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, DefaultScopes: cfg.DefaultScopes, ProfileResolver: identity.DefaultResolver(), SupportedScopeTypes: []string{"organization", "user"}, Capabilities: []core.CapabilityDescriptor{{Name: "projects.read", RequiredGrants: []string{"read:project"}, DeniedBehavior: core.CapabilityDeniedBehaviorBlock}}})
	if err != nil {
		return nil, err
	}
	return &Provider{Provider: base, runtime: cfg.TrackerRuntime}, nil
}

type projectNode struct {
	ID        string    `json:"id"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	UpdatedAt time.Time `json:"updatedAt"`
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
	query := `query($first:Int!,$after:String){viewer{projectsV2(first:$first,after:$after,orderBy:{field:UPDATED_AT,direction:DESC}){nodes{id number title url updatedAt}pageInfo{hasNextPage endCursor}}}}`
	var response tracker.GraphQLResponse[struct {
		Viewer struct {
			Projects struct {
				Nodes    []projectNode `json:"nodes"`
				PageInfo pageInfo      `json:"pageInfo"`
			} `json:"projectsV2"`
		} `json:"viewer"`
	}]
	if err := p.runtime.DoGraphQL(ctx, credential, endpoint, query, map[string]any{"first": tracker.Limit(input.Limit, 50, 100), "after": nullable(input.Cursor)}, &response); err != nil {
		return core.TrackerNodePage{}, err
	}
	if err := graphQLErrors(response.Errors); err != nil {
		return core.TrackerNodePage{}, err
	}
	projects := response.Data.Viewer.Projects
	items := make([]core.TrackerNode, 0, len(projects.Nodes))
	for _, project := range projects.Nodes {
		items = append(items, core.TrackerNode{ExternalID: project.ID, Type: "project_v2", Name: project.Title, Capabilities: []string{"items.read", "schema.read"}, NativeRevision: project.UpdatedAt.UTC().Format(time.RFC3339Nano), Metadata: map[string]any{"number": project.Number, "canonical_url": project.URL}})
	}
	return core.TrackerNodePage{Items: items, NextCursor: projects.PageInfo.EndCursor, HasMore: projects.PageInfo.HasNextPage, Revision: tracker.Revision(items)}, nil
}

func (p *Provider) DiscoverTrackerSchema(ctx context.Context, input core.TrackerDiscoveryRequest) (core.TrackerSchemaPage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerSchemaPage{}, err
	}
	if strings.TrimSpace(input.ResourceID) == "" {
		return core.TrackerSchemaPage{}, core.NewTrackerProviderError(core.TrackerErrorSchemaChanged, "GitHub project id is required for schema discovery", false, 0, nil)
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerSchemaPage{}, err
	}
	query := `query($id:ID!,$first:Int!,$after:String){node(id:$id){... on ProjectV2{updatedAt fields(first:$first,after:$after){nodes{... on ProjectV2FieldCommon{id name dataType}... on ProjectV2SingleSelectField{id name dataType options{id name}}... on ProjectV2IterationField{id name dataType configuration{iterations{id title}}}}pageInfo{hasNextPage endCursor}}}}}`
	var response tracker.GraphQLResponse[struct {
		Node struct {
			UpdatedAt time.Time `json:"updatedAt"`
			Fields    struct {
				Nodes []struct {
					ID            string                      `json:"id"`
					Name          string                      `json:"name"`
					DataType      string                      `json:"dataType"`
					Options       []struct{ ID, Name string } `json:"options"`
					Configuration struct {
						Iterations []struct{ ID, Title string } `json:"iterations"`
					} `json:"configuration"`
				} `json:"nodes"`
				PageInfo pageInfo `json:"pageInfo"`
			} `json:"fields"`
		} `json:"node"`
	}]
	if err := p.runtime.DoGraphQL(ctx, credential, endpoint, query, map[string]any{"id": input.ResourceID, "first": tracker.Limit(input.Limit, 50, 100), "after": nullable(input.Cursor)}, &response); err != nil {
		return core.TrackerSchemaPage{}, err
	}
	if err := graphQLErrors(response.Errors); err != nil {
		return core.TrackerSchemaPage{}, err
	}
	revision := response.Data.Node.UpdatedAt.UTC().Format(time.RFC3339Nano)
	items := make([]core.TrackerField, 0, len(response.Data.Node.Fields.Nodes))
	for _, field := range response.Data.Node.Fields.Nodes {
		options := make([]core.TrackerNode, 0, len(field.Options)+len(field.Configuration.Iterations))
		for _, option := range field.Options {
			options = append(options, core.TrackerNode{ExternalID: option.ID, Type: "option", Name: option.Name, NativeRevision: revision})
		}
		for _, option := range field.Configuration.Iterations {
			options = append(options, core.TrackerNode{ExternalID: option.ID, Type: "iteration", Name: option.Title, NativeRevision: revision})
		}
		items = append(items, core.TrackerField{ID: field.ID, Name: field.Name, Type: strings.ToLower(field.DataType), Options: options, NativeRevision: revision})
	}
	fields := response.Data.Node.Fields
	return core.TrackerSchemaPage{Items: items, NextCursor: fields.PageInfo.EndCursor, HasMore: fields.PageInfo.HasNextPage, Revision: revision}, nil
}

func (p *Provider) ListTrackerChanges(ctx context.Context, input core.TrackerChangesRequest) (core.TrackerChangePage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerChangePage{}, err
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerChangePage{}, err
	}
	query := `query($id:ID!,$first:Int!,$after:String){node(id:$id){... on ProjectV2{items(first:$first,after:$after){nodes{id updatedAt type content{... on Issue{id number title body state url updatedAt}... on PullRequest{id number title body state url updatedAt}... on DraftIssue{id title body updatedAt}}fieldValues(first:100){nodes{__typename ... on ProjectV2ItemFieldTextValue{text field{... on ProjectV2FieldCommon{id name}}}... on ProjectV2ItemFieldSingleSelectValue{name optionId field{... on ProjectV2FieldCommon{id name}}}... on ProjectV2ItemFieldDateValue{date field{... on ProjectV2FieldCommon{id name}}}... on ProjectV2ItemFieldNumberValue{number field{... on ProjectV2FieldCommon{id name}}}}}}pageInfo{hasNextPage endCursor}}}}}`
	var response tracker.GraphQLResponse[struct {
		Node struct {
			Items struct {
				Nodes []struct {
					ID          string          `json:"id"`
					UpdatedAt   time.Time       `json:"updatedAt"`
					Type        string          `json:"type"`
					Content     json.RawMessage `json:"content"`
					FieldValues json.RawMessage `json:"fieldValues"`
				} `json:"nodes"`
				PageInfo pageInfo `json:"pageInfo"`
			} `json:"items"`
		} `json:"node"`
	}]
	if err := p.runtime.DoGraphQL(ctx, credential, endpoint, query, map[string]any{"id": input.ResourceID, "first": tracker.Limit(input.Limit, 50, 100), "after": nullable(input.Cursor)}, &response); err != nil {
		return core.TrackerChangePage{}, err
	}
	if err := graphQLErrors(response.Errors); err != nil {
		return core.TrackerChangePage{}, err
	}
	observedAt := time.Now().UTC()
	items := make([]core.TrackerResource, 0, len(response.Data.Node.Items.Nodes))
	for _, item := range response.Data.Node.Items.Nodes {
		var content struct {
			ID, Title, Body, State, URL string
			Number                      int
			UpdatedAt                   time.Time
		}
		_ = json.Unmarshal(item.Content, &content)
		revision := item.UpdatedAt
		if content.UpdatedAt.After(revision) {
			revision = content.UpdatedAt
		}
		normalized := map[string]any{"title": content.Title, "body": content.Body, "state": content.State, "number": content.Number, "content_id": content.ID, "field_values": item.FieldValues}
		native := tracker.RawJSON(map[string]any{"id": item.ID, "type": item.Type, "content": item.Content, "field_values": item.FieldValues})
		items = append(items, core.TrackerResource{ProviderID: ProviderID, ResourceType: "project_item", ExternalID: item.ID, ProviderRevision: revision.UTC().Format(time.RFC3339Nano), CanonicalURL: canonicalURL(content.URL, item.ID), ObservedAt: observedAt, NormalizedFields: tracker.RawJSON(normalized), NativeExtension: native, SchemaRevision: "github.project-v2.v1"})
	}
	page := response.Data.Node.Items.PageInfo
	return core.TrackerChangePage{Items: items, NextCursor: page.EndCursor, HasMore: page.HasNextPage}, nil
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

func graphQLErrors(input []struct {
	Message string `json:"message"`
}) error {
	messages := make([]string, 0, len(input))
	for _, item := range input {
		messages = append(messages, item.Message)
	}
	return tracker.GraphQLError(messages)
}

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func canonicalURL(value, id string) string {
	if strings.HasPrefix(value, "https://") {
		return value
	}
	return "https://github.com/orgs/_/projects/0?item=" + id
}

var _ core.TrackerProvider = (*Provider)(nil)
