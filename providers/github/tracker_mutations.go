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

var githubIssueFields = []string{"title", "description", "state", "labels", "assignees", "milestone"}

func (p *Provider) DiscoverTrackerFieldCapabilities(ctx context.Context, input core.TrackerFieldCapabilityRequest) ([]core.TrackerFieldCapability, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return nil, err
	}
	repo, err := p.loadRepository(ctx, credential, *endpoint, input.RepositoryID)
	if err != nil {
		return nil, err
	}
	read := repo.HasIssues && (repo.Permissions.Pull || repo.Permissions.Triage || repo.Permissions.Push || repo.Permissions.Maintain || repo.Permissions.Admin)
	revision := trackerruntime.Revision(map[string]any{"repository_id": repo.ID, "updated_at": repo.Updated, "has_issues": repo.HasIssues, "archived": repo.Archived, "permissions": repo.Permissions, "credential_write_proven": repo.credentialWriteProven})
	values := make([]core.TrackerFieldCapability, 0, len(githubIssueFields)*3)
	for _, field := range githubIssueFields {
		values = append(values, trackerFieldCapability(field, "read", read, "issues_not_readable", revision))
		write := githubFieldWriteGranted(repo, field)
		values = append(values, trackerFieldCapability(field, "update", write, githubWriteDenial(repo), revision))
		createGranted := write && field != "state"
		reason := githubWriteDenial(repo)
		if field == "state" && write {
			reason = "GitHub creates issues in the open state"
		}
		values = append(values, trackerFieldCapability(field, "create", createGranted, reason, revision))
	}
	return values, nil
}

func trackerFieldCapability(field, operation string, granted bool, reason, revision string) core.TrackerFieldCapability {
	if granted {
		reason = ""
	}
	return core.TrackerFieldCapability{Field: field, Operation: operation, Granted: granted, Reason: reason, Revision: revision}
}

func githubWriteDenial(repo repository) string {
	if repo.Archived {
		return "repository_archived"
	}
	if !repo.credentialWriteProven {
		return "credential_write_grant_unproven"
	}
	return "issues_not_writable"
}

func (p *Provider) CreateTrackerIssue(ctx context.Context, input core.TrackerIssueCreateRequest) (core.TrackerMutationReceipt, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	payload, err := githubCreateIssuePayload(input.Fields)
	if err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	fields := make([]string, 0, len(input.Fields))
	for field := range input.Fields {
		fields = append(fields, field)
	}
	if _, err := p.requireRepositoryWrite(ctx, credential, *endpoint, input.RepositoryID, fields...); err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	requestURL := *endpoint
	requestURL.Path += "/repos/" + escapePath(input.RepositoryID) + "/issues"
	request, _ := jsonRequest(ctx, http.MethodPost, requestURL.String(), payload)
	request.Header.Set("X-Idempotency-Key", input.IdempotencyKey)
	var created issue
	metadata, err := p.runtime.DoJSONWithCredentialMetadata(ctx, credential, request, &created)
	if err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	receipt, err := githubMutationReceipt(input.RepositoryID, created, metadata)
	receipt.CorrelationID, receipt.CausationID = input.CorrelationID, input.CausationID
	return receipt, err
}

func (p *Provider) UpdateTrackerIssue(ctx context.Context, input core.TrackerIssueUpdateRequest) (core.TrackerMutationReceipt, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	payload, err := githubUpdateIssuePayload(input.Field, input.Value)
	if err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	if _, err := p.requireRepositoryWrite(ctx, credential, *endpoint, input.RepositoryID, input.Field); err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	requestURL := *endpoint
	requestURL.Path += "/repos/" + escapePath(input.RepositoryID) + "/issues/" + strconv.Itoa(input.IssueNumber)
	currentRequest, _ := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	var current issue
	if _, err := p.runtime.DoJSONWithCredentialMetadata(ctx, credential, currentRequest, &current); err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	if strconv.FormatInt(current.ID, 10) != input.IssueID || githubIssueRevision(current) != input.ExpectedRevision {
		return core.TrackerMutationReceipt{}, core.NewTrackerProviderError(core.TrackerErrorConflict, "GitHub issue revision changed", false, 0, nil)
	}
	request, _ := jsonRequest(ctx, http.MethodPatch, requestURL.String(), payload)
	request.Header.Set("X-Idempotency-Key", input.IdempotencyKey)
	var updated issue
	metadata, err := p.runtime.DoJSONWithCredentialMetadata(ctx, credential, request, &updated)
	if err != nil {
		return core.TrackerMutationReceipt{}, err
	}
	if strconv.FormatInt(updated.ID, 10) != input.IssueID {
		return core.TrackerMutationReceipt{}, core.NewTrackerProviderError(core.TrackerErrorConflict, "GitHub issue identity changed", false, 0, nil)
	}
	receipt, err := githubMutationReceipt(input.RepositoryID, updated, metadata)
	receipt.CorrelationID, receipt.CausationID = input.CorrelationID, input.CausationID
	return receipt, err
}

func githubCreateIssuePayload(fields map[string]json.RawMessage) (map[string]any, error) {
	payload := map[string]any{}
	for field, raw := range fields {
		if field == "state" {
			return nil, core.NewTrackerProviderError(core.TrackerErrorValidation, "GitHub issue create does not accept state", false, 0, nil)
		}
		mapped, value, err := githubIssueFieldValue(field, raw)
		if err != nil {
			return nil, err
		}
		payload[mapped] = value
	}
	title, ok := payload["title"].(string)
	if !ok || strings.TrimSpace(title) == "" {
		return nil, core.NewTrackerProviderError(core.TrackerErrorValidation, "GitHub issue title is required", false, 0, nil)
	}
	return payload, nil
}

func githubUpdateIssuePayload(field string, raw json.RawMessage) (map[string]any, error) {
	mapped, value, err := githubIssueFieldValue(field, raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{mapped: value}, nil
}

func githubIssueFieldValue(field string, raw json.RawMessage) (string, any, error) {
	mapped := field
	if field == "description" {
		mapped = "body"
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", nil, core.NewTrackerProviderError(core.TrackerErrorValidation, "GitHub issue field is invalid JSON", false, 0, err)
	}
	switch field {
	case "title", "description", "state":
		text, ok := value.(string)
		if !ok || field == "title" && strings.TrimSpace(text) == "" || field == "state" && text != "open" && text != "closed" {
			return "", nil, core.NewTrackerProviderError(core.TrackerErrorValidation, "GitHub issue scalar field is invalid", false, 0, nil)
		}
	case "labels", "assignees":
		items, ok := value.([]any)
		if !ok {
			return "", nil, core.NewTrackerProviderError(core.TrackerErrorValidation, "GitHub issue collection field is invalid", false, 0, nil)
		}
		for _, item := range items {
			if _, ok := item.(string); !ok {
				return "", nil, core.NewTrackerProviderError(core.TrackerErrorValidation, "GitHub issue collection values must be strings", false, 0, nil)
			}
		}
	case "milestone":
		if value != nil {
			number, ok := value.(float64)
			if !ok || number < 1 || number != float64(int(number)) {
				return "", nil, core.NewTrackerProviderError(core.TrackerErrorValidation, "GitHub issue milestone must be a positive integer or null", false, 0, nil)
			}
		}
	default:
		return "", nil, core.NewTrackerProviderError(core.TrackerErrorValidation, "unsupported GitHub issue field", false, 0, errors.New(field))
	}
	return mapped, value, nil
}

func githubIssueRevision(value issue) string {
	return value.UpdatedAt.UTC().Format(time.RFC3339Nano)
}

func (p *Provider) loadRepository(ctx context.Context, credential core.ActiveCredential, endpoint url.URL, repositoryID string) (repository, error) {
	requestURL := endpoint
	requestURL.Path += "/repos/" + escapePath(repositoryID)
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	var repo repository
	metadata, err := p.runtime.DoJSONWithCredentialMetadata(ctx, credential, request, &repo)
	if err != nil {
		return repository{}, err
	}
	if repo.ID < 1 || !strings.EqualFold(repo.FullName, repositoryID) {
		return repository{}, core.NewTrackerProviderError(core.TrackerErrorConflict, "GitHub repository identity changed", false, 0, nil)
	}
	for _, scope := range metadata.OAuthScopes {
		if scope == "repo" || scope == "public_repo" && !repo.Private {
			repo.credentialWriteProven = true
		}
	}
	return repo, nil
}

func githubFieldWriteGranted(repo repository, field string) bool {
	if !repo.HasIssues || repo.Archived || !repo.credentialWriteProven {
		return false
	}
	push := repo.Permissions.Push || repo.Permissions.Maintain || repo.Permissions.Admin
	switch field {
	case "title", "description", "state":
		return push || repo.Permissions.Triage
	case "labels", "assignees", "milestone":
		return push
	default:
		return false
	}
}

func (p *Provider) requireRepositoryWrite(ctx context.Context, credential core.ActiveCredential, endpoint url.URL, repositoryID string, fields ...string) (repository, error) {
	repo, err := p.loadRepository(ctx, credential, endpoint, repositoryID)
	if err != nil {
		return repository{}, err
	}
	for _, field := range fields {
		if !githubFieldWriteGranted(repo, field) {
			return repository{}, core.NewTrackerProviderError(core.TrackerErrorPermission, githubWriteDenial(repo), false, 0, nil)
		}
	}
	return repo, nil
}

func githubMutationReceipt(repositoryID string, value issue, metadata trackerruntime.ResponseMetadata) (core.TrackerMutationReceipt, error) {
	if value.ID < 1 || value.Number < 1 || value.UpdatedAt.IsZero() {
		return core.TrackerMutationReceipt{}, core.NewTrackerProviderError(core.TrackerErrorExternal, "GitHub mutation response omitted issue identity", false, 0, nil)
	}
	receipt := core.TrackerMutationReceipt{ProviderID: ProviderID, RepositoryID: repositoryID, ExternalID: strconv.FormatInt(value.ID, 10), DisplayID: strconv.Itoa(value.Number), CanonicalURL: value.HTMLURL, ProviderRevision: githubIssueRevision(value), Status: "succeeded", RequestID: metadata.RequestID, RateLimit: core.TrackerRateLimit{Remaining: metadata.RateLimitRemaining, ResetAt: metadata.RateLimitReset}, Metadata: map[string]any{"etag": metadata.ETag, "last_modified": metadata.LastModified}}
	return receipt, receipt.Validate()
}

var _ core.TrackerMutationProvider = (*Provider)(nil)
