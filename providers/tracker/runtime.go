package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-services/core"
)

const DefaultMaxResponseBodyBytes int64 = 4 << 20

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Authenticator func(context.Context, *http.Request, core.ActiveCredential) error

type Runtime struct {
	Credentials          core.TrackerCredentialResolver
	HTTPClient           HTTPDoer
	Authenticate         Authenticator
	MaxResponseBodyBytes int64
}

type ResponseMetadata struct {
	StatusCode         int
	RequestID          string
	ETag               string
	LastModified       string
	RateLimitRemaining int
	RateLimitReset     time.Time
	OAuthScopes        []string
}

func (r Runtime) DoJSON(ctx context.Context, connectionID string, request *http.Request, output any) error {
	if r.Credentials == nil || request == nil {
		return core.NewTrackerProviderError(core.TrackerErrorUnavailable, "tracker runtime is incomplete", true, 0, nil)
	}
	credential, err := r.ResolveCredential(ctx, connectionID)
	if err != nil {
		return err
	}
	return r.DoJSONWithCredential(ctx, credential, request, output)
}

func (r Runtime) ResolveCredential(ctx context.Context, connectionID string) (core.ActiveCredential, error) {
	if r.Credentials == nil {
		return core.ActiveCredential{}, core.NewTrackerProviderError(core.TrackerErrorUnavailable, "tracker credential runtime is incomplete", true, 0, nil)
	}
	credential, err := r.Credentials.ResolveTrackerCredential(ctx, strings.TrimSpace(connectionID))
	if err != nil {
		return core.ActiveCredential{}, core.NewTrackerProviderError(core.TrackerErrorCredentialRevoked, "active credential is unavailable", false, 0, err)
	}
	return credential, nil
}

func (r Runtime) DoJSONWithCredential(ctx context.Context, credential core.ActiveCredential, request *http.Request, output any) error {
	_, err := r.DoJSONWithCredentialMetadata(ctx, credential, request, output)
	return err
}

func (r Runtime) DoJSONWithCredentialMetadata(ctx context.Context, credential core.ActiveCredential, request *http.Request, output any) (ResponseMetadata, error) {
	if request == nil {
		return ResponseMetadata{}, core.NewTrackerProviderError(core.TrackerErrorUnavailable, "tracker request is missing", false, 0, nil)
	}
	authenticate := r.Authenticate
	if authenticate == nil {
		authenticate = BearerAuthenticator
	}
	if err := authenticate(ctx, request, credential); err != nil {
		return ResponseMetadata{}, core.NewTrackerProviderError(core.TrackerErrorCredentialRevoked, "credential cannot authenticate request", false, 0, err)
	}
	client := r.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := client.Do(request.WithContext(ctx))
	if err != nil {
		return ResponseMetadata{}, core.NewTrackerProviderError(core.TrackerErrorUnavailable, "provider request failed", true, 0, err)
	}
	defer func() { _ = response.Body.Close() }()
	metadata := responseMetadata(response)
	limit := r.MaxResponseBodyBytes
	if limit <= 0 {
		limit = DefaultMaxResponseBodyBytes
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return metadata, core.NewTrackerProviderError(core.TrackerErrorExternal, "provider response read failed", true, 0, err)
	}
	if int64(len(body)) > limit {
		return metadata, core.NewTrackerProviderError(core.TrackerErrorExternal, "provider response exceeds configured bound", false, 0, nil)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return metadata, responseError(response, redactTrackerResponse(body, credential))
	}
	if output == nil || len(body) == 0 {
		return metadata, nil
	}
	if err := json.Unmarshal(body, output); err != nil {
		return metadata, core.NewTrackerProviderError(core.TrackerErrorExternal, "provider response is invalid JSON", false, 0, err)
	}
	return metadata, nil
}

func redactTrackerResponse(body []byte, credential core.ActiveCredential) []byte {
	message := string(body)
	for _, secret := range []string{credential.AccessToken, credential.RefreshToken} {
		if secret = strings.TrimSpace(secret); secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	for key, value := range credential.Metadata {
		name := strings.ToLower(strings.TrimSpace(key))
		if !strings.Contains(name, "token") && !strings.Contains(name, "secret") && !strings.Contains(name, "password") {
			continue
		}
		secret := strings.TrimSpace(fmt.Sprint(value))
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return []byte(message)
}

func responseMetadata(response *http.Response) ResponseMetadata {
	remaining, _ := strconv.Atoi(strings.TrimSpace(response.Header.Get("X-RateLimit-Remaining")))
	resetUnix, _ := strconv.ParseInt(strings.TrimSpace(response.Header.Get("X-RateLimit-Reset")), 10, 64)
	metadata := ResponseMetadata{StatusCode: response.StatusCode, RequestID: strings.TrimSpace(response.Header.Get("X-GitHub-Request-Id")), ETag: strings.TrimSpace(response.Header.Get("ETag")), LastModified: strings.TrimSpace(response.Header.Get("Last-Modified")), RateLimitRemaining: remaining}
	for _, scope := range strings.Split(response.Header.Get("X-OAuth-Scopes"), ",") {
		if scope = strings.TrimSpace(scope); scope != "" {
			metadata.OAuthScopes = append(metadata.OAuthScopes, scope)
		}
	}
	if resetUnix > 0 {
		metadata.RateLimitReset = time.Unix(resetUnix, 0).UTC()
	}
	return metadata
}

func BearerAuthenticator(_ context.Context, request *http.Request, credential core.ActiveCredential) error {
	token := strings.TrimSpace(credential.AccessToken)
	if token == "" {
		return errors.New("tracker: access token is empty")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	return nil
}

func BasicAuthenticator(_ context.Context, request *http.Request, credential core.ActiveCredential) error {
	token := strings.TrimSpace(credential.AccessToken)
	if token == "" {
		return errors.New("tracker: basic credential is empty")
	}
	request.Header.Set("Authorization", "Basic "+token)
	return nil
}

func responseError(response *http.Response, body []byte) error {
	message := strings.TrimSpace(string(body))
	if len(message) > 512 {
		message = message[:512]
	}
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return core.NewTrackerProviderError(core.TrackerErrorCredentialRevoked, message, false, 0, nil)
	case http.StatusForbidden:
		if strings.TrimSpace(response.Header.Get("X-RateLimit-Remaining")) == "0" {
			return core.NewTrackerProviderError(core.TrackerErrorRateLimited, message, true, retryAfter(response.Header), nil)
		}
		return core.NewTrackerProviderError(core.TrackerErrorPermission, message, false, 0, nil)
	case http.StatusTooManyRequests:
		return core.NewTrackerProviderError(core.TrackerErrorRateLimited, message, true, retryAfter(response.Header), nil)
	case http.StatusNotFound:
		return core.NewTrackerProviderError(core.TrackerErrorNotFound, message, false, 0, nil)
	case http.StatusUnprocessableEntity:
		return core.NewTrackerProviderError(core.TrackerErrorValidation, message, false, 0, nil)
	case http.StatusGone:
		return core.NewTrackerProviderError(core.TrackerErrorCursorInvalid, message, true, 0, nil)
	case http.StatusConflict, http.StatusPreconditionFailed:
		return core.NewTrackerProviderError(core.TrackerErrorConflict, message, true, 0, nil)
	default:
		retryable := response.StatusCode >= 500
		return core.NewTrackerProviderError(core.TrackerErrorExternal, fmt.Sprintf("provider returned HTTP %d: %s", response.StatusCode, message), retryable, 0, nil)
	}
}

func retryAfter(header http.Header) time.Duration {
	value := strings.TrimSpace(header.Get("Retry-After"))
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		delay := time.Until(at)
		if delay > 0 {
			return delay
		}
	}
	return 0
}
