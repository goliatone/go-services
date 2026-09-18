package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const TrackerContractVersion = "go-services.tracker.v1"

const (
	TrackerErrorSearchUnsupported = "search_unsupported"
	TrackerErrorUnavailable       = "provider_unavailable"
	TrackerErrorCredentialRevoked = "credential_revoked"
	TrackerErrorRateLimited       = "rate_limited"
	TrackerErrorSchemaChanged     = "schema_changed"
	TrackerErrorCursorInvalid     = "cursor_invalid"
	TrackerErrorDuplicateDelivery = "duplicate_delivery"
	TrackerErrorExternal          = "provider_external_failure"
)

// TrackerCredentialResolver resolves decrypted credential material at the
// provider I/O boundary. Provider results and errors must never retain it.
type TrackerCredentialResolver interface {
	ResolveTrackerCredential(context.Context, string) (ActiveCredential, error)
}

type TrackerDiscoveryRequest struct {
	Search       string
	ConnectionID string
	ResourceType string
	ResourceID   string
	Cursor       string
	Limit        int
}

func (r TrackerDiscoveryRequest) Validate() error {
	if strings.TrimSpace(r.ConnectionID) == "" {
		return errors.New("core: tracker connection id is required")
	}
	if !utf8.ValidString(r.Search) || utf8.RuneCountInString(strings.TrimSpace(r.Search)) > 256 {
		return errors.New("core: tracker discovery search must be valid UTF-8 and at most 256 characters")
	}
	if r.Limit < 0 || r.Limit > 500 {
		return errors.New("core: tracker discovery limit must be between 0 and 500")
	}
	return nil
}

type TrackerChangesRequest struct {
	ConnectionID string
	ResourceType string
	ResourceID   string
	Cursor       string
	Limit        int
}

func (r TrackerChangesRequest) Validate() error {
	if strings.TrimSpace(r.ConnectionID) == "" || strings.TrimSpace(r.ResourceType) == "" || strings.TrimSpace(r.ResourceID) == "" {
		return errors.New("core: tracker change connection, resource type, and resource id are required")
	}
	if r.Limit < 0 || r.Limit > 500 {
		return errors.New("core: tracker change limit must be between 0 and 500")
	}
	return nil
}

type TrackerNode struct {
	ExternalID     string         `json:"external_id"`
	Type           string         `json:"type"`
	Name           string         `json:"name"`
	ParentID       string         `json:"parent_id,omitempty"`
	Capabilities   []string       `json:"capabilities,omitempty"`
	NativeRevision string         `json:"native_revision"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

func (n TrackerNode) Validate() error {
	if strings.TrimSpace(n.ExternalID) == "" || strings.TrimSpace(n.Type) == "" || strings.TrimSpace(n.Name) == "" || strings.TrimSpace(n.NativeRevision) == "" {
		return errors.New("core: tracker node identity, type, name, and revision are required")
	}
	return nil
}

type TrackerField struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Type           string         `json:"type"`
	Required       bool           `json:"required"`
	Options        []TrackerNode  `json:"options,omitempty"`
	NativeRevision string         `json:"native_revision"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

func (f TrackerField) Validate() error {
	if strings.TrimSpace(f.ID) == "" || strings.TrimSpace(f.Name) == "" || strings.TrimSpace(f.Type) == "" || strings.TrimSpace(f.NativeRevision) == "" {
		return errors.New("core: tracker field identity, name, type, and revision are required")
	}
	for _, option := range f.Options {
		if err := option.Validate(); err != nil {
			return fmt.Errorf("core: tracker field option: %w", err)
		}
	}
	return nil
}

type TrackerResource struct {
	ProviderID       string          `json:"provider_id"`
	ResourceType     string          `json:"resource_type"`
	ExternalID       string          `json:"external_id"`
	ProviderRevision string          `json:"provider_revision"`
	CanonicalURL     string          `json:"canonical_url"`
	ObservedAt       time.Time       `json:"observed_at"`
	NormalizedFields json.RawMessage `json:"normalized_fields"`
	NativeExtension  json.RawMessage `json:"native_extension"`
	SchemaRevision   string          `json:"schema_revision"`
	Cursor           string          `json:"cursor,omitempty"`
}

func (r TrackerResource) Validate() error {
	if strings.TrimSpace(r.ProviderID) == "" || strings.TrimSpace(r.ResourceType) == "" || strings.TrimSpace(r.ExternalID) == "" || strings.TrimSpace(r.ProviderRevision) == "" || strings.TrimSpace(r.SchemaRevision) == "" || r.ObservedAt.IsZero() || len(r.NormalizedFields) == 0 || len(r.NativeExtension) == 0 {
		return errors.New("core: tracker resource identity, revisions, observation, and fields are required")
	}
	parsed, err := url.Parse(strings.TrimSpace(r.CanonicalURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("core: tracker resource canonical url must be HTTPS")
	}
	if !json.Valid(r.NormalizedFields) || !json.Valid(r.NativeExtension) {
		return errors.New("core: tracker resource fields must be valid JSON")
	}
	return nil
}

type TrackerNodePage struct {
	Items      []TrackerNode  `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
	HasMore    bool           `json:"has_more"`
	Revision   string         `json:"revision"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

func (p TrackerNodePage) Validate() error {
	if p.HasMore && strings.TrimSpace(p.NextCursor) == "" {
		return errors.New("core: tracker node page with more results requires a cursor")
	}
	if strings.TrimSpace(p.Revision) == "" {
		return errors.New("core: tracker node page revision is required")
	}
	for _, item := range p.Items {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type TrackerSchemaPage struct {
	Items      []TrackerField `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
	HasMore    bool           `json:"has_more"`
	Revision   string         `json:"revision"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

func (p TrackerSchemaPage) Validate() error {
	if p.HasMore && strings.TrimSpace(p.NextCursor) == "" {
		return errors.New("core: tracker schema page with more results requires a cursor")
	}
	if strings.TrimSpace(p.Revision) == "" {
		return errors.New("core: tracker schema page revision is required")
	}
	for _, item := range p.Items {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type TrackerChangePage struct {
	Items      []TrackerResource `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
	HasMore    bool              `json:"has_more"`
	Metadata   map[string]any    `json:"metadata,omitempty"`
}

func (p TrackerChangePage) Validate() error {
	if p.HasMore && strings.TrimSpace(p.NextCursor) == "" {
		return errors.New("core: tracker change page with more results requires a cursor")
	}
	for _, item := range p.Items {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// TrackerProvider is the complete read-observation contract. It deliberately
// excludes downstream identity and write/reconciliation policy.
type TrackerProvider interface {
	Provider
	DiscoverTrackerScopes(context.Context, TrackerDiscoveryRequest) (TrackerNodePage, error)
	DiscoverTrackerSchema(context.Context, TrackerDiscoveryRequest) (TrackerSchemaPage, error)
	ListTrackerChanges(context.Context, TrackerChangesRequest) (TrackerChangePage, error)
}

type TrackerProviderError struct {
	Code       string
	Message    string
	Retryable  bool
	RetryAfter time.Duration
	Cause      error
}

func (e *TrackerProviderError) Error() string {
	if e == nil {
		return ""
	}
	message := strings.TrimSpace(e.Message)
	if message == "" && e.Cause != nil {
		message = e.Cause.Error()
	}
	if message == "" {
		message = e.Code
	}
	return strings.TrimSpace(e.Code) + ": " + message
}

func (e *TrackerProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func NewTrackerProviderError(code, message string, retryable bool, retryAfter time.Duration, cause error) error {
	return &TrackerProviderError{Code: strings.TrimSpace(code), Message: strings.TrimSpace(message), Retryable: retryable, RetryAfter: retryAfter, Cause: cause}
}
