package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	goerrors "github.com/goliatone/go-errors"
)

const (
	defaultProviderOperationKind           = "rest"
	defaultProviderOperationInitialBackoff = 200 * time.Millisecond
	defaultProviderOperationMaxBackoff     = 5 * time.Second
)

var defaultProviderOperationRetryStatuses = []int{
	http.StatusTooManyRequests,
	http.StatusInternalServerError,
	http.StatusBadGateway,
	http.StatusServiceUnavailable,
	http.StatusGatewayTimeout,
}

func (s *Service) ExecuteProviderOperation(
	ctx context.Context,
	req ProviderOperationRequest,
) (result ProviderOperationResult, err error) {
	startedAt := time.Now().UTC()
	fields := map[string]any{
		"provider_id":    req.ProviderID,
		"connection_id":  req.ConnectionID,
		"transport_kind": req.TransportKind,
		"operation":      req.Operation,
	}
	defer func() {
		if result.Attempts > 0 {
			fields["attempts"] = result.Attempts
		}
		if result.Idempotency != "" {
			fields["idempotency"] = result.Idempotency
		}
		s.observeOperation(ctx, startedAt, "provider_operation", err, fields)
	}()

	if s == nil {
		return ProviderOperationResult{}, fmt.Errorf("core: service is nil")
	}

	resolved, err := s.resolveProviderOperationRequest(ctx, req)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	result = ProviderOperationResult{
		ProviderID:    resolved.provider.ID(),
		ConnectionID:  resolved.connectionID,
		Operation:     resolved.operation,
		TransportKind: resolved.transportKind,
		AuthStrategy:  resolved.authStrategy,
		Idempotency:   resolved.idempotencyKey,
		Metadata:      copyAnyMap(req.Metadata),
	}

	retry := normalizeProviderRetryPolicy(req.Retry)
	var lastErr error
	var lastStatus int
	for attempt := 1; attempt <= retry.MaxAttempts; attempt++ {
		result.Attempts = attempt
		transportRequest := cloneTransportRequest(resolved.transportRequest)

		if resolved.rateLimitEnabled {
			beforeErr := s.rateLimitPolicy.BeforeCall(ctx, resolved.rateLimitKey)
			if beforeErr != nil {
				lastErr = s.wrapProviderOperationError(
					resolved,
					attempt,
					retry.MaxAttempts,
					0,
					beforeErr,
					true,
				)
				shouldRetry, delay := s.shouldRetryProviderOperation(
					ctx,
					resolved.provider,
					retry,
					attempt,
					beforeErr,
					ProviderResponseMeta{},
				)
				if !shouldRetry {
					return result, lastErr
				}
				result.Retried = true
				if sleepErr := sleepProviderRetry(ctx, retry.Sleep, delay); sleepErr != nil {
					return result, sleepErr
				}
				continue
			}
		}

		var signingMetadata map[string]any
		if resolved.signer != nil && resolved.credential != nil {
			signed, metadata, signErr := signProviderTransportRequest(
				ctx,
				resolved.signer,
				transportRequest,
				*resolved.credential,
			)
			if signErr != nil {
				return result, s.wrapProviderOperationError(
					resolved,
					attempt,
					retry.MaxAttempts,
					0,
					signErr,
					false,
				)
			}
			transportRequest = signed
			signingMetadata = metadata
		}

		response, callErr := resolved.adapter.Do(ctx, transportRequest)
		if callErr != nil {
			lastErr = s.wrapProviderOperationError(
				resolved,
				attempt,
				retry.MaxAttempts,
				0,
				callErr,
				true,
			)
			shouldRetry, delay := s.shouldRetryProviderOperation(
				ctx,
				resolved.provider,
				retry,
				attempt,
				callErr,
				ProviderResponseMeta{},
			)
			if !shouldRetry {
				return result, lastErr
			}
			result.Retried = true
			if sleepErr := sleepProviderRetry(ctx, retry.Sleep, delay); sleepErr != nil {
				return result, sleepErr
			}
			continue
		}

		meta, normalizeErr := normalizeProviderOperationResponse(ctx, req.Normalize, response)
		if normalizeErr != nil {
			return result, s.wrapProviderOperationError(
				resolved,
				attempt,
				retry.MaxAttempts,
				response.StatusCode,
				normalizeErr,
				false,
			)
		}
		meta.Metadata = mergeProviderOperationMetadata(meta.Metadata, signingMetadata)
		if skewHint, ok := computeSigningClockSkewHint(signingMetadata, response.Headers); ok {
			meta.Metadata["clock_skew_hint_seconds"] = skewHint
		}

		result.Response = response
		result.Meta = meta
		lastStatus = meta.StatusCode

		if resolved.rateLimitEnabled {
			afterErr := s.rateLimitPolicy.AfterCall(ctx, resolved.rateLimitKey, meta)
			if afterErr != nil {
				lastErr = s.wrapProviderOperationError(
					resolved,
					attempt,
					retry.MaxAttempts,
					meta.StatusCode,
					afterErr,
					true,
				)
				shouldRetry, delay := s.shouldRetryProviderOperation(
					ctx,
					resolved.provider,
					retry,
					attempt,
					afterErr,
					meta,
				)
				if !shouldRetry {
					return result, lastErr
				}
				result.Retried = true
				if sleepErr := sleepProviderRetry(ctx, retry.Sleep, delay); sleepErr != nil {
					return result, sleepErr
				}
				continue
			}
		}

		shouldRetry, delay := s.shouldRetryProviderOperation(
			ctx,
			resolved.provider,
			retry,
			attempt,
			nil,
			meta,
		)
		if !shouldRetry {
			if meta.StatusCode >= http.StatusBadRequest {
				return result, s.wrapProviderOperationError(
					resolved,
					attempt,
					retry.MaxAttempts,
					meta.StatusCode,
					fmt.Errorf("provider operation returned status %d", meta.StatusCode),
					false,
				)
			}
			return result, nil
		}
		result.Retried = true
		lastErr = s.wrapProviderOperationError(
			resolved,
			attempt,
			retry.MaxAttempts,
			meta.StatusCode,
			fmt.Errorf("provider operation status %d marked retryable", meta.StatusCode),
			true,
		)
		if sleepErr := sleepProviderRetry(ctx, retry.Sleep, delay); sleepErr != nil {
			return result, sleepErr
		}
	}

	if lastErr != nil {
		return result, lastErr
	}
	return result, s.wrapProviderOperationError(
		resolved,
		retry.MaxAttempts,
		retry.MaxAttempts,
		lastStatus,
		fmt.Errorf("provider operation exceeded retry attempts"),
		true,
	)
}

type resolvedProviderOperationRequest struct {
	provider         Provider
	adapter          TransportAdapter
	signer           Signer
	credential       *ActiveCredential
	connectionID     string
	operation        string
	transportKind    string
	authStrategy     string
	transportRequest TransportRequest
	idempotencyKey   string
	rateLimitKey     RateLimitKey
	rateLimitEnabled bool
}

func (s *Service) resolveProviderOperationRequest(
	ctx context.Context,
	req ProviderOperationRequest,
) (resolvedProviderOperationRequest, error) {
	providerID := strings.TrimSpace(req.ProviderID)
	connectionID := strings.TrimSpace(req.ConnectionID)

	var connection Connection
	if connectionID != "" && s.connectionStore != nil {
		loaded, err := s.connectionStore.Get(ctx, connectionID)
		if err != nil {
			return resolvedProviderOperationRequest{}, s.mapError(err)
		}
		connection = loaded
		if providerID == "" {
			providerID = strings.TrimSpace(connection.ProviderID)
		}
		if !strings.EqualFold(providerID, connection.ProviderID) {
			return resolvedProviderOperationRequest{}, s.mapError(
				fmt.Errorf(
					"core: provider mismatch for connection %q: got %q want %q",
					connectionID,
					providerID,
					connection.ProviderID,
				),
			)
		}
	}
	if providerID == "" {
		return resolvedProviderOperationRequest{}, s.mapError(fmt.Errorf("core: provider id is required"))
	}

	provider, err := s.resolveProvider(providerID)
	if err != nil {
		return resolvedProviderOperationRequest{}, err
	}

	strategy := s.resolveAuthStrategy(provider)
	authStrategy := ""
	if strategy != nil {
		authStrategy = strings.TrimSpace(strings.ToLower(strategy.Type()))
	}

	adapter, transportKind, err := s.resolveProviderOperationAdapter(req)
	if err != nil {
		return resolvedProviderOperationRequest{}, s.mapError(err)
	}

	transportRequest := cloneTransportRequest(req.TransportRequest)
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = strings.TrimSpace(transportRequest.Idempotency)
	}
	if idempotencyKey == "" {
		idempotencyKey = generateIdempotencyKey(providerID, connectionID, req.Operation, transportRequest)
	}
	transportRequest.Idempotency = idempotencyKey
	transportRequest.Headers = copyStringMap(transportRequest.Headers)
	if _, exists := transportRequest.Headers["Idempotency-Key"]; !exists {
		transportRequest.Headers["Idempotency-Key"] = idempotencyKey
	}

	credential, err := s.resolveProviderOperationCredential(ctx, req, connectionID)
	if err != nil {
		return resolvedProviderOperationRequest{}, s.mapError(err)
	}
	signer := s.resolveSignerForCredential(provider, credential)

	operation := strings.TrimSpace(req.Operation)
	if operation == "" {
		method := strings.TrimSpace(strings.ToUpper(transportRequest.Method))
		if method == "" {
			method = http.MethodGet
		}
		operation = normalizeOperation(method + "_" + transportKind)
	}

	scope := ScopeRef{
		Type: strings.TrimSpace(strings.ToLower(req.Scope.Type)),
		ID:   strings.TrimSpace(req.Scope.ID),
	}
	if scope.Type == "" && scope.ID == "" && connection.ID != "" {
		scope = ScopeRef{Type: connection.ScopeType, ID: connection.ScopeID}
	}
	bucketKey := strings.TrimSpace(strings.ToLower(req.BucketKey))
	if bucketKey == "" {
		bucketKey = normalizeOperation(operation)
	}
	if bucketKey == "" {
		bucketKey = "default"
	}

	rateLimitEnabled := false
	rateLimitKey := RateLimitKey{}
	if s.rateLimitPolicy != nil && scope.Type != "" && scope.ID != "" {
		if err := scope.Validate(); err == nil {
			rateLimitEnabled = true
			rateLimitKey = RateLimitKey{
				ProviderID: providerID,
				ScopeType:  scope.Type,
				ScopeID:    scope.ID,
				BucketKey:  bucketKey,
			}
		}
	}

	return resolvedProviderOperationRequest{
		provider:         provider,
		adapter:          adapter,
		signer:           signer,
		credential:       credential,
		connectionID:     connectionID,
		operation:        operation,
		transportKind:    transportKind,
		authStrategy:     authStrategy,
		transportRequest: transportRequest,
		idempotencyKey:   idempotencyKey,
		rateLimitKey:     rateLimitKey,
		rateLimitEnabled: rateLimitEnabled,
	}, nil
}

func (s *Service) resolveProviderOperationAdapter(
	req ProviderOperationRequest,
) (TransportAdapter, string, error) {
	transportKind := strings.TrimSpace(strings.ToLower(req.TransportKind))
	if req.Adapter != nil {
		if transportKind == "" {
			transportKind = strings.TrimSpace(strings.ToLower(req.Adapter.Kind()))
		}
		if transportKind == "" {
			transportKind = defaultProviderOperationKind
		}
		return req.Adapter, transportKind, nil
	}

	if s.transportResolver == nil {
		return nil, "", fmt.Errorf("core: transport resolver is required")
	}
	if transportKind == "" {
		transportKind = defaultProviderOperationKind
	}
	adapter, err := s.transportResolver.Build(transportKind, copyAnyMap(req.TransportConfig))
	if err != nil {
		return nil, "", err
	}
	return adapter, transportKind, nil
}

func (s *Service) resolveProviderOperationCredential(
	ctx context.Context,
	req ProviderOperationRequest,
	connectionID string,
) (*ActiveCredential, error) {
	if req.Credential != nil {
		clone := *req.Credential
		return &clone, nil
	}
	if strings.TrimSpace(connectionID) == "" || s.credentialStore == nil {
		return nil, nil
	}
	stored, err := s.credentialStore.GetActiveByConnection(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	active, err := s.credentialToActive(ctx, stored)
	if err != nil {
		return nil, err
	}
	return &active, nil
}

func (s *Service) shouldRetryProviderOperation(
	ctx context.Context,
	provider Provider,
	policy ProviderOperationRetryPolicy,
	attempt int,
	opErr error,
	meta ProviderResponseMeta,
) (bool, time.Duration) {
	if attempt >= policy.MaxAttempts {
		return false, 0
	}
	if policy.ShouldRetry != nil {
		retry, delay := policy.ShouldRetry(ctx, provider, attempt, policy.MaxAttempts, opErr, meta)
		if !retry {
			return false, 0
		}
		if delay <= 0 {
			delay = defaultRetryDelayForAttempt(policy, attempt, meta.RetryAfter)
		}
		return true, delay
	}

	if opErr != nil {
		if isContextCancellation(opErr) {
			return false, 0
		}
		return true, defaultRetryDelayForAttempt(policy, attempt, meta.RetryAfter)
	}
	if slices.Contains(policy.RetryableStatusCodes, meta.StatusCode) {
		return true, defaultRetryDelayForAttempt(policy, attempt, meta.RetryAfter)
	}
	return false, 0
}

func normalizeProviderRetryPolicy(policy ProviderOperationRetryPolicy) ProviderOperationRetryPolicy {
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 1
	}
	if policy.InitialBackoff <= 0 {
		policy.InitialBackoff = defaultProviderOperationInitialBackoff
	}
	if policy.MaxBackoff <= 0 {
		policy.MaxBackoff = defaultProviderOperationMaxBackoff
	}
	if policy.MaxBackoff < policy.InitialBackoff {
		policy.MaxBackoff = policy.InitialBackoff
	}
	if len(policy.RetryableStatusCodes) == 0 {
		policy.RetryableStatusCodes = append([]int(nil), defaultProviderOperationRetryStatuses...)
	}
	return policy
}

func defaultRetryDelayForAttempt(
	policy ProviderOperationRetryPolicy,
	attempt int,
	retryAfter *time.Duration,
) time.Duration {
	if retryAfter != nil && *retryAfter > 0 {
		return *retryAfter
	}
	delay := policy.InitialBackoff
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= policy.MaxBackoff {
			return policy.MaxBackoff
		}
	}
	if delay > policy.MaxBackoff {
		return policy.MaxBackoff
	}
	return delay
}

func sleepProviderRetry(
	ctx context.Context,
	sleepFn func(ctx context.Context, delay time.Duration) error,
	delay time.Duration,
) error {
	if delay <= 0 {
		return nil
	}
	if sleepFn != nil {
		return sleepFn(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Service) wrapProviderOperationError(
	resolved resolvedProviderOperationRequest,
	attempt int,
	maxAttempts int,
	statusCode int,
	source error,
	retryable bool,
) error {
	if source == nil {
		source = fmt.Errorf("provider operation failed")
	}
	category := goerrors.CategoryExternal
	textCode := ServiceErrorProviderOperationFailed
	if statusCode == http.StatusTooManyRequests || strings.Contains(strings.ToLower(source.Error()), "throttl") {
		category = goerrors.CategoryRateLimit
		textCode = ServiceErrorRateLimited
	}
	metadata := map[string]any{
		"provider_id":    resolved.provider.ID(),
		"operation":      resolved.operation,
		"attempt":        attempt,
		"max_attempts":   maxAttempts,
		"status_code":    statusCode,
		"retryable":      retryable,
		"transport_kind": resolved.transportKind,
		"idempotency":    resolved.idempotencyKey,
	}

	opErr := &ProviderOperationError{
		ProviderID:    resolved.provider.ID(),
		Operation:     resolved.operation,
		Attempt:       attempt,
		MaxAttempts:   maxAttempts,
		StatusCode:    statusCode,
		Retryable:     retryable,
		Idempotency:   resolved.idempotencyKey,
		TransportKind: resolved.transportKind,
		Cause:         source,
	}

	wrapped := goerrors.Wrap(opErr, category, source.Error()).
		WithTextCode(textCode).
		WithMetadata(metadata)
	return ensureServiceErrorEnvelope(wrapped)
}

func normalizeProviderOperationResponse(
	ctx context.Context,
	normalizer ProviderResponseNormalizer,
	response TransportResponse,
) (ProviderResponseMeta, error) {
	if normalizer != nil {
		return normalizer(ctx, response)
	}
	meta := ProviderResponseMeta{
		StatusCode: response.StatusCode,
		Headers:    copyStringMap(response.Headers),
		Metadata:   copyAnyMap(response.Metadata),
	}
	if retryAfter, ok := parseRetryAfterHeader(response.Headers); ok {
		meta.RetryAfter = &retryAfter
	}
	return meta, nil
}

func parseRetryAfterHeader(headers map[string]string) (time.Duration, bool) {
	if len(headers) == 0 {
		return 0, false
	}
	raw := ""
	for key, value := range headers {
		if strings.EqualFold(strings.TrimSpace(key), "retry-after") {
			raw = strings.TrimSpace(value)
			break
		}
	}
	if raw == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second, true
	}
	if retryAt, err := time.Parse(time.RFC1123, raw); err == nil {
		if retryAt.After(time.Now().UTC()) {
			return retryAt.Sub(time.Now().UTC()), true
		}
	}
	if retryAt, err := time.Parse(time.RFC1123Z, raw); err == nil {
		if retryAt.After(time.Now().UTC()) {
			return retryAt.Sub(time.Now().UTC()), true
		}
	}
	return 0, false
}

func signProviderTransportRequest(
	ctx context.Context,
	signer Signer,
	request TransportRequest,
	credential ActiveCredential,
) (TransportRequest, map[string]any, error) {
	httpRequest, err := transportToHTTPRequest(ctx, request)
	if err != nil {
		return TransportRequest{}, nil, err
	}
	if err := signer.Sign(ctx, httpRequest, credential); err != nil {
		return TransportRequest{}, nil, err
	}
	signed, err := httpToTransportRequest(httpRequest, request)
	if err != nil {
		return TransportRequest{}, nil, err
	}
	return signed, collectSigningMetadata(httpRequest, credential), nil
}

func collectSigningMetadata(req *http.Request, credential ActiveCredential) map[string]any {
	if req == nil {
		return nil
	}
	authKind := strings.TrimSpace(strings.ToLower(resolveCredentialAuthKind(credential)))
	if authKind != AuthKindAWSSigV4 {
		return nil
	}

	signedRegion := metadataString(credential.Metadata, "aws_region", "region")
	signedService := metadataString(credential.Metadata, "aws_service", "service")
	metadata := map[string]any{
		"signing_profile": AuthKindAWSSigV4,
		"signed_host":     req.URL.Host,
	}

	if authHeader := strings.TrimSpace(req.Header.Get("Authorization")); strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256") {
		metadata["signing_mode"] = "header"
		if signedHeaders := parseAuthDirective(authHeader, "SignedHeaders"); signedHeaders != "" {
			metadata["signed_headers"] = signedHeaders
		}
		if region, service, ok := parseSigV4CredentialScope(parseAuthDirective(authHeader, "Credential")); ok {
			signedRegion = region
			signedService = service
		}
	} else if signedHeaders := strings.TrimSpace(req.URL.Query().Get("X-Amz-SignedHeaders")); signedHeaders != "" {
		metadata["signing_mode"] = "query"
		metadata["signed_headers"] = signedHeaders
		if region, service, ok := parseSigV4CredentialScope(req.URL.Query().Get("X-Amz-Credential")); ok {
			signedRegion = region
			signedService = service
		}
	}
	if signedRegion != "" {
		metadata["signed_region"] = signedRegion
	}
	if signedService != "" {
		metadata["signed_service"] = signedService
	}
	if signedAt := firstNonEmpty(
		strings.TrimSpace(req.Header.Get("X-Amz-Date")),
		strings.TrimSpace(req.URL.Query().Get("X-Amz-Date")),
	); signedAt != "" {
		metadata["signed_at"] = signedAt
	}
	if expires := strings.TrimSpace(req.URL.Query().Get("X-Amz-Expires")); expires != "" {
		metadata["signing_expires_seconds"] = expires
	}
	return metadata
}

func parseAuthDirective(header string, key string) string {
	parts := strings.Split(header, ",")
	target := strings.ToLower(strings.TrimSpace(key))
	for _, part := range parts {
		section := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(section) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(section[0], "AWS4-HMAC-SHA256 ")))
		if k != target {
			continue
		}
		return strings.TrimSpace(section[1])
	}
	return ""
}

func parseSigV4CredentialScope(value string) (region string, service string, ok bool) {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) < 5 {
		return "", "", false
	}
	region = strings.TrimSpace(parts[2])
	service = strings.TrimSpace(parts[3])
	if region == "" || service == "" {
		return "", "", false
	}
	return region, service, true
}

func mergeProviderOperationMetadata(base map[string]any, extras map[string]any) map[string]any {
	if len(base) == 0 && len(extras) == 0 {
		return map[string]any{}
	}
	merged := copyAnyMap(base)
	for key, value := range extras {
		merged[key] = value
	}
	return merged
}

func computeSigningClockSkewHint(signing map[string]any, headers map[string]string) (int64, bool) {
	if len(signing) == 0 {
		return 0, false
	}
	rawSignedAt, ok := signing["signed_at"]
	if !ok {
		return 0, false
	}
	signedAtValue := strings.TrimSpace(fmt.Sprint(rawSignedAt))
	if signedAtValue == "" {
		return 0, false
	}
	signedAt, err := time.Parse("20060102T150405Z", signedAtValue)
	if err != nil {
		return 0, false
	}
	rawResponseDate := ""
	for key, value := range headers {
		if strings.EqualFold(strings.TrimSpace(key), "date") {
			rawResponseDate = strings.TrimSpace(value)
			break
		}
	}
	if rawResponseDate == "" {
		return 0, false
	}
	responseDate, err := time.Parse(time.RFC1123, rawResponseDate)
	if err != nil {
		responseDate, err = time.Parse(time.RFC1123Z, rawResponseDate)
		if err != nil {
			return 0, false
		}
	}
	diff := responseDate.Sub(signedAt).Seconds()
	if diff < 0 {
		diff = -diff
	}
	if diff < 30 {
		return 0, false
	}
	// Return signed difference to indicate direction (positive means response clock is ahead).
	return int64(responseDate.Sub(signedAt).Seconds()), true
}

func transportToHTTPRequest(ctx context.Context, request TransportRequest) (*http.Request, error) {
	method := strings.TrimSpace(strings.ToUpper(request.Method))
	if method == "" {
		method = http.MethodGet
	}
	rawURL := strings.TrimSpace(request.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("core: transport request url is required")
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	query := parsedURL.Query()
	for key, value := range request.Query {
		query.Set(strings.TrimSpace(key), strings.TrimSpace(value))
	}
	parsedURL.RawQuery = query.Encode()

	body := io.NopCloser(bytes.NewReader(request.Body))
	httpRequest, err := http.NewRequestWithContext(ctx, method, parsedURL.String(), body)
	if err != nil {
		return nil, err
	}
	for key, value := range request.Headers {
		httpRequest.Header.Set(key, value)
	}
	return httpRequest, nil
}

func httpToTransportRequest(
	httpRequest *http.Request,
	original TransportRequest,
) (TransportRequest, error) {
	if httpRequest == nil {
		return TransportRequest{}, fmt.Errorf("core: http request is required")
	}
	out := cloneTransportRequest(original)
	out.Method = httpRequest.Method
	out.URL = httpRequest.URL.String()
	out.Headers = map[string]string{}
	for key, values := range httpRequest.Header {
		if len(values) == 0 {
			continue
		}
		out.Headers[key] = values[len(values)-1]
	}
	out.Query = map[string]string{}
	for key, values := range httpRequest.URL.Query() {
		if len(values) == 0 {
			continue
		}
		out.Query[key] = values[len(values)-1]
	}
	if httpRequest.Body != nil {
		payload, err := io.ReadAll(httpRequest.Body)
		if err != nil {
			return TransportRequest{}, err
		}
		out.Body = payload
		httpRequest.Body = io.NopCloser(bytes.NewReader(payload))
	}
	return out, nil
}

func cloneTransportRequest(in TransportRequest) TransportRequest {
	return TransportRequest{
		Method:               in.Method,
		URL:                  in.URL,
		Headers:              copyStringMap(in.Headers),
		Query:                copyStringMap(in.Query),
		Body:                 append([]byte(nil), in.Body...),
		Metadata:             copyAnyMap(in.Metadata),
		Timeout:              in.Timeout,
		MaxResponseBodyBytes: in.MaxResponseBodyBytes,
		Idempotency:          in.Idempotency,
	}
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func generateIdempotencyKey(
	providerID string,
	connectionID string,
	operation string,
	request TransportRequest,
) string {
	canonicalURL := canonicalTransportRequestURL(request.URL, request.Query)
	builder := strings.Builder{}
	builder.WriteString(strings.TrimSpace(strings.ToLower(providerID)))
	builder.WriteString("|")
	builder.WriteString(strings.TrimSpace(connectionID))
	builder.WriteString("|")
	builder.WriteString(strings.TrimSpace(strings.ToLower(operation)))
	builder.WriteString("|")
	builder.WriteString(strings.TrimSpace(strings.ToUpper(request.Method)))
	builder.WriteString("|")
	builder.WriteString(canonicalURL)
	builder.WriteString("|")
	builder.Write(request.Body)
	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}

func canonicalTransportRequestURL(rawURL string, query map[string]string) string {
	trimmedURL := strings.TrimSpace(rawURL)
	parsedURL, err := url.Parse(trimmedURL)
	if err != nil || parsedURL == nil {
		return trimmedURL
	}

	values := parsedURL.Query()
	for key, value := range query {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		values.Set(trimmedKey, strings.TrimSpace(value))
	}
	parsedURL.RawQuery = values.Encode()
	return parsedURL.String()
}

func isContextCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
