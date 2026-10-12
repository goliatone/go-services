package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	trackerruntime "github.com/goliatone/go-services/providers/tracker"
)

type discussionComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
	UpdatedAt time.Time `json:"updated_at"`
	User      struct {
		ID      int64  `json:"id"`
		Login   string `json:"login"`
		HTMLURL string `json:"html_url"`
		Type    string `json:"type"`
	} `json:"user"`
}

type discussionPageClient struct {
	client trackerruntime.HTTPDoer
	next   bool
}

func (c *discussionPageClient) Do(request *http.Request) (*http.Response, error) {
	response, err := c.client.Do(request)
	if err != nil {
		return response, err
	}
	for link := range strings.SplitSeq(response.Header.Get("Link"), ",") {
		for _, parameter := range strings.Split(link, ";")[1:] {
			if strings.TrimSpace(parameter) == `rel="next"` || strings.TrimSpace(parameter) == "rel=next" {
				c.next = true
			}
		}
	}
	return response, nil
}

type discussionIdentity struct {
	ID     int64
	Number int
}

func discussionTarget(target core.TrackerDiscussionTarget) (discussionIdentity, error) {
	if err := target.Validate(); err != nil {
		return discussionIdentity{}, err
	}
	parts := strings.Split(target.ResourceID, ":")
	repo := strings.Split(target.ScopeID, "/")
	if target.ProviderID != ProviderID || len(parts) != 2 || len(repo) != 2 || repo[0] == "" || repo[1] == "" || strings.ContainsAny(target.ScopeID, "?#%\\") || repo[0] == "." || repo[0] == ".." || repo[1] == "." || repo[1] == ".." {
		return discussionIdentity{}, discussionError(core.TrackerErrorValidation, "explicit GitHub repository and issue identity required")
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	number, numberErr := strconv.Atoi(parts[1])
	if err != nil || numberErr != nil || id < 1 || number < 1 || strconv.FormatInt(id, 10) != parts[0] || strconv.Itoa(number) != parts[1] {
		return discussionIdentity{}, discussionError(core.TrackerErrorValidation, "GitHub discussion identity must contain immutable issue ID and number")
	}
	return discussionIdentity{ID: id, Number: number}, nil
}

func discussionError(category string, reason string) error {
	return core.NewTrackerProviderError(category, reason, false, 0, nil)
}

func (p *Provider) discussionPreflight(ctx context.Context, target core.TrackerDiscussionTarget) (core.ActiveCredential, *url.URL, repository, discussionIdentity, error) {
	identity, err := discussionTarget(target)
	if err != nil {
		return core.ActiveCredential{}, nil, repository{}, identity, err
	}
	credential, endpoint, err := p.resolve(ctx, target.ConnectionID)
	if err != nil {
		return credential, endpoint, repository{}, identity, err
	}
	repo, err := p.loadRepository(ctx, credential, *endpoint, target.ScopeID)
	if err != nil {
		return credential, endpoint, repo, identity, err
	}
	issueURL := *endpoint
	issueURL.Path += "/repos/" + escapePath(target.ScopeID) + "/issues/" + strconv.Itoa(identity.Number)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, issueURL.String(), nil)
	if err != nil {
		return credential, endpoint, repo, identity, err
	}
	var current issue
	if err = p.runtime.DoJSONWithCredential(ctx, credential, request, &current); err != nil {
		return credential, endpoint, repo, identity, err
	}
	if current.ID != identity.ID || current.Number != identity.Number || len(current.PullRequest) > 0 && string(current.PullRequest) != "null" {
		return credential, endpoint, repo, identity, discussionError(core.TrackerErrorConflict, "discussion target is not the selected issue")
	}
	return credential, endpoint, repo, identity, nil
}

func discussionCapabilities(repo repository) core.TrackerDiscussionCapabilities {
	read := repo.HasIssues && (repo.Permissions.Pull || repo.Permissions.Triage || repo.Permissions.Push || repo.Permissions.Maintain || repo.Permissions.Admin)
	reply := read && !repo.Archived && repo.credentialWriteProven
	capabilities := core.TrackerDiscussionCapabilities{
		Revision:      trackerruntime.Revision(map[string]any{"repository": repo, "credential_write_proven": repo.credentialWriteProven}),
		Read:          core.TrackerDiscussionCapability{Available: read, Reason: "discussion_read_not_granted"},
		Reply:         core.TrackerDiscussionCapability{Available: reply, Reason: "discussion_reply_grant_unproven"},
		Webhook:       core.TrackerDiscussionCapability{Available: true},
		ReceiptLookup: core.TrackerDiscussionCapability{Reason: "operation_receipt_lookup_unsupported"},
	}
	if read {
		capabilities.Read.Reason = ""
	}
	if reply {
		capabilities.Reply.Reason = ""
	} else if repo.Archived {
		capabilities.Reply.Reason = "repository_archived"
	}
	return capabilities
}

func (p *Provider) DiscoverTrackerDiscussionCapabilities(ctx context.Context, target core.TrackerDiscussionTarget) (core.TrackerDiscussionCapabilities, error) {
	_, _, repo, _, err := p.discussionPreflight(ctx, target)
	if err != nil {
		return core.TrackerDiscussionCapabilities{}, err
	}
	capabilities := discussionCapabilities(repo)
	return capabilities, capabilities.Validate()
}

func discussionRevision(comment discussionComment, deleted bool) string {
	return trackerruntime.Revision(struct {
		ID        int64
		Body      string
		UpdatedAt time.Time
		Deleted   bool
	}{comment.ID, comment.Body, comment.UpdatedAt.UTC(), deleted})
}

func validateDiscussionCommentIdentity(target core.TrackerDiscussionTarget, identity discussionIdentity, comment discussionComment) error {
	expected := "https://github.com/" + target.ScopeID + "/issues/" + strconv.Itoa(identity.Number) + "#issuecomment-" + strconv.FormatInt(comment.ID, 10)
	if comment.ID < 1 || comment.UpdatedAt.IsZero() || !strings.EqualFold(comment.HTMLURL, expected) {
		return discussionError(core.TrackerErrorExternal, "comment response differs from selected issue or lacks identity")
	}
	return nil
}

func discussionMessage(target core.TrackerDiscussionTarget, identity discussionIdentity, comment discussionComment) (core.TrackerDiscussionMessage, error) {
	if err := validateDiscussionCommentIdentity(target, identity, comment); err != nil {
		return core.TrackerDiscussionMessage{}, err
	}
	if comment.User.ID < 1 {
		return core.TrackerDiscussionMessage{}, discussionError(core.TrackerErrorExternal, "live comment author is missing")
	}
	message := core.TrackerDiscussionMessage{ProviderID: ProviderID, Target: target, MessageID: strconv.FormatInt(comment.ID, 10), Revision: discussionRevision(comment, false), CanonicalURL: comment.HTMLURL,
		Author: core.TrackerDiscussionAuthor{ID: strconv.FormatInt(comment.User.ID, 10), Name: comment.User.Login, URL: comment.User.HTMLURL, Bot: comment.User.Type == "Bot"}, Body: comment.Body, ObservedAt: time.Now().UTC()}
	return message, message.Validate()
}

func (p *Provider) ReadTrackerDiscussion(ctx context.Context, input core.TrackerDiscussionReadRequest) (core.TrackerDiscussionPage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerDiscussionPage{}, err
	}
	pageNumber, err := trackerruntime.Page(input.Cursor)
	if err != nil {
		return core.TrackerDiscussionPage{}, err
	}
	credential, endpoint, repo, identity, err := p.discussionPreflight(ctx, input.Target)
	if err != nil {
		return core.TrackerDiscussionPage{}, err
	}
	if !discussionCapabilities(repo).Read.Available {
		return core.TrackerDiscussionPage{}, discussionError(core.TrackerErrorPermission, "discussion_read_not_granted")
	}
	limit := trackerruntime.Limit(input.Limit, core.TrackerDiscussionDefaultLimit, core.TrackerDiscussionMaxLimit)
	requestURL := *endpoint
	requestURL.Path += "/repos/" + escapePath(input.Target.ScopeID) + "/issues/" + strconv.Itoa(identity.Number) + "/comments"
	query := requestURL.Query()
	query.Set("page", strconv.Itoa(pageNumber))
	query.Set("per_page", strconv.Itoa(limit))
	requestURL.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return core.TrackerDiscussionPage{}, err
	}
	var comments []discussionComment
	capture := &discussionPageClient{client: p.runtime.HTTPClient}
	if capture.client == nil {
		capture.client = &http.Client{Timeout: 30 * time.Second}
	}
	runtime := p.runtime
	runtime.HTTPClient = capture
	metadata, err := runtime.DoJSONWithCredentialMetadata(ctx, credential, request, &comments)
	if err != nil {
		return core.TrackerDiscussionPage{}, err
	}
	page := core.TrackerDiscussionPage{Target: input.Target, RateLimit: core.TrackerRateLimit{Remaining: metadata.RateLimitRemaining, ResetAt: metadata.RateLimitReset}}
	for _, comment := range comments {

		message, err := discussionMessage(input.Target, identity, comment)
		if err != nil {
			return core.TrackerDiscussionPage{}, err
		}
		page.Items = append(page.Items, message)
	}
	if capture.next {
		page.HasMore = true
		page.NextCursor = strconv.Itoa(pageNumber + 1)
	}
	if err := page.ValidateFor(input); err != nil {
		return core.TrackerDiscussionPage{}, err
	}
	return page, nil
}

func (p *Provider) discussionReplyPreflight(ctx context.Context, target core.TrackerDiscussionTarget, parentID, expectedRevision string) (core.ActiveCredential, *url.URL, discussionIdentity, error) {
	credential, endpoint, repo, identity, err := p.discussionPreflight(ctx, target)
	if err != nil {
		return credential, endpoint, identity, err
	}
	if !discussionCapabilities(repo).Reply.Available {
		return credential, endpoint, identity, discussionError(core.TrackerErrorPermission, discussionCapabilities(repo).Reply.Reason)
	}
	if parentID == "" {
		if expectedRevision != "" {
			return credential, endpoint, identity, discussionError(core.TrackerErrorValidation, "parent revision requires a parent comment")
		}
		return credential, endpoint, identity, nil
	}
	id, err := strconv.ParseInt(parentID, 10, 64)
	if err != nil || id < 1 || strings.TrimSpace(expectedRevision) == "" {
		return credential, endpoint, identity, discussionError(core.TrackerErrorValidation, "parent comment requires identity and approved revision")
	}
	parentURL := *endpoint
	parentURL.Path += "/repos/" + escapePath(target.ScopeID) + "/issues/comments/" + strconv.FormatInt(id, 10)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parentURL.String(), nil)
	if err != nil {
		return credential, endpoint, identity, err
	}
	var parent discussionComment
	if err = p.runtime.DoJSONWithCredential(ctx, credential, request, &parent); err != nil {
		return credential, endpoint, identity, err
	}
	if parent.ID != id {
		return credential, endpoint, identity, discussionError(core.TrackerErrorConflict, "parent comment identity changed")
	}
	message, err := discussionMessage(target, identity, parent)
	if err != nil {
		return credential, endpoint, identity, err
	}
	if message.Revision != expectedRevision {
		return credential, endpoint, identity, discussionError(core.TrackerErrorConflict, "parent comment revision changed")
	}
	return credential, endpoint, identity, nil
}

func (p *Provider) ValidateTrackerDiscussionReply(ctx context.Context, target core.TrackerDiscussionTarget, parentID, expectedRevision string) error {
	_, _, _, err := p.discussionReplyPreflight(ctx, target, parentID, expectedRevision)
	return err
}

func (p *Provider) ReplyTrackerDiscussion(ctx context.Context, input core.TrackerDiscussionReplyRequest) (core.TrackerDiscussionReceipt, error) {
	return p.ReplyTrackerDiscussionAtRevision(ctx, input, "")
}

func (p *Provider) ReplyTrackerDiscussionAtRevision(ctx context.Context, input core.TrackerDiscussionReplyRequest, expectedRevision string) (core.TrackerDiscussionReceipt, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerDiscussionReceipt{}, err
	}
	credential, endpoint, identity, err := p.discussionReplyPreflight(ctx, input.Target, input.ParentID, expectedRevision)
	if err != nil {
		return unsentDiscussionReceipt(input, err)
	}

	requestURL := *endpoint
	requestURL.Path += "/repos/" + escapePath(input.Target.ScopeID) + "/issues/" + strconv.Itoa(identity.Number) + "/comments"
	request, err := jsonRequest(ctx, http.MethodPost, requestURL.String(), map[string]string{"body": input.Body})
	if err != nil {
		return unsentDiscussionReceipt(input, err)
	}
	var comment discussionComment
	metadata, err := p.runtime.DoJSONWithCredentialMetadata(ctx, credential, request, &comment)
	if err != nil {
		return core.TrackerDiscussionReceipt{}, err
	}
	if comment.Body != input.Body {
		return core.TrackerDiscussionReceipt{}, discussionError(core.TrackerErrorExternal, "reply response differs from approved content")
	}
	message, err := discussionMessage(input.Target, identity, comment)
	if err != nil {
		return core.TrackerDiscussionReceipt{}, err
	}
	receipt := core.TrackerDiscussionReceipt{ProviderID: ProviderID, Target: input.Target, IdempotencyKey: input.IdempotencyKey, Status: "confirmed", MessageID: message.MessageID, Revision: message.Revision, CanonicalURL: message.CanonicalURL, RateLimit: core.TrackerRateLimit{Remaining: metadata.RateLimitRemaining, ResetAt: metadata.RateLimitReset}}
	return receipt, receipt.ValidateFor(core.TrackerDiscussionReceiptRequest{Target: input.Target, IdempotencyKey: input.IdempotencyKey})
}

func unsentDiscussionReceipt(input core.TrackerDiscussionReplyRequest, err error) (core.TrackerDiscussionReceipt, error) {
	receipt := core.TrackerDiscussionReceipt{ProviderID: ProviderID, Target: input.Target, IdempotencyKey: input.IdempotencyKey, Status: "failed", ErrorCode: "discussion_preflight_failed"}
	if providerErr, ok := errors.AsType[*core.TrackerProviderError](err); ok {
		receipt.ErrorCode = providerErr.Code
		if providerErr.Code == core.TrackerErrorConflict || providerErr.Code == core.TrackerErrorNotFound {
			receipt.ErrorCode = "source_revision_changed"
		}
		if providerErr.RetryAfter > 0 {
			receipt.RateLimit.ResetAt = time.Now().UTC().Add(providerErr.RetryAfter)
		}
	}
	return receipt, receipt.ValidateFor(core.TrackerDiscussionReceiptRequest{Target: input.Target, IdempotencyKey: input.IdempotencyKey})
}

func (p *Provider) VerifyTrackerDiscussionWebhook(ctx context.Context, target core.TrackerDiscussionTarget, request core.TrackerWebhookVerificationRequest) (core.TrackerDiscussionWebhookHint, error) {
	identity, err := discussionTarget(target)
	if err != nil {
		return core.TrackerDiscussionWebhookHint{}, err
	}
	if err = p.VerifyTrackerWebhook(ctx, request); err != nil {
		return core.TrackerDiscussionWebhookHint{}, err
	}
	if request.Event != "issue_comment" {
		return core.TrackerDiscussionWebhookHint{}, discussionError(core.TrackerErrorValidation, "unsupported discussion webhook event")
	}
	var payload struct {
		Action     string            `json:"action"`
		Repository repository        `json:"repository"`
		Issue      issue             `json:"issue"`
		Comment    discussionComment `json:"comment"`
	}
	if err = json.Unmarshal(request.Body, &payload); err != nil {
		return core.TrackerDiscussionWebhookHint{}, discussionError(core.TrackerErrorValidation, "invalid discussion webhook body")
	}
	if !strings.EqualFold(payload.Repository.FullName, target.ScopeID) || payload.Issue.ID != identity.ID || payload.Issue.Number != identity.Number || len(payload.Issue.PullRequest) > 0 && string(payload.Issue.PullRequest) != "null" {
		return core.TrackerDiscussionWebhookHint{}, discussionError(core.TrackerErrorPermission, "discussion webhook target differs from selection")
	}
	action := ""
	switch payload.Action {
	case "created":
		action = "create"
	case "edited":
		action = "update"
	case "deleted":
		action = "remove"
	default:
		return core.TrackerDiscussionWebhookHint{}, discussionError(core.TrackerErrorValidation, "unsupported discussion webhook action")
	}
	if action == "remove" {
		if err = validateDiscussionCommentIdentity(target, identity, payload.Comment); err != nil {
			return core.TrackerDiscussionWebhookHint{}, err
		}
	} else if _, err = discussionMessage(target, identity, payload.Comment); err != nil {
		return core.TrackerDiscussionWebhookHint{}, err
	}
	hint := core.TrackerDiscussionWebhookHint{ProviderID: ProviderID, Target: target, DeliveryID: request.DeliveryID, MessageID: strconv.FormatInt(payload.Comment.ID, 10), Revision: discussionRevision(payload.Comment, action == "remove"), Action: action}
	return hint, hint.ValidateFor(target)
}

var _ core.TrackerDiscussionCapabilityProvider = (*Provider)(nil)
var _ core.TrackerDiscussionReader = (*Provider)(nil)
var _ core.TrackerDiscussionWriter = (*Provider)(nil)
var _ core.TrackerDiscussionWebhookProvider = (*Provider)(nil)

var _ core.TrackerDiscussionConditionalWriter = (*Provider)(nil)
