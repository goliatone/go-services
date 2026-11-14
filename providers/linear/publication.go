package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/tracker"
)

type publicationRef struct{ ID string }
type publicationPageInfo struct {
	HasNextPage *bool
	EndCursor   string
}
type publicationPage[T any] struct {
	Nodes    []T
	PageInfo *publicationPageInfo
}

func (p publicationPage[T]) valid(limit int) bool {
	return p.Nodes != nil && len(p.Nodes) <= limit && p.PageInfo != nil && p.PageInfo.HasNextPage != nil && (!*p.PageInfo.HasNextPage || p.PageInfo.EndCursor != "")
}

func publicationError(code, message string) error {
	return core.NewTrackerProviderError(code, message, false, 0, nil)
}

func (p *Provider) publicationContext(ctx context.Context, scope core.TrackerPublicationScope, assignee string, states bool) (core.ActiveCredential, string, core.TrackerPublicationDestination, error) {
	var out core.TrackerPublicationDestination
	if err := scope.Validate(); err != nil {
		return core.ActiveCredential{}, "", out, err
	}
	credential, endpoint, err := p.resolve(ctx, scope.ConnectionID)
	if err != nil {
		return credential, endpoint, out, err
	}
	stateFields := ""
	if states {
		stateFields = `states(first:100){nodes{id name type}pageInfo{hasNextPage endCursor}}`
	}
	query := `query($team:String!,$project:String!,$filter:TeamFilter!){organization{id}team(id:$team){id archivedAt organization{id}autoCloseParentIssues autoCloseChildIssues ` + stateFields + `}project(id:$project){id url archivedAt teams(first:1,filter:$filter){nodes{id}pageInfo{hasNextPage endCursor}}}}`
	var response tracker.GraphQLResponse[struct {
		Organization publicationRef
		Team         struct {
			ID                    string
			ArchivedAt            *time.Time
			Organization          publicationRef
			AutoCloseParentIssues *bool
			AutoCloseChildIssues  *bool
			States                publicationPage[struct{ ID, Name, Type string }]
		}
		Project struct {
			ID, URL    string
			ArchivedAt *time.Time
			Teams      publicationPage[publicationRef]
		}
	}]
	err = p.doGraphQL(ctx, &credential, endpoint, query, map[string]any{"team": scope.TeamID, "project": scope.ProjectID, "filter": map[string]any{"id": map[string]any{"eq": scope.TeamID}}}, &response)
	if err == nil {
		err = graphqlErrors(response.Errors)
	}
	if err != nil {
		return credential, endpoint, out, err
	}
	data := response.Data
	if data.Organization.ID != scope.AccountID || data.Team.ID != scope.TeamID || data.Team.Organization.ID != scope.AccountID || data.Project.ID != scope.ProjectID || data.Team.ArchivedAt != nil || data.Project.ArchivedAt != nil || !data.Project.Teams.valid(1) || *data.Project.Teams.PageInfo.HasNextPage || len(data.Project.Teams.Nodes) != 1 || data.Project.Teams.Nodes[0].ID != scope.TeamID {
		return credential, endpoint, out, publicationError(core.TrackerErrorPermission, "Linear publication destination is inaccessible or outside the selected account/team/project")
	}
	if states && (!data.Team.States.valid(100) || *data.Team.States.PageInfo.HasNextPage) {
		return credential, endpoint, out, publicationError(core.TrackerErrorUnavailable, "Linear publication state discovery is incomplete")
	}
	out = core.TrackerPublicationDestination{Scope: scope, ProjectURL: data.Project.URL, AutoCloseParentIssues: data.Team.AutoCloseParentIssues, AutoCloseChildIssues: data.Team.AutoCloseChildIssues}
	if states {
		seen := map[string]bool{}
		for _, state := range data.Team.States.Nodes {
			if state.ID == "" || state.Name == "" || state.Type == "" || seen[state.ID] {
				return credential, endpoint, core.TrackerPublicationDestination{}, publicationError(core.TrackerErrorUnavailable, "Linear publication state identity is incomplete")
			}
			seen[state.ID] = true
			out.States = append(out.States, core.TrackerNode{ExternalID: state.ID, Name: state.Name, Type: state.Type, ParentID: scope.TeamID, NativeRevision: tracker.Revision(state)})
		}
	}
	if assignee != "" {
		query = `query($team:String!,$filter:UserFilter!){team(id:$team){id members(first:1,filter:$filter){nodes{id name app active isAssignable organization{id}}pageInfo{hasNextPage endCursor}}}}`
		var users tracker.GraphQLResponse[struct {
			Team struct {
				ID      string
				Members publicationPage[struct {
					ID, Name                  string
					App, Active, IsAssignable *bool
					Organization              publicationRef
				}]
			}
		}]
		err = p.doGraphQL(ctx, &credential, endpoint, query, map[string]any{"team": scope.TeamID, "filter": map[string]any{"id": map[string]any{"eq": assignee}}}, &users)
		if err == nil {
			err = graphqlErrors(users.Errors)
		}
		if err != nil {
			return credential, endpoint, core.TrackerPublicationDestination{}, err
		}
		members := users.Data.Team.Members
		if users.Data.Team.ID != scope.TeamID || !members.valid(1) || *members.PageInfo.HasNextPage || len(members.Nodes) != 1 {
			return credential, endpoint, core.TrackerPublicationDestination{}, publicationError(core.TrackerErrorPermission, "Linear default assignee is not an accessible team member")
		}
		user := members.Nodes[0]
		if user.ID != assignee || user.Organization.ID != scope.AccountID || user.App == nil || *user.App || user.Active == nil || !*user.Active || user.IsAssignable == nil || !*user.IsAssignable {
			return credential, endpoint, core.TrackerPublicationDestination{}, publicationError(core.TrackerErrorPermission, "Linear default assignee must be an active assignable human")
		}
		out.AssigneeID, out.AssigneeName = user.ID, user.Name
	}
	out.WriteGranted = linearWriteGranted(credential)
	return credential, endpoint, out, nil
}

func (p *Provider) InspectTrackerPublication(ctx context.Context, input core.TrackerPublicationInspectRequest) (core.TrackerPublicationDestination, error) {
	_, _, out, err := p.publicationContext(ctx, input.Scope, input.AssigneeID, true)
	return out, err
}

func publicationState(destination core.TrackerPublicationDestination, id string) error {
	for _, state := range destination.States {
		if state.ExternalID == id {
			return nil
		}
	}
	return publicationError(core.TrackerErrorValidation, "Linear publication state is outside the selected team")
}

func publicationWrite(credential core.ActiveCredential) error {
	if !linearWriteGranted(credential) {
		return publicationError(core.TrackerErrorPermission, "Linear publication write grant is unproven")
	}
	return nil
}

func publicationReceipt(scope core.TrackerPublicationScope, execution core.TrackerPublicationExecution, id, display, url, revision, status string) *core.TrackerMutationReceipt {
	return &core.TrackerMutationReceipt{ProviderID: ProviderID, RepositoryID: scope.TeamID, ExternalID: id, DisplayID: display, CanonicalURL: url, ProviderRevision: revision, Status: status, CorrelationID: execution.CorrelationID, CausationID: execution.CausationID, Metadata: map[string]any{"consistency": "optimistic", "idempotency_key": execution.IdempotencyKey}}
}

var _ core.TrackerPublicationProvider = (*Provider)(nil)

func (p *Provider) publicationMutation(ctx context.Context, credential core.ActiveCredential, endpoint, query string, variables map[string]any, output any) *core.TrackerPublicationResult {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return &core.TrackerPublicationResult{Status: "failed", ErrorCode: core.TrackerErrorValidation}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return &core.TrackerPublicationResult{Status: "failed", ErrorCode: core.TrackerErrorValidation}
	}
	request.Header.Set("Content-Type", "application/json")
	runtime := p.runtime
	client, _ := runtime.HTTPClient.(*http.Client)
	if runtime.HTTPClient == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if client != nil {
		copyClient := *client
		copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		runtime.HTTPClient = &copyClient
	}
	metadata, err := runtime.DoJSONWithCredentialMetadata(ctx, credential, request, output)
	if err == nil {
		return nil
	}
	result := publicationUnknown("publication_mutation_unconfirmed", err)
	if !metadata.RequestDispatched || metadata.StatusCode == http.StatusUnauthorized || metadata.StatusCode == http.StatusForbidden || metadata.StatusCode == http.StatusTooManyRequests {
		result.Status = "failed"
	}
	return &result
}

func publicationDescription(raw json.RawMessage) (string, error) {
	var value *string
	if len(raw) == 0 {
		return "", publicationError(core.TrackerErrorUnavailable, "Linear description observation is missing")
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", publicationError(core.TrackerErrorUnavailable, "Linear description observation is invalid")
	}
	if value == nil {
		return "", nil
	}
	return *value, nil
}

func publicationNullableID(raw json.RawMessage) (string, error) {
	var value *publicationRef
	if len(raw) == 0 {
		return "", publicationError(core.TrackerErrorUnavailable, "Linear relationship observation is missing")
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", publicationError(core.TrackerErrorUnavailable, "Linear relationship observation is invalid")
	}
	if value == nil {
		return "", nil
	}
	if value.ID == "" {
		return "", publicationError(core.TrackerErrorUnavailable, "Linear relationship identity is missing")
	}
	return value.ID, nil
}
