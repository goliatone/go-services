package webhooks

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/goliatone/go-services/core"
)

type BurstMode string

const (
	BurstModeNone     BurstMode = "none"
	BurstModeCoalesce BurstMode = "coalesce"
	BurstModeDebounce BurstMode = "debounce"
)

type BurstDecision struct {
	Allow    bool
	Metadata map[string]any
}

type BurstController interface {
	Allow(ctx context.Context, req core.InboundRequest) (BurstDecision, error)
}

type BurstKeyExtractor func(req core.InboundRequest) (string, bool)

type BurstOptions struct {
	Mode       BurstMode
	Window     time.Duration
	MaxEntries int
	ExtractKey BurstKeyExtractor
	Now        func() time.Time
}

type DefaultBurstController struct {
	mode       BurstMode
	window     time.Duration
	maxEntries int
	extractKey BurstKeyExtractor
	now        func() time.Time

	mu      sync.Mutex
	entries map[string]time.Time
}

func NewBurstController(opts BurstOptions) *DefaultBurstController {
	mode := normalizeBurstMode(opts.Mode)
	window := opts.Window
	if window <= 0 {
		window = 2 * time.Second
	}
	maxEntries := opts.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 4096
	}
	extractKey := opts.ExtractKey
	if extractKey == nil {
		extractKey = DefaultBurstKeyExtractor
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &DefaultBurstController{
		mode:       mode,
		window:     window,
		maxEntries: maxEntries,
		extractKey: extractKey,
		now:        now,
		entries:    map[string]time.Time{},
	}
}

func (c *DefaultBurstController) Allow(_ context.Context, req core.InboundRequest) (BurstDecision, error) {
	if c == nil {
		return BurstDecision{Allow: true}, nil
	}
	if c.mode == BurstModeNone {
		return BurstDecision{Allow: true}, nil
	}
	key, ok := c.extractKey(req)
	if !ok {
		return BurstDecision{Allow: true}, nil
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return BurstDecision{Allow: true}, nil
	}

	now := c.now().UTC()
	c.mu.Lock()
	defer c.mu.Unlock()

	lastSeen, exists := c.entries[key]
	c.entries[key] = now
	c.cleanup(now)
	if !exists {
		return BurstDecision{Allow: true}, nil
	}
	if now.Sub(lastSeen) >= c.window {
		return BurstDecision{Allow: true}, nil
	}

	metadata := map[string]any{
		"burst_mode":      string(c.mode),
		"burst_key":       key,
		"burst_window_ms": c.window.Milliseconds(),
	}
	switch c.mode {
	case BurstModeCoalesce:
		metadata["coalesced"] = true
	case BurstModeDebounce:
		metadata["debounced"] = true
	default:
		return BurstDecision{Allow: true}, nil
	}
	return BurstDecision{Allow: false, Metadata: metadata}, nil
}

func (c *DefaultBurstController) cleanup(now time.Time) {
	if len(c.entries) <= c.maxEntries {
		for key, seenAt := range c.entries {
			if now.Sub(seenAt) > c.window*4 {
				delete(c.entries, key)
			}
		}
		return
	}
	for key, seenAt := range c.entries {
		if now.Sub(seenAt) > c.window {
			delete(c.entries, key)
		}
		if len(c.entries) <= c.maxEntries {
			break
		}
	}
}

func DefaultBurstKeyExtractor(req core.InboundRequest) (string, bool) {
	providerID := strings.TrimSpace(strings.ToLower(req.ProviderID))
	if providerID == "" {
		return "", false
	}
	if req.Metadata != nil {
		for _, key := range []string{"burst_key", "channel_id", "resource_id"} {
			value := strings.TrimSpace(fmt.Sprint(req.Metadata[key]))
			if value != "" && value != "<nil>" {
				return providerID + ":" + strings.ToLower(value), true
			}
		}
	}
	if req.Headers != nil {
		for _, key := range []string{"x-goog-channel-id", "x-channel-id", "x-resource-id"} {
			value := strings.TrimSpace(headerValue(req.Headers, key))
			if value != "" {
				return providerID + ":" + strings.ToLower(value), true
			}
		}
	}
	return "", false
}

func normalizeBurstMode(mode BurstMode) BurstMode {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case string(BurstModeCoalesce):
		return BurstModeCoalesce
	case string(BurstModeDebounce):
		return BurstModeDebounce
	default:
		return BurstModeNone
	}
}

var _ BurstController = (*DefaultBurstController)(nil)
