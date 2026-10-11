package linear

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

const linearOAuthTokenURL = "https://api.linear.app/oauth/token"

type linearOAuthToken struct {
	value     string
	scopes    []string
	expiresAt time.Time
}

func (p *Provider) oauthCredential(ctx context.Context, credential core.ActiveCredential, force bool) (core.ActiveCredential, error) {
	clientID, _ := credential.Metadata["client_id"].(string)
	clientSecret, _ := credential.Metadata["client_secret"].(string)
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(clientSecret) == "" {
		return core.ActiveCredential{}, core.NewTrackerProviderError(core.TrackerErrorCredentialRevoked, "Linear OAuth client credentials are unavailable", false, 0, nil)
	}
	keyBytes := sha256.Sum256([]byte(credential.ConnectionID + "\x00" + clientID + "\x00" + clientSecret))
	key := hex.EncodeToString(keyBytes[:])
	p.oauthMu.Lock()
	defer p.oauthMu.Unlock()
	if p.oauthTokens == nil {
		p.oauthTokens = map[string]linearOAuthToken{}
	}
	entry := p.oauthTokens[key]
	if force || entry.value == "" || !entry.expiresAt.After(time.Now().Add(5*time.Minute)) {
		form := url.Values{"grant_type": {"client_credentials"}, "client_id": {clientID}, "client_secret": {clientSecret}, "scope": {"read,write"}}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, linearOAuthTokenURL, strings.NewReader(form.Encode()))
		if err != nil {
			return core.ActiveCredential{}, err
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		client := p.runtime.HTTPClient
		if client == nil {
			client = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		} else if httpClient, ok := client.(*http.Client); ok {
			copyClient := *httpClient
			copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			client = &copyClient
		}
		response, err := client.Do(request)
		if err != nil {
			return core.ActiveCredential{}, core.NewTrackerProviderError(core.TrackerErrorUnavailable, "Linear OAuth token exchange failed", true, 0, nil)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			delete(p.oauthTokens, key)
			return core.ActiveCredential{}, core.NewTrackerProviderError(core.TrackerErrorCredentialRevoked, "Linear OAuth token exchange was refused", false, 0, nil)
		}
		var token struct {
			AccessToken string `json:"access_token"`
			TokenType   string `json:"token_type"`
			Scope       any    `json:"scope"`
			ExpiresIn   int64  `json:"expires_in"`
		}
		if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&token) != nil || strings.TrimSpace(token.AccessToken) == "" || !strings.EqualFold(token.TokenType, "Bearer") || token.ExpiresIn <= 300 || token.ExpiresIn > 366*24*3600 {
			delete(p.oauthTokens, key)
			return core.ActiveCredential{}, core.NewTrackerProviderError(core.TrackerErrorExternal, "Linear OAuth token response is incomplete", false, 0, nil)
		}
		scopes := linearTokenScopes(token.Scope)
		granted := false
		for _, scope := range scopes {
			if scope == "write" {
				granted = true
				break
			}
		}
		if !granted {
			delete(p.oauthTokens, key)
			return core.ActiveCredential{}, core.NewTrackerProviderError(core.TrackerErrorPermission, "Linear OAuth write grant is unavailable", false, 0, nil)
		}
		entry = linearOAuthToken{value: token.AccessToken, scopes: scopes, expiresAt: time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)}
		p.oauthTokens[key] = entry
	}
	credential.AccessToken = entry.value
	credential.TokenType = "Bearer"
	credential.GrantedScopes = append([]string(nil), entry.scopes...)
	credential.ExpiresAt = &entry.expiresAt
	return credential, nil
}

func linearTokenScopes(value any) []string {
	switch scope := value.(type) {
	case string:
		return strings.Fields(strings.ReplaceAll(scope, ",", " "))
	case []any:
		out := make([]string, 0, len(scope))
		for _, item := range scope {
			text, ok := item.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return nil
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

func (p *Provider) doGraphQL(ctx context.Context, credential core.ActiveCredential, endpoint, query string, variables map[string]any, output any) error {
	err := p.runtime.DoGraphQL(ctx, credential, endpoint, query, variables, output)
	var providerErr *core.TrackerProviderError
	if err == nil || credential.Metadata["auth_type"] != "oauth_client" || !errors.As(err, &providerErr) || providerErr.Code != core.TrackerErrorCredentialRevoked {
		return err
	}
	credential, refreshErr := p.oauthCredential(ctx, credential, true)
	if refreshErr != nil {
		return refreshErr
	}
	return p.runtime.DoGraphQL(ctx, credential, endpoint, query, variables, output)
}
