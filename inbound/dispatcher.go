package inbound

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

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

type IdempotencyStore interface {
	Reserve(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

type IdempotencyKeyExtractor func(req core.InboundRequest) (string, error)

type Dispatcher struct {
	Verifier   Verifier
	Store      IdempotencyStore
	ExtractKey IdempotencyKeyExtractor
	KeyTTL     time.Duration

	mu       sync.RWMutex
	handlers map[string]core.InboundHandler
}

func NewDispatcher(verifier Verifier, store IdempotencyStore) *Dispatcher {
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
		return fmt.Errorf("inbound: dispatcher is nil")
	}
	if handler == nil {
		return fmt.Errorf("inbound: handler is nil")
	}
	surface := normalizeSurface(handler.Surface())
	if !isSupportedSurface(surface) {
		return fmt.Errorf("inbound: unsupported surface %q", surface)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.handlers[surface]; exists {
		return fmt.Errorf("inbound: handler already registered for surface %q", surface)
	}
	d.handlers[surface] = handler
	return nil
}

func (d *Dispatcher) Dispatch(ctx context.Context, req core.InboundRequest) (core.InboundResult, error) {
	if d == nil {
		return core.InboundResult{}, fmt.Errorf("inbound: dispatcher is nil")
	}
	req.ProviderID = strings.TrimSpace(req.ProviderID)
	req.Surface = normalizeSurface(req.Surface)
	if req.ProviderID == "" {
		return core.InboundResult{}, fmt.Errorf("inbound: provider id is required")
	}
	if !isSupportedSurface(req.Surface) {
		return core.InboundResult{}, fmt.Errorf("inbound: unsupported surface %q", req.Surface)
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
			}, err
		}
	}

	if d.Store != nil {
		extractor := d.ExtractKey
		if extractor == nil {
			extractor = DefaultIdempotencyKeyExtractor
		}
		key, err := extractor(req)
		if err != nil {
			return core.InboundResult{}, err
		}
		accepted, err := d.Store.Reserve(ctx, req.ProviderID+":"+req.Surface+":"+key, d.keyTTL())
		if err != nil {
			return core.InboundResult{}, err
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
		return core.InboundResult{}, fmt.Errorf("inbound: no handler registered for surface %q", req.Surface)
	}
	result, err := handler.Handle(ctx, req)
	if err != nil {
		return core.InboundResult{}, err
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
	return "", fmt.Errorf("inbound: idempotency key is required")
}

type InMemoryIdempotencyStore struct {
	mu      sync.Mutex
	entries map[string]time.Time
	Now     func() time.Time
}

func NewInMemoryIdempotencyStore() *InMemoryIdempotencyStore {
	return &InMemoryIdempotencyStore{
		entries: map[string]time.Time{},
		Now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *InMemoryIdempotencyStore) Reserve(_ context.Context, key string, ttl time.Duration) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("inbound: idempotency store is nil")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return false, fmt.Errorf("inbound: idempotency key is required")
	}
	now := s.now()
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for existingKey, expiresAt := range s.entries {
		if now.After(expiresAt) {
			delete(s.entries, existingKey)
		}
	}
	if expiresAt, exists := s.entries[key]; exists && now.Before(expiresAt) {
		return false, nil
	}
	s.entries[key] = now.Add(ttl)
	return true, nil
}

func (s *InMemoryIdempotencyStore) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
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
