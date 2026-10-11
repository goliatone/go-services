package core

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	TrackerDiscussionContractVersion = "go-services.tracker.discussion.v1"
	TrackerDiscussionDefaultLimit    = 25
	TrackerDiscussionMaxLimit        = 100
	TrackerDiscussionMaxBodyBytes    = 8 << 10
)

type TrackerDiscussionTarget struct {
	ProviderID   string `json:"provider_id"`
	ConnectionID string `json:"connection_id"`
	ScopeID      string `json:"scope_id"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
}

func (t TrackerDiscussionTarget) Validate() error {
	if strings.TrimSpace(t.ProviderID) == "" || strings.TrimSpace(t.ConnectionID) == "" || strings.TrimSpace(t.ScopeID) == "" || strings.TrimSpace(t.ResourceID) == "" || t.ResourceType != "issue" {
		return errors.New("core: discussion requires a provider, connection, scope and explicit issue target")
	}
	return nil
}

func (t TrackerDiscussionTarget) ValidateFor(expected TrackerDiscussionTarget) error {
	if err := expected.Validate(); err != nil {
		return err
	}
	if err := t.Validate(); err != nil {
		return err
	}
	if t != expected {
		return errors.New("core: discussion target differs from the requested source")
	}
	return nil
}

type TrackerDiscussionCapability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

func (c TrackerDiscussionCapability) Validate() error {
	if !c.Available && strings.TrimSpace(c.Reason) == "" || c.Available && c.Reason != "" {
		return errors.New("core: unavailable discussion capability requires a reason")
	}
	return nil
}

type TrackerDiscussionCapabilities struct {
	Revision      string                      `json:"revision"`
	Read          TrackerDiscussionCapability `json:"read"`
	Reply         TrackerDiscussionCapability `json:"reply"`
	Webhook       TrackerDiscussionCapability `json:"webhook"`
	ReceiptLookup TrackerDiscussionCapability `json:"receipt_lookup"`
}

func (c TrackerDiscussionCapabilities) Validate() error {
	if strings.TrimSpace(c.Revision) == "" {
		return errors.New("core: discussion capabilities require a revision")
	}
	for _, capability := range []TrackerDiscussionCapability{c.Read, c.Reply, c.Webhook, c.ReceiptLookup} {
		if err := capability.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type TrackerDiscussionReadRequest struct {
	Target TrackerDiscussionTarget
	Cursor string
	Limit  int
}

func (r TrackerDiscussionReadRequest) Validate() error {
	if err := r.Target.Validate(); err != nil {
		return err
	}
	if r.Limit < 0 || r.Limit > TrackerDiscussionMaxLimit {
		return errors.New("core: discussion page limit must be between 0 and 100")
	}
	return nil
}

type TrackerDiscussionAuthor struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
	Bot  bool   `json:"bot"`
}

type TrackerDiscussionMessage struct {
	ProviderID   string                  `json:"provider_id"`
	Target       TrackerDiscussionTarget `json:"target"`
	MessageID    string                  `json:"message_id"`
	ParentID     string                  `json:"parent_id,omitempty"`
	Revision     string                  `json:"revision"`
	CanonicalURL string                  `json:"canonical_url"`
	Author       TrackerDiscussionAuthor `json:"author"`
	Body         string                  `json:"body,omitempty"`
	Tombstone    bool                    `json:"tombstone"`
	ObservedAt   time.Time               `json:"observed_at"`
}

func (m TrackerDiscussionMessage) Validate() error {
	if err := m.Target.Validate(); err != nil {
		return err
	}
	if m.ProviderID != m.Target.ProviderID || strings.TrimSpace(m.MessageID) == "" || strings.TrimSpace(m.Revision) == "" || m.ObservedAt.IsZero() || !m.Tombstone && strings.TrimSpace(m.Author.ID) == "" {
		return errors.New("core: discussion message identity, revision, author and observation are required")
	}
	if !utf8.ValidString(m.Body) || len(m.Body) > TrackerDiscussionMaxBodyBytes || !m.Tombstone && m.Body == "" || m.Tombstone && m.Body != "" {
		return errors.New("core: live discussion body must be nonempty bounded UTF-8; tombstones cannot retain content")
	}
	return validateDiscussionURL(m.CanonicalURL)
}

type TrackerDiscussionPage struct {
	Target     TrackerDiscussionTarget    `json:"target"`
	Items      []TrackerDiscussionMessage `json:"items"`
	NextCursor string                     `json:"next_cursor,omitempty"`
	HasMore    bool                       `json:"has_more"`
	RateLimit  TrackerRateLimit           `json:"rate_limit"`
}

func (p TrackerDiscussionPage) Validate() error {
	if err := p.Target.Validate(); err != nil {
		return err
	}
	if len(p.Items) > TrackerDiscussionMaxLimit || p.HasMore != (strings.TrimSpace(p.NextCursor) != "") {
		return errors.New("core: discussion page requires bounded items and truthful continuation")
	}
	for _, item := range p.Items {
		if item.Target != p.Target {
			return errors.New("core: discussion page crosses its target")
		}
		if err := item.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (p TrackerDiscussionPage) ValidateFor(r TrackerDiscussionReadRequest) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	limit := r.Limit
	if limit == 0 {
		limit = TrackerDiscussionDefaultLimit
	}
	if err := p.Target.ValidateFor(r.Target); err != nil {
		return err
	}
	if len(p.Items) > limit {
		return errors.New("core: discussion page exceeds the requested limit")
	}
	return nil
}

type TrackerDiscussionReplyRequest struct {
	Target         TrackerDiscussionTarget
	ParentID       string
	Body           string
	ActorID        string
	IdempotencyKey string
	CorrelationID  string
	CausationID    string
}

func (r TrackerDiscussionReplyRequest) Validate() error {
	if err := r.Target.Validate(); err != nil {
		return err
	}
	if !utf8.ValidString(r.Body) || strings.TrimSpace(r.Body) == "" || len(r.Body) > TrackerDiscussionMaxBodyBytes || strings.TrimSpace(r.ActorID) == "" || strings.TrimSpace(r.IdempotencyKey) == "" || strings.TrimSpace(r.CorrelationID) == "" || strings.TrimSpace(r.CausationID) == "" {
		return errors.New("core: discussion reply requires bounded content and execution identity")
	}
	return nil
}

type TrackerDiscussionReceipt struct {
	ProviderID     string                  `json:"provider_id"`
	Target         TrackerDiscussionTarget `json:"target"`
	IdempotencyKey string                  `json:"idempotency_key"`
	Status         string                  `json:"status"`
	MessageID      string                  `json:"message_id,omitempty"`
	Revision       string                  `json:"revision,omitempty"`
	CanonicalURL   string                  `json:"canonical_url,omitempty"`
	ErrorCode      string                  `json:"error_code,omitempty"`
	RateLimit      TrackerRateLimit        `json:"rate_limit"`
}

func (r TrackerDiscussionReceipt) Validate() error {
	if err := r.Target.Validate(); err != nil {
		return err
	}
	if r.ProviderID != r.Target.ProviderID || strings.TrimSpace(r.IdempotencyKey) == "" {
		return errors.New("core: discussion receipt requires provider and operation identity")
	}
	switch r.Status {
	case "confirmed":
		if strings.TrimSpace(r.MessageID) == "" || strings.TrimSpace(r.Revision) == "" || r.ErrorCode != "" {
			return errors.New("core: confirmed discussion receipt requires message and revision")
		}
		return validateDiscussionURL(r.CanonicalURL)
	case "pending", "failed", "unknown":
		if strings.TrimSpace(r.ErrorCode) == "" {
			return errors.New("core: unconfirmed discussion receipt requires a reason")
		}
		if r.CanonicalURL != "" {
			return validateDiscussionURL(r.CanonicalURL)
		}
		return nil
	default:
		return errors.New("core: unsupported discussion receipt status")
	}
}

type TrackerDiscussionReceiptRequest struct {
	Target         TrackerDiscussionTarget
	IdempotencyKey string
	MessageID      string
}

func (r TrackerDiscussionReceiptRequest) Validate() error {
	if err := r.Target.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		return errors.New("core: discussion receipt lookup requires operation identity")
	}
	return nil
}

func (r TrackerDiscussionReceipt) ValidateFor(request TrackerDiscussionReceiptRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if err := r.Validate(); err != nil {
		return err
	}
	if err := r.Target.ValidateFor(request.Target); err != nil {
		return err
	}
	if r.IdempotencyKey != request.IdempotencyKey || request.MessageID != "" && r.MessageID != "" && r.MessageID != request.MessageID {
		return errors.New("core: discussion receipt differs from the requested operation")
	}
	return nil
}

type TrackerDiscussionWebhookHint struct {
	ProviderID string
	Target     TrackerDiscussionTarget
	DeliveryID string
	MessageID  string
	Revision   string
	Action     string
}

func (h TrackerDiscussionWebhookHint) Validate() error {
	if err := h.Target.Validate(); err != nil {
		return err
	}
	if h.ProviderID != h.Target.ProviderID || strings.TrimSpace(h.DeliveryID) == "" || strings.TrimSpace(h.MessageID) == "" || strings.TrimSpace(h.Revision) == "" {
		return errors.New("core: discussion webhook hint identity and revision are required")
	}
	switch h.Action {
	case "create", "update", "remove":
		return nil
	}
	return errors.New("core: unsupported discussion webhook action")
}

func (h TrackerDiscussionWebhookHint) ValidateFor(target TrackerDiscussionTarget) error {
	if err := h.Validate(); err != nil {
		return err
	}
	return h.Target.ValidateFor(target)
}

type TrackerDiscussionCapabilityProvider interface {
	Provider
	DiscoverTrackerDiscussionCapabilities(context.Context, TrackerDiscussionTarget) (TrackerDiscussionCapabilities, error)
}

type TrackerDiscussionReader interface {
	Provider
	ReadTrackerDiscussion(context.Context, TrackerDiscussionReadRequest) (TrackerDiscussionPage, error)
}

type TrackerDiscussionWriter interface {
	Provider
	ReplyTrackerDiscussion(context.Context, TrackerDiscussionReplyRequest) (TrackerDiscussionReceipt, error)
}

// TrackerDiscussionConditionalWriter refuses replies when the cited parent revision changed.
type TrackerDiscussionConditionalWriter interface {
	TrackerDiscussionWriter
	ValidateTrackerDiscussionReply(context.Context, TrackerDiscussionTarget, string, string) error
	ReplyTrackerDiscussionAtRevision(context.Context, TrackerDiscussionReplyRequest, string) (TrackerDiscussionReceipt, error)
}

type TrackerDiscussionWebhookProvider interface {
	Provider
	VerifyTrackerDiscussionWebhook(context.Context, TrackerDiscussionTarget, TrackerWebhookVerificationRequest) (TrackerDiscussionWebhookHint, error)
}

type TrackerDiscussionReceiptReader interface {
	Provider
	LookupTrackerDiscussionReceipt(context.Context, TrackerDiscussionReceiptRequest) (TrackerDiscussionReceipt, error)
}

func validateDiscussionURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return errors.New("core: discussion URL must be HTTPS without credentials")
	}
	return nil
}
