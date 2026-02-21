package inbound

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-services/core"
)

const (
	SurfaceWebhook       = "webhook"
	SurfaceCommand       = "command"
	SurfaceInteraction   = "interaction"
	SurfaceEventCallback = "event_callback"
)

type Verifier interface {
	Verify(ctx context.Context, req core.InboundRequest) error
}

type IdempotencyKeyExtractor func(req core.InboundRequest) (string, error)

type Dispatcher struct {
	Verifier   Verifier
	Store      core.IdempotencyClaimStore
	ExtractKey IdempotencyKeyExtractor
	KeyTTL     time.Duration

	mu       sync.RWMutex
	handlers map[string]core.InboundHandler
}

func NewDispatcher(verifier Verifier, store core.IdempotencyClaimStore) *Dispatcher {
	return &Dispatcher{
		Verifier:   verifier,
		Store:      store,
		ExtractKey: DefaultIdempotencyKeyExtractor,
		KeyTTL:     10 * time.Minute,
		handlers:   map[string]core.InboundHandler{},
	}
}

func (d *Dispatcher) Register(handler core.InboundHandler) error {
	if d == nil {
		return inboundInternal("inbound: dispatcher is nil", nil)
	}
	if handler == nil {
		return inboundBadInput("inbound: handler is nil", nil)
	}
	surface := normalizeSurface(handler.Surface())
	if !isSupportedSurface(surface) {
		return inboundBadInput(
			fmt.Sprintf("inbound: unsupported surface %q", surface),
			map[string]any{"surface": surface},
		)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.handlers[surface]; exists {
		return inboundError(
			fmt.Sprintf("inbound: handler already registered for surface %q", surface),
			goerrors.CategoryConflict,
			http.StatusConflict,
			core.ServiceErrorConflict,
			map[string]any{"surface": surface},
		)
	}
	d.handlers[surface] = handler
	return nil
}

func (d *Dispatcher) Dispatch(ctx context.Context, req core.InboundRequest) (core.InboundResult, error) {
	if d == nil {
		return core.InboundResult{}, inboundInternal("inbound: dispatcher is nil", nil)
	}
	req.ProviderID = strings.TrimSpace(req.ProviderID)
	req.Surface = normalizeSurface(req.Surface)
	if req.ProviderID == "" {
		return core.InboundResult{}, inboundBadInput("inbound: provider id is required", map[string]any{
			"surface": req.Surface,
		})
	}
	if !isSupportedSurface(req.Surface) {
		return core.InboundResult{}, inboundBadInput(
			fmt.Sprintf("inbound: unsupported surface %q", req.Surface),
			map[string]any{"provider_id": req.ProviderID, "surface": req.Surface},
		)
	}
	if d.Verifier != nil {
		if err := d.Verifier.Verify(ctx, req); err != nil {
			return core.InboundResult{
				Accepted:   false,
				StatusCode: http.StatusUnauthorized,
				Metadata: map[string]any{
					"provider_id": req.ProviderID,
					"surface":     req.Surface,
					"rejected":    true,
				},
			}, inboundWrapError(
				err,
				goerrors.CategoryAuth,
				"inbound: request verification failed",
				http.StatusUnauthorized,
				core.ServiceErrorUnauthorized,
				map[string]any{"provider_id": req.ProviderID, "surface": req.Surface},
			)
		}
	}

	claimID := ""
	if d.Store != nil {
		extractor := d.ExtractKey
		if extractor == nil {
			extractor = DefaultIdempotencyKeyExtractor
		}
		key, err := extractor(req)
		if err != nil {
			return core.InboundResult{}, inboundWrapError(
				err,
				goerrors.CategoryBadInput,
				"inbound: resolve idempotency key",
				http.StatusBadRequest,
				core.ServiceErrorBadInput,
				map[string]any{"provider_id": req.ProviderID, "surface": req.Surface},
			)
		}
		var accepted bool
		claimID, accepted, err = d.Store.Claim(ctx, req.ProviderID+":"+req.Surface+":"+key, d.keyTTL())
		if err != nil {
			return core.InboundResult{}, inboundWrapError(
				err,
				goerrors.CategoryOperation,
				"inbound: idempotency claim failed",
				http.StatusInternalServerError,
				core.ServiceErrorOperationFailed,
				map[string]any{
					"provider_id": req.ProviderID,
					"surface":     req.Surface,
					"idempotency": key,
				},
			)
		}
		if !accepted {
			return core.InboundResult{
				Accepted:   true,
				StatusCode: http.StatusOK,
				Metadata: map[string]any{
					"provider_id": req.ProviderID,
					"surface":     req.Surface,
					"deduped":     true,
				},
			}, nil
		}
	}

	handler := d.handlerFor(req.Surface)
	if handler == nil {
		return core.InboundResult{}, inboundError(
			fmt.Sprintf("inbound: no handler registered for surface %q", req.Surface),
			goerrors.CategoryNotFound,
			http.StatusNotFound,
			core.ServiceErrorNotFound,
			map[string]any{"provider_id": req.ProviderID, "surface": req.Surface},
		)
	}
	result, err := handler.Handle(ctx, req)
	if err != nil {
		handlerErr := inboundWrapError(
			err,
			goerrors.CategoryOperation,
			"inbound: handler execution failed",
			http.StatusBadGateway,
			core.ServiceErrorOperationFailed,
			map[string]any{"provider_id": req.ProviderID, "surface": req.Surface},
		)
		if d.Store != nil && claimID != "" {
			if failErr := d.Store.Fail(ctx, claimID, err, time.Time{}); failErr != nil {
				return core.InboundResult{}, errors.Join(
					handlerErr,
					inboundWrapError(
						failErr,
						goerrors.CategoryOperation,
						"inbound: mark idempotency claim failed",
						http.StatusInternalServerError,
						core.ServiceErrorInternal,
						map[string]any{"provider_id": req.ProviderID, "surface": req.Surface, "claim_id": claimID},
					),
				)
			}
		}
		return core.InboundResult{}, handlerErr
	}
	retryableFailure := !result.Accepted || result.StatusCode >= http.StatusInternalServerError
	if retryableFailure {
		retryErr := inboundError(
			fmt.Sprintf("inbound: handler returned retryable status %d", result.StatusCode),
			goerrors.CategoryOperation,
			http.StatusBadGateway,
			core.ServiceErrorOperationFailed,
			map[string]any{
				"provider_id": req.ProviderID,
				"surface":     req.Surface,
				"status_code": result.StatusCode,
			},
		)
		if d.Store != nil && claimID != "" {
			if failErr := d.Store.Fail(ctx, claimID, retryErr, time.Time{}); failErr != nil {
				return result, errors.Join(
					retryErr,
					inboundWrapError(
						failErr,
						goerrors.CategoryOperation,
						"inbound: mark idempotency claim failed",
						http.StatusInternalServerError,
						core.ServiceErrorInternal,
						map[string]any{"provider_id": req.ProviderID, "surface": req.Surface, "claim_id": claimID},
					),
				)
			}
		}
		return result, retryErr
	}
	if d.Store != nil && claimID != "" {
		if err := d.Store.Complete(ctx, claimID); err != nil {
			return core.InboundResult{}, inboundWrapError(
				err,
				goerrors.CategoryOperation,
				"inbound: complete idempotency claim",
				http.StatusInternalServerError,
				core.ServiceErrorOperationFailed,
				map[string]any{"provider_id": req.ProviderID, "surface": req.Surface, "claim_id": claimID},
			)
		}
	}
	result.Metadata = ensureMetadata(result.Metadata)
	result.Metadata["provider_id"] = req.ProviderID
	result.Metadata["surface"] = req.Surface
	return result, nil
}

func DefaultIdempotencyKeyExtractor(req core.InboundRequest) (string, error) {
	if req.Metadata != nil {
		if value := trimAny(req.Metadata["idempotency_key"]); value != "" {
			return value, nil
		}
		if value := trimAny(req.Metadata["delivery_id"]); value != "" {
			return value, nil
		}
		if value := trimAny(req.Metadata["message_id"]); value != "" {
			return value, nil
		}
	}
	if req.Headers != nil {
		if value := headerValue(req.Headers, "idempotency-key"); value != "" {
			return value, nil
		}
		if value := headerValue(req.Headers, "x-idempotency-key"); value != "" {
			return value, nil
		}
		if value := headerValue(req.Headers, "x-message-id"); value != "" {
			return value, nil
		}
	}
	return "", inboundBadInput("inbound: idempotency key is required", map[string]any{
		"provider_id": req.ProviderID,
		"surface":     req.Surface,
	})
}

type claimStatus string

const (
	claimStatusProcessing claimStatus = "processing"
	claimStatusRetryReady claimStatus = "retry_ready"
	claimStatusComplete   claimStatus = "complete"
)

type claimEntry struct {
	Key            string
	Status         claimStatus
	ClaimID        string
	Attempts       int
	KeyTTL         time.Duration
	LeaseExpiresAt time.Time
	RetryAt        time.Time
}

type InMemoryClaimStore struct {
	mu      sync.Mutex
	entries map[string]claimEntry
	claims  map[string]string
	nextID  int
	Now     func() time.Time
}

type InMemoryIdempotencyStore = InMemoryClaimStore

func NewInMemoryClaimStore() *InMemoryClaimStore {
	return &InMemoryClaimStore{
		entries: map[string]claimEntry{},
		claims:  map[string]string{},
		Now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func NewInMemoryIdempotencyStore() *InMemoryClaimStore {
	return NewInMemoryClaimStore()
}

func (s *InMemoryClaimStore) Claim(
	_ context.Context,
	key string,
	lease time.Duration,
) (string, bool, error) {
	if s == nil {
		return "", false, inboundInternal("inbound: idempotency store is nil", nil)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false, inboundBadInput("inbound: idempotency key is required", nil)
	}
	now := s.now()
	if lease <= 0 {
		lease = 10 * time.Minute
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictExpiredLocked(now)
	entry, exists := s.entries[key]
	if !exists {
		claimID := s.nextClaimID()
		s.entries[key] = claimEntry{
			Key:            key,
			Status:         claimStatusProcessing,
			ClaimID:        claimID,
			Attempts:       1,
			KeyTTL:         lease,
			LeaseExpiresAt: now.Add(lease),
		}
		s.claims[claimID] = key
		return claimID, true, nil
	}

	switch entry.Status {
	case claimStatusComplete:
		if !entry.LeaseExpiresAt.IsZero() && now.Before(entry.LeaseExpiresAt) {
			return "", false, nil
		}
	case claimStatusProcessing:
		if now.Before(entry.LeaseExpiresAt) {
			return "", false, nil
		}
	case claimStatusRetryReady:
		if !entry.RetryAt.IsZero() && now.Before(entry.RetryAt) {
			return "", false, nil
		}
	}

	if entry.ClaimID != "" {
		delete(s.claims, entry.ClaimID)
	}
	claimID := s.nextClaimID()
	entry.Status = claimStatusProcessing
	entry.ClaimID = claimID
	entry.Attempts++
	entry.KeyTTL = lease
	entry.LeaseExpiresAt = now.Add(lease)
	entry.RetryAt = time.Time{}
	s.entries[key] = entry
	s.claims[claimID] = key
	return claimID, true, nil
}

func (s *InMemoryClaimStore) Complete(_ context.Context, claimID string) error {
	if s == nil {
		return inboundInternal("inbound: idempotency store is nil", nil)
	}
	claimID = strings.TrimSpace(claimID)
	if claimID == "" {
		return inboundBadInput("inbound: claim id is required", nil)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.claims[claimID]
	if !ok {
		return nil
	}
	entry, exists := s.entries[key]
	if !exists || entry.ClaimID != claimID || entry.Status != claimStatusProcessing {
		delete(s.claims, claimID)
		return nil
	}
	ttl := entry.KeyTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	now := s.now()
	entry.Status = claimStatusComplete
	entry.LeaseExpiresAt = now.Add(ttl)
	entry.RetryAt = time.Time{}
	s.entries[key] = entry
	delete(s.claims, claimID)
	return nil
}

func (s *InMemoryClaimStore) Fail(
	_ context.Context,
	claimID string,
	_ error,
	retryAt time.Time,
) error {
	if s == nil {
		return inboundInternal("inbound: idempotency store is nil", nil)
	}
	claimID = strings.TrimSpace(claimID)
	if claimID == "" {
		return inboundBadInput("inbound: claim id is required", nil)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.claims[claimID]
	if !ok {
		return nil
	}
	entry, exists := s.entries[key]
	if !exists || entry.ClaimID != claimID || entry.Status != claimStatusProcessing {
		delete(s.claims, claimID)
		return nil
	}
	if retryAt.IsZero() {
		retryAt = s.now()
	}
	entry.Status = claimStatusRetryReady
	entry.RetryAt = retryAt.UTC()
	entry.LeaseExpiresAt = time.Time{}
	s.entries[key] = entry
	delete(s.claims, claimID)
	return nil
}

func (s *InMemoryClaimStore) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *InMemoryClaimStore) nextClaimID() string {
	s.nextID++
	return fmt.Sprintf("claim_%d", s.nextID)
}

func (s *InMemoryClaimStore) evictExpiredLocked(now time.Time) {
	for key, entry := range s.entries {
		if entry.Status != claimStatusComplete {
			continue
		}
		if entry.LeaseExpiresAt.IsZero() || !now.Before(entry.LeaseExpiresAt) {
			if entry.ClaimID != "" {
				delete(s.claims, entry.ClaimID)
			}
			delete(s.entries, key)
		}
	}
}

func (d *Dispatcher) keyTTL() time.Duration {
	if d != nil && d.KeyTTL > 0 {
		return d.KeyTTL
	}
	return 10 * time.Minute
}

func (d *Dispatcher) handlerFor(surface string) core.InboundHandler {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.handlers[normalizeSurface(surface)]
}

func normalizeSurface(surface string) string {
	return strings.TrimSpace(strings.ToLower(surface))
}

func isSupportedSurface(surface string) bool {
	switch normalizeSurface(surface) {
	case SurfaceWebhook, SurfaceCommand, SurfaceInteraction, SurfaceEventCallback:
		return true
	default:
		return false
	}
}

func trimAny(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func ensureMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return map[string]any{}
	}
	return metadata
}

func headerValue(headers map[string]string, key string) string {
	for existing, value := range headers {
		if strings.EqualFold(strings.TrimSpace(existing), strings.TrimSpace(key)) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
