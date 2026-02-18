package security

import "time"

// KeyRotationWindow gates when a key version is allowed to encrypt/decrypt.
type KeyRotationWindow struct {
	NotBefore time.Time
	NotAfter  time.Time
}

func (w KeyRotationWindow) Allows(at time.Time) bool {
	ts := at.UTC()
	if !w.NotBefore.IsZero() && ts.Before(w.NotBefore.UTC()) {
		return false
	}
	if !w.NotAfter.IsZero() && ts.After(w.NotAfter.UTC()) {
		return false
	}
	return true
}
