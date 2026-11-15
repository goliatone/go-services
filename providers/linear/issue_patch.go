package linear

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/tracker"
)

type issuePatchState struct {
	ID, Name, Type string
	Team           publicationRef
	ArchivedAt     json.RawMessage
}

type issuePatchData struct {
	Organization publicationRef
	Team         struct {
		ID                    string
		Organization          publicationRef
		ArchivedAt            json.RawMessage
		AutoCloseParentIssues *bool
		AutoCloseChildIssues  *bool
		States                publicationPage[issuePatchState]
	}
	Issue struct {
		ID, Identifier, URL, Title                                 string
		Description, ArchivedAt, Project, ProjectMilestone, Parent json.RawMessage
		UpdatedAt                                                  time.Time
		Team, State                                                publicationRef
	}
}

func (p *Provider) InspectTrackerIssuePatch(ctx context.Context, input core.TrackerIssuePatchInspectRequest) (core.TrackerIssuePatchInspection, error) {
	_, _, inspection, err := p.issuePatchContext(ctx, input)
	return inspection, err
}

func (p *Provider) issuePatchContext(ctx context.Context, input core.TrackerIssuePatchInspectRequest) (core.ActiveCredential, string, core.TrackerIssuePatchInspection, error) {
	var out core.TrackerIssuePatchInspection
	if err := input.Validate(); err != nil {
		return core.ActiveCredential{}, "", out, err
	}
	credential, endpoint, err := p.resolve(ctx, input.Target.ConnectionID)
	if err != nil {
		return credential, endpoint, out, err
	}
	if credential.ConnectionID != input.Target.ConnectionID {
		return credential, endpoint, out, publicationError(core.TrackerErrorPermission, "Linear patch credential does not match the selected connection")
	}
	out, err = p.readIssuePatch(ctx, &credential, endpoint, input)
	return credential, endpoint, out, err
}

func (p *Provider) readIssuePatch(ctx context.Context, credential *core.ActiveCredential, endpoint string, input core.TrackerIssuePatchInspectRequest) (core.TrackerIssuePatchInspection, error) {
	var out core.TrackerIssuePatchInspection
	stateFields := ""
	if input.Field == "state" {
		stateFields = `autoCloseParentIssues autoCloseChildIssues states(first:100){nodes{id name type archivedAt team{id}}pageInfo{hasNextPage endCursor}}`
	}
	query := `query($team:String!,$id:String!){organization{id}team(id:$team){id organization{id}archivedAt ` + stateFields + `}issue(id:$id){id identifier url title description updatedAt archivedAt team{id}state{id}project{id}projectMilestone{id}parent{id}}}`
	var response tracker.GraphQLResponse[issuePatchData]
	err := p.doGraphQL(ctx, credential, endpoint, query, map[string]any{"team": input.Target.TeamID, "id": input.Target.IssueID}, &response)
	if err == nil {
		err = graphqlErrors(response.Errors)
	}
	if err != nil {
		return out, err
	}
	data := response.Data
	if credential.ConnectionID != input.Target.ConnectionID || data.Organization.ID != input.Target.AccountID || data.Team.Organization.ID != input.Target.AccountID || data.Team.ID != input.Target.TeamID || data.Issue.Team.ID != input.Target.TeamID {
		return out, publicationError(core.TrackerErrorPermission, "Linear issue is outside the selected connection/account/team")
	}
	if data.Issue.ID != input.Target.IssueID {
		return out, publicationError(core.TrackerErrorNotFound, "Linear exact issue identity is unavailable")
	}
	teamArchived, err := issuePatchArchived(data.Team.ArchivedAt)
	if err != nil {
		return out, err
	}
	if teamArchived {
		return out, publicationError(core.TrackerErrorPermission, "Linear selected team is archived")
	}
	archived, err := issuePatchArchived(data.Issue.ArchivedAt)
	if err != nil {
		return out, err
	}
	description, err := publicationDescription(data.Issue.Description)
	if err != nil {
		return out, err
	}
	project, err := publicationNullableID(data.Issue.Project)
	if err != nil {
		return out, err
	}
	milestone, err := publicationNullableID(data.Issue.ProjectMilestone)
	if err != nil {
		return out, err
	}
	parent, err := publicationNullableID(data.Issue.Parent)
	if err != nil {
		return out, err
	}
	canonical, err := url.Parse(data.Issue.URL)
	if err != nil || canonical.Scheme != "https" || canonical.Host != "linear.app" || canonical.User != nil || data.Issue.UpdatedAt.IsZero() {
		return out, publicationError(core.TrackerErrorUnavailable, "Linear issue URL or revision is unavailable")
	}
	out.Observation = core.TrackerIssuePatchObservation{Target: input.Target, DisplayID: data.Issue.Identifier, CanonicalURL: data.Issue.URL, Revision: data.Issue.UpdatedAt.UTC().Format(time.RFC3339Nano), ObservedAt: time.Now().UTC(), Title: data.Issue.Title, Description: &description, StateID: data.Issue.State.ID, Archived: &archived, ProjectID: &project, MilestoneID: &milestone, ParentID: &parent}
	out.Capability = core.TrackerFieldCapability{Field: input.Field, Operation: "patch", Granted: true}
	if input.Field == "state" {
		out.StateSchema = issuePatchSchema(data, input.Target.TeamID)
		if out.StateSchema == nil {
			out.Capability.Granted, out.Capability.Reason = false, "state_schema_unavailable"
		}
	}
	if !linearWriteGranted(*credential) {
		out.Capability.Granted, out.Capability.Reason = false, "credential_write_grant_unproven"
	}
	if archived {
		out.Capability.Granted, out.Capability.Reason = false, "issue_archived"
	}
	out.Capability.Revision = tracker.Revision(struct {
		Target        core.TrackerIssuePatchTarget
		Field, Reason string
		Granted       bool
	}{input.Target, input.Field, out.Capability.Reason, out.Capability.Granted})
	if err := out.ValidateFor(input); err != nil {
		return core.TrackerIssuePatchInspection{}, publicationError(core.TrackerErrorUnavailable, "Linear issue patch observation is incomplete")
	}
	return out, nil
}

func issuePatchArchived(raw json.RawMessage) (bool, error) {
	var value *time.Time
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return false, publicationError(core.TrackerErrorUnavailable, "Linear archive observation is missing or invalid")
	}
	return value != nil, nil
}

func issuePatchSchema(data issuePatchData, team string) *core.TrackerIssuePatchStateSchema {
	page := data.Team.States
	if !page.valid(core.TrackerIssuePatchMaxStates) || *page.PageInfo.HasNextPage || len(page.Nodes) == 0 {
		return nil
	}
	schema := &core.TrackerIssuePatchStateSchema{Complete: true, States: []core.TrackerNode{}, AutoCloseParentIssues: data.Team.AutoCloseParentIssues, AutoCloseChildIssues: data.Team.AutoCloseChildIssues}
	for _, state := range page.Nodes {
		archived, err := issuePatchArchived(state.ArchivedAt)
		if err != nil || archived || state.Team.ID != team {
			return nil
		}
		schema.States = append(schema.States, core.TrackerNode{ExternalID: state.ID, Name: state.Name, Type: state.Type, ParentID: team, NativeRevision: tracker.Revision(state)})
	}
	sort.Slice(schema.States, func(i, j int) bool { return schema.States[i].ExternalID < schema.States[j].ExternalID })
	schema.Revision = tracker.Revision(schema)
	return schema
}

func (p *Provider) PatchTrackerIssue(ctx context.Context, input core.TrackerIssuePatchRequest) (core.TrackerIssuePatchResult, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerIssuePatchResult{}, err
	}
	request := core.TrackerIssuePatchInspectRequest{Target: input.Target, Field: input.Field}
	credential, endpoint, inspection, err := p.issuePatchContext(ctx, request)
	if err != nil {
		return core.TrackerIssuePatchResult{}, err
	}
	if !inspection.Capability.Granted {
		code := core.TrackerErrorUnavailable
		if inspection.Capability.Reason == "credential_write_grant_unproven" {
			code = core.TrackerErrorPermission
		}
		return core.TrackerIssuePatchResult{}, publicationError(code, inspection.Capability.Reason)
	}
	current := inspection.Observation
	var desired, expected string
	_ = json.Unmarshal(input.Value, &desired)
	_ = json.Unmarshal(input.ExpectedValue, &expected)
	if issuePatchField(current, input.Field) == desired {
		return validatedIssuePatchResult(input, core.TrackerIssuePatchResult{Status: core.TrackerIssuePatchDesiredStateObserved, Observation: &current})
	}
	if current.Revision != input.ExpectedRevision || issuePatchField(current, input.Field) != expected {
		return validatedIssuePatchResult(input, core.TrackerIssuePatchResult{Status: core.TrackerIssuePatchConflict, ErrorCode: core.TrackerErrorConflict, Observation: &current})
	}
	field := input.Field
	if field == "state" {
		if inspection.StateSchema.Revision != input.ExpectedSchemaRevision {
			return validatedIssuePatchResult(input, core.TrackerIssuePatchResult{Status: core.TrackerIssuePatchConflict, ErrorCode: core.TrackerErrorSchemaChanged, Observation: &current})
		}
		if err := issuePatchStateAllowed(inspection.StateSchema, desired); err != nil {
			return core.TrackerIssuePatchResult{}, err
		}
		field = "stateId"
	}
	if !linearWriteGranted(credential) {
		return core.TrackerIssuePatchResult{}, publicationError(core.TrackerErrorPermission, "Linear patch write grant is unproven")
	}
	var response tracker.GraphQLResponse[struct {
		IssueUpdate struct {
			Success bool
			Issue   publicationRef
		}
	}]
	if rejected := p.publicationMutation(ctx, credential, endpoint, `mutation($id:String!,$input:IssueUpdateInput!){issueUpdate(id:$id,input:$input){success issue{id}}}`, map[string]any{"id": input.Target.IssueID, "input": map[string]any{field: desired}}, &response); rejected != nil {
		return core.TrackerIssuePatchResult{Status: rejected.Status, ErrorCode: rejected.ErrorCode, RetryAfter: rejected.RetryAfter}, nil
	}
	if err = graphqlErrors(response.Errors); err != nil || !response.Data.IssueUpdate.Success {
		return issuePatchUnknown("issue_patch_unconfirmed", err), nil
	}
	if response.Data.IssueUpdate.Issue.ID != input.Target.IssueID {
		return issuePatchUnknown("issue_patch_identity_mismatch", nil), nil
	}
	// Read-back does not need state discovery; the mutation has already passed its schema guard.
	request.Field = "title"
	observed, err := p.readIssuePatch(ctx, &credential, endpoint, request)
	if err != nil {
		return issuePatchUnknown("issue_patch_readback_unavailable", err), nil
	}
	result := core.TrackerIssuePatchResult{Status: core.TrackerIssuePatchUnknown, ErrorCode: "issue_patch_readback_mismatch", Observation: &observed.Observation}
	if *observed.Observation.Archived || issuePatchField(observed.Observation, input.Field) != desired {
		return validatedIssuePatchResult(input, result)
	}
	result.Status, result.ErrorCode = core.TrackerIssuePatchConfirmed, ""
	result.Receipt = publicationReceipt(core.TrackerPublicationScope{TeamID: input.Target.TeamID}, input.Execution, input.Target.IssueID, observed.Observation.DisplayID, observed.Observation.CanonicalURL, observed.Observation.Revision, core.TrackerIssuePatchConfirmed)
	return validatedIssuePatchResult(input, result)
}

func issuePatchStateAllowed(schema *core.TrackerIssuePatchStateSchema, id string) error {
	for _, state := range schema.States {
		if state.ExternalID != id {
			continue
		}
		switch state.Type {
		case "triage", "backlog", "unstarted", "started":
			return nil
		case "completed", "canceled":
			if schema.AutoCloseParentIssues == nil || *schema.AutoCloseParentIssues || schema.AutoCloseChildIssues == nil || *schema.AutoCloseChildIssues {
				return publicationError(core.TrackerErrorUnavailable, "Linear terminal state requires both auto-close settings explicitly disabled")
			}
			return nil
		default:
			return publicationError(core.TrackerErrorUnavailable, "Linear state requires an unsupported workflow transition")
		}
	}
	return publicationError(core.TrackerErrorValidation, "Linear patch state is outside the selected team's schema")
}

func issuePatchField(observation core.TrackerIssuePatchObservation, field string) string {
	switch field {
	case "description":
		return *observation.Description
	case "state":
		return observation.StateID
	default:
		return observation.Title
	}
}

func issuePatchUnknown(code string, err error) core.TrackerIssuePatchResult {
	result := publicationUnknown(code, err)
	return core.TrackerIssuePatchResult{Status: core.TrackerIssuePatchUnknown, ErrorCode: result.ErrorCode, RetryAfter: result.RetryAfter}
}

func validatedIssuePatchResult(input core.TrackerIssuePatchRequest, result core.TrackerIssuePatchResult) (core.TrackerIssuePatchResult, error) {
	if result.ValidateFor(input) != nil {
		return issuePatchUnknown("issue_patch_result_invalid", nil), nil
	}
	return result, nil
}

var _ core.TrackerIssuePatchProvider = (*Provider)(nil)
