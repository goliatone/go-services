package core

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const TrackerPublicationContractVersion = "go-services.tracker.publication.v1"

type TrackerPublicationScope struct {
	ConnectionID string
	AccountID    string
	TeamID       string
	ProjectID    string
}

func (s TrackerPublicationScope) Validate() error {
	for _, value := range []string{s.ConnectionID, s.AccountID, s.TeamID, s.ProjectID} {
		if strings.TrimSpace(value) == "" {
			return errors.New("core: publication connection, account, team and project are required")
		}
	}
	return nil
}

type TrackerPublicationExecution struct {
	ActorID        string
	IdempotencyKey string
	CorrelationID  string
	CausationID    string
}

func (e TrackerPublicationExecution) Validate() error {
	if strings.TrimSpace(e.ActorID) == "" || strings.TrimSpace(e.IdempotencyKey) == "" || strings.TrimSpace(e.CorrelationID) == "" {
		return errors.New("core: publication execution identity is required")
	}
	return nil
}

type TrackerPublicationInspectRequest struct {
	Scope      TrackerPublicationScope
	AssigneeID string
}

type TrackerPublicationDestination struct {
	Scope                 TrackerPublicationScope
	ProjectURL            string
	AssigneeID            string
	AssigneeName          string
	WriteGranted          bool
	States                []TrackerNode
	AutoCloseParentIssues *bool
	AutoCloseChildIssues  *bool
}

type TrackerPublicationFields struct {
	Title       string
	Description string
	StateID     string
}

func (f TrackerPublicationFields) Validate() error {
	if strings.TrimSpace(f.Title) == "" || len(f.Title) > 512 || len(f.Description) > 64<<10 || strings.TrimSpace(f.StateID) == "" {
		return errors.New("core: publication requires bounded title, description and explicit state")
	}
	return nil
}

type TrackerPublicationIssue struct {
	Scope       TrackerPublicationScope
	ID          string
	DisplayID   string
	URL         string
	Revision    string
	ParentID    string
	MilestoneID string
	Fields      TrackerPublicationFields
	AssigneeID  string
	// False does not prove absence of children hidden by provider permissions.
	HasChildren bool
}

type TrackerPublicationMilestone struct {
	Scope       TrackerPublicationScope
	ID          string
	Name        string
	Description string
	Revision    string
	ProjectURL  string
}

type TrackerPublicationMilestonePage struct {
	Items      []TrackerPublicationMilestone
	NextCursor string
	HasMore    bool
}

type TrackerPublicationListRequest struct {
	Scope  TrackerPublicationScope
	Cursor string
	Limit  int
}

func (r TrackerPublicationListRequest) Validate() error {
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if r.Limit < 0 || r.Limit > 100 || len(r.Cursor) > 4096 {
		return errors.New("core: publication page bounds exceeded")
	}
	return nil
}

type TrackerPublicationReadRequest struct {
	Scope TrackerPublicationScope
	ID    string
}

func (r TrackerPublicationReadRequest) Validate() error {
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(r.ID) == "" {
		return errors.New("core: publication exact resource id is required")
	}
	return nil
}

type TrackerPublicationMilestoneCreateRequest struct {
	Scope       TrackerPublicationScope
	Execution   TrackerPublicationExecution
	RequestedID string
	Name        string
	Description string
}

func (r TrackerPublicationMilestoneCreateRequest) Validate() error {
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if err := r.Execution.Validate(); err != nil {
		return err
	}
	if err := validatePublicationCreateID(r.RequestedID); err != nil {
		return err
	}
	if strings.TrimSpace(r.Name) == "" || len(r.Name) > 255 || len(r.Description) > 64<<10 {
		return errors.New("core: publication milestone text is invalid")
	}
	return nil
}

type TrackerPublicationIssueCreateRequest struct {
	Scope       TrackerPublicationScope
	Execution   TrackerPublicationExecution
	RequestedID string
	MilestoneID string
	ParentID    string
	AssigneeID  string
	Fields      TrackerPublicationFields
}

func (r TrackerPublicationIssueCreateRequest) Validate() error {
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if err := r.Execution.Validate(); err != nil {
		return err
	}
	if err := validatePublicationCreateID(r.RequestedID); err != nil {
		return err
	}
	if strings.TrimSpace(r.MilestoneID) == "" || strings.TrimSpace(r.AssigneeID) == "" || r.ParentID == r.RequestedID {
		return errors.New("core: publication milestone, human assignee and valid parent are required")
	}
	return r.Fields.Validate()
}

type TrackerPublicationPatch struct {
	Title       *string
	Description *string
	StateID     *string
}

func (p TrackerPublicationPatch) Apply(base TrackerPublicationFields) TrackerPublicationFields {
	if p.Title != nil {
		base.Title = *p.Title
	}
	if p.Description != nil {
		base.Description = *p.Description
	}
	if p.StateID != nil {
		base.StateID = *p.StateID
	}
	return base
}

type TrackerPublicationIssueUpdateRequest struct {
	Target      TrackerPublicationReadRequest
	Execution   TrackerPublicationExecution
	ParentID    string
	MilestoneID string
	Expected    TrackerPublicationFields
	Patch       TrackerPublicationPatch
}

func (r TrackerPublicationIssueUpdateRequest) Validate() error {
	if err := r.Target.Validate(); err != nil {
		return err
	}
	if err := r.Execution.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(r.MilestoneID) == "" || (r.Patch.Title == nil && r.Patch.Description == nil && r.Patch.StateID == nil) {
		return errors.New("core: publication update requires milestone and managed patch")
	}
	if err := r.Expected.Validate(); err != nil {
		return err
	}
	return r.Patch.Apply(r.Expected).Validate()
}

type TrackerPublicationResult struct {
	RetryAfter           time.Duration
	Status               string
	ErrorCode            string
	Receipt              *TrackerMutationReceipt
	Issue                *TrackerPublicationIssue
	Milestone            *TrackerPublicationMilestone
	AssignmentOverridden bool
}

type TrackerPublicationProvider interface {
	Provider
	InspectTrackerPublication(context.Context, TrackerPublicationInspectRequest) (TrackerPublicationDestination, error)
	ListTrackerPublicationMilestones(context.Context, TrackerPublicationListRequest) (TrackerPublicationMilestonePage, error)
	ReadTrackerPublicationMilestone(context.Context, TrackerPublicationReadRequest) (TrackerPublicationMilestone, error)
	CreateTrackerPublicationMilestone(context.Context, TrackerPublicationMilestoneCreateRequest) (TrackerPublicationResult, error)
	ReadTrackerPublicationIssue(context.Context, TrackerPublicationReadRequest) (TrackerPublicationIssue, error)
	CreateTrackerPublicationIssue(context.Context, TrackerPublicationIssueCreateRequest) (TrackerPublicationResult, error)
	UpdateTrackerPublicationIssue(context.Context, TrackerPublicationIssueUpdateRequest) (TrackerPublicationResult, error)
}

func validatePublicationCreateID(value string) error {
	id, err := uuid.Parse(value)
	if err != nil || id.Version() != 4 || id.Variant() != uuid.RFC4122 || id.String() != value {
		return errors.New("core: publication create requires a canonical UUID v4")
	}
	return nil
}
