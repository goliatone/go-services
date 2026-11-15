package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	TrackerIssuePatchContractVersion      = "go-services.tracker.issue-patch.v1"
	TrackerIssuePatchMaxTitleBytes        = 512
	TrackerIssuePatchMaxDescriptionBytes  = 64 << 10
	TrackerIssuePatchMaxStates            = 100
	TrackerIssuePatchConfirmed            = "confirmed"
	TrackerIssuePatchDesiredStateObserved = "desired_state_observed"
	TrackerIssuePatchConflict             = "conflict"
	TrackerIssuePatchFailed               = "failed"
	TrackerIssuePatchUnknown              = "unknown"
)

type TrackerIssuePatchTarget struct {
	ConnectionID string
	AccountID    string
	TeamID       string
	IssueID      string
}

func (t TrackerIssuePatchTarget) Validate() error {
	if strings.TrimSpace(t.ConnectionID) == "" || strings.TrimSpace(t.AccountID) == "" || strings.TrimSpace(t.TeamID) == "" || !issuePatchUUID(t.IssueID) {
		return errors.New("core: issue patch requires connection, account, team and immutable issue UUID")
	}
	return nil
}

type TrackerIssuePatchInspectRequest struct {
	Target TrackerIssuePatchTarget
	Field  string
}

func (r TrackerIssuePatchInspectRequest) Validate() error {
	if err := r.Target.Validate(); err != nil {
		return err
	}
	if r.Field != "title" && r.Field != "description" && r.Field != "state" {
		return errors.New("core: issue patch supports only title, description or state")
	}
	return nil
}

type TrackerIssuePatchObservation struct {
	Target       TrackerIssuePatchTarget
	DisplayID    string
	CanonicalURL string
	Revision     string
	ObservedAt   time.Time
	Title        string
	// Nil is unavailable; a known empty description or absent relationship uses a pointer to "".
	Description *string
	StateID     string
	Archived    *bool
	ProjectID   *string
	ParentID    *string
	MilestoneID *string
}

func (o TrackerIssuePatchObservation) ValidateFor(target TrackerIssuePatchTarget) error {
	if err := target.Validate(); err != nil {
		return err
	}
	if o.Target != target || strings.TrimSpace(o.DisplayID) == "" || strings.TrimSpace(o.Revision) == "" || o.ObservedAt.IsZero() || strings.TrimSpace(o.Title) == "" || !utf8.ValidString(o.Title) || o.Description == nil || !utf8.ValidString(*o.Description) || !issuePatchUUID(o.StateID) || o.Archived == nil {
		return errors.New("core: issue patch observation is incomplete or outside the requested target")
	}
	parsed, err := url.Parse(o.CanonicalURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return errors.New("core: issue patch observation requires a canonical HTTPS URL")
	}
	for _, id := range []*string{o.ProjectID, o.ParentID, o.MilestoneID} {
		if id != nil && *id != "" && !issuePatchUUID(*id) {
			return errors.New("core: issue patch relationship must be a UUID or known absent")
		}
	}
	return nil
}

type TrackerIssuePatchStateSchema struct {
	Revision              string
	Complete              bool
	States                []TrackerNode
	AutoCloseParentIssues *bool
	AutoCloseChildIssues  *bool
}

type TrackerIssuePatchInspection struct {
	Observation TrackerIssuePatchObservation
	Capability  TrackerFieldCapability
	StateSchema *TrackerIssuePatchStateSchema
}

func (i TrackerIssuePatchInspection) ValidateFor(request TrackerIssuePatchInspectRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if err := i.Observation.ValidateFor(request.Target); err != nil {
		return err
	}
	if err := i.Capability.Validate(); err != nil {
		return err
	}
	if i.Capability.Field != request.Field || i.Capability.Operation != "patch" || i.Capability.Granted && (i.Capability.Reason != "" || *i.Observation.Archived) {
		return errors.New("core: issue patch capability does not match the requested field or active issue")
	}
	if request.Field != "state" {
		if i.StateSchema != nil {
			return errors.New("core: text patch inspection must omit state schema")
		}
		return nil
	}
	schema := i.StateSchema
	if schema == nil {
		if i.Capability.Granted {
			return errors.New("core: state patch grant requires a complete schema")
		}
		return nil
	}
	if strings.TrimSpace(schema.Revision) == "" || len(schema.States) > TrackerIssuePatchMaxStates || i.Capability.Granted && (!schema.Complete || len(schema.States) == 0) {
		return errors.New("core: issue patch state schema is incomplete or exceeds its bound")
	}
	seen := make(map[string]bool, len(schema.States))
	for _, state := range schema.States {
		if err := state.Validate(); err != nil {
			return err
		}
		if !issuePatchUUID(state.ExternalID) || state.ParentID != request.Target.TeamID || seen[state.ExternalID] {
			return errors.New("core: issue patch state must be unique and scoped to the selected team")
		}
		seen[state.ExternalID] = true
	}
	return nil
}

type TrackerIssuePatchRequest struct {
	Target                 TrackerIssuePatchTarget
	Field                  string
	ExpectedRevision       string
	ExpectedSchemaRevision string
	ExpectedValue          json.RawMessage
	Value                  json.RawMessage
	Execution              TrackerPublicationExecution
}

func (r TrackerIssuePatchRequest) Validate() error {
	if err := (TrackerIssuePatchInspectRequest{Target: r.Target, Field: r.Field}).Validate(); err != nil {
		return err
	}
	if err := r.Execution.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(r.ExpectedRevision) == "" || r.Field == "state" && strings.TrimSpace(r.ExpectedSchemaRevision) == "" || r.Field != "state" && r.ExpectedSchemaRevision != "" {
		return errors.New("core: issue patch requires an observation revision and a schema revision for state only")
	}
	for _, raw := range []json.RawMessage{r.ExpectedValue, r.Value} {
		if _, err := issuePatchValue(r.Field, raw); err != nil {
			return err
		}
	}
	return nil
}

type TrackerIssuePatchResult struct {
	Status      string
	Observation *TrackerIssuePatchObservation
	Receipt     *TrackerMutationReceipt
	ErrorCode   string
	RetryAfter  time.Duration
}

func (r TrackerIssuePatchResult) ValidateFor(request TrackerIssuePatchRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if r.RetryAfter < 0 {
		return errors.New("core: issue patch retry timing cannot be negative")
	}
	if r.Observation != nil {
		if err := r.Observation.ValidateFor(request.Target); err != nil {
			return err
		}
	}
	switch r.Status {
	case TrackerIssuePatchConfirmed, TrackerIssuePatchDesiredStateObserved:
		if r.Observation == nil || *r.Observation.Archived || r.ErrorCode != "" || r.RetryAfter != 0 {
			return errors.New("core: successful issue patch result requires active observation and no error")
		}
		desired, _ := issuePatchValue(request.Field, request.Value)
		observed := r.Observation.Title
		if request.Field == "description" {
			observed = *r.Observation.Description
		}
		if request.Field == "state" {
			observed = r.Observation.StateID
		}
		if observed != desired {
			return errors.New("core: issue patch success does not match the desired value")
		}
	case TrackerIssuePatchConflict:
		if r.Observation == nil || strings.TrimSpace(r.ErrorCode) == "" {
			return errors.New("core: pre-dispatch patch conflict requires observation and reason")
		}
	case TrackerIssuePatchFailed, TrackerIssuePatchUnknown:
		if strings.TrimSpace(r.ErrorCode) == "" {
			return errors.New("core: failed or unknown issue patch requires a reason")
		}
	default:
		return errors.New("core: invalid issue patch result status")
	}
	if r.Status != TrackerIssuePatchConfirmed {
		if r.Receipt != nil {
			return errors.New("core: only confirmed issue patches carry mutation receipts")
		}
		return nil
	}
	if r.Receipt == nil {
		return errors.New("core: confirmed issue patch requires a mutation receipt")
	}
	if err := r.Receipt.Validate(); err != nil {
		return err
	}
	receipt, observation := r.Receipt, r.Observation
	if receipt.Status != TrackerIssuePatchConfirmed || receipt.RepositoryID != request.Target.TeamID || receipt.ExternalID != request.Target.IssueID || receipt.ProviderRevision != observation.Revision || receipt.DisplayID != observation.DisplayID || receipt.CanonicalURL != observation.CanonicalURL || receipt.CorrelationID != request.Execution.CorrelationID || receipt.CausationID != request.Execution.CausationID {
		return errors.New("core: issue patch receipt differs from requested identity, execution or observation")
	}
	return nil
}

// TrackerIssuePatchProvider checks revisions before dispatch; it does not provide atomic revision enforcement.
// Inspection is read-only, and an unknown patch result never authorizes mutation replay.
type TrackerIssuePatchProvider interface {
	Provider
	InspectTrackerIssuePatch(context.Context, TrackerIssuePatchInspectRequest) (TrackerIssuePatchInspection, error)
	PatchTrackerIssue(context.Context, TrackerIssuePatchRequest) (TrackerIssuePatchResult, error)
}

func issuePatchValue(field string, raw json.RawMessage) (string, error) {
	var value *string
	if !utf8.Valid(raw) || json.Unmarshal(raw, &value) != nil || value == nil {
		return "", errors.New("core: issue patch values must be non-null JSON strings")
	}
	switch field {
	case "title":
		if strings.TrimSpace(*value) == "" || len(*value) > TrackerIssuePatchMaxTitleBytes {
			return "", errors.New("core: issue patch title is empty or exceeds its bound")
		}
	case "description":
		if len(*value) > TrackerIssuePatchMaxDescriptionBytes {
			return "", errors.New("core: issue patch description exceeds its bound")
		}
	case "state":
		if !issuePatchUUID(*value) {
			return "", errors.New("core: issue patch state requires a native UUID")
		}
	default:
		return "", errors.New("core: unsupported issue patch field")
	}
	return *value, nil
}

func issuePatchUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}
