package identity

import (
	"errors"
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestProfileNotFoundError_ToServiceError(t *testing.T) {
	err := &ProfileNotFoundError{Cause: errors.New("upstream unavailable")}
	mapped := err.ToServiceError()
	if mapped == nil {
		t.Fatalf("expected mapped error")
	}
	if mapped.TextCode != core.ServiceErrorProfileNotFound {
		t.Fatalf("expected %q text code, got %q", core.ServiceErrorProfileNotFound, mapped.TextCode)
	}
	if mapped.Code != 404 {
		t.Fatalf("expected status code 404, got %d", mapped.Code)
	}
}

func TestProfileNotFoundError_PreservesSentinel(t *testing.T) {
	err := profileNotFound(errors.New("token parse failed"))
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("expected errors.Is(err, ErrProfileNotFound) to be true")
	}
}
