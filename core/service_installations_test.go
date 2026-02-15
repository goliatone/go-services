package core

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestService_InstallationLifecycle_APIs(t *testing.T) {
	ctx := context.Background()
	store := newMemoryInstallationStore()
	svc, err := NewService(Config{}, WithInstallationStore(store))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	grantedAt := time.Now().UTC().Add(-time.Minute)
	installation, err := svc.UpsertInstallation(ctx, UpsertInstallationInput{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "org", ID: "org_1"},
		InstallType: "marketplace_app",
		Status:      InstallationStatusActive,
		GrantedAt:   &grantedAt,
		Metadata:    map[string]any{"installer": "admin_1"},
	})
	if err != nil {
		t.Fatalf("upsert installation: %v", err)
	}
	if installation.ID == "" {
		t.Fatalf("expected installation id")
	}

	stored, err := svc.GetInstallation(ctx, installation.ID)
	if err != nil {
		t.Fatalf("get installation: %v", err)
	}
	if stored.InstallType != "marketplace_app" {
		t.Fatalf("unexpected install type %q", stored.InstallType)
	}

	items, err := svc.ListInstallations(ctx, "github", ScopeRef{Type: "org", ID: "org_1"})
	if err != nil {
		t.Fatalf("list installations: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one installation, got %d", len(items))
	}

	if err := svc.UpdateInstallationStatus(ctx, installation.ID, string(InstallationStatusSuspended), "policy"); err != nil {
		t.Fatalf("update installation status: %v", err)
	}
	updated, err := svc.GetInstallation(ctx, installation.ID)
	if err != nil {
		t.Fatalf("get updated installation: %v", err)
	}
	if updated.Status != InstallationStatusSuspended {
		t.Fatalf("expected suspended status, got %q", updated.Status)
	}
}

func TestService_InstallationLifecycle_ValidatesInput(t *testing.T) {
	ctx := context.Background()
	store := newMemoryInstallationStore()
	svc, err := NewService(Config{}, WithInstallationStore(store))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if _, err := svc.UpsertInstallation(ctx, UpsertInstallationInput{
		ProviderID:  "",
		Scope:       ScopeRef{Type: "org", ID: "org_1"},
		InstallType: "marketplace_app",
	}); err == nil {
		t.Fatalf("expected missing provider id error")
	}
	if _, err := svc.GetInstallation(ctx, ""); err == nil {
		t.Fatalf("expected missing installation id error")
	}
	if _, err := svc.ListInstallations(ctx, "", ScopeRef{Type: "org", ID: "org_1"}); err == nil {
		t.Fatalf("expected missing provider id error")
	}
	if err := svc.UpdateInstallationStatus(ctx, "", string(InstallationStatusActive), ""); err == nil {
		t.Fatalf("expected missing id error")
	}
}

func TestService_InstallationLifecycle_EnforcesStatusTransitions(t *testing.T) {
	ctx := context.Background()
	store := newMemoryInstallationStore()
	svc, err := NewService(Config{}, WithInstallationStore(store))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	installation, err := svc.UpsertInstallation(ctx, UpsertInstallationInput{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "org", ID: "org_2"},
		InstallType: "marketplace_app",
		Status:      InstallationStatusActive,
	})
	if err != nil {
		t.Fatalf("upsert installation: %v", err)
	}

	if err := svc.UpdateInstallationStatus(ctx, installation.ID, string(InstallationStatusUninstalled), "removed"); err != nil {
		t.Fatalf("uninstall transition: %v", err)
	}
	err = svc.UpdateInstallationStatus(ctx, installation.ID, string(InstallationStatusActive), "reactivate")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "invalid installation status transition") {
		t.Fatalf("expected invalid transition error, got %v", err)
	}

	_, err = svc.UpsertInstallation(ctx, UpsertInstallationInput{
		ProviderID:  "github",
		Scope:       ScopeRef{Type: "org", ID: "org_3"},
		InstallType: "marketplace_app",
		Status:      InstallationStatusSuspended,
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "created with status active") {
		t.Fatalf("expected active-on-create enforcement error, got %v", err)
	}

	err = svc.UpdateInstallationStatus(ctx, installation.ID, "invalid_status", "bad")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "invalid installation status") {
		t.Fatalf("expected invalid status error, got %v", err)
	}
}
