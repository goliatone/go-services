package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
)

const (
	TrackerErrorPermission = "permission_denied"
	TrackerErrorNotFound   = "not_found"
	TrackerErrorConflict   = "provider_revision_changed"
	TrackerErrorValidation = "validation_failed"
)

type TrackerFieldCapabilityRequest struct {
	ConnectionID string
	RepositoryID string
}

func (r TrackerFieldCapabilityRequest) Validate() error {
	if strings.TrimSpace(r.ConnectionID) == "" || strings.TrimSpace(r.RepositoryID) == "" {
		return errors.New("core: tracker field capability connection and repository are required")
	}
	return nil
}

type TrackerFieldCapability struct {
	Field     string `json:"field"`
	Operation string `json:"operation"`
	Granted   bool   `json:"granted"`
	Reason    string `json:"reason,omitempty"`
	Revision  string `json:"revision"`
}

func (c TrackerFieldCapability) Validate() error {
	if strings.TrimSpace(c.Field) == "" || strings.TrimSpace(c.Operation) == "" || strings.TrimSpace(c.Revision) == "" || !c.Granted && strings.TrimSpace(c.Reason) == "" {
		return errors.New("core: tracker field capability is incomplete")
	}
	return nil
}

type TrackerIssueCreateRequest struct {
	ConnectionID   string
	RepositoryID   string
	Fields         map[string]json.RawMessage
	IdempotencyKey string
	ActorID        string
	CorrelationID  string
	CausationID    string
}

func (r TrackerIssueCreateRequest) Validate() error {
	if strings.TrimSpace(r.ConnectionID) == "" || strings.TrimSpace(r.RepositoryID) == "" || strings.TrimSpace(r.IdempotencyKey) == "" || strings.TrimSpace(r.ActorID) == "" || strings.TrimSpace(r.CorrelationID) == "" || len(r.Fields["title"]) == 0 {
		return errors.New("core: tracker issue create connection, repository, title, and execution identity are required")
	}
	for _, value := range r.Fields {
		if !json.Valid(value) {
			return errors.New("core: tracker issue create fields must be typed JSON")
		}
	}
	return nil
}

type TrackerIssueUpdateRequest struct {
	ConnectionID     string
	RepositoryID     string
	IssueID          string
	IssueNumber      int
	Field            string
	Value            json.RawMessage
	ExpectedRevision string
	IdempotencyKey   string
	ActorID          string
	CorrelationID    string
	CausationID      string
}

func (r TrackerIssueUpdateRequest) Validate() error {
	if strings.TrimSpace(r.ConnectionID) == "" || strings.TrimSpace(r.RepositoryID) == "" || strings.TrimSpace(r.IssueID) == "" || r.IssueNumber < 1 || strings.TrimSpace(r.Field) == "" || !json.Valid(r.Value) || strings.TrimSpace(r.ExpectedRevision) == "" || strings.TrimSpace(r.IdempotencyKey) == "" || strings.TrimSpace(r.ActorID) == "" || strings.TrimSpace(r.CorrelationID) == "" {
		return errors.New("core: tracker issue update identity, typed value, revision, and execution identity are required")
	}
	return nil
}

type TrackerRateLimit struct {
	Remaining int       `json:"remaining,omitempty"`
	ResetAt   time.Time `json:"reset_at"`
}

type TrackerMutationReceipt struct {
	ProviderID       string           `json:"provider_id"`
	RepositoryID     string           `json:"repository_id"`
	ExternalID       string           `json:"external_id"`
	DisplayID        string           `json:"display_id"`
	CanonicalURL     string           `json:"canonical_url"`
	ProviderRevision string           `json:"provider_revision"`
	Status           string           `json:"status"`
	RequestID        string           `json:"request_id,omitempty"`
	CorrelationID    string           `json:"correlation_id,omitempty"`
	CausationID      string           `json:"causation_id,omitempty"`
	RateLimit        TrackerRateLimit `json:"rate_limit"`
	Metadata         map[string]any   `json:"metadata,omitempty"`
}

func (r TrackerMutationReceipt) Validate() error {
	if strings.TrimSpace(r.ProviderID) == "" || strings.TrimSpace(r.RepositoryID) == "" || strings.TrimSpace(r.ExternalID) == "" || strings.TrimSpace(r.DisplayID) == "" || strings.TrimSpace(r.ProviderRevision) == "" || strings.TrimSpace(r.Status) == "" {
		return errors.New("core: tracker mutation receipt identity, revision, and status are required")
	}
	parsed, err := url.Parse(strings.TrimSpace(r.CanonicalURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("core: tracker mutation receipt canonical url must be HTTPS")
	}
	return nil
}

type TrackerMutationProvider interface {
	Provider
	DiscoverTrackerFieldCapabilities(context.Context, TrackerFieldCapabilityRequest) ([]TrackerFieldCapability, error)
	CreateTrackerIssue(context.Context, TrackerIssueCreateRequest) (TrackerMutationReceipt, error)
	UpdateTrackerIssue(context.Context, TrackerIssueUpdateRequest) (TrackerMutationReceipt, error)
}

type TrackerWebhookVerificationRequest struct {
	Secret     []byte
	Body       []byte
	Signature  string
	DeliveryID string
	Event      string
}

func (r TrackerWebhookVerificationRequest) Validate() error {
	if len(r.Secret) == 0 || len(r.Body) == 0 || len(r.Body) > 4<<20 || strings.TrimSpace(r.Signature) == "" || strings.TrimSpace(r.DeliveryID) == "" || strings.TrimSpace(r.Event) == "" {
		return errors.New("core: tracker webhook secret, bounded body, signature, delivery, and event are required")
	}
	return nil
}

type TrackerWebhookVerificationProvider interface {
	Provider
	VerifyTrackerWebhook(context.Context, TrackerWebhookVerificationRequest) error
}
