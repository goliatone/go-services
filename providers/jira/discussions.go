package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/tracker"
)

type discussionIssue struct {
	ID, Key string
	Fields  struct{ Project struct{ ID, Key string } }
}

type discussionVisibility struct {
	Type       string `json:"type"`
	Value      string `json:"value,omitempty"`
	Identifier string `json:"identifier,omitempty"`
}

type discussionComment struct {
	ID, Self, Updated string
	Body              json.RawMessage
	Author            struct {
		AccountID   string `json:"accountId"`
		DisplayName string `json:"displayName"`
		AccountType string `json:"accountType"`
	}
	Visibility *discussionVisibility
	JsdPublic  *bool `json:"jsdPublic"`
}

type discussionContext struct {
	credential  core.ActiveCredential
	endpoint    *url.URL
	issue       discussionIssue
	read, reply bool
}

func discussionError(code, message string) error {
	return core.NewTrackerProviderError(code, message, false, 0, nil)
}

func nativeDiscussionID(id string) bool {
	value, err := strconv.ParseUint(id, 10, 64)
	return err == nil && value > 0 && strconv.FormatUint(value, 10) == id
}

func discussionSegment(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func validateDiscussionTarget(target core.TrackerDiscussionTarget) error {
	if err := target.Validate(); err != nil {
		return err
	}
	if target.ProviderID != ProviderID || !nativeDiscussionID(target.ResourceID) || !discussionSegment(target.ScopeID) {
		return discussionError(core.TrackerErrorValidation, "Jira discussion requires an immutable issue ID and explicit project")
	}
	return nil
}

func (p *Provider) discussionContext(ctx context.Context, target core.TrackerDiscussionTarget) (discussionContext, error) {
	var out discussionContext
	if err := validateDiscussionTarget(target); err != nil {
		return out, err
	}
	credential, endpoint, err := p.resolve(ctx, target.ConnectionID)
	if err != nil {
		return out, err
	}
	out.credential, out.endpoint = credential, endpoint
	if !discussionReadScopeGranted(credential) {
		return out, nil
	}
	issueURL := endpointPath(endpoint, "/rest/api/3/issue/"+target.ResourceID)
	query := issueURL.Query()
	query.Set("fields", "project")
	issueURL.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, issueURL.String(), nil)
	if err != nil {
		return out, err
	}
	if err = p.runtime.DoJSONWithCredential(ctx, credential, request, &out.issue); err != nil {
		return out, err
	}
	project := out.issue.Fields.Project
	if out.issue.ID != target.ResourceID || !discussionSegment(out.issue.Key) || project.ID == "" || project.Key == "" || target.ScopeID != project.ID && target.ScopeID != project.Key {
		return out, discussionError(core.TrackerErrorConflict, "Jira issue differs from the selected issue/project")
	}
	permissionURL := endpointPath(endpoint, "/rest/api/3/mypermissions")
	query = permissionURL.Query()
	query.Set("issueId", target.ResourceID)
	query.Set("permissions", "BROWSE_PROJECTS,ADD_COMMENTS")
	permissionURL.RawQuery = query.Encode()
	request, err = http.NewRequestWithContext(ctx, http.MethodGet, permissionURL.String(), nil)
	if err != nil {
		return out, err
	}
	var permissions struct {
		Permissions map[string]struct {
			HavePermission bool `json:"havePermission"`
		}
	}
	if err = p.runtime.DoJSONWithCredential(ctx, credential, request, &permissions); err != nil {
		return out, err
	}
	out.read = permissions.Permissions["BROWSE_PROJECTS"].HavePermission && discussionReadScopeGranted(credential)
	out.reply = out.read && permissions.Permissions["ADD_COMMENTS"].HavePermission && discussionReplyScopeGranted(credential)
	return out, nil
}

var discussionReadGrants = []string{
	"read:issue-meta:jira",
	"read:issue-security-level:jira",
	"read:issue.vote:jira",
	"read:issue.changelog:jira",
	"read:avatar:jira",
	"read:issue:jira",
	"read:status:jira",
	"read:user:jira",
	"read:field-configuration:jira",
	"read:permission:jira",
	"read:comment:jira",
	"read:comment.property:jira",
	"read:group:jira",
	"read:project:jira",
	"read:project-role:jira",
}

var discussionReplyGrants = []string{
	"read:comment:jira",
	"read:comment.property:jira",
	"read:group:jira",
	"read:project:jira",
	"read:project-role:jira",
	"read:user:jira",
	"write:comment:jira",
	"read:avatar:jira",
}

func discussionReadScopeGranted(credential core.ActiveCredential) bool {
	return discussionScopeGranted(credential, "read:jira-work", discussionReadGrants)
}

func discussionReplyScopeGranted(credential core.ActiveCredential) bool {
	return discussionScopeGranted(credential, "write:jira-work", discussionReplyGrants)
}

func discussionScopeGranted(credential core.ActiveCredential, classic string, granular []string) bool {
	granted := make(map[string]bool, len(credential.GrantedScopes))
	nativeScopes := false
	for _, grant := range credential.GrantedScopes {
		grant = strings.TrimSpace(grant)
		granted[grant] = true
		if strings.HasSuffix(grant, ":jira") || strings.Contains(grant, ":jira-") {
			nativeScopes = true
		}
	}
	if granted[classic] {
		return true
	}
	complete := true
	for _, grant := range granular {
		complete = complete && granted[grant]
	}
	return complete || !nativeScopes && !strings.EqualFold(strings.TrimSpace(credential.TokenType), "bearer")
}

func (p *Provider) DiscoverTrackerDiscussionCapabilities(ctx context.Context, target core.TrackerDiscussionTarget) (core.TrackerDiscussionCapabilities, error) {
	current, err := p.discussionContext(ctx, target)
	if err != nil {
		return core.TrackerDiscussionCapabilities{}, err
	}
	caps := core.TrackerDiscussionCapabilities{
		Revision: tracker.Revision(struct {
			Target      core.TrackerDiscussionTarget
			Read, Reply bool
		}{target, current.read, current.reply}),
		Read:          core.TrackerDiscussionCapability{Reason: "discussion_read_not_granted"},
		Reply:         core.TrackerDiscussionCapability{Reason: "discussion_reply_not_granted"},
		Webhook:       core.TrackerDiscussionCapability{Reason: "verified_discussion_webhook_unsupported"},
		ReceiptLookup: core.TrackerDiscussionCapability{Reason: "operation_receipt_lookup_unsupported"},
	}
	if current.read {
		caps.Read = core.TrackerDiscussionCapability{Available: true}
	} else if !discussionReadScopeGranted(current.credential) {
		caps.Read.Reason = "credential_comment_read_grant_unproven"
	}
	if current.reply {
		caps.Reply = core.TrackerDiscussionCapability{Available: true}
	} else if !discussionReadScopeGranted(current.credential) || !discussionReplyScopeGranted(current.credential) {
		caps.Reply.Reason = "credential_comment_grant_unproven"
	}
	return caps, caps.Validate()
}

type discussionCursor struct {
	Target string `json:"t"`
	Offset int    `json:"o"`
}

func discussionOffset(target core.TrackerDiscussionTarget, cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	if len(cursor) > 384 {
		return 0, discussionError(core.TrackerErrorCursorInvalid, "Jira discussion cursor exceeds its bound")
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	var decoded discussionCursor
	if err != nil || len(raw) > 256 || json.Unmarshal(raw, &decoded) != nil || decoded.Target != tracker.Revision(target) || decoded.Offset < 1 {
		return 0, discussionError(core.TrackerErrorCursorInvalid, "Jira discussion cursor is invalid for this target")
	}
	return decoded.Offset, nil
}

func discussionNextCursor(target core.TrackerDiscussionTarget, offset int) string {
	return base64.RawURLEncoding.EncodeToString(tracker.RawJSON(discussionCursor{tracker.Revision(target), offset}))
}

func (p *Provider) ReadTrackerDiscussion(ctx context.Context, input core.TrackerDiscussionReadRequest) (core.TrackerDiscussionPage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerDiscussionPage{}, err
	}
	offset, err := discussionOffset(input.Target, input.Cursor)
	if err != nil {
		return core.TrackerDiscussionPage{}, err
	}
	current, err := p.discussionContext(ctx, input.Target)
	if err != nil {
		return core.TrackerDiscussionPage{}, err
	}
	if !current.read {
		return core.TrackerDiscussionPage{}, discussionError(core.TrackerErrorPermission, "Jira comment read is not granted")
	}
	limit := tracker.Limit(input.Limit, core.TrackerDiscussionDefaultLimit, core.TrackerDiscussionMaxLimit)
	target := endpointPath(current.endpoint, "/rest/api/3/issue/"+input.Target.ResourceID+"/comment")
	query := target.Query()
	query.Set("startAt", strconv.Itoa(offset))
	query.Set("maxResults", strconv.Itoa(limit))
	query.Set("orderBy", "created")
	target.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return core.TrackerDiscussionPage{}, err
	}
	var response struct {
		Comments                   []discussionComment
		StartAt, MaxResults, Total *int
	}
	metadata, err := p.runtime.DoJSONWithCredentialMetadata(ctx, current.credential, request, &response)
	if err != nil {
		return core.TrackerDiscussionPage{}, err
	}
	if response.Comments == nil || response.StartAt == nil || response.MaxResults == nil || response.Total == nil || *response.StartAt != offset || *response.MaxResults < 1 || *response.Total < offset || len(response.Comments) > limit || len(response.Comments) > *response.MaxResults || len(response.Comments) > *response.Total-offset {
		return core.TrackerDiscussionPage{}, discussionError(core.TrackerErrorExternal, "Jira discussion page is incomplete or exceeds the requested bound")
	}
	page := core.TrackerDiscussionPage{Target: input.Target, HasMore: len(response.Comments) < *response.Total-offset, RateLimit: core.TrackerRateLimit{Remaining: metadata.RateLimitRemaining, ResetAt: metadata.RateLimitReset}}
	if page.HasMore {
		if len(response.Comments) == 0 {
			return core.TrackerDiscussionPage{}, discussionError(core.TrackerErrorExternal, "Jira discussion cursor did not advance")
		}
		page.NextCursor = discussionNextCursor(input.Target, offset+len(response.Comments))
	}
	seen := map[string]bool{}
	for _, comment := range response.Comments {
		message, err := normalizeDiscussionComment(input.Target, current, comment)
		if err != nil {
			return core.TrackerDiscussionPage{}, err
		}
		if seen[message.MessageID] {
			return core.TrackerDiscussionPage{}, discussionError(core.TrackerErrorExternal, "Jira discussion page repeats a comment")
		}
		seen[message.MessageID] = true
		page.Items = append(page.Items, message)
	}
	if err := page.ValidateFor(input); err != nil {
		return core.TrackerDiscussionPage{}, err
	}
	return page, nil
}

func visibilityIdentity(value *discussionVisibility) (string, error) {
	if value == nil {
		return "", nil
	}
	if value.Type != "group" && value.Type != "role" || strings.TrimSpace(value.Identifier) == "" && strings.TrimSpace(value.Value) == "" {
		return "", discussionError(core.TrackerErrorExternal, "Jira comment visibility is invalid")
	}
	return value.Type + ":" + firstNonEmpty(value.Identifier, value.Value), nil
}

func normalizeDiscussionComment(target core.TrackerDiscussionTarget, current discussionContext, comment discussionComment) (core.TrackerDiscussionMessage, error) {
	expected := endpointPath(current.endpoint, "/rest/api/3/issue/"+target.ResourceID+"/comment/"+comment.ID)
	actual, err := url.Parse(comment.Self)
	if !nativeDiscussionID(comment.ID) || err != nil || actual.Scheme != expected.Scheme || actual.Host != expected.Host || actual.Path != expected.Path || actual.User != nil || actual.RawQuery != "" || actual.Fragment != "" {
		return core.TrackerDiscussionMessage{}, discussionError(core.TrackerErrorConflict, "Jira comment differs from the selected site/issue")
	}
	updated, err := time.Parse(time.RFC3339Nano, comment.Updated)
	if err != nil {
		updated, err = time.Parse("2006-01-02T15:04:05.999-0700", comment.Updated)
	}
	if err != nil || updated.IsZero() {
		return core.TrackerDiscussionMessage{}, discussionError(core.TrackerErrorExternal, "Jira comment revision time is invalid")
	}
	visibility, err := visibilityIdentity(comment.Visibility)
	if err != nil {
		return core.TrackerDiscussionMessage{}, err
	}
	body, err := discussionBody(comment.Body)
	if err != nil {
		return core.TrackerDiscussionMessage{}, err
	}
	canonical := endpointPath(current.endpoint, "/browse/"+current.issue.Key)
	query := canonical.Query()
	query.Set("focusedCommentId", comment.ID)
	canonical.RawQuery = query.Encode()
	canonical.Fragment = "comment-" + comment.ID
	var document any
	if err := json.Unmarshal(comment.Body, &document); err != nil {
		return core.TrackerDiscussionMessage{}, err
	}
	revision := tracker.Revision(struct {
		ID                                       string
		Body                                     any
		Author, AccountType, Updated, Visibility string
		Public                                   bool
	}{comment.ID, document, comment.Author.AccountID, comment.Author.AccountType, updated.UTC().Format(time.RFC3339Nano), visibility, comment.JsdPublic == nil || *comment.JsdPublic})
	message := core.TrackerDiscussionMessage{ProviderID: ProviderID, Target: target, MessageID: comment.ID, Revision: revision, CanonicalURL: canonical.String(), Author: core.TrackerDiscussionAuthor{ID: comment.Author.AccountID, Name: comment.Author.DisplayName, Bot: comment.Author.AccountType == "app"}, Body: body, ObservedAt: time.Now().UTC()}
	return message, message.Validate()
}

func (p *Provider) discussionReplyPreflight(ctx context.Context, target core.TrackerDiscussionTarget, parent, revision string) (discussionContext, *discussionVisibility, error) {
	current, err := p.discussionContext(ctx, target)
	if err != nil {
		return current, nil, err
	}
	if !current.reply {
		return current, nil, discussionError(core.TrackerErrorPermission, "Jira comment write permission is not granted")
	}
	if parent == "" {
		if revision != "" {
			return current, nil, discussionError(core.TrackerErrorValidation, "Jira cited revision requires a comment")
		}
		return current, nil, nil
	}
	if !nativeDiscussionID(parent) || revision == "" {
		return current, nil, discussionError(core.TrackerErrorValidation, "Jira cited reply requires a comment ID and revision")
	}
	targetURL := endpointPath(current.endpoint, "/rest/api/3/issue/"+target.ResourceID+"/comment/"+parent)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL.String(), nil)
	if err != nil {
		return current, nil, err
	}
	var comment discussionComment
	if err = p.runtime.DoJSONWithCredential(ctx, current.credential, request, &comment); err != nil {
		return current, nil, err
	}
	message, err := normalizeDiscussionComment(target, current, comment)
	if err != nil {
		return current, nil, err
	}
	if message.MessageID != parent || message.Revision != revision {
		return current, nil, discussionError(core.TrackerErrorConflict, "Jira cited comment changed or was deleted")
	}
	if comment.JsdPublic != nil && !*comment.JsdPublic {
		return current, nil, discussionError(core.TrackerErrorUnavailable, "Jira internal service comments require the Jira Service Management reply API")
	}
	return current, comment.Visibility, nil
}

func (p *Provider) ValidateTrackerDiscussionReply(ctx context.Context, target core.TrackerDiscussionTarget, parent, revision string) error {
	if parent == "" || revision == "" {
		return discussionError(core.TrackerErrorValidation, "Jira conditional reply requires a cited comment")
	}
	_, _, err := p.discussionReplyPreflight(ctx, target, parent, revision)
	return err
}

func (p *Provider) ReplyTrackerDiscussion(ctx context.Context, input core.TrackerDiscussionReplyRequest) (core.TrackerDiscussionReceipt, error) {
	return p.replyDiscussion(ctx, input, "")
}

func (p *Provider) ReplyTrackerDiscussionAtRevision(ctx context.Context, input core.TrackerDiscussionReplyRequest, revision string) (core.TrackerDiscussionReceipt, error) {
	if input.ParentID == "" || revision == "" {
		return core.TrackerDiscussionReceipt{}, discussionError(core.TrackerErrorValidation, "Jira conditional reply requires a cited comment")
	}
	return p.replyDiscussion(ctx, input, revision)
}

func (p *Provider) replyDiscussion(ctx context.Context, input core.TrackerDiscussionReplyRequest, revision string) (core.TrackerDiscussionReceipt, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerDiscussionReceipt{}, err
	}
	receipt := core.TrackerDiscussionReceipt{ProviderID: ProviderID, Target: input.Target, IdempotencyKey: input.IdempotencyKey, Status: "failed", ErrorCode: "discussion_preflight_failed"}
	current, visibility, err := p.discussionReplyPreflight(ctx, input.Target, input.ParentID, revision)
	if err == nil {
		target := endpointPath(current.endpoint, "/rest/api/3/issue/"+input.Target.ResourceID+"/comment")
		var request *http.Request
		request, err = jsonRequest(ctx, http.MethodPost, target.String(), struct {
			Body       adfNode               `json:"body"`
			Visibility *discussionVisibility `json:"visibility,omitempty"`
		}{approvedDiscussionBody(input.Body), visibility})
		if err == nil {
			var comment discussionComment
			metadata, postErr := p.runtime.DoJSONWithCredentialMetadata(ctx, current.credential, request, &comment)
			receipt.RateLimit = core.TrackerRateLimit{Remaining: metadata.RateLimitRemaining, ResetAt: metadata.RateLimitReset}
			if postErr != nil && !metadata.RequestDispatched {
				return failedDiscussionReceipt(receipt, postErr)
			}
			receipt.Status, receipt.ErrorCode = "unknown", "comment_create_unconfirmed"
			if postErr != nil {
				return receipt, receipt.ValidateFor(core.TrackerDiscussionReceiptRequest{Target: input.Target, IdempotencyKey: input.IdempotencyKey})
			}
			message, normalizeErr := normalizeDiscussionComment(input.Target, current, comment)
			expectedVisibility, _ := visibilityIdentity(visibility)
			actualVisibility, visibilityErr := visibilityIdentity(comment.Visibility)
			if normalizeErr != nil || visibilityErr != nil || message.Body != input.Body || actualVisibility != expectedVisibility || comment.JsdPublic != nil && !*comment.JsdPublic {
				receipt.ErrorCode = "comment_create_response_mismatch"
				return receipt, receipt.ValidateFor(core.TrackerDiscussionReceiptRequest{Target: input.Target, IdempotencyKey: input.IdempotencyKey})
			}
			receipt.Status, receipt.ErrorCode, receipt.MessageID, receipt.Revision, receipt.CanonicalURL = "confirmed", "", message.MessageID, message.Revision, message.CanonicalURL
			return receipt, receipt.ValidateFor(core.TrackerDiscussionReceiptRequest{Target: input.Target, IdempotencyKey: input.IdempotencyKey})
		}
	}
	return failedDiscussionReceipt(receipt, err)
}

func failedDiscussionReceipt(receipt core.TrackerDiscussionReceipt, err error) (core.TrackerDiscussionReceipt, error) {
	var providerErr *core.TrackerProviderError
	if errors.As(err, &providerErr) {
		receipt.ErrorCode = providerErr.Code
		if providerErr.Code == core.TrackerErrorNotFound || providerErr.Code == core.TrackerErrorConflict {
			receipt.ErrorCode = "source_revision_changed"
		}
		if providerErr.RetryAfter > 0 {
			receipt.RateLimit.ResetAt = time.Now().UTC().Add(providerErr.RetryAfter)
		}
	}
	return receipt, receipt.ValidateFor(core.TrackerDiscussionReceiptRequest{Target: receipt.Target, IdempotencyKey: receipt.IdempotencyKey})
}

var _ core.TrackerDiscussionCapabilityProvider = (*Provider)(nil)
var _ core.TrackerDiscussionReader = (*Provider)(nil)
var _ core.TrackerDiscussionConditionalWriter = (*Provider)(nil)
