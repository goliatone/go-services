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
	input, err := c.resolveExchangeInput(req)
	if err != nil {
		return core.EmbeddedAccessToken{}, err
	}
	httpReq, cancel, err := c.buildExchangeRequest(ctx, input)
	if err != nil {
		return core.EmbeddedAccessToken{}, err
	}
	defer cancel()
	response, err := c.httpClient.Do(httpReq)
	if err != nil {
		return core.EmbeddedAccessToken{}, &ExchangeError{
			Message: "exchange request failed",
			Cause:   err,
		}
	}
	defer func() { _ = response.Body.Close() }()
	payload, err := decodeExchangeResponse(response)
	if err != nil {
		return core.EmbeddedAccessToken{}, err
	}
	return c.exchangeAccessToken(response.StatusCode, payload, input)
}

type exchangeInput struct {
	clientID            string
	clientSecret        string
	shopDomain          string
	sessionToken        string
	tokenURL            string
	normalizedTokenType core.EmbeddedRequestedTokenType
	requestedTypeURN    string
}

func (c *SessionTokenExchangeClient) resolveExchangeInput(req ExchangeSessionTokenRequest) (exchangeInput, error) {
	if c == nil || c.httpClient == nil {
		return exchangeInput{}, &ExchangeError{Message: "http client is not configured", Cause: ErrTokenExchangeFailed}
	}
	input := exchangeInput{
		clientID:     strings.TrimSpace(c.config.ClientID),
		clientSecret: strings.TrimSpace(c.config.ClientSecret),
		sessionToken: strings.TrimSpace(req.SessionToken),
	}
	if input.clientID == "" || input.clientSecret == "" {
		return exchangeInput{}, &ExchangeError{Message: "client id and client secret are required", Cause: ErrTokenExchangeFailed}
	}
	var err error
	input.shopDomain, err = normalizeShopDomain(req.ShopDomain)
	if err != nil {
		return exchangeInput{}, &ExchangeError{Message: "invalid shop domain", Cause: err}
	}
	if input.sessionToken == "" {
		return exchangeInput{}, &ExchangeError{Message: "session token is required", Cause: ErrTokenExchangeFailed}
	}
	input.tokenURL, err = c.config.BuildTokenURL(input.shopDomain)
	if err != nil {
		return exchangeInput{}, &ExchangeError{Message: "resolve token url", Cause: err}
	}
	input.normalizedTokenType, input.requestedTypeURN, err = resolveRequestedTokenType(req.RequestedTokenType)
	if err != nil {
		return exchangeInput{}, &ExchangeError{Message: "invalid requested token type", Cause: err}
	}
	return input, nil
}

func (c *SessionTokenExchangeClient) buildExchangeRequest(
	ctx context.Context,
	input exchangeInput,
) (*http.Request, context.CancelFunc, error) {
	values := url.Values{
		"grant_type":           {tokenExchangeGrantType},
		"subject_token":        {input.sessionToken},
		"subject_token_type":   {subjectTokenTypeIDToken},
		"requested_token_type": {input.requestedTypeURN},
		"client_id":            {input.clientID},
		"client_secret":        {input.clientSecret},
	}
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithCancel(ctx)
	if c.config.TokenRequestTimeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, c.config.TokenRequestTimeout)
	}
	request, err := http.NewRequestWithContext(
		requestCtx, http.MethodPost, input.tokenURL, strings.NewReader(values.Encode()),
	)
	if err != nil {
		cancel()
		return nil, func() {}, &ExchangeError{Message: "build exchange request", Cause: err}
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	return request, cancel, nil
}

func decodeExchangeResponse(response *http.Response) (map[string]any, error) {
	body, err := io.ReadAll(io.LimitReader(response.Body, maxExchangeResponseBodyBytes+1))
	if err != nil {
		return nil, &ExchangeError{Message: "read exchange response", Cause: err}
	}
	if int64(len(body)) > maxExchangeResponseBodyBytes {
		return nil, &ExchangeError{Message: fmt.Sprintf("exchange response exceeds %d bytes", maxExchangeResponseBodyBytes), Cause: ErrTokenExchangeFailed}
	}
	payload := map[string]any{}
	if len(strings.TrimSpace(string(body))) == 0 {
		return payload, nil
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &ExchangeError{StatusCode: response.StatusCode, Message: "decode exchange response", Cause: err}
	}
	errorCode := strings.TrimSpace(readAnyString(payload["error"]))
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices && errorCode == "" {
		return payload, nil
	}
	description := strings.TrimSpace(readAnyString(payload["error_description"]))
	if description == "" {
		description = "shopify token exchange failed"
	}
	return nil, &ExchangeError{StatusCode: response.StatusCode, ErrorCode: errorCode, Message: description, Cause: ErrTokenExchangeFailed}
}

func (c *SessionTokenExchangeClient) exchangeAccessToken(
	statusCode int,
	payload map[string]any,
	input exchangeInput,
) (core.EmbeddedAccessToken, error) {
	accessToken := strings.TrimSpace(readAnyString(payload["access_token"]))
	if accessToken == "" {
		return core.EmbeddedAccessToken{}, &ExchangeError{
			StatusCode: statusCode,
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
	metadata["requested_token_type"] = input.requestedTypeURN
	metadata["requested_token_mode"] = input.normalizedTokenType
	metadata["shop_domain"] = input.shopDomain

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
