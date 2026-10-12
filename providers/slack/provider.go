package slack

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}
type Config struct {
	Credentials                            CredentialResolver
	SigningSecrets                         SigningSecretResolver
	HTTPClient                             HTTPDoer
	BaseURL, ClientID, ClientSecret, AppID string
	Now                                    func() time.Time
}
type Provider struct{ config Config }

func New(cfg Config) (*Provider, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://slack.com/api"
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && slices.Contains([]string{"localhost", "127.0.0.1", "::1"}, u.Hostname()))) {
		return nil, ErrInvalid
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Provider{config: cfg}, nil
}
func (p *Provider) ID() string { return ProviderID }
func (p *Provider) AuthKind() core.AuthKind {
	if p.config.ClientID != "" {
		return core.AuthKindOAuth2AuthCode
	}
	return core.AuthKindPAT
}
func (p *Provider) SupportedScopeTypes() []string { return []string{"user", "org"} }
func (p *Provider) Capabilities() []core.CapabilityDescriptor {
	return []core.CapabilityDescriptor{
		{Name: "messages.write", RequiredGrants: []string{"chat:write"}, DeniedBehavior: core.CapabilityDeniedBehaviorBlock},
		{Name: "channels.read", OptionalGrants: []string{"channels:read", "groups:read"}, DeniedBehavior: core.CapabilityDeniedBehaviorDegrade},
		{Name: "events.http", DeniedBehavior: core.CapabilityDeniedBehaviorBlock},
	}
}
func (p *Provider) credential(ctx context.Context, id string) (core.ActiveCredential, error) {
	if strings.TrimSpace(id) == "" || p.config.Credentials == nil {
		return core.ActiveCredential{}, ErrUnavailable
	}
	c, err := p.config.Credentials.ResolveSlackCredential(ctx, id)
	if err != nil || strings.TrimSpace(c.AccessToken) == "" {
		return core.ActiveCredential{}, ErrDenied
	}
	if c.ConnectionID != "" && c.ConnectionID != id {
		return core.ActiveCredential{}, ErrDenied
	}
	if c.ExpiresAt != nil && !p.config.Now().Before(*c.ExpiresAt) {
		return core.ActiveCredential{}, ErrDenied
	}
	return c, nil
}
func (p *Provider) call(ctx context.Context, c core.ActiveCredential, method string, payload any) ([]byte, http.Header, int, bool, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, 0, false, ErrInvalid
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.config.BaseURL, "/")+"/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, nil, 0, false, ErrInvalid
	}
	if method == "auth.test" || method == "conversations.list" || method == "conversations.replies" {
		values := map[string]any{}
		if json.Unmarshal(body, &values) != nil {
			return nil, nil, 0, false, ErrInvalid
		}
		query := req.URL.Query()
		for name, value := range values {
			query.Set(name, fmt.Sprint(value))
		}
		req.URL.RawQuery = query.Encode()
		req.Method = http.MethodGet
		req.Body = nil
		req.ContentLength = 0
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	if ctx.Err() != nil {
		return nil, nil, 0, false, ctx.Err()
	}
	response, err := p.config.HTTPClient.Do(req)
	if err != nil {
		return nil, nil, 0, true, ErrUnavailable
	}
	if response == nil || response.Body == nil {
		return nil, nil, 0, true, ErrUnavailable
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxEventBytes+1))
	if err != nil || len(data) > MaxEventBytes {
		return nil, response.Header, response.StatusCode, true, ErrUnavailable
	}
	return data, response.Header, response.StatusCode, true, nil
}

type logicalResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

func logical(data []byte, status int) error {
	if status < 200 || status >= 300 {
		return ErrUnavailable
	}
	var response logicalResponse
	if json.Unmarshal(data, &response) != nil {
		return ErrUnavailable
	}
	if !response.OK {
		return fmt.Errorf("slack: %s", safeCode(response.Error))
	}
	return nil
}
func safeCode(code string) string {
	known := []string{"not_authed", "invalid_auth", "token_revoked", "account_inactive", "missing_scope", "not_in_channel", "channel_not_found", "is_archived", "ratelimited", "rate_limited", "invalid_arguments", "invalid_arg_name", "thread_not_found"}
	if slices.Contains(known, code) {
		return code
	}
	return "provider_rejected"
}
func (p *Provider) inspect(ctx context.Context, c core.ActiveCredential) (Installation, error) {
	data, header, status, _, err := p.call(ctx, c, "auth.test", map[string]any{})
	if err != nil {
		return Installation{}, err
	}
	if err = logical(data, status); err != nil {
		return Installation{}, err
	}
	var response struct {
		TeamID       string `json:"team_id"`
		Team         string `json:"team"`
		EnterpriseID string `json:"enterprise_id"`
		BotID        string `json:"bot_id"`
		UserID       string `json:"user_id"`
	}
	if json.Unmarshal(data, &response) != nil || response.TeamID == "" || response.UserID == "" {
		return Installation{}, ErrUnavailable
	}
	grants := append([]string(nil), c.GrantedScopes...)
	if advertised := header.Get("X-OAuth-Scopes"); advertised != "" {
		grants = nil
		for grant := range strings.SplitSeq(advertised, ",") {
			if grant = strings.TrimSpace(grant); grant != "" {
				grants = append(grants, grant)
			}
		}
	}
	return Installation{TeamID: response.TeamID, TeamLabel: response.Team, EnterpriseID: response.EnterpriseID, BotID: response.BotID, BotUserID: response.UserID, Grants: grants}, nil
}
func (p *Provider) Inspect(ctx context.Context, id string) (Installation, error) {
	c, err := p.credential(ctx, id)
	if err != nil {
		return Installation{}, err
	}
	return p.inspect(ctx, c)
}
func (p *Provider) Channels(ctx context.Context, input ChannelRequest) (ChannelPage, error) {
	if input.Limit < 0 || input.Limit > 100 || len(input.Cursor) > 4096 {
		return ChannelPage{}, ErrInvalid
	}
	if input.Limit == 0 {
		input.Limit = 25
	}
	c, err := p.credential(ctx, input.ConnectionID)
	if err != nil {
		return ChannelPage{}, err
	}
	data, _, status, _, err := p.call(ctx, c, "conversations.list", map[string]any{"limit": input.Limit, "cursor": input.Cursor, "exclude_archived": true, "types": "public_channel,private_channel"})
	if err != nil {
		return ChannelPage{}, err
	}
	if err = logical(data, status); err != nil {
		return ChannelPage{}, err
	}
	var response struct {
		Channels []struct{ ID, Name string }
		Metadata struct {
			Next string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if json.Unmarshal(data, &response) != nil || len(response.Channels) > input.Limit {
		return ChannelPage{}, ErrUnavailable
	}
	result := ChannelPage{Channels: []Channel{}, NextCursor: response.Metadata.Next}
	for _, channel := range response.Channels {
		if channel.ID == "" {
			return ChannelPage{}, ErrUnavailable
		}
		result.Channels = append(result.Channels, Channel{ID: channel.ID, Label: channel.Name})
	}
	return result, nil
}
func clientMessageID(id string) string {
	sum := sha256.Sum256([]byte("slack.message:" + id))
	value := hex.EncodeToString(sum[:16])
	return value[:8] + "-" + value[8:12] + "-5" + value[13:16] + "-a" + value[17:20] + "-" + value[20:]
}
func (p *Provider) Send(ctx context.Context, input MessageRequest) (MessageReceipt, error) {
	result := MessageReceipt{OperationID: input.OperationID, State: Unsent, Reason: "invalid_input"}
	if err := input.Validate(); err != nil {
		return result, err
	}
	c, err := p.credential(ctx, input.ConnectionID)
	if err != nil {
		result.Reason = "credential_unavailable"
		return result, nil
	}
	install, err := p.inspect(ctx, c)
	if err != nil {
		result.Reason = "installation_unavailable"
		return result, nil
	}
	if install.TeamID != input.TeamID {
		result.Reason = "selection_required"
		return result, nil
	}
	if !slices.Contains(install.Grants, "chat:write") {
		result.Reason = "grant_missing"
		return result, nil
	}
	data, header, status, dispatched, err := p.call(ctx, c, "chat.postMessage", map[string]any{"channel": input.ChannelID, "thread_ts": input.ThreadID, "text": input.Body, "client_msg_id": clientMessageID(input.OperationID), "unfurl_links": false, "unfurl_media": false})
	if err != nil {
		if dispatched {
			result.State, result.Reason = Unknown, "outcome_unknown"
		} else {
			result.Reason = "request_not_sent"
		}
		return result, nil
	}
	if status == 429 {
		result.State, result.Reason = RateLimited, "rate_limited"
		at := p.retryAt(header)
		result.RetryAt = &at
		return result, nil
	}
	if status >= 500 || status >= 300 && status < 400 {
		result.State, result.Reason = Unknown, "outcome_unknown"
		return result, nil
	}
	if status < 200 || status >= 300 {
		result.Reason = "provider_rejected"
		return result, nil
	}
	var response struct {
		logicalResponse
		Channel string `json:"channel"`
		TS      string `json:"ts"`
		Message struct {
			Thread string `json:"thread_ts"`
		} `json:"message"`
	}
	if json.Unmarshal(data, &response) != nil {
		result.State, result.Reason = Unknown, "outcome_unknown"
		return result, nil
	}
	if !response.OK {
		result.Reason = safeCode(response.Error)
		if result.Reason == "ratelimited" || result.Reason == "rate_limited" {
			result.State = RateLimited
			at := p.retryAt(header)
			result.RetryAt = &at
		}
		return result, nil
	}
	result.State, result.Reason = Sent, ""
	if input.ThreadID == "" {
		response.Message.Thread = response.TS
	}
	result.External = &MessageIdentity{TeamID: install.TeamID, ChannelID: response.Channel, ThreadID: response.Message.Thread, MessageID: response.TS, URL: "https://app.slack.com/archives/" + url.PathEscape(response.Channel) + "/p" + strings.ReplaceAll(response.TS, ".", "")}
	if err := result.ValidateFor(input); err != nil {
		return MessageReceipt{OperationID: input.OperationID, State: Unknown, Reason: "receipt_mismatch"}, nil
	}
	return result, nil
}
func (p *Provider) retryAt(header http.Header) time.Time {
	seconds, err := strconv.ParseInt(header.Get("Retry-After"), 10, 64)
	if err != nil || seconds <= 0 || seconds > 86400 {
		seconds = 60
	}
	return p.config.Now().Add(time.Duration(seconds) * time.Second).UTC()
}
func (p *Provider) Reconcile(ctx context.Context, input MessageRequest) (MessageReceipt, error) {
	result := MessageReceipt{OperationID: input.OperationID, State: Unknown, Reason: "outcome_unknown"}
	if err := input.Validate(); err != nil {
		return result, err
	}
	c, err := p.credential(ctx, input.ConnectionID)
	if err != nil {
		return result, nil
	}
	install, err := p.inspect(ctx, c)
	if err != nil || install.TeamID != input.TeamID {
		return result, nil
	}
	data, _, status, _, err := p.call(ctx, c, "conversations.replies", map[string]any{"channel": input.ChannelID, "ts": input.ThreadID, "limit": 100})
	if err != nil || logical(data, status) != nil {
		return result, nil
	}
	var response struct {
		Messages []struct {
			TS       string `json:"ts"`
			Thread   string `json:"thread_ts"`
			ClientID string `json:"client_msg_id"`
		}
	}
	if json.Unmarshal(data, &response) != nil {
		return result, nil
	}
	for _, message := range response.Messages {
		if message.ClientID != clientMessageID(input.OperationID) {
			continue
		}
		if result.External != nil {
			return MessageReceipt{OperationID: input.OperationID, State: Unknown, Reason: "multiple_receipts"}, nil
		}
		result.State, result.Reason = Sent, ""
		result.External = &MessageIdentity{TeamID: install.TeamID, ChannelID: input.ChannelID, ThreadID: message.Thread, MessageID: message.TS, URL: "https://app.slack.com/archives/" + url.PathEscape(input.ChannelID) + "/p" + strings.ReplaceAll(message.TS, ".", "")}
	}
	if result.State == Sent {
		if result.ValidateFor(input) != nil {
			return MessageReceipt{OperationID: input.OperationID, State: Unknown, Reason: "receipt_mismatch"}, nil
		}
	}
	return result, nil
}

var _ core.Provider = (*Provider)(nil)
var _ Messaging = (*Provider)(nil)

func (p *Provider) HTTPAvailable(ctx context.Context, id string) bool {
	if p.config.SigningSecrets == nil {
		return false
	}
	secret, err := p.config.SigningSecrets.ResolveSlackSigningSecret(ctx, id)
	return err == nil && len(secret) > 0
}
