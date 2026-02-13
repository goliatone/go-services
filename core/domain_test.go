package core

import (
	"errors"
	"testing"
	"time"
)

func TestConnectionTransitionTo_ValidAndInvalid(t *testing.T) {
	now := time.Now().UTC()
	conn := Connection{Status: ConnectionStatusActive}

	if err := conn.TransitionTo(ConnectionStatusPendingReauth, "token expired", now); err != nil {
		t.Fatalf("expected valid transition, got error: %v", err)
	}
	if conn.Status != ConnectionStatusPendingReauth {
		t.Fatalf("expected pending_reauth, got %q", conn.Status)
	}
	if conn.LastError == "" {
		t.Fatalf("expected last_error to be set")
	}

	err := conn.TransitionTo(ConnectionStatusNeedsReconsent, "", now)
	if !errors.Is(err, ErrInvalidConnectionStatusTransition) {
		t.Fatalf("expected invalid transition error, got: %v", err)
	}
}

func TestCredentialTransitionTo_ValidAndInvalid(t *testing.T) {
	now := time.Now().UTC()
	cred := Credential{Status: CredentialStatusActive}

	if err := cred.TransitionTo(CredentialStatusExpired, now); err != nil {
		t.Fatalf("expected active->expired to work: %v", err)
	}
	if cred.Status != CredentialStatusExpired {
		t.Fatalf("expected expired status, got %q", cred.Status)
	}

	if err := cred.TransitionTo(CredentialStatusActive, now); err != nil {
		t.Fatalf("expected expired->active to work: %v", err)
	}
	if err := cred.TransitionTo(CredentialStatusRevoked, now); err != nil {
		t.Fatalf("expected active->revoked to work: %v", err)
	}

	err := cred.TransitionTo(CredentialStatusActive, now)
	if !errors.Is(err, ErrInvalidCredentialStatusTransition) {
		t.Fatalf("expected invalid transition error, got: %v", err)
	}
}

func TestInstallationTransitionTo_ValidAndInvalid(t *testing.T) {
	now := time.Now().UTC()
	installation := Installation{Status: InstallationStatusActive}

	if err := installation.TransitionTo(InstallationStatusNeedsReconsent, now); err != nil {
		t.Fatalf("expected active->needs_reconsent to work: %v", err)
	}
	if err := installation.TransitionTo(InstallationStatusUninstalled, now); err != nil {
		t.Fatalf("expected needs_reconsent->uninstalled to work: %v", err)
	}

	err := installation.TransitionTo(InstallationStatusActive, now)
	if !errors.Is(err, ErrInvalidInstallationStatusTransition) {
		t.Fatalf("expected invalid transition error, got: %v", err)
	}
}
