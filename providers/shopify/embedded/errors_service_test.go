package embedded

import (
	"testing"

	"github.com/goliatone/go-services/core"
)

func TestValidationError_ToServiceError(t *testing.T) {
	err := (&ValidationError{
		Code:  "invalid_session_token",
		Field: "session_token",
		Cause: ErrInvalidSessionToken,
	}).ToServiceError()

	if err == nil {
		t.Fatalf("expected mapped error")
	}
	if err.TextCode != core.ServiceErrorEmbeddedSessionInvalid {
		t.Fatalf("expected %q text code, got %q", core.ServiceErrorEmbeddedSessionInvalid, err.TextCode)
	}
	if err.Code != 401 {
		t.Fatalf("expected status code 401, got %d", err.Code)
	}
}

func TestExchangeError_ToServiceError(t *testing.T) {
	err := (&ExchangeError{
		StatusCode: 429,
		ErrorCode:  "throttled",
		Message:    "rate limit",
		Cause:      ErrTokenExchangeFailed,
	}).ToServiceError()

	if err == nil {
		t.Fatalf("expected mapped error")
	}
	if err.TextCode != core.ServiceErrorRateLimited {
		t.Fatalf("expected %q text code, got %q", core.ServiceErrorRateLimited, err.TextCode)
	}
	if err.Code != 429 {
		t.Fatalf("expected status code 429, got %d", err.Code)
	}
}
