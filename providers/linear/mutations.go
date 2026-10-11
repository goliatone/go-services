package linear

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/tracker"
)

var linearIssueFields = []string{"title", "description", "state", "labels", "assignees", "project"}

func (p *Provider) DiscoverTrackerFieldCapabilities(ctx context.Context, input core.TrackerFieldCapabilityRequest) ([]core.TrackerFieldCapability, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return nil, err
	}
	if err := p.requireTeam(ctx, &credential, endpoint, input.RepositoryID); err != nil {
		return nil, err
	}
	write := linearWriteGranted(credential)
	revision := tracker.Revision(map[string]any{"team": input.RepositoryID, "viewer": credential.Metadata["viewer_id"], "write": write})
	caps := make([]core.TrackerFieldCapability, 0, len(linearIssueFields)*3)
	for _, field := range linearIssueFields {
		for _, operation := range []string{"read", "create", "update"} {
			granted := operation == "read" || operation == "create" && write
			reason := ""
			if !granted {
				reason = "credential_write_grant_unproven"
				if operation == "update" {
					reason = "atomic_revision_guard_unavailable"
				}
			}
			caps = append(caps, core.TrackerFieldCapability{Field: field, Operation: operation, Granted: granted, Reason: reason, Revision: revision})
		}
	}
	return caps, nil
}

func linearWriteGranted(credential core.ActiveCredential) bool {
	for _, grant := range credential.GrantedScopes {
		switch strings.ToLower(strings.TrimSpace(grant)) {
		case "write":
			return true
		}
	}
	return false
}

func (p *Provider) requireTeam(ctx context.Context, credential *core.ActiveCredential, endpoint, teamID string) error {
	query := `query($team:String!){viewer{id}team(id:$team){id}}`
	var response tracker.GraphQLResponse[struct {
		Viewer struct {
			ID string `json:"id"`
		} `json:"viewer"`
		Team struct {
			ID string `json:"id"`
		} `json:"team"`
	}]
	if err := p.doGraphQL(ctx, credential, endpoint, query, map[string]any{"team": teamID}, &response); err != nil {
		return err
	}
	if err := graphqlErrors(response.Errors); err != nil {
		return err
	}
	if response.Data.Viewer.ID == "" || response.Data.Team.ID != teamID {
		return core.NewTrackerProviderError(core.TrackerErrorPermission, "Linear credential cannot access the selected team", false, 0, nil)
	}
	return nil
}

func (p *Provider) CreateTrackerIssue(ctx context.Context, input core.TrackerIssueCreateRequest) (core.TrackerMutationReceipt, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	fields, err := linearIssueInput(input.Fields)
	if err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	fields["teamId"] = input.RepositoryID
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	if !linearWriteGranted(credential) {
		return core.TrackerMutationReceipt{}, core.NewTrackerProviderError(core.TrackerErrorPermission, "Linear issue write grant is unproven", false, 0, nil)
	}
	if err := p.requireTeam(ctx, &credential, endpoint, input.RepositoryID); err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	if fields["stateId"] != nil || fields["labelIds"] != nil || fields["projectId"] != nil {
		schema, err := p.discoverSchema(ctx, core.TrackerDiscoveryRequest{ConnectionID: input.ConnectionID, ResourceType: "team", ResourceID: input.RepositoryID})
		if err != nil {
			return core.TrackerMutationReceipt{}, err
		}
		for _, candidate := range []struct{ input, field string }{{"stateId", "state"}, {"projectId", "project"}} {
			if id, ok := fields[candidate.input].(string); ok && !linearSchemaContains(schema.Items, candidate.field, id) {
				return core.TrackerMutationReceipt{}, core.NewTrackerProviderError(core.TrackerErrorValidation, "Linear issue field is outside the selected team", false, 0, nil)
			}
		}
		if ids, ok := fields["labelIds"].([]string); ok {
			for _, id := range ids {
				if !linearSchemaContains(schema.Items, "labels", id) {
					return core.TrackerMutationReceipt{}, core.NewTrackerProviderError(core.TrackerErrorValidation, "Linear issue field is outside the selected team", false, 0, nil)
				}
			}
		}
	}
	query := `mutation($input:IssueCreateInput!){issueCreate(input:$input){success issue{id identifier url updatedAt team{id}}}}`
	var response tracker.GraphQLResponse[struct {
		IssueCreate linearIssueMutation `json:"issueCreate"`
	}]
	if err := p.doGraphQL(ctx, &credential, endpoint, query, map[string]any{"input": fields}, &response); err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	if err := graphqlErrors(response.Errors); err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	if !response.Data.IssueCreate.Success {
		return core.TrackerMutationReceipt{}, core.NewTrackerProviderError(core.TrackerErrorExternal, "Linear issue creation was not confirmed", false, 0, nil)
	}
	return linearMutationReceipt(input.RepositoryID, response.Data.IssueCreate.Issue, input.CorrelationID, input.CausationID)
}

func linearSchemaContains(fields []core.TrackerField, fieldID, optionID string) bool {
	for _, field := range fields {
		if field.ID != fieldID {
			continue
		}
		for _, option := range field.Options {
			if option.ExternalID == optionID {
				return true
			}
		}
	}
	return false
}

func (p *Provider) UpdateTrackerIssue(ctx context.Context, input core.TrackerIssueUpdateRequest) (core.TrackerMutationReceipt, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	return core.TrackerMutationReceipt{}, core.NewTrackerProviderError(core.TrackerErrorUnavailable, "Linear issue updates require an atomic revision guard that the provider API does not expose", false, 0, nil)
}

type linearIssueMutation struct {
	Success bool                `json:"success"`
	Issue   linearMutationIssue `json:"issue"`
}

type linearMutationIssue struct {
	ID         string    `json:"id"`
	Identifier string    `json:"identifier"`
	URL        string    `json:"url"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Team       struct {
		ID string `json:"id"`
	} `json:"team"`
}

func linearIssueInput(fields map[string]json.RawMessage) (map[string]any, error) {
	input := make(map[string]any, len(fields))
	for field, raw := range fields {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, core.NewTrackerProviderError(core.TrackerErrorValidation, "Linear issue field is invalid JSON", false, 0, err)
		}
		switch field {
		case "title", "description":
			text, ok := value.(string)
			if !ok || field == "title" && strings.TrimSpace(text) == "" {
				return nil, core.NewTrackerProviderError(core.TrackerErrorValidation, "Linear issue text field is invalid", false, 0, nil)
			}
			input[field] = text
		case "state", "project":
			text, ok := value.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return nil, core.NewTrackerProviderError(core.TrackerErrorValidation, "Linear native field id is required", false, 0, nil)
			}
			if field == "state" {
				input["stateId"] = text
			} else {
				input["projectId"] = text
			}
		case "labels", "assignees":
			items, ok := value.([]any)
			if !ok || field == "assignees" && len(items) > 1 {
				return nil, core.NewTrackerProviderError(core.TrackerErrorValidation, "Linear issue collection is invalid", false, 0, nil)
			}
			ids := make([]string, 0, len(items))
			for _, item := range items {
				id, ok := item.(string)
				if !ok || strings.TrimSpace(id) == "" {
					return nil, core.NewTrackerProviderError(core.TrackerErrorValidation, "Linear issue collection id is invalid", false, 0, nil)
				}
				ids = append(ids, id)
			}
			if field == "labels" {
				input["labelIds"] = ids
			} else if len(ids) == 0 {
				input["assigneeId"] = nil
			} else {
				input["assigneeId"] = ids[0]
			}
		default:
			return nil, core.NewTrackerProviderError(core.TrackerErrorValidation, "unsupported Linear issue field", false, 0, nil)
		}
	}
	return input, nil
}

func linearMutationReceipt(teamID string, issue linearMutationIssue, correlation, causation string) (core.TrackerMutationReceipt, error) {
	if issue.Team.ID != teamID || issue.UpdatedAt.IsZero() {
		return core.TrackerMutationReceipt{}, core.NewTrackerProviderError(core.TrackerErrorConflict, "Linear issue response changed team or omitted revision", false, 0, nil)
	}
	receipt := core.TrackerMutationReceipt{ProviderID: ProviderID, RepositoryID: teamID, ExternalID: issue.ID, DisplayID: issue.Identifier, CanonicalURL: issue.URL, ProviderRevision: issue.UpdatedAt.UTC().Format(time.RFC3339Nano), Status: "succeeded", CorrelationID: correlation, CausationID: causation}
	return receipt, receipt.Validate()
}

var _ core.TrackerMutationProvider = (*Provider)(nil)
