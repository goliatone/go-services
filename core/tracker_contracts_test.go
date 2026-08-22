package core

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTrackerResourceRequiresLosslessVersionedIdentity(t *testing.T) {
	resource := TrackerResource{ProviderID: "linear", ResourceType: "issue", ExternalID: "LIN-1", ProviderRevision: "r1", CanonicalURL: "https://linear.app/issue/LIN-1", ObservedAt: time.Now().UTC(), NormalizedFields: json.RawMessage(`{"title":"Issue","state":"open"}`), NativeExtension: json.RawMessage(`{"priority":1}`), SchemaRevision: "schema-r1"}
	if err := resource.Validate(); err != nil {
		t.Fatal(err)
	}
	resource.CanonicalURL = "http://linear.test/issue/LIN-1"
	if err := resource.Validate(); err == nil {
		t.Fatal("insecure canonical URL was accepted")
	}
}

func TestTrackerPagesRequireContinuationCursor(t *testing.T) {
	if err := (TrackerNodePage{HasMore: true, Revision: "r1"}).Validate(); err == nil {
		t.Fatal("node page accepted has_more without cursor")
	}
	if err := (TrackerSchemaPage{HasMore: true, Revision: "r1"}).Validate(); err == nil {
		t.Fatal("schema page accepted has_more without cursor")
	}
	if err := (TrackerChangePage{HasMore: true}).Validate(); err == nil {
		t.Fatal("change page accepted has_more without cursor")
	}
}
