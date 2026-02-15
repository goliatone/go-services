package services

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/goliatone/go-services/core"
)

type ProviderPack struct {
	Name      string
	Providers []core.Provider
}

type CapabilityDescriptorPack struct {
	Name        string
	ProviderID  string
	Descriptors []core.CapabilityDescriptor
}

type CommandQueryBundleFactory func(service CommandQueryService) (any, error)

type ExtensionHooks struct {
	mu sync.RWMutex

	providerPacks   map[string]ProviderPack
	capabilityPacks map[string]CapabilityDescriptorPack
	bundles         map[string]CommandQueryBundleFactory
}

func NewExtensionHooks() *ExtensionHooks {
	return &ExtensionHooks{
		providerPacks:   map[string]ProviderPack{},
		capabilityPacks: map[string]CapabilityDescriptorPack{},
		bundles:         map[string]CommandQueryBundleFactory{},
	}
}

func (h *ExtensionHooks) RegisterProviderPack(pack ProviderPack) error {
	if h == nil {
		return fmt.Errorf("services: extension hooks are nil")
	}
	name := strings.TrimSpace(pack.Name)
	if name == "" {
		return fmt.Errorf("services: provider pack name is required")
	}
	if len(pack.Providers) == 0 {
		return fmt.Errorf("services: provider pack %q has no providers", name)
	}

	normalized := ProviderPack{
		Name:      name,
		Providers: append([]core.Provider(nil), pack.Providers...),
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.providerPacks[name]; exists {
		return fmt.Errorf("services: provider pack %q already registered", name)
	}
	h.providerPacks[name] = normalized
	return nil
}

func (h *ExtensionHooks) RegisterCapabilityPack(pack CapabilityDescriptorPack) error {
	if h == nil {
		return fmt.Errorf("services: extension hooks are nil")
	}
	name := strings.TrimSpace(pack.Name)
	providerID := strings.TrimSpace(strings.ToLower(pack.ProviderID))
	if name == "" {
		return fmt.Errorf("services: capability pack name is required")
	}
	if providerID == "" {
		return fmt.Errorf("services: capability pack %q provider id is required", name)
	}
	if len(pack.Descriptors) == 0 {
		return fmt.Errorf("services: capability pack %q has no descriptors", name)
	}

	normalized := CapabilityDescriptorPack{
		Name:       name,
		ProviderID: providerID,
		Descriptors: append(
			[]core.CapabilityDescriptor(nil),
			pack.Descriptors...,
		),
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.capabilityPacks[name]; exists {
		return fmt.Errorf("services: capability pack %q already registered", name)
	}
	h.capabilityPacks[name] = normalized
	return nil
}

func (h *ExtensionHooks) RegisterCommandQueryBundle(
	name string,
	factory CommandQueryBundleFactory,
) error {
	if h == nil {
		return fmt.Errorf("services: extension hooks are nil")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("services: command/query bundle name is required")
	}
	if factory == nil {
		return fmt.Errorf("services: command/query bundle %q factory is required", name)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.bundles[name]; exists {
		return fmt.Errorf("services: command/query bundle %q already registered", name)
	}
	h.bundles[name] = factory
	return nil
}

func (h *ExtensionHooks) ApplyProviderPacks(registry core.Registry) error {
	if h == nil {
		return nil
	}
	if registry == nil {
		return fmt.Errorf("services: registry is required")
	}

	packs := h.ProviderPacks()
	for _, pack := range packs {
		for _, provider := range pack.Providers {
			if provider == nil {
				return fmt.Errorf("services: provider pack %q contains nil provider", pack.Name)
			}
			if err := registry.Register(provider); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *ExtensionHooks) BuildCommandQueryBundles(
	service CommandQueryService,
) (map[string]any, error) {
	if h == nil {
		return map[string]any{}, nil
	}
	if service == nil {
		return nil, fmt.Errorf("services: command/query service is required")
	}

	h.mu.RLock()
	names := make([]string, 0, len(h.bundles))
	for name := range h.bundles {
		names = append(names, name)
	}
	sort.Strings(names)
	factories := make(map[string]CommandQueryBundleFactory, len(h.bundles))
	for name, factory := range h.bundles {
		factories[name] = factory
	}
	h.mu.RUnlock()

	result := make(map[string]any, len(names))
	for _, name := range names {
		bundle, err := factories[name](service)
		if err != nil {
			return nil, err
		}
		result[name] = bundle
	}
	return result, nil
}

func (h *ExtensionHooks) ProviderPacks() []ProviderPack {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()

	names := make([]string, 0, len(h.providerPacks))
	for name := range h.providerPacks {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]ProviderPack, 0, len(names))
	for _, name := range names {
		pack := h.providerPacks[name]
		out = append(out, ProviderPack{
			Name:      pack.Name,
			Providers: append([]core.Provider(nil), pack.Providers...),
		})
	}
	return out
}

func (h *ExtensionHooks) CapabilityDescriptors(providerID string) []core.CapabilityDescriptor {
	if h == nil {
		return nil
	}
	providerID = strings.TrimSpace(strings.ToLower(providerID))
	h.mu.RLock()
	defer h.mu.RUnlock()

	packNames := make([]string, 0, len(h.capabilityPacks))
	for name, pack := range h.capabilityPacks {
		if pack.ProviderID == providerID {
			packNames = append(packNames, name)
		}
	}
	sort.Strings(packNames)

	out := []core.CapabilityDescriptor{}
	for _, name := range packNames {
		pack := h.capabilityPacks[name]
		out = append(out, pack.Descriptors...)
	}
	return append([]core.CapabilityDescriptor(nil), out...)
}

func (h *ExtensionHooks) BundleNames() []string {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	names := make([]string, 0, len(h.bundles))
	for name := range h.bundles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
