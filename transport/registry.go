package transport

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/goliatone/go-services/core"
)

type AdapterFactory func(config map[string]any) (core.TransportAdapter, error)

type Registry struct {
	mu        sync.RWMutex
	adapters  map[string]core.TransportAdapter
	factories map[string]AdapterFactory
}

func NewRegistry() *Registry {
	return &Registry{
		adapters:  map[string]core.TransportAdapter{},
		factories: map[string]AdapterFactory{},
	}
}

func NewDefaultRegistry() *Registry {
	registry := NewRegistry()
	_ = registry.Register(NewRESTAdapter(nil))
	_ = registry.Register(NewGraphQLAdapter("", nil))
	for _, kind := range []string{KindSOAP, KindBulk, KindStream, KindFile} {
		_ = registry.RegisterFactory(kind, defaultProtocolFactory(kind))
	}
	return registry
}

func (r *Registry) Register(adapter core.TransportAdapter) error {
	if r == nil {
		return fmt.Errorf("transport: registry is nil")
	}
	if adapter == nil {
		return fmt.Errorf("transport: adapter is nil")
	}
	kind := normalizeKind(adapter.Kind())
	if kind == "" {
		return fmt.Errorf("transport: adapter kind is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.adapters[kind]; exists {
		return fmt.Errorf("transport: adapter kind %q already registered", kind)
	}
	r.adapters[kind] = adapter
	return nil
}

func (r *Registry) RegisterFactory(kind string, factory AdapterFactory) error {
	if r == nil {
		return fmt.Errorf("transport: registry is nil")
	}
	kind = normalizeKind(kind)
	if kind == "" {
		return fmt.Errorf("transport: adapter kind is required")
	}
	if factory == nil {
		return fmt.Errorf("transport: adapter factory is nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[kind]; exists {
		return fmt.Errorf("transport: adapter factory kind %q already registered", kind)
	}
	r.factories[kind] = factory
	return nil
}

func (r *Registry) Build(kind string, config map[string]any) (core.TransportAdapter, error) {
	if r == nil {
		return nil, fmt.Errorf("transport: registry is nil")
	}
	kind = normalizeKind(kind)
	if kind == "" {
		return nil, fmt.Errorf("transport: adapter kind is required")
	}

	r.mu.RLock()
	adapter, ok := r.adapters[kind]
	factory := r.factories[kind]
	r.mu.RUnlock()
	if ok {
		return adapter, nil
	}
	if factory == nil {
		return nil, fmt.Errorf("transport: adapter kind %q not registered", kind)
	}
	built, err := factory(cloneMap(config))
	if err != nil {
		return nil, err
	}
	if built == nil {
		return nil, fmt.Errorf("transport: factory for %q returned nil adapter", kind)
	}
	return built, nil
}

func (r *Registry) Get(kind string) (core.TransportAdapter, bool) {
	if r == nil {
		return nil, false
	}
	kind = normalizeKind(kind)
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapter, ok := r.adapters[kind]
	return adapter, ok
}

func (r *Registry) List() []core.TransportAdapter {
	if r == nil {
		return []core.TransportAdapter{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	kinds := make([]string, 0, len(r.adapters))
	for kind := range r.adapters {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	result := make([]core.TransportAdapter, 0, len(kinds))
	for _, kind := range kinds {
		result = append(result, r.adapters[kind])
	}
	return result
}

func normalizeKind(kind string) string {
	return strings.TrimSpace(strings.ToLower(kind))
}

func defaultNoopFactory(kind string) AdapterFactory {
	return func(config map[string]any) (core.TransportAdapter, error) {
		reason := strings.TrimSpace(fmt.Sprint(config["reason"]))
		return NewUnsupportedAdapter(kind, reason), nil
	}
}

func defaultProtocolFactory(kind string) AdapterFactory {
	return func(config map[string]any) (core.TransportAdapter, error) {
		switch normalizeKind(kind) {
		case KindSOAP:
			return NewSOAPAdapter(nil), nil
		case KindBulk:
			return NewBulkAdapter(nil), nil
		case KindStream:
			return NewStreamAdapter(nil), nil
		case KindFile:
			return NewFileAdapter(nil), nil
		default:
			return defaultNoopFactory(kind)(config)
		}
	}
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
