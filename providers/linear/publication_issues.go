package linear

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/tracker"
)

const publicationIssueFields = `id identifier url title description updatedAt archivedAt team{id}project{id}projectMilestone{id}parent{id}assignee{id}state{id}children(first:1){nodes{id}pageInfo{hasNextPage endCursor}}`

type publicationIssue struct {
	ID, Identifier, URL, Title             string
	Description, Parent, Assignee          json.RawMessage
	UpdatedAt                              time.Time
	ArchivedAt                             *time.Time
	Team, Project, ProjectMilestone, State publicationRef
	Children                               publicationPage[publicationRef]
}

func (i publicationIssue) normalize(scope core.TrackerPublicationScope) (core.TrackerPublicationIssue, error) {
	description, err := publicationDescription(i.Description)
	if err != nil {
		return core.TrackerPublicationIssue{}, err
	}
	parent, err := publicationNullableID(i.Parent)
	if err != nil {
		return core.TrackerPublicationIssue{}, err
	}
	assignee, err := publicationNullableID(i.Assignee)
	if err != nil {
		return core.TrackerPublicationIssue{}, err
	}
	fields := core.TrackerPublicationFields{Title: i.Title, Description: description, StateID: i.State.ID}
	if i.ID == "" || i.Identifier == "" || i.URL == "" || i.UpdatedAt.IsZero() || i.ArchivedAt != nil || i.Team.ID != scope.TeamID || i.Project.ID != scope.ProjectID || i.ProjectMilestone.ID == "" || !i.Children.valid(1) {
		return core.TrackerPublicationIssue{}, publicationError(core.TrackerErrorConflict, "Linear publication issue scope or observation is incomplete")
	}
	if err := fields.Validate(); err != nil {
		return core.TrackerPublicationIssue{}, err
	}
	for _, child := range i.Children.Nodes {
		if child.ID == "" {
			return core.TrackerPublicationIssue{}, publicationError(core.TrackerErrorUnavailable, "Linear child identity is unavailable")
		}
	}
	return core.TrackerPublicationIssue{Scope: scope, ID: i.ID, DisplayID: i.Identifier, URL: i.URL, Revision: i.UpdatedAt.UTC().Format(time.RFC3339Nano), ParentID: parent, MilestoneID: i.ProjectMilestone.ID, Fields: fields, AssigneeID: assignee, HasChildren: len(i.Children.Nodes) > 0 || *i.Children.PageInfo.HasNextPage}, nil
}

func (p *Provider) readPublicationIssue(ctx context.Context, credential *core.ActiveCredential, endpoint string, input core.TrackerPublicationReadRequest) (core.TrackerPublicationIssue, error) {
	var response tracker.GraphQLResponse[struct{ Issue publicationIssue }]
	err := p.doGraphQL(ctx, credential, endpoint, `query($id:String!){issue(id:$id){`+publicationIssueFields+`}}`, map[string]any{"id": input.ID}, &response)
	if err == nil {
		err = graphqlErrors(response.Errors)
	}
	if err != nil {
		return core.TrackerPublicationIssue{}, err
	}
	if response.Data.Issue.ID != input.ID {
		return core.TrackerPublicationIssue{}, publicationError(core.TrackerErrorNotFound, "Linear issue exact identity is unavailable; absence does not authorize creation retry")
	}
	return response.Data.Issue.normalize(input.Scope)
}

func (p *Provider) ReadTrackerPublicationIssue(ctx context.Context, input core.TrackerPublicationReadRequest) (core.TrackerPublicationIssue, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerPublicationIssue{}, err
	}
	credential, endpoint, _, err := p.publicationContext(ctx, input.Scope, "", false)
	if err != nil {
		return core.TrackerPublicationIssue{}, err
	}
	return p.readPublicationIssue(ctx, &credential, endpoint, input)
}

func publicationUnknown(code string, err error) core.TrackerPublicationResult {
	result := core.TrackerPublicationResult{Status: "unknown", ErrorCode: code}
	var providerErr *core.TrackerProviderError
	if errors.As(err, &providerErr) {
		result.ErrorCode = providerErr.Code
		result.RetryAfter = providerErr.RetryAfter
	}
	return result
}

func (p *Provider) CreateTrackerPublicationIssue(ctx context.Context, input core.TrackerPublicationIssueCreateRequest) (core.TrackerPublicationResult, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerPublicationResult{}, err
	}
	credential, endpoint, destination, err := p.publicationContext(ctx, input.Scope, input.AssigneeID, true)
	if err == nil {
		err = publicationWrite(credential)
	}
	if err == nil {
		err = publicationState(destination, input.Fields.StateID)
	}
	if err != nil {
		return core.TrackerPublicationResult{}, err
	}
	if _, err = p.readPublicationMilestone(ctx, &credential, endpoint, core.TrackerPublicationReadRequest{Scope: input.Scope, ID: input.MilestoneID}); err != nil {
		return core.TrackerPublicationResult{}, err
	}
	if input.ParentID != "" {
		parent, err := p.readPublicationIssue(ctx, &credential, endpoint, core.TrackerPublicationReadRequest{Scope: input.Scope, ID: input.ParentID})
		if err != nil {
			return core.TrackerPublicationResult{}, err
		}
		if parent.MilestoneID != input.MilestoneID || parent.ParentID != "" {
			return core.TrackerPublicationResult{}, publicationError(core.TrackerErrorConflict, "Linear child requires a spec parent in the same milestone")
		}
	}
	if err = publicationWrite(credential); err != nil {
		return core.TrackerPublicationResult{}, err
	}
	fields := map[string]any{"id": input.RequestedID, "teamId": input.Scope.TeamID, "projectId": input.Scope.ProjectID, "projectMilestoneId": input.MilestoneID, "assigneeId": input.AssigneeID, "title": input.Fields.Title, "description": input.Fields.Description, "stateId": input.Fields.StateID}
	if input.ParentID != "" {
		fields["parentId"] = input.ParentID
	}
	var response tracker.GraphQLResponse[struct {
		IssueCreate struct {
			Success bool
			Issue   publicationRef
		}
	}]
	if rejected := p.publicationMutation(ctx, credential, endpoint, `mutation($input:IssueCreateInput!){issueCreate(input:$input){success issue{id}}}`, map[string]any{"input": fields}, &response); rejected != nil {
		return *rejected, nil
	}
	err = graphqlErrors(response.Errors)
	if err != nil || !response.Data.IssueCreate.Success {
		return publicationUnknown("issue_create_unconfirmed", err), nil
	}
	if response.Data.IssueCreate.Issue.ID != input.RequestedID {
		return publicationUnknown("issue_create_identity_mismatch", nil), nil
	}
	observed, err := p.readPublicationIssue(ctx, &credential, endpoint, core.TrackerPublicationReadRequest{Scope: input.Scope, ID: input.RequestedID})
	if err != nil {
		return publicationUnknown("issue_create_readback_unavailable", err), nil
	}
	if observed.ParentID != input.ParentID || observed.MilestoneID != input.MilestoneID || observed.Fields != input.Fields {
		return core.TrackerPublicationResult{Status: "conflict", ErrorCode: "issue_create_readback_mismatch", Issue: &observed}, nil
	}
	return publicationIssueResult(input.Scope, input.Execution, observed, "confirmed", observed.AssigneeID != input.AssigneeID), nil
}

func publicationIssueResult(scope core.TrackerPublicationScope, execution core.TrackerPublicationExecution, issue core.TrackerPublicationIssue, status string, override bool) core.TrackerPublicationResult {
	receipt := publicationReceipt(scope, execution, issue.ID, issue.DisplayID, issue.URL, issue.Revision, status)
	if err := receipt.Validate(); err != nil {
		return publicationUnknown("issue_receipt_invalid", nil)
	}
	return core.TrackerPublicationResult{Status: status, Issue: &issue, Receipt: receipt, AssignmentOverridden: override}
}

func (p *Provider) UpdateTrackerPublicationIssue(ctx context.Context, input core.TrackerPublicationIssueUpdateRequest) (core.TrackerPublicationResult, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerPublicationResult{}, err
	}
	credential, endpoint, destination, err := p.publicationContext(ctx, input.Target.Scope, "", input.Patch.StateID != nil)
	if err == nil {
		err = publicationWrite(credential)
	}
	if err != nil {
		return core.TrackerPublicationResult{}, err
	}
	desired := input.Patch.Apply(input.Expected)
	if input.Patch.StateID != nil {
		if err = publicationState(destination, desired.StateID); err != nil {
			return core.TrackerPublicationResult{}, err
		}
	}
	current, err := p.readPublicationIssue(ctx, &credential, endpoint, input.Target)
	if err != nil {
		return core.TrackerPublicationResult{}, err
	}
	if current.ParentID != input.ParentID || current.MilestoneID != input.MilestoneID {
		return core.TrackerPublicationResult{Status: "conflict", ErrorCode: "issue_hierarchy_changed", Issue: &current}, nil
	}
	if current.Fields == desired {
		return publicationIssueResult(input.Target.Scope, input.Execution, current, "desired_state_observed", false), nil
	}
	if current.Fields != input.Expected {
		return core.TrackerPublicationResult{Status: "conflict", ErrorCode: core.TrackerErrorConflict, Issue: &current}, nil
	}
	fields := map[string]any{}
	if desired.Title != current.Fields.Title {
		fields["title"] = desired.Title
	}
	if desired.Description != current.Fields.Description {
		fields["description"] = desired.Description
	}
	if desired.StateID != current.Fields.StateID {
		fields["stateId"] = desired.StateID
	}
	if err = publicationWrite(credential); err != nil {
		return core.TrackerPublicationResult{}, err
	}
	var response tracker.GraphQLResponse[struct {
		IssueUpdate struct {
			Success bool
			Issue   publicationRef
		}
	}]
	if rejected := p.publicationMutation(ctx, credential, endpoint, `mutation($id:String!,$input:IssueUpdateInput!){issueUpdate(id:$id,input:$input){success issue{id}}}`, map[string]any{"id": input.Target.ID, "input": fields}, &response); rejected != nil {
		return *rejected, nil
	}
	err = graphqlErrors(response.Errors)
	if err != nil || !response.Data.IssueUpdate.Success {
		return publicationUnknown("issue_update_unconfirmed", err), nil
	}
	if response.Data.IssueUpdate.Issue.ID != input.Target.ID {
		return publicationUnknown("issue_update_identity_mismatch", nil), nil
	}
	observed, err := p.readPublicationIssue(ctx, &credential, endpoint, input.Target)
	if err != nil {
		return publicationUnknown("issue_update_readback_unavailable", err), nil
	}
	if observed.ParentID != input.ParentID || observed.MilestoneID != input.MilestoneID || observed.Fields != desired {
		return core.TrackerPublicationResult{Status: "conflict", ErrorCode: "issue_update_readback_mismatch", Issue: &observed}, nil
	}
	return publicationIssueResult(input.Target.Scope, input.Execution, observed, "confirmed", false), nil
}
