package services

import (
	"context"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestExtensionHooks_RegisterAndApplyProviderPacks(t *testing.T) {
	hooks := NewExtensionHooks()
	pack := ProviderPack{
		Name: "downstream-pack",
		Providers: []core.Provider{
			extensionProvider{id: "custom_provider"},
		},
	}
	if err := hooks.RegisterProviderPack(pack); err != nil {
		t.Fatalf("register provider pack: %v", err)
	}
	if err := hooks.RegisterProviderPack(pack); err == nil {
		t.Fatalf("expected duplicate provider pack registration error")
	}

	registry := core.NewProviderRegistry()
	if err := hooks.ApplyProviderPacks(registry); err != nil {
		t.Fatalf("apply provider packs: %v", err)
	}
	if _, ok := registry.Get("custom_provider"); !ok {
		t.Fatalf("expected provider pack registration in registry")
	}
}

func TestExtensionHooks_CapabilityDescriptorsAndBundles(t *testing.T) {
	hooks := NewExtensionHooks()
	if err := hooks.RegisterCapabilityPack(CapabilityDescriptorPack{
		Name:       "pack_b",
		ProviderID: "custom_provider",
		Descriptors: []core.CapabilityDescriptor{
			{Name: "orders.read"},
		},
	}); err != nil {
		t.Fatalf("register capability pack b: %v", err)
	}
	if err := hooks.RegisterCapabilityPack(CapabilityDescriptorPack{
		Name:       "pack_a",
		ProviderID: "custom_provider",
		Descriptors: []core.CapabilityDescriptor{
			{Name: "orders.write"},
		},
	}); err != nil {
		t.Fatalf("register capability pack a: %v", err)
	}
	descriptors := hooks.CapabilityDescriptors("custom_provider")
	if len(descriptors) != 2 {
		t.Fatalf("expected two capability descriptors, got %d", len(descriptors))
	}
	if descriptors[0].Name != "orders.write" || descriptors[1].Name != "orders.read" {
		t.Fatalf("expected deterministic capability pack ordering, got %#v", descriptors)
	}

	if err := hooks.RegisterCommandQueryBundle("orders_bundle", func(service CommandQueryService) (any, error) {
		return map[string]any{
			"revoke_fn":      service.Revoke,
			"load_cursor_fn": service.LoadSyncCursor,
		}, nil
	}); err != nil {
		t.Fatalf("register bundle: %v", err)
	}
	if err := hooks.RegisterCommandQueryBundle("orders_bundle", func(CommandQueryService) (any, error) { return nil, nil }); err == nil {
		t.Fatalf("expected duplicate bundle registration error")
	}

	svc := &stubFacadeService{}
	bundles, err := hooks.BuildCommandQueryBundles(svc)
	if err != nil {
		t.Fatalf("build bundles: %v", err)
	}
	if len(bundles) != 1 {
		t.Fatalf("expected one bundle, got %d", len(bundles))
	}
	if _, ok := bundles["orders_bundle"]; !ok {
		t.Fatalf("expected orders_bundle entry in built bundles")
	}
}

type extensionProvider struct {
	id string
}

func (p extensionProvider) ID() string { return p.id }

func (extensionProvider) AuthKind() string { return core.AuthKindOAuth2AuthCode }

func (extensionProvider) SupportedScopeTypes() []string { return []string{"user", "org"} }

func (extensionProvider) Capabilities() []core.CapabilityDescriptor { return nil }

func (p extensionProvider) BeginAuth(context.Context, core.BeginAuthRequest) (core.BeginAuthResponse, error) {
	return core.BeginAuthResponse{URL: "https://example.test/auth", State: p.id}, nil
}

func (extensionProvider) CompleteAuth(context.Context, core.CompleteAuthRequest) (core.CompleteAuthResponse, error) {
	return core.CompleteAuthResponse{}, nil
}

func (extensionProvider) Refresh(context.Context, core.ActiveCredential) (core.RefreshResult, error) {
	return core.RefreshResult{}, nil
}
