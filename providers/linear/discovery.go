package linear

import (
	"context"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/tracker"
)

func (p *Provider) discoverProjects(ctx context.Context, input core.TrackerDiscoveryRequest) (core.TrackerNodePage, error) {
	if strings.TrimSpace(input.ResourceID) == "" {
		return core.TrackerNodePage{}, core.NewTrackerProviderError(core.TrackerErrorValidation, "Linear team id is required for project discovery", false, 0, nil)
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerNodePage{}, err
	}
	query := `query($team:String!,$first:Int!,$after:String){team(id:$team){id projects(first:$first,after:$after){nodes{id name updatedAt}pageInfo{hasNextPage endCursor}}}}`
	var response tracker.GraphQLResponse[struct {
		Team struct {
			ID       string `json:"id"`
			Projects struct {
				Nodes []struct {
					ID, Name  string
					UpdatedAt time.Time
				} `json:"nodes"`
				PageInfo pageInfo `json:"pageInfo"`
			} `json:"projects"`
		} `json:"team"`
	}]
	variables := map[string]any{"team": input.ResourceID, "first": tracker.Limit(input.Limit, 50, 100), "after": nullable(input.Cursor)}
	if err := p.doGraphQL(ctx, &credential, endpoint, query, variables, &response); err != nil {
		return core.TrackerNodePage{}, err
	}
	if err := graphqlErrors(response.Errors); err != nil {
		return core.TrackerNodePage{}, err
	}
	if response.Data.Team.ID != input.ResourceID {
		return core.TrackerNodePage{}, core.NewTrackerProviderError(core.TrackerErrorPermission, "Linear team projects are unavailable", false, 0, nil)
	}
	items := make([]core.TrackerNode, 0, len(response.Data.Team.Projects.Nodes))
	for _, project := range response.Data.Team.Projects.Nodes {
		items = append(items, core.TrackerNode{ExternalID: project.ID, Type: "project", Name: project.Name, ParentID: input.ResourceID, NativeRevision: project.UpdatedAt.UTC().Format(time.RFC3339Nano), Capabilities: []string{"issues.read"}})
	}
	page := response.Data.Team.Projects.PageInfo
	if page.HasNextPage && page.EndCursor == "" {
		return core.TrackerNodePage{}, core.NewTrackerProviderError(core.TrackerErrorCursorInvalid, "Linear project cursor is missing", false, 0, nil)
	}
	return core.TrackerNodePage{Items: items, NextCursor: page.EndCursor, HasMore: page.HasNextPage, Revision: tracker.Revision(items)}, nil
}

func (p *Provider) discoverSchema(ctx context.Context, input core.TrackerDiscoveryRequest) (core.TrackerSchemaPage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerSchemaPage{}, err
	}
	if strings.TrimSpace(input.Search) != "" {
		return core.TrackerSchemaPage{}, core.NewTrackerProviderError(core.TrackerErrorSearchUnsupported, "search is not supported for schema discovery", false, 0, nil)
	}
	if strings.TrimSpace(input.ResourceID) == "" {
		return core.TrackerSchemaPage{}, core.NewTrackerProviderError(core.TrackerErrorValidation, "Linear team id is required for schema discovery", false, 0, nil)
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerSchemaPage{}, err
	}
	query := `query($team:String!,$first:Int!,$statesAfter:String,$labelsAfter:String){team(id:$team){id updatedAt states(first:$first,after:$statesAfter){nodes{id name type color updatedAt}pageInfo{hasNextPage endCursor}}labels(first:$first,after:$labelsAfter){nodes{id name color updatedAt}pageInfo{hasNextPage endCursor}}}}`
	var statesAfter, labelsAfter string
	stateOptions := []core.TrackerNode{}
	labelOptions := []core.TrackerNode{}
	seenStates := map[string]bool{}
	seenLabels := map[string]bool{}
	schemaRevision := ""
	for range 100 {
		var response tracker.GraphQLResponse[struct {
			Team struct {
				ID        string    `json:"id"`
				UpdatedAt time.Time `json:"updatedAt"`
				States    struct {
					Nodes []struct {
						ID, Name, Type, Color string
						UpdatedAt             time.Time
					} `json:"nodes"`
					PageInfo pageInfo `json:"pageInfo"`
				} `json:"states"`
				Labels struct {
					Nodes []struct {
						ID, Name, Color string
						UpdatedAt       time.Time
					} `json:"nodes"`
					PageInfo pageInfo `json:"pageInfo"`
				} `json:"labels"`
			} `json:"team"`
		}]
		variables := map[string]any{"team": input.ResourceID, "first": 100, "statesAfter": nullable(statesAfter), "labelsAfter": nullable(labelsAfter)}
		if err := p.doGraphQL(ctx, &credential, endpoint, query, variables, &response); err != nil {
			return core.TrackerSchemaPage{}, err
		}
		if err := graphqlErrors(response.Errors); err != nil {
			return core.TrackerSchemaPage{}, err
		}
		team := response.Data.Team
		if team.ID != input.ResourceID || team.UpdatedAt.IsZero() {
			return core.TrackerSchemaPage{}, core.NewTrackerProviderError(core.TrackerErrorPermission, "Linear team schema is unavailable", false, 0, nil)
		}
		revision := team.UpdatedAt.UTC().Format(time.RFC3339Nano)
		if schemaRevision != "" && schemaRevision != revision {
			return core.TrackerSchemaPage{}, core.NewTrackerProviderError(core.TrackerErrorSchemaChanged, "Linear team schema changed during pagination", false, 0, nil)
		}
		schemaRevision = revision
		for _, state := range team.States.Nodes {
			if !seenStates[state.ID] {
				seenStates[state.ID] = true
				stateOptions = append(stateOptions, core.TrackerNode{ExternalID: state.ID, Type: "workflow_state", Name: state.Name, NativeRevision: state.UpdatedAt.UTC().Format(time.RFC3339Nano), Metadata: map[string]any{"category": state.Type, "color": state.Color}})
			}
		}
		for _, label := range team.Labels.Nodes {
			if !seenLabels[label.ID] {
				seenLabels[label.ID] = true
				labelOptions = append(labelOptions, core.TrackerNode{ExternalID: label.ID, Type: "label", Name: label.Name, NativeRevision: label.UpdatedAt.UTC().Format(time.RFC3339Nano), Metadata: map[string]any{"color": label.Color}})
			}
		}
		statePage, labelPage := team.States.PageInfo, team.Labels.PageInfo
		if !statePage.HasNextPage && !labelPage.HasNextPage {
			projects, err := p.discoverProjectOptions(ctx, input.ConnectionID, input.ResourceID)
			if err != nil {
				return core.TrackerSchemaPage{}, err
			}
			fields := []core.TrackerField{{ID: "title", Name: "Title", Type: "string", Required: true, NativeRevision: revision}, {ID: "description", Name: "Description", Type: "markdown", NativeRevision: revision}, {ID: "state", Name: "State", Type: "workflow_state", Required: true, Options: stateOptions, NativeRevision: revision}, {ID: "labels", Name: "Labels", Type: "list", Options: labelOptions, NativeRevision: revision}, {ID: "assignees", Name: "Assignees", Type: "user", NativeRevision: revision}, {ID: "project", Name: "Project", Type: "project", Options: projects, NativeRevision: revision}}
			snapshotRevision := tracker.Revision(fields)
			for i := range fields {
				fields[i].NativeRevision = snapshotRevision
			}
			return core.TrackerSchemaPage{Items: fields, Revision: snapshotRevision}, nil
		}
		if statePage.HasNextPage && (statePage.EndCursor == "" || statePage.EndCursor == statesAfter) || labelPage.HasNextPage && (labelPage.EndCursor == "" || labelPage.EndCursor == labelsAfter) {
			return core.TrackerSchemaPage{}, core.NewTrackerProviderError(core.TrackerErrorCursorInvalid, "Linear schema cursor did not advance", false, 0, nil)
		}
		if statePage.HasNextPage {
			statesAfter = statePage.EndCursor
		}
		if labelPage.HasNextPage {
			labelsAfter = labelPage.EndCursor
		}
	}
	return core.TrackerSchemaPage{}, core.NewTrackerProviderError(core.TrackerErrorExternal, "Linear schema exceeded the page limit", false, 0, nil)
}

func (p *Provider) discoverProjectOptions(ctx context.Context, connectionID, teamID string) ([]core.TrackerNode, error) {
	options := []core.TrackerNode{}
	seen := map[string]bool{}
	cursor := ""
	for range 100 {
		page, err := p.discoverProjects(ctx, core.TrackerDiscoveryRequest{ConnectionID: connectionID, ResourceType: "project", ResourceID: teamID, Cursor: cursor, Limit: 100})
		if err != nil {
			return nil, err
		}
		for _, project := range page.Items {
			if !seen[project.ExternalID] {
				seen[project.ExternalID] = true
				options = append(options, project)
			}
		}
		if !page.HasMore {
			return options, nil
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			return nil, core.NewTrackerProviderError(core.TrackerErrorCursorInvalid, "Linear project cursor did not advance", false, 0, nil)
		}
		cursor = page.NextCursor
	}
	return nil, core.NewTrackerProviderError(core.TrackerErrorExternal, "Linear projects exceeded the page limit", false, 0, nil)
}
