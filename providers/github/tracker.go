package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

const defaultAPIURL = "https://api.github.com"

type Provider struct {
	core.Provider
	runtime trackerruntime.Runtime
}

type repository struct {
	credentialWriteProven bool
	ID                    int64  `json:"id"`
	FullName              string `json:"full_name"`
	HTMLURL               string `json:"html_url"`
	Updated               string `json:"updated_at"`
	Private               bool   `json:"private"`
	Visibility            string `json:"visibility"`
	Archived              bool   `json:"archived"`
	HasIssues             bool   `json:"has_issues"`
	Owner                 struct {
		Login string `json:"login"`
	} `json:"owner"`
	Permissions struct {
		Admin    bool `json:"admin"`
		Maintain bool `json:"maintain"`
		Push     bool `json:"push"`
		Triage   bool `json:"triage"`
		Pull     bool `json:"pull"`
	} `json:"permissions"`
}

type issue struct {
	ID          int64           `json:"id"`
	Number      int             `json:"number"`
	Title       string          `json:"title"`
	Body        string          `json:"body"`
	State       string          `json:"state"`
	StateReason string          `json:"state_reason"`
	HTMLURL     string          `json:"html_url"`
	UpdatedAt   time.Time       `json:"updated_at"`
	User        json.RawMessage `json:"user"`
	Assignees   json.RawMessage `json:"assignees"`
	Labels      json.RawMessage `json:"labels"`
	Milestone   json.RawMessage `json:"milestone"`
	PullRequest json.RawMessage `json:"pull_request"`
}

func (p *Provider) DiscoverTrackerScopes(ctx context.Context, input core.TrackerDiscoveryRequest) (core.TrackerNodePage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerNodePage{}, err
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerNodePage{}, err
	}
	page, err := trackerruntime.Page(input.Cursor)
	if err != nil {
		return core.TrackerNodePage{}, err
	}
	limit := trackerruntime.Limit(input.Limit, 100, 100)
	requestURL := *endpoint
	requestURL.Path += "/user/repos"
	query := requestURL.Query()
	query.Set("affiliation", "owner,collaborator,organization_member")
	query.Set("sort", "updated")
	query.Set("per_page", strconv.Itoa(limit))
	query.Set("page", strconv.Itoa(page))
	requestURL.RawQuery = query.Encode()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	var response []repository
	if err := p.runtime.DoJSONWithCredential(ctx, credential, request, &response); err != nil {
		return core.TrackerNodePage{}, err
	}
	items := make([]core.TrackerNode, 0, len(response))
	for _, repo := range response {
		capabilities := []string{}
		if repo.HasIssues && (repo.Permissions.Pull || repo.Permissions.Triage || repo.Permissions.Push || repo.Permissions.Maintain || repo.Permissions.Admin) {
			capabilities = append(capabilities, "issues.read")
		}
		if repo.HasIssues && !repo.Archived && (repo.Permissions.Triage || repo.Permissions.Push || repo.Permissions.Maintain || repo.Permissions.Admin) {
			capabilities = append(capabilities, "issues.write")
		}
		if !repo.Archived && repo.Permissions.Admin {
			capabilities = append(capabilities, "webhook.manage")
		}
		visibility := strings.TrimSpace(repo.Visibility)
		if visibility == "" {
			visibility = "public"
			if repo.Private {
				visibility = "private"
			}
		}
		items = append(items, core.TrackerNode{ExternalID: repo.FullName, Type: "repository", Name: repo.FullName, Capabilities: capabilities, NativeRevision: firstNonEmpty(repo.Updated, strconv.FormatInt(repo.ID, 10)), Metadata: map[string]any{"repository_id": strconv.FormatInt(repo.ID, 10), "canonical_url": repo.HTMLURL, "owner": repo.Owner.Login, "visibility": visibility, "archived": repo.Archived}})
	}
	hasMore := len(response) == limit
	next := ""
	if hasMore {
		next = strconv.Itoa(page + 1)
	}
	return core.TrackerNodePage{Items: items, NextCursor: next, HasMore: hasMore, Revision: trackerruntime.Revision(items)}, nil
}

func (p *Provider) DiscoverTrackerSchema(_ context.Context, input core.TrackerDiscoveryRequest) (core.TrackerSchemaPage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerSchemaPage{}, err
	}
	fields := []core.TrackerField{
		{ID: "title", Name: "Title", Type: "string", Required: true, NativeRevision: "github.issue.v1"},
		{ID: "body", Name: "Body", Type: "markdown", NativeRevision: "github.issue.v1"},
		{ID: "state", Name: "State", Type: "string", Required: true, NativeRevision: "github.issue.v1"},
		{ID: "state_reason", Name: "State reason", Type: "string", NativeRevision: "github.issue.v1"},
		{ID: "labels", Name: "Labels", Type: "list", NativeRevision: "github.issue.v1"},
		{ID: "assignees", Name: "Assignees", Type: "list", NativeRevision: "github.issue.v1"},
		{ID: "milestone", Name: "Milestone", Type: "object", NativeRevision: "github.issue.v1"},
	}
	return core.TrackerSchemaPage{Items: fields, Revision: trackerruntime.Revision(fields)}, nil
}

func (p *Provider) ListTrackerChanges(ctx context.Context, input core.TrackerChangesRequest) (core.TrackerChangePage, error) {
	if err := input.Validate(); err != nil {
		return core.TrackerChangePage{}, err
	}
	if input.ResourceType != "repository" {
		return core.TrackerChangePage{}, core.NewTrackerProviderError(core.TrackerErrorSchemaChanged, "GitHub issues require a repository resource", false, 0, nil)
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.TrackerChangePage{}, err
	}
	page, err := trackerruntime.Page(input.Cursor)
	if err != nil {
		return core.TrackerChangePage{}, err
	}
	limit := trackerruntime.Limit(input.Limit, 100, 100)
	requestURL := *endpoint
	requestURL.Path += "/repos/" + escapePath(input.ResourceID) + "/issues"
	query := requestURL.Query()
	query.Set("state", "all")
	query.Set("sort", "updated")
	query.Set("direction", "asc")
	query.Set("per_page", strconv.Itoa(limit))
	query.Set("page", strconv.Itoa(page))
	requestURL.RawQuery = query.Encode()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	var response []issue
	if err := p.runtime.DoJSONWithCredential(ctx, credential, request, &response); err != nil {
		return core.TrackerChangePage{}, err
	}
	observedAt := time.Now().UTC()
	items := make([]core.TrackerResource, 0, len(response))
	for _, item := range response {
		if len(item.PullRequest) != 0 && string(item.PullRequest) != "null" {
			continue
		}
		normalized := map[string]any{"title": item.Title, "body": item.Body, "state": item.State, "state_reason": item.StateReason, "user": item.User, "assignees": item.Assignees, "labels": item.Labels, "milestone": item.Milestone}
		native, _ := json.Marshal(item)
		items = append(items, core.TrackerResource{ProviderID: ProviderID, ResourceType: "issue", ExternalID: strconv.FormatInt(item.ID, 10), ProviderRevision: item.UpdatedAt.UTC().Format(time.RFC3339Nano), CanonicalURL: item.HTMLURL, ObservedAt: observedAt, NormalizedFields: trackerruntime.RawJSON(normalized), NativeExtension: native, SchemaRevision: "github.issue.v1"})
	}
	hasMore := len(response) == limit
	next := ""
	if hasMore {
		next = strconv.Itoa(page + 1)
	}
	return core.TrackerChangePage{Items: items, NextCursor: next, HasMore: hasMore}, nil
}

func (p *Provider) Subscribe(ctx context.Context, input core.SubscribeRequest) (core.SubscriptionResult, error) {
	if err := validateGitHubSubscribe(input); err != nil {
		return core.SubscriptionResult{}, err
	}
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return core.SubscriptionResult{}, err
	}
	requestURL := *endpoint
	requestURL.Path += "/repos/" + escapePath(input.ResourceID) + "/hooks"
	payload, err := githubWebhookPayload(input)
	if err != nil {
		return core.SubscriptionResult{}, err
	}
	existingID, err := p.findGitHubHook(ctx, credential, requestURL, input.CallbackURL)
	if err != nil {
		return core.SubscriptionResult{}, err
	}
	method := http.MethodPost
	if existingID != "" {
		method = http.MethodPatch
		requestURL.Path += "/" + url.PathEscape(existingID)
	}
	request, _ := jsonRequest(ctx, method, requestURL.String(), payload)
	var response struct {
		ID int64 `json:"id"`
	}
	if err := p.runtime.DoJSONWithCredential(ctx, credential, request, &response); err != nil {
		return core.SubscriptionResult{}, redactGitHubSecret(err, metadataString(input.Metadata, "webhook_secret"))
	}
	id := strconv.FormatInt(response.ID, 10)
	return core.SubscriptionResult{ChannelID: id, RemoteSubscriptionID: id}, nil
}

func redactGitHubSecret(err error, secret string) error {
	secret = strings.TrimSpace(secret)
	if err == nil || secret == "" || !strings.Contains(err.Error(), secret) {
		return err
	}
	var providerErr *core.TrackerProviderError
	if errors.As(err, &providerErr) {
		return core.NewTrackerProviderError(providerErr.Code, strings.ReplaceAll(providerErr.Message, secret, "[redacted]"), providerErr.Retryable, providerErr.RetryAfter, nil)
	}
	return errors.New(strings.ReplaceAll(err.Error(), secret, "[redacted]"))
}

func validateGitHubSubscribe(input core.SubscribeRequest) error {
	callback, err := url.Parse(strings.TrimSpace(input.CallbackURL))
	if strings.TrimSpace(input.ConnectionID) == "" || input.ResourceType != "repository" || strings.TrimSpace(input.ResourceID) == "" || err != nil || callback.Scheme != "https" || callback.Host == "" {
		return errors.New("github: webhook connection, repository, and HTTPS callback are required")
	}
	return nil
}

func (p *Provider) findGitHubHook(ctx context.Context, credential core.ActiveCredential, endpoint url.URL, callbackURL string) (string, error) {
	for page := 1; page <= 10; page++ {
		requestURL := endpoint
		query := requestURL.Query()
		query.Set("per_page", "100")
		query.Set("page", strconv.Itoa(page))
		requestURL.RawQuery = query.Encode()
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
		var hooks []struct {
			ID     int64          `json:"id"`
			Active bool           `json:"active"`
			Events []string       `json:"events"`
			Config map[string]any `json:"config"`
		}
		if err := p.runtime.DoJSONWithCredential(ctx, credential, request, &hooks); err != nil {
			return "", err
		}
		for _, hook := range hooks {
			urlValue, _ := hook.Config["url"].(string)
			if strings.TrimRight(strings.TrimSpace(urlValue), "/") == strings.TrimRight(strings.TrimSpace(callbackURL), "/") && containsString(hook.Events, "issues") {
				return strconv.FormatInt(hook.ID, 10), nil
			}
		}
		if len(hooks) < 100 {
			return "", nil
		}
	}
	return "", core.NewTrackerProviderError(core.TrackerErrorExternal, "GitHub hook discovery exceeded page limit", false, 0, nil)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func githubWebhookPayload(input core.SubscribeRequest) (map[string]any, error) {
	secret := strings.TrimSpace(metadataString(input.Metadata, "webhook_secret"))
	if secret == "" {
		return nil, errors.New("github: webhook secret is required")
	}
	return map[string]any{"name": "web", "active": true, "events": []string{"issues"}, "config": map[string]any{"url": input.CallbackURL, "content_type": "json", "insecure_ssl": "0", "secret": secret}}, nil
}

func VerifyWebhookSignature(secret, body []byte, signature string) bool {
	if len(secret) == 0 || len(body) == 0 || !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	return hmac.Equal(provided, mac.Sum(nil))
}

func (p *Provider) VerifyTrackerWebhook(_ context.Context, request core.TrackerWebhookVerificationRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if !VerifyWebhookSignature(request.Secret, request.Body, request.Signature) {
		return core.NewTrackerProviderError(core.TrackerErrorPermission, "GitHub webhook signature is invalid", false, 0, nil)
	}
	return nil
}

func metadataString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func (p *Provider) RenewSubscription(_ context.Context, input core.RenewSubscriptionRequest) (core.SubscriptionResult, error) {
	return core.SubscriptionResult{ChannelID: input.RemoteSubscriptionID, RemoteSubscriptionID: input.RemoteSubscriptionID}, nil
}

func (p *Provider) CancelSubscription(ctx context.Context, input core.CancelSubscriptionRequest) error {
	credential, endpoint, err := p.resolve(ctx, input.ConnectionID)
	if err != nil {
		return err
	}
	requestURL := *endpoint
	requestURL.Path += "/repos/" + escapePath(input.ResourceID) + "/hooks/" + url.PathEscape(input.RemoteSubscriptionID)
	request, _ := http.NewRequestWithContext(ctx, http.MethodDelete, requestURL.String(), nil)
	return p.runtime.DoJSONWithCredential(ctx, credential, request, nil)
}

func (p *Provider) resolve(ctx context.Context, connectionID string) (core.ActiveCredential, *url.URL, error) {
	credential, err := p.runtime.ResolveCredential(ctx, connectionID)
	if err != nil {
		return core.ActiveCredential{}, nil, err
	}
	endpoint, err := trackerruntime.Endpoint(credential, "api_url", defaultAPIURL)
	return credential, endpoint, err
}

func escapePath(value string) string {
	parts := strings.Split(strings.Trim(value, "/"), "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return strings.Join(parts, "/")
}

func jsonRequest(ctx context.Context, method, target string, payload any) (*http.Request, error) {
	body := strings.NewReader(string(trackerruntime.RawJSON(payload)))
	request, err := http.NewRequestWithContext(ctx, method, target, body)
	if err == nil {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	return request, err
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "unknown"
}

var _ core.TrackerProvider = (*Provider)(nil)
var _ core.SubscribableProvider = (*Provider)(nil)
var _ core.TrackerWebhookVerificationProvider = (*Provider)(nil)
