package slack

import (
	"context"
	"encoding/json"
	"github.com/goliatone/go-services/core"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (p *Provider) BeginAuth(_ context.Context, r core.BeginAuthRequest) (core.BeginAuthResponse, error) {
	if p.config.ClientID == "" || r.State == "" || r.RedirectURI == "" {
		return core.BeginAuthResponse{}, ErrUnavailable
	}
	redirect, err := url.Parse(r.RedirectURI)
	if err != nil || redirect.Host == "" || (redirect.Scheme != "https" && redirect.Hostname() != "localhost" && redirect.Hostname() != "127.0.0.1") {
		return core.BeginAuthResponse{}, ErrInvalid
	}
	values := url.Values{"client_id": {p.config.ClientID}, "scope": {strings.Join(r.RequestedGrants, ",")}, "state": {r.State}, "redirect_uri": {r.RedirectURI}}
	return core.BeginAuthResponse{URL: "https://slack.com/oauth/v2/authorize?" + values.Encode(), State: r.State, RequestedGrants: append([]string(nil), r.RequestedGrants...)}, nil
}
func (p *Provider) exchange(ctx context.Context, values url.Values) (core.CompleteAuthResponse, error) {
	if p.config.ClientID == "" || p.config.ClientSecret == "" {
		return core.CompleteAuthResponse{}, ErrUnavailable
	}
	values.Set("client_id", p.config.ClientID)
	values.Set("client_secret", p.config.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.config.BaseURL, "/")+"/oauth.v2.access", strings.NewReader(values.Encode()))
	if err != nil {
		return core.CompleteAuthResponse{}, ErrInvalid
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := p.config.HTTPClient.Do(req)
	if err != nil || response == nil || response.Body == nil {
		return core.CompleteAuthResponse{}, ErrUnavailable
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxEventBytes+1))
	if err != nil || len(data) > MaxEventBytes || logical(data, response.StatusCode) != nil {
		return core.CompleteAuthResponse{}, ErrUnavailable
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scope        string
		TokenType    string `json:"token_type"`
		Expires      int64  `json:"expires_in"`
		BotUserID    string `json:"bot_user_id"`
		Team         struct{ ID, Name string }
	}
	if json.Unmarshal(data, &token) != nil || token.AccessToken == "" || token.Team.ID == "" || token.BotUserID == "" {
		return core.CompleteAuthResponse{}, ErrUnavailable
	}
	grants := []string{}
	for _, grant := range strings.Split(token.Scope, ",") {
		if grant = strings.TrimSpace(grant); grant != "" {
			grants = append(grants, grant)
		}
	}
	credential := core.ActiveCredential{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, TokenType: token.TokenType, GrantedScopes: grants, Refreshable: token.RefreshToken != "", Metadata: map[string]any{"team_id": token.Team.ID, "bot_user_id": token.BotUserID}}
	if token.Expires > 0 && token.Expires <= 86400*365 {
		at := p.config.Now().Add(time.Duration(token.Expires) * time.Second)
		credential.ExpiresAt = &at
	}
	return core.CompleteAuthResponse{ExternalAccountID: token.Team.ID, Credential: credential, GrantedGrants: grants}, nil
}
func (p *Provider) CompleteAuth(ctx context.Context, r core.CompleteAuthRequest) (core.CompleteAuthResponse, error) {
	if r.Code == "" || r.State == "" {
		return core.CompleteAuthResponse{}, ErrInvalid
	}
	return p.exchange(ctx, url.Values{"code": {r.Code}, "redirect_uri": {r.RedirectURI}})
}
func (p *Provider) Refresh(ctx context.Context, c core.ActiveCredential) (core.RefreshResult, error) {
	if !c.Refreshable || c.RefreshToken == "" {
		if c.ExpiresAt != nil && !p.config.Now().Before(*c.ExpiresAt) {
			return core.RefreshResult{}, ErrDenied
		}
		return core.RefreshResult{Credential: c, GrantedGrants: append([]string(nil), c.GrantedScopes...)}, nil
	}
	result, err := p.exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c.RefreshToken}})
	if err != nil {
		return core.RefreshResult{}, err
	}
	result.Credential.ConnectionID = c.ConnectionID
	result.Credential.Metadata = c.Metadata
	return core.RefreshResult{Credential: result.Credential, GrantedGrants: result.GrantedGrants}, nil
}
