package embedded

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

const (
	defaultExchangeRequestTimeout = 30 * time.Second
	maxExchangeResponseBodyBytes  = 1 << 20

	tokenExchangeGrantType  = "urn:ietf:params:oauth:grant-type:token-exchange"
	subjectTokenTypeIDToken = "urn:ietf:params:oauth:token-type:id_token"
	requestedTypeOfflineURN = "urn:shopify:params:oauth:token-type:offline-access-token"
	requestedTypeOnlineURN  = "urn:shopify:params:oauth:token-type:online-access-token"
)

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type ExchangeClientConfig struct {
	ClientID            string
	ClientSecret        string
	TokenRequestTimeout time.Duration
	HTTPClient          HTTPDoer
	Now                 func() time.Time
	BuildTokenURL       func(shopDomain string) (string, error)
}

type SessionTokenExchangeClient struct {
	config     ExchangeClientConfig
	httpClient HTTPDoer
}

func NewSessionTokenExchangeClient(cfg ExchangeClientConfig) *SessionTokenExchangeClient {
	timeout := cfg.TokenRequestTimeout
	if timeout <= 0 {
		timeout = defaultExchangeRequestTimeout
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	builder := cfg.BuildTokenURL
	if builder == nil {
		builder = defaultTokenURLBuilder
	}
	return &SessionTokenExchangeClient{
		config: ExchangeClientConfig{
			ClientID:            strings.TrimSpace(cfg.ClientID),
			ClientSecret:        strings.TrimSpace(cfg.ClientSecret),
			TokenRequestTimeout: timeout,
			Now:                 now,
			BuildTokenURL:       builder,
		},
		httpClient: httpClient,
	}
}

func (c *SessionTokenExchangeClient) ExchangeSessionToken(
	ctx context.Context,
	req ExchangeSessionTokenRequest,
) (core.EmbeddedAccessToken, error) {
	if c == nil || c.httpClient == nil {
		return core.EmbeddedAccessToken{}, &ExchangeError{
			Message: "http client is not configured",
			Cause:   ErrTokenExchangeFailed,
		}
	}
	clientID := strings.TrimSpace(c.config.ClientID)
	clientSecret := strings.TrimSpace(c.config.ClientSecret)
	if clientID == "" || clientSecret == "" {
		return core.EmbeddedAccessToken{}, &ExchangeError{
			Message: "client id and client secret are required",
			Cause:   ErrTokenExchangeFailed,
		}
	}
	shopDomain, err := normalizeShopDomain(req.ShopDomain)
	if err != nil {
		return core.EmbeddedAccessToken{}, &ExchangeError{
			Message: "invalid shop domain",
			Cause:   err,
		}
	}
	sessionToken := strings.TrimSpace(req.SessionToken)
	if sessionToken == "" {
		return core.EmbeddedAccessToken{}, &ExchangeError{
			Message: "session token is required",
			Cause:   ErrTokenExchangeFailed,
		}
	}
	tokenURL, err := c.config.BuildTokenURL(shopDomain)
	if err != nil {
		return core.EmbeddedAccessToken{}, &ExchangeError{
			Message: "resolve token url",
			Cause:   err,
		}
	}
	normalizedTokenType, requestedTypeURN, err := resolveRequestedTokenType(req.RequestedTokenType)
	if err != nil {
		return core.EmbeddedAccessToken{}, &ExchangeError{
			Message: "invalid requested token type",
			Cause:   err,
		}
	}

	values := url.Values{}
	values.Set("grant_type", tokenExchangeGrantType)
	values.Set("subject_token", sessionToken)
	values.Set("subject_token_type", subjectTokenTypeIDToken)
	values.Set("requested_token_type", requestedTypeURN)
	values.Set("client_id", clientID)
	values.Set("client_secret", clientSecret)

	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx := ctx
	cancel := func() {}
	if c.config.TokenRequestTimeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, c.config.TokenRequestTimeout)
	}
	defer cancel()

	httpReq, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodPost,
		tokenURL,
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		return core.EmbeddedAccessToken{}, &ExchangeError{
			Message: "build exchange request",
			Cause:   err,
		}
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(httpReq)
	if err != nil {
		return core.EmbeddedAccessToken{}, &ExchangeError{
			Message: "exchange request failed",
			Cause:   err,
		}
	}
	defer response.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxExchangeResponseBodyBytes+1))
	if readErr != nil {
		return core.EmbeddedAccessToken{}, &ExchangeError{
			Message: "read exchange response",
			Cause:   readErr,
		}
	}
	if int64(len(body)) > maxExchangeResponseBodyBytes {
		return core.EmbeddedAccessToken{}, &ExchangeError{
			Message: fmt.Sprintf("exchange response exceeds %d bytes", maxExchangeResponseBodyBytes),
			Cause:   ErrTokenExchangeFailed,
		}
	}

	payload := map[string]any{}
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			return core.EmbeddedAccessToken{}, &ExchangeError{
				StatusCode: response.StatusCode,
				Message:    "decode exchange response",
				Cause:      err,
			}
		}
	}

	errorCode := strings.TrimSpace(readAnyString(payload["error"]))
	errorDescription := strings.TrimSpace(readAnyString(payload["error_description"]))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || errorCode != "" {
		if errorDescription == "" {
			errorDescription = "shopify token exchange failed"
		}
		return core.EmbeddedAccessToken{}, &ExchangeError{
			StatusCode: response.StatusCode,
			ErrorCode:  errorCode,
			Message:    errorDescription,
			Cause:      ErrTokenExchangeFailed,
		}
	}

	accessToken := strings.TrimSpace(readAnyString(payload["access_token"]))
	if accessToken == "" {
		return core.EmbeddedAccessToken{}, &ExchangeError{
			StatusCode: response.StatusCode,
			Message:    "exchange response missing access token",
			Cause:      ErrTokenExchangeFailed,
		}
	}

	tokenType := strings.ToLower(strings.TrimSpace(readAnyString(payload["token_type"])))
	if tokenType == "" {
		tokenType = "bearer"
	}
	scope := parseScopeList(readAnyString(payload["scope"]))
	expiresIn := readAnyInt64(payload["expires_in"])
	var expiresAt *time.Time
	if expiresIn > 0 {
		value := c.config.Now().UTC().Add(time.Duration(expiresIn) * time.Second)
		expiresAt = &value
	}
	metadata := sanitizeExchangeMetadata(payload)
	metadata["requested_token_type"] = requestedTypeURN
	metadata["requested_token_mode"] = normalizedTokenType
	metadata["shop_domain"] = shopDomain

	return core.EmbeddedAccessToken{
		AccessToken: accessToken,
		TokenType:   tokenType,
		Scope:       scope,
		ExpiresAt:   expiresAt,
		Metadata:    metadata,
	}, nil
}

func defaultTokenURLBuilder(shopDomain string) (string, error) {
	normalized, err := normalizeShopDomain(shopDomain)
	if err != nil {
		return "", err
	}
	return (&url.URL{
		Scheme: "https",
		Host:   normalized,
		Path:   "/admin/oauth/access_token",
	}).String(), nil
}

func parseScopeList(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return []string{}
	}
	parts := strings.Fields(strings.ReplaceAll(trimmed, ",", " "))
	set := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := set[part]; ok {
			continue
		}
		set[part] = struct{}{}
		out = append(out, part)
	}
	sort.Strings(out)
	return out
}

func readAnyInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return parsed
		}
		floatParsed, floatErr := typed.Float64()
		if floatErr == nil {
			return int64(floatParsed)
		}
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func sanitizeExchangeMetadata(payload map[string]any) map[string]any {
	metadata := copyAnyMap(payload)
	delete(metadata, "access_token")
	delete(metadata, "refresh_token")
	delete(metadata, "id_token")
	return metadata
}

var _ SessionTokenExchanger = (*SessionTokenExchangeClient)(nil)
