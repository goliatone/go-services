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
	if request == nil {
		return core.NewTrackerProviderError(core.TrackerErrorUnavailable, "tracker request is missing", false, 0, nil)
	}
	authenticate := r.Authenticate
	if authenticate == nil {
		authenticate = BearerAuthenticator
	}
	if err := authenticate(ctx, request, credential); err != nil {
		return core.NewTrackerProviderError(core.TrackerErrorCredentialRevoked, "credential cannot authenticate request", false, 0, err)
	}
	client := r.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := client.Do(request.WithContext(ctx))
	if err != nil {
		return core.NewTrackerProviderError(core.TrackerErrorUnavailable, "provider request failed", true, 0, err)
	}
	defer func() { _ = response.Body.Close() }()
	limit := r.MaxResponseBodyBytes
	if limit <= 0 {
		limit = DefaultMaxResponseBodyBytes
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return core.NewTrackerProviderError(core.TrackerErrorExternal, "provider response read failed", true, 0, err)
	}
	if int64(len(body)) > limit {
		return core.NewTrackerProviderError(core.TrackerErrorExternal, "provider response exceeds configured bound", false, 0, nil)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseError(response, body)
	}
	if output == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, output); err != nil {
		return core.NewTrackerProviderError(core.TrackerErrorExternal, "provider response is invalid JSON", false, 0, err)
	}
	return nil
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
	case http.StatusUnauthorized, http.StatusForbidden:
		return core.NewTrackerProviderError(core.TrackerErrorCredentialRevoked, message, false, 0, nil)
	case http.StatusTooManyRequests:
		return core.NewTrackerProviderError(core.TrackerErrorRateLimited, message, true, retryAfter(response.Header), nil)
	case http.StatusGone:
		return core.NewTrackerProviderError(core.TrackerErrorCursorInvalid, message, true, 0, nil)
	case http.StatusConflict, http.StatusPreconditionFailed:
		return core.NewTrackerProviderError(core.TrackerErrorSchemaChanged, message, true, 0, nil)
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
