package core

import "testing"

func TestRedactSensitiveMapPreservesTraceabilityMetadata(t *testing.T) {
	redacted := RedactSensitiveMap(map[string]any{
		"trace_id":       "trace_1",
		"request_id":     "req_1",
		"connection_id":  "conn_1",
		"access_token":   "secret-token",
		"authorization":  "Bearer secret-token",
		"nested":         map[string]any{"refresh_token": "refresh", "trace_id": "trace_nested"},
		"events":         []any{map[string]any{"api_key": "key_1"}, map[string]any{"external_id": "ext_1"}},
		"source_version": "v1",
	})

	if redacted["trace_id"] != "trace_1" {
		t.Fatalf("expected trace_id to remain visible, got %#v", redacted["trace_id"])
	}
	if redacted["access_token"] != RedactedValue {
		t.Fatalf("expected access_token to be redacted, got %#v", redacted["access_token"])
	}
	nested, ok := redacted["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested redacted map")
	}
	if nested["refresh_token"] != RedactedValue {
		t.Fatalf("expected nested refresh_token to be redacted, got %#v", nested["refresh_token"])
	}
	if nested["trace_id"] != "trace_nested" {
		t.Fatalf("expected nested trace_id to remain visible, got %#v", nested["trace_id"])
	}
}
