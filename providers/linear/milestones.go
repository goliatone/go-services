package linear

import (
	"context"
	"encoding/json"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/tracker"
)

const publicationMilestoneFields = `id name description updatedAt archivedAt project{id url}`

type publicationMilestone struct {
	ID, Name    string
	Description json.RawMessage
	UpdatedAt   time.Time
	ArchivedAt  *time.Time
	Project     struct{ ID, URL string }
}

func (m publicationMilestone) normalize(scope core.TrackerPublicationScope) (core.TrackerPublicationMilestone, error) {
	description, err := publicationDescription(m.Description)
	if err != nil {
		return core.TrackerPublicationMilestone{}, err
	}
	if m.ID == "" || m.Name == "" || m.UpdatedAt.IsZero() || m.ArchivedAt != nil || m.Project.ID != scope.ProjectID || m.Project.URL == "" {
		return core.TrackerPublicationMilestone{}, publicationError(core.TrackerErrorConflict, "Linear publication milestone is missing, archived or outside the project")
	}
	return core.TrackerPublicationMilestone{Scope: scope, ID: m.ID, Name: m.Name, Description: description, Revision: m.UpdatedAt.UTC().Format(time.RFC3339Nano), ProjectURL: m.Project.URL}, nil
}

func (p *Provider) ListTrackerPublicationMilestones(ctx context.Context, input core.TrackerPublicationListRequest) (core.TrackerPublicationMilestonePage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerPublicationMilestonePage{}, err
	}
	credential, endpoint, _, err := p.publicationContext(ctx, input.Scope, "", false)
	if err != nil {
		return core.TrackerPublicationMilestonePage{}, err
	}
	limit := tracker.Limit(input.Limit, 50, 100)
	query := `query($project:String!,$first:Int!,$after:String){project(id:$project){id projectMilestones(first:$first,after:$after){nodes{` + publicationMilestoneFields + `}pageInfo{hasNextPage endCursor}}}}`
	var response tracker.GraphQLResponse[struct {
		Project struct {
			ID                string
			ProjectMilestones publicationPage[publicationMilestone]
		}
	}]
	err = p.doGraphQL(ctx, &credential, endpoint, query, map[string]any{"project": input.Scope.ProjectID, "first": limit, "after": nullable(input.Cursor)}, &response)
	if err == nil {
		err = graphqlErrors(response.Errors)
	}
	if err != nil {
		return core.TrackerPublicationMilestonePage{}, err
	}
	page := response.Data.Project.ProjectMilestones
	if response.Data.Project.ID != input.Scope.ProjectID || !page.valid(limit) || (*page.PageInfo.HasNextPage && page.PageInfo.EndCursor == input.Cursor) {
		return core.TrackerPublicationMilestonePage{}, publicationError(core.TrackerErrorUnavailable, "Linear milestone page is incomplete")
	}
	out := core.TrackerPublicationMilestonePage{Items: []core.TrackerPublicationMilestone{}, NextCursor: page.PageInfo.EndCursor, HasMore: *page.PageInfo.HasNextPage}
	seen := map[string]bool{}
	for _, m := range page.Nodes {
		mapped, err := m.normalize(input.Scope)
		if err != nil {
			return core.TrackerPublicationMilestonePage{}, err
		}
		if seen[m.ID] {
			return core.TrackerPublicationMilestonePage{}, publicationError(core.TrackerErrorConflict, "Linear milestone page contains duplicate identities")
		}
		seen[m.ID] = true
		out.Items = append(out.Items, mapped)
	}
	return out, nil
}

func (p *Provider) readPublicationMilestone(ctx context.Context, credential *core.ActiveCredential, endpoint string, input core.TrackerPublicationReadRequest) (core.TrackerPublicationMilestone, error) {
	var response tracker.GraphQLResponse[struct{ ProjectMilestone publicationMilestone }]
	err := p.doGraphQL(ctx, credential, endpoint, `query($id:String!){projectMilestone(id:$id){`+publicationMilestoneFields+`}}`, map[string]any{"id": input.ID}, &response)
	if err == nil {
		err = graphqlErrors(response.Errors)
	}
	if err != nil {
		return core.TrackerPublicationMilestone{}, err
	}
	if response.Data.ProjectMilestone.ID != input.ID {
		return core.TrackerPublicationMilestone{}, publicationError(core.TrackerErrorNotFound, "Linear milestone exact identity is unavailable; absence does not authorize creation retry")
	}
	return response.Data.ProjectMilestone.normalize(input.Scope)
}

func (p *Provider) ReadTrackerPublicationMilestone(ctx context.Context, input core.TrackerPublicationReadRequest) (core.TrackerPublicationMilestone, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerPublicationMilestone{}, err
	}
	credential, endpoint, _, err := p.publicationContext(ctx, input.Scope, "", false)
	if err != nil {
		return core.TrackerPublicationMilestone{}, err
	}
	return p.readPublicationMilestone(ctx, &credential, endpoint, input)
}

func (p *Provider) CreateTrackerPublicationMilestone(ctx context.Context, input core.TrackerPublicationMilestoneCreateRequest) (core.TrackerPublicationResult, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerPublicationResult{}, err
	}
	credential, endpoint, _, err := p.publicationContext(ctx, input.Scope, "", false)
	if err == nil {
		err = publicationWrite(credential)
	}
	if err != nil {
		return core.TrackerPublicationResult{}, err
	}
	var response tracker.GraphQLResponse[struct {
		ProjectMilestoneCreate struct {
			Success          bool
			ProjectMilestone publicationMilestone
		}
	}]
	fields := map[string]any{"id": input.RequestedID, "projectId": input.Scope.ProjectID, "name": input.Name, "description": input.Description}
	query := `mutation($input:ProjectMilestoneCreateInput!){projectMilestoneCreate(input:$input){success projectMilestone{` + publicationMilestoneFields + `}}}`
	if rejected := p.publicationMutation(ctx, credential, endpoint, query, map[string]any{"input": fields}, &response); rejected != nil {
		return *rejected, nil
	}
	err = graphqlErrors(response.Errors)
	if err != nil || !response.Data.ProjectMilestoneCreate.Success {
		return publicationUnknown("milestone_create_unconfirmed", err), nil
	}
	if response.Data.ProjectMilestoneCreate.ProjectMilestone.ID != input.RequestedID {
		return publicationUnknown("milestone_create_identity_mismatch", nil), nil
	}
	observed, err := p.readPublicationMilestone(ctx, &credential, endpoint, core.TrackerPublicationReadRequest{Scope: input.Scope, ID: input.RequestedID})
	if err != nil {
		return publicationUnknown("milestone_create_readback_unavailable", err), nil
	}
	if observed.Name != input.Name || observed.Description != input.Description {
		return core.TrackerPublicationResult{Status: "conflict", ErrorCode: "milestone_create_readback_mismatch", Milestone: &observed}, nil
	}
	receipt := publicationReceipt(input.Scope, input.Execution, observed.ID, observed.Name, observed.ProjectURL, observed.Revision, "confirmed")
	if err := receipt.Validate(); err != nil {
		return publicationUnknown("milestone_create_receipt_invalid", nil), nil
	}
	return core.TrackerPublicationResult{Status: "confirmed", Milestone: &observed, Receipt: receipt}, nil
}
