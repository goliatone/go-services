package core

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type ProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{providers: make(map[string]Provider)}
}

func (r *ProviderRegistry) Register(provider Provider) error {
	if provider == nil {
		return fmt.Errorf("core: provider is nil")
	}
	id := strings.TrimSpace(provider.ID())
	if id == "" {
		return fmt.Errorf("core: provider id is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[id]; exists {
		return fmt.Errorf("core: provider already registered: %s", id)
	}
	r.providers[id] = provider
	return nil
}

func (r *ProviderRegistry) Get(providerID string) (Provider, bool) {
	id := strings.TrimSpace(providerID)
	if id == "" {
		return nil, false
	}
	r.mu.RLock()
	provider, ok := r.providers[id]
	r.mu.RUnlock()
	return provider, ok
}

func (r *ProviderRegistry) List() []Provider {
	r.mu.RLock()
	providers := make([]Provider, 0, len(r.providers))
	keys := make([]string, 0, len(r.providers))
	for id := range r.providers {
		keys = append(keys, id)
	}
	r.mu.RUnlock()
	sort.Strings(keys)
	r.mu.RLock()
	for _, id := range keys {
		providers = append(providers, r.providers[id])
	}
	r.mu.RUnlock()
	return providers
}
