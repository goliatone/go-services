package core

import "testing"

func TestProviderRegistry_ListDeterministicOrder(t *testing.T) {
	registry := NewProviderRegistry()
	for _, provider := range []Provider{
		testProvider{id: "zeta"},
		testProvider{id: "alpha"},
		testProvider{id: "beta"},
	} {
		if err := registry.Register(provider); err != nil {
			t.Fatalf("register provider: %v", err)
		}
	}

	listed := registry.List()
	if len(listed) != 3 {
		t.Fatalf("expected 3 providers, got %d", len(listed))
	}

	got := []string{listed[0].ID(), listed[1].ID(), listed[2].ID()}
	want := []string{"alpha", "beta", "zeta"}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("unexpected ordering at index %d: got %v want %v", idx, got, want)
		}
	}
}

func TestProviderRegistry_DuplicateIDRejected(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.Register(testProvider{id: "github"}); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	if err := registry.Register(testProvider{id: "github"}); err == nil {
		t.Fatalf("expected duplicate registration to fail")
	}
}
