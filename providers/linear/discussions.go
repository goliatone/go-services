package linear

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/tracker"
)

const discussionCommentFields = `id body updatedAt archivedAt url parentId user{id name} externalUser{id name} botActor{id name} issue{id team{id}}`

type discussionIssue struct {
	ID   string
	Team struct{ ID string }
}

type discussionComment struct {
	ID, Body, URL, ParentID string
	UpdatedAt               time.Time
	ArchivedAt              *time.Time
	User, ExternalUser      *struct{ ID, Name string }
	BotActor                *struct{ ID, Name string }
	Issue                   discussionIssue
}

func validateDiscussionTarget(target core.TrackerDiscussionTarget) error {
	if err := target.Validate(); err != nil {
		return err
	}
	if target.ProviderID != ProviderID {
		return core.NewTrackerProviderError(core.TrackerErrorValidation, "Linear discussion requires a Linear issue", false, 0, nil)
	}
	return nil
}

func (p *Provider) discussionContext(ctx context.Context, target core.TrackerDiscussionTarget) (core.ActiveCredential, string, error) {
	if err := validateDiscussionTarget(target); err != nil {
		return core.ActiveCredential{}, "", err
	}
	credential, endpoint, err := p.resolve(ctx, target.ConnectionID)
	if err != nil {
		return credential, endpoint, err
	}
	var response tracker.GraphQLResponse[struct{ Issue discussionIssue }]
	query := `query($issue:String!){issue(id:$issue){id team{id}}}`
	if err = p.doGraphQL(ctx, &credential, endpoint, query, map[string]any{"issue": target.ResourceID}, &response); err != nil {
		return credential, endpoint, err
	}
	if err = graphqlErrors(response.Errors); err != nil {
		return credential, endpoint, err
	}
	if response.Data.Issue.ID != target.ResourceID || response.Data.Issue.Team.ID != target.ScopeID {
		return credential, endpoint, core.NewTrackerProviderError(core.TrackerErrorPermission, "Linear issue is unavailable in the selected team", false, 0, nil)
	}
	return credential, endpoint, nil
}

func discussionReplyGranted(credential core.ActiveCredential) bool {
	for _, grant := range credential.GrantedScopes {
		switch strings.ToLower(strings.TrimSpace(grant)) {
		case "write", "comments:create":
			return true
		}
	}
	return false
}

func (p *Provider) DiscoverTrackerDiscussionCapabilities(ctx context.Context, target core.TrackerDiscussionTarget) (core.TrackerDiscussionCapabilities, error) {
	credential, _, err := p.discussionContext(ctx, target)
	if err != nil {
		return core.TrackerDiscussionCapabilities{}, err
	}
	reply := core.TrackerDiscussionCapability{Reason: "credential_comment_grant_unproven"}
	if discussionReplyGranted(credential) {
		reply = core.TrackerDiscussionCapability{Available: true}
	}
	caps := core.TrackerDiscussionCapabilities{
		Revision: tracker.Revision(struct {
			Target core.TrackerDiscussionTarget
			Reply  bool
		}{target, reply.Available}),
		Read: core.TrackerDiscussionCapability{Available: true}, Reply: reply,
		Webhook:       core.TrackerDiscussionCapability{Reason: "per_subscription_signing_secret_unavailable"},
		ReceiptLookup: core.TrackerDiscussionCapability{Reason: "operation_receipt_lookup_unsupported"},
	}
	return caps, caps.Validate()
}

func normalizeDiscussionComment(target core.TrackerDiscussionTarget, item discussionComment) (core.TrackerDiscussionMessage, error) {
	if item.Issue.ID != target.ResourceID || item.Issue.Team.ID != target.ScopeID || item.UpdatedAt.IsZero() {
		return core.TrackerDiscussionMessage{}, core.NewTrackerProviderError(core.TrackerErrorConflict, "Linear comment differs from the selected issue/team or omits revision", false, 0, nil)
	}
	u, err := url.Parse(item.URL)
	if err != nil || u.Scheme != "https" || u.Host != "linear.app" || u.User != nil {
		return core.TrackerDiscussionMessage{}, core.NewTrackerProviderError(core.TrackerErrorExternal, "Linear comment URL is invalid", false, 0, nil)
	}
	tombstone := item.ArchivedAt != nil
	body := item.Body
	if tombstone {
		body = ""
	}
	author := core.TrackerDiscussionAuthor{Bot: item.BotActor != nil}
	if item.BotActor != nil && strings.TrimSpace(item.BotActor.ID) != "" {
		author.ID, author.Name = "bot:"+item.BotActor.ID, item.BotActor.Name
	} else if item.User != nil && strings.TrimSpace(item.User.ID) != "" {
		author.ID, author.Name = item.User.ID, item.User.Name
	} else if item.ExternalUser != nil && strings.TrimSpace(item.ExternalUser.ID) != "" {
		author.ID, author.Name = "external:"+item.ExternalUser.ID, item.ExternalUser.Name
	}
	revision := tracker.Revision(struct {
		ID, Body, ParentID, UpdatedAt string
		Tombstone                     bool
	}{item.ID, body, item.ParentID, item.UpdatedAt.UTC().Format(time.RFC3339Nano), tombstone})
	message := core.TrackerDiscussionMessage{ProviderID: ProviderID, Target: target, MessageID: item.ID, ParentID: item.ParentID, Revision: revision, CanonicalURL: item.URL, Author: author, Body: body, Tombstone: tombstone, ObservedAt: time.Now().UTC()}
	return message, message.Validate()
}

func (p *Provider) ReadTrackerDiscussion(ctx context.Context, input core.TrackerDiscussionReadRequest) (core.TrackerDiscussionPage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerDiscussionPage{}, err
	}
	credential, endpoint, err := p.discussionContext(ctx, input.Target)
	if err != nil {
		return core.TrackerDiscussionPage{}, err
	}
	query := `query($issue:String!,$first:Int!,$after:String){issue(id:$issue){id team{id} comments(first:$first,after:$after,includeArchived:true){nodes{` + discussionCommentFields + `}pageInfo{hasNextPage endCursor}}}}`
	var response tracker.GraphQLResponse[struct {
		Issue struct {
			discussionIssue
			Comments *struct {
				Nodes    []discussionComment
				PageInfo *pageInfo
			}
		}
	}]
	if err = p.doGraphQL(ctx, &credential, endpoint, query, map[string]any{"issue": input.Target.ResourceID, "first": tracker.Limit(input.Limit, core.TrackerDiscussionDefaultLimit, core.TrackerDiscussionMaxLimit), "after": nullable(input.Cursor)}, &response); err != nil {
		return core.TrackerDiscussionPage{}, err
	}
	if err = graphqlErrors(response.Errors); err != nil {
		return core.TrackerDiscussionPage{}, err
	}
	if response.Data.Issue.ID != input.Target.ResourceID || response.Data.Issue.Team.ID != input.Target.ScopeID {
		return core.TrackerDiscussionPage{}, core.NewTrackerProviderError(core.TrackerErrorConflict, "Linear discussion changed issue/team", false, 0, nil)
	}
	if response.Data.Issue.Comments == nil || response.Data.Issue.Comments.PageInfo == nil || response.Data.Issue.Comments.Nodes == nil {
		return core.TrackerDiscussionPage{}, core.NewTrackerProviderError(core.TrackerErrorExternal, "Linear discussion omitted its comment page", false, 0, nil)
	}
	page := core.TrackerDiscussionPage{Target: input.Target, HasMore: response.Data.Issue.Comments.PageInfo.HasNextPage}
	if page.HasMore {
		page.NextCursor = response.Data.Issue.Comments.PageInfo.EndCursor
	}
	for _, item := range response.Data.Issue.Comments.Nodes {
		message, err := normalizeDiscussionComment(input.Target, item)
		if err != nil {
			return core.TrackerDiscussionPage{}, err
		}
		page.Items = append(page.Items, message)
	}
	if page.HasMore && page.NextCursor == input.Cursor {
		return core.TrackerDiscussionPage{}, core.NewTrackerProviderError(core.TrackerErrorExternal, "Linear discussion cursor did not advance", false, 0, nil)
	}
	if err = page.ValidateFor(input); err != nil {
		return core.TrackerDiscussionPage{}, err
	}
	return page, nil
}

func (p *Provider) checkDiscussionParent(ctx context.Context, credential *core.ActiveCredential, endpoint string, target core.TrackerDiscussionTarget, parent, revision string) error {
	if strings.TrimSpace(parent) == "" || strings.TrimSpace(revision) == "" {
		return core.NewTrackerProviderError(core.TrackerErrorValidation, "Linear cited reply requires a parent revision", false, 0, nil)
	}
	query := `query($id:String!){comment(id:$id){` + discussionCommentFields + `}}`
	var response tracker.GraphQLResponse[struct{ Comment discussionComment }]
	if err := p.doGraphQL(ctx, credential, endpoint, query, map[string]any{"id": parent}, &response); err != nil {
		return err
	}
	if err := graphqlErrors(response.Errors); err != nil {
		return err
	}
	message, err := normalizeDiscussionComment(target, response.Data.Comment)
	if err != nil {
		return err
	}
	if message.MessageID != parent || message.Tombstone || message.Revision != revision {
		return core.NewTrackerProviderError(core.TrackerErrorConflict, "Linear cited comment changed or was deleted", false, 0, nil)
	}
	return nil
}

func (p *Provider) ValidateTrackerDiscussionReply(ctx context.Context, target core.TrackerDiscussionTarget, parent, revision string) error {
	credential, endpoint, err := p.discussionContext(ctx, target)
	if err != nil {
		return err
	}
	if !discussionReplyGranted(credential) {
		return core.NewTrackerProviderError(core.TrackerErrorPermission, "Linear comment write grant is unproven", false, 0, nil)
	}
	if err = p.checkDiscussionParent(ctx, &credential, endpoint, target, parent, revision); err != nil {
		return err
	}
	if !discussionReplyGranted(credential) {
		return core.NewTrackerProviderError(core.TrackerErrorPermission, "Linear comment write grant is unproven", false, 0, nil)
	}
	return nil
}

func (p *Provider) ReplyTrackerDiscussion(ctx context.Context, input core.TrackerDiscussionReplyRequest) (core.TrackerDiscussionReceipt, error) {
	return p.replyDiscussion(ctx, input, "")
}
func (p *Provider) ReplyTrackerDiscussionAtRevision(ctx context.Context, input core.TrackerDiscussionReplyRequest, revision string) (core.TrackerDiscussionReceipt, error) {
	if input.ParentID == "" || revision == "" {
		return core.TrackerDiscussionReceipt{}, core.NewTrackerProviderError(core.TrackerErrorValidation, "Linear conditional reply requires a cited parent", false, 0, nil)
	}
	return p.replyDiscussion(ctx, input, revision)
}

func (p *Provider) replyDiscussion(ctx context.Context, input core.TrackerDiscussionReplyRequest, revision string) (core.TrackerDiscussionReceipt, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerDiscussionReceipt{}, err
	}
	receipt := core.TrackerDiscussionReceipt{ProviderID: ProviderID, Target: input.Target, IdempotencyKey: input.IdempotencyKey, Status: "failed", ErrorCode: "discussion_preflight_failed"}
	credential, endpoint, err := p.discussionContext(ctx, input.Target)
	if err == nil && !discussionReplyGranted(credential) {
		err = core.NewTrackerProviderError(core.TrackerErrorPermission, "Linear comment write grant is unproven", false, 0, nil)
	}
	if err == nil && input.ParentID != "" {
		err = p.checkDiscussionParent(ctx, &credential, endpoint, input.Target, input.ParentID, revision)
	}
	if err == nil && !discussionReplyGranted(credential) {
		err = core.NewTrackerProviderError(core.TrackerErrorPermission, "Linear comment write grant is unproven", false, 0, nil)
	}
	if err != nil {
		if providerErr, ok := errors.AsType[*core.TrackerProviderError](err); ok {
			receipt.ErrorCode = providerErr.Code
			if providerErr.RetryAfter > 0 {
				receipt.RateLimit.ResetAt = time.Now().UTC().Add(providerErr.RetryAfter)
			}
		}
		return receipt, receipt.Validate()
	}
	fields := map[string]any{"issueId": input.Target.ResourceID, "body": input.Body}
	if input.ParentID != "" {
		fields["parentId"] = input.ParentID
	}
	query := `mutation($input:CommentCreateInput!){commentCreate(input:$input){success comment{` + discussionCommentFields + `}}}`
	var response tracker.GraphQLResponse[struct {
		CommentCreate struct {
			Success bool
			Comment discussionComment
		}
	}]
	// A possible mutation must never be repeated by OAuth refresh handling.
	err = p.runtime.DoGraphQL(ctx, credential, endpoint, query, map[string]any{"input": fields}, &response)
	if err == nil {
		err = graphqlErrors(response.Errors)
	}
	if err != nil || !response.Data.CommentCreate.Success {
		receipt.Status, receipt.ErrorCode = "unknown", "comment_create_unconfirmed"
		return receipt, nil
	}
	message, err := normalizeDiscussionComment(input.Target, response.Data.CommentCreate.Comment)
	if err != nil || message.Tombstone || message.Body != input.Body || message.ParentID != input.ParentID {
		receipt.Status, receipt.ErrorCode = "unknown", "comment_create_response_mismatch"
		return receipt, nil
	}
	receipt.Status, receipt.ErrorCode, receipt.MessageID, receipt.Revision, receipt.CanonicalURL = "confirmed", "", message.MessageID, message.Revision, message.CanonicalURL
	return receipt, receipt.Validate()
}

var _ core.TrackerDiscussionCapabilityProvider = (*Provider)(nil)
var _ core.TrackerDiscussionReader = (*Provider)(nil)
var _ core.TrackerDiscussionConditionalWriter = (*Provider)(nil)
