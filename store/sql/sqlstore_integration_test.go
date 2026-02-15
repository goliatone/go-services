package sqlstore_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	persistence "github.com/goliatone/go-persistence-bun"
	"github.com/goliatone/go-services/core"
	servicemigrations "github.com/goliatone/go-services/migrations"
	servicesratelimit "github.com/goliatone/go-services/ratelimit"
	servicessecurity "github.com/goliatone/go-services/security"
	sqlstore "github.com/goliatone/go-services/store/sql"
	servicesync "github.com/goliatone/go-services/sync"
	serviceswebhooks "github.com/goliatone/go-services/webhooks"
	_ "github.com/mattn/go-sqlite3"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

type testPersistenceConfig struct {
	driver string
	server string
}

func (c testPersistenceConfig) GetDebug() bool {
	return false
}

func (c testPersistenceConfig) GetDriver() string {
	return c.driver
}

func (c testPersistenceConfig) GetServer() string {
	return c.server
}

func (c testPersistenceConfig) GetPingTimeout() time.Duration {
	return time.Second
}

func (c testPersistenceConfig) GetOtelIdentifier() string {
	return "go-services-tests"
}

func TestMigrationSmokeApplySQLite(t *testing.T) {
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	var tableName string
	if err := client.DB().NewRaw(
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?",
		"service_connections",
	).Scan(context.Background(), &tableName); err != nil {
		t.Fatalf("query sqlite master: %v", err)
	}
	if tableName != "service_connections" {
		t.Fatalf("expected service_connections table, got %q", tableName)
	}
}

func TestConnectionAndCredentialStores_EnforceVersioningAndUniqueness(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	factory, err := sqlstore.NewRepositoryFactoryFromPersistence(client)
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}

	connectionStore := factory.ConnectionStore()
	credentialStore := factory.CredentialStore()
	if connectionStore == nil || credentialStore == nil {
		t.Fatalf("expected connection and credential stores from factory")
	}

	connection, err := connectionStore.Create(ctx, core.CreateConnectionInput{
		ProviderID:        "github",
		Scope:             core.ScopeRef{Type: "user", ID: "usr_1"},
		ExternalAccountID: "acct_1",
		Status:            core.ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	if _, err := connectionStore.Create(ctx, core.CreateConnectionInput{
		ProviderID:        "github",
		Scope:             core.ScopeRef{Type: "user", ID: "usr_1"},
		ExternalAccountID: "acct_1",
		Status:            core.ConnectionStatusActive,
	}); err == nil {
		t.Fatalf("expected unique active connection constraint violation")
	}

	firstCredential, err := credentialStore.SaveNewVersion(ctx, core.SaveCredentialInput{
		ConnectionID:      connection.ID,
		EncryptedPayload:  []byte("cipher-v1"),
		TokenType:         "bearer",
		RequestedScopes:   []string{"repo:read"},
		GrantedScopes:     []string{"repo:read"},
		Refreshable:       true,
		Status:            core.CredentialStatusActive,
		EncryptionKeyID:   "app-key",
		EncryptionVersion: 1,
	})
	if err != nil {
		t.Fatalf("save first credential: %v", err)
	}
	if firstCredential.Version != 1 {
		t.Fatalf("expected first credential version=1, got %d", firstCredential.Version)
	}
	if firstCredential.PayloadFormat != core.CredentialPayloadFormatLegacyToken {
		t.Fatalf("expected legacy payload format default, got %q", firstCredential.PayloadFormat)
	}
	if firstCredential.PayloadVersion != core.CredentialPayloadVersionV1 {
		t.Fatalf("expected payload version=1, got %d", firstCredential.PayloadVersion)
	}

	secondCredential, err := credentialStore.SaveNewVersion(ctx, core.SaveCredentialInput{
		ConnectionID:      connection.ID,
		EncryptedPayload:  []byte("cipher-v2"),
		PayloadFormat:     core.CredentialPayloadFormatJSONV1,
		PayloadVersion:    core.CredentialPayloadVersionV1,
		TokenType:         "bearer",
		RequestedScopes:   []string{"repo:read"},
		GrantedScopes:     []string{"repo:read"},
		Refreshable:       true,
		Status:            core.CredentialStatusActive,
		EncryptionKeyID:   "app-key",
		EncryptionVersion: 1,
	})
	if err != nil {
		t.Fatalf("save second credential: %v", err)
	}
	if secondCredential.Version != 2 {
		t.Fatalf("expected second credential version=2, got %d", secondCredential.Version)
	}
	if secondCredential.PayloadFormat != core.CredentialPayloadFormatJSONV1 {
		t.Fatalf("expected explicit payload format persisted, got %q", secondCredential.PayloadFormat)
	}

	activeCredential, err := credentialStore.GetActiveByConnection(ctx, connection.ID)
	if err != nil {
		t.Fatalf("get active credential: %v", err)
	}
	if activeCredential.ID != secondCredential.ID {
		t.Fatalf("expected latest credential active; got %q want %q", activeCredential.ID, secondCredential.ID)
	}

	var activeCount int
	if err := client.DB().NewRaw(
		"SELECT COUNT(*) FROM service_credentials WHERE connection_id = ? AND status = ?",
		connection.ID,
		string(core.CredentialStatusActive),
	).Scan(ctx, &activeCount); err != nil {
		t.Fatalf("count active credentials: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active credential, got %d", activeCount)
	}
}

func TestCredentialSaveNewVersion_RollsBackRevocationWhenInsertFails(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	factory, err := sqlstore.NewRepositoryFactoryFromPersistence(client)
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}

	connection, err := factory.ConnectionStore().Create(ctx, core.CreateConnectionInput{
		ProviderID:        "github",
		Scope:             core.ScopeRef{Type: "user", ID: "usr_rollback"},
		ExternalAccountID: "acct_rollback",
		Status:            core.ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	firstCredential, err := factory.CredentialStore().SaveNewVersion(ctx, core.SaveCredentialInput{
		ConnectionID:      connection.ID,
		EncryptedPayload:  []byte("cipher-ok"),
		TokenType:         "bearer",
		RequestedScopes:   []string{"repo:read"},
		GrantedScopes:     []string{"repo:read"},
		Refreshable:       true,
		Status:            core.CredentialStatusActive,
		EncryptionKeyID:   "app-key",
		EncryptionVersion: 1,
	})
	if err != nil {
		t.Fatalf("save first credential: %v", err)
	}

	_, err = factory.CredentialStore().SaveNewVersion(ctx, core.SaveCredentialInput{
		ConnectionID:      connection.ID,
		EncryptedPayload:  nil, // NOT NULL column forces insert failure.
		TokenType:         "bearer",
		RequestedScopes:   []string{"repo:read"},
		GrantedScopes:     []string{"repo:read"},
		Refreshable:       true,
		Status:            core.CredentialStatusActive,
		EncryptionKeyID:   "app-key",
		EncryptionVersion: 1,
	})
	if err == nil {
		t.Fatalf("expected transactional insert failure")
	}

	activeCredential, err := factory.CredentialStore().GetActiveByConnection(ctx, connection.ID)
	if err != nil {
		t.Fatalf("get active credential after rollback: %v", err)
	}
	if activeCredential.ID != firstCredential.ID {
		t.Fatalf("expected original active credential after rollback; got %q want %q", activeCredential.ID, firstCredential.ID)
	}
}

func TestAuditAndGrantStores_RedactSensitiveMetadata(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	factory, err := sqlstore.NewRepositoryFactoryFromPersistence(client)
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}

	connection, err := factory.ConnectionStore().Create(ctx, core.CreateConnectionInput{
		ProviderID:        "github",
		Scope:             core.ScopeRef{Type: "user", ID: "usr_redaction"},
		ExternalAccountID: "acct_redaction",
		Status:            core.ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	eventStore, err := sqlstore.NewServiceEventStore(client.DB())
	if err != nil {
		t.Fatalf("new event store: %v", err)
	}
	if err := eventStore.Append(ctx, sqlstore.AppendServiceEventInput{
		ConnectionID: connection.ID,
		ProviderID:   connection.ProviderID,
		ScopeType:    connection.ScopeType,
		ScopeID:      connection.ScopeID,
		EventType:    "refresh",
		Status:       "ok",
		Metadata: map[string]any{
			"access_token": "plain-token",
			"detail":       "kept",
		},
	}); err != nil {
		t.Fatalf("append service event: %v", err)
	}

	grantStore, err := sqlstore.NewGrantStore(client.DB())
	if err != nil {
		t.Fatalf("new grant store: %v", err)
	}
	if err := grantStore.SaveSnapshot(ctx, core.SaveGrantSnapshotInput{
		ConnectionID: connection.ID,
		Version:      1,
		Requested:    []string{"repo:read"},
		Granted:      []string{"repo:read"},
		CapturedAt:   time.Now().UTC(),
		Metadata: map[string]any{
			"refresh_token": "plain-refresh",
			"source":        "integration-test",
		},
	}); err != nil {
		t.Fatalf("save grant snapshot: %v", err)
	}

	if err := grantStore.AppendEvent(ctx, core.AppendGrantEventInput{
		ConnectionID: connection.ID,
		EventType:    "expanded",
		Added:        []string{"repo:write"},
		Removed:      []string{},
		OccurredAt:   time.Now().UTC(),
		Metadata: map[string]any{
			"authorization": "Bearer 123",
			"state":         "ok",
		},
	}); err != nil {
		t.Fatalf("append grant event: %v", err)
	}

	var eventMetadata string
	if err := client.DB().NewRaw(
		"SELECT metadata FROM service_events LIMIT 1",
	).Scan(ctx, &eventMetadata); err != nil {
		t.Fatalf("load event metadata: %v", err)
	}
	if strings.Contains(eventMetadata, "plain-token") {
		t.Fatalf("expected redacted event metadata")
	}
	if !strings.Contains(eventMetadata, "[REDACTED]") {
		t.Fatalf("expected redaction marker in event metadata")
	}

	var grantMetadata string
	if err := client.DB().NewRaw(
		"SELECT metadata FROM service_grant_events WHERE event_type = ? ORDER BY created_at DESC LIMIT 1",
		"expanded",
	).Scan(ctx, &grantMetadata); err != nil {
		t.Fatalf("load grant metadata: %v", err)
	}
	if strings.Contains(grantMetadata, "Bearer 123") {
		t.Fatalf("expected redacted grant metadata")
	}
	if !strings.Contains(grantMetadata, "[REDACTED]") {
		t.Fatalf("expected redaction marker in grant metadata")
	}

	var snapshotMetadata string
	if err := client.DB().NewRaw(
		"SELECT metadata FROM service_grant_snapshots WHERE connection_id = ? ORDER BY created_at DESC LIMIT 1",
		connection.ID,
	).Scan(ctx, &snapshotMetadata); err != nil {
		t.Fatalf("load grant snapshot metadata: %v", err)
	}
	if strings.Contains(snapshotMetadata, "plain-refresh") {
		t.Fatalf("expected redacted grant snapshot metadata")
	}
	if !strings.Contains(snapshotMetadata, "[REDACTED]") {
		t.Fatalf("expected redaction marker in grant snapshot metadata")
	}
}

func TestGrantStore_GetLatestSnapshotNotFound(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	grantStore, err := sqlstore.NewGrantStore(client.DB())
	if err != nil {
		t.Fatalf("new grant store: %v", err)
	}

	snapshot, found, err := grantStore.GetLatestSnapshot(ctx, "missing-connection")
	if err != nil {
		t.Fatalf("get latest snapshot: %v", err)
	}
	if found {
		t.Fatalf("expected no snapshot for missing connection, got %+v", snapshot)
	}
}

func TestGrantStore_SaveSnapshotAndEvent_RollsBackOnEventValidationFailure(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	repoFactory, err := sqlstore.NewRepositoryFactoryFromPersistence(client)
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}
	connection, err := repoFactory.ConnectionStore().Create(ctx, core.CreateConnectionInput{
		ProviderID:        "github",
		Scope:             core.ScopeRef{Type: "user", ID: "usr_tx"},
		ExternalAccountID: "acct_tx",
		Status:            core.ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	grantStore, err := sqlstore.NewGrantStore(client.DB())
	if err != nil {
		t.Fatalf("new grant store: %v", err)
	}

	err = grantStore.SaveSnapshotAndEvent(ctx, core.SaveGrantSnapshotInput{
		ConnectionID: connection.ID,
		Version:      1,
		Requested:    []string{"repo:read"},
		Granted:      []string{"repo:read"},
		CapturedAt:   time.Now().UTC(),
		Metadata:     map[string]any{"source": "test"},
	}, &core.AppendGrantEventInput{
		ConnectionID: connection.ID,
		EventType:    "",
		Added:        []string{"repo:read"},
		OccurredAt:   time.Now().UTC(),
		Metadata:     map[string]any{"source": "test"},
	})
	if err == nil {
		t.Fatalf("expected transactional save to fail when event type is invalid")
	}

	var snapshotCount int
	if err := client.DB().NewRaw(
		"SELECT COUNT(*) FROM service_grant_snapshots WHERE connection_id = ?",
		connection.ID,
	).Scan(ctx, &snapshotCount); err != nil {
		t.Fatalf("count snapshots: %v", err)
	}
	if snapshotCount != 0 {
		t.Fatalf("expected snapshot insert to roll back, found %d", snapshotCount)
	}

	var eventCount int
	if err := client.DB().NewRaw(
		"SELECT COUNT(*) FROM service_grant_events WHERE connection_id = ?",
		connection.ID,
	).Scan(ctx, &eventCount); err != nil {
		t.Fatalf("count grant events: %v", err)
	}
	if eventCount != 0 {
		t.Fatalf("expected no grant event rows after rollback, found %d", eventCount)
	}
}

func TestNewService_WiresStoresFromPersistenceAndRepositoryFactory(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	repoFactory := sqlstore.NewRepositoryFactory()
	svc, err := core.NewService(core.Config{ServiceName: "services"},
		core.WithPersistenceClient(client),
		core.WithRepositoryFactory(repoFactory),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	deps := svc.Dependencies()
	if deps.PersistenceClient != client {
		t.Fatalf("expected persistence client override")
	}
	if deps.RepositoryFactory != repoFactory {
		t.Fatalf("expected repository factory override")
	}
	if deps.ConnectionStore == nil {
		t.Fatalf("expected connection store from repository factory build")
	}
	if deps.CredentialStore == nil {
		t.Fatalf("expected credential store from repository factory build")
	}

	customConn := &stubConnectionStore{}
	customCred := &stubCredentialStore{}
	svc, err = core.NewService(core.Config{ServiceName: "services"},
		core.WithPersistenceClient(client),
		core.WithRepositoryFactory(repoFactory),
		core.WithConnectionStore(customConn),
		core.WithCredentialStore(customCred),
	)
	if err != nil {
		t.Fatalf("new service with explicit stores: %v", err)
	}
	deps = svc.Dependencies()
	if deps.ConnectionStore != customConn {
		t.Fatalf("expected explicit connection store override precedence")
	}
	if deps.CredentialStore != customCred {
		t.Fatalf("expected explicit credential store override precedence")
	}
	_ = ctx
}

func TestWebhookDeliveryStore_ClaimLifecycle(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	deliveryStore, err := sqlstore.NewWebhookDeliveryStore(client.DB())
	if err != nil {
		t.Fatalf("new webhook delivery store: %v", err)
	}

	record, claimed, err := deliveryStore.Claim(
		ctx,
		"github",
		"delivery-1",
		[]byte(`{"ok":true}`),
		time.Minute,
	)
	if err != nil {
		t.Fatalf("claim initial delivery: %v", err)
	}
	if !claimed {
		t.Fatalf("expected initial delivery to be claimable")
	}
	if record.Status != "processing" {
		t.Fatalf("expected processing status after claim, got %q", record.Status)
	}
	if record.ClaimID == "" {
		t.Fatalf("expected claim id on claimed delivery")
	}
	firstClaimID := record.ClaimID

	record, claimed, err = deliveryStore.Claim(
		ctx,
		"github",
		"delivery-1",
		[]byte(`{"ok":true}`),
		time.Minute,
	)
	if err != nil {
		t.Fatalf("claim duplicate in-flight delivery: %v", err)
	}
	if claimed {
		t.Fatalf("expected duplicate in-flight delivery to not be claimable")
	}
	if record.Attempts != 1 {
		t.Fatalf("expected attempts to remain 1 while in-flight, got %d", record.Attempts)
	}
	if record.Status != "processing" {
		t.Fatalf("expected in-flight status processing, got %q", record.Status)
	}

	nextAttempt := time.Now().UTC().Add(2 * time.Minute)
	if err := deliveryStore.Fail(ctx, firstClaimID, fmt.Errorf("transient"), nextAttempt, 3); err != nil {
		t.Fatalf("fail claimed delivery: %v", err)
	}

	retried, err := deliveryStore.Get(ctx, "github", "delivery-1")
	if err != nil {
		t.Fatalf("get retried delivery: %v", err)
	}
	if retried.Status != "retry_ready" {
		t.Fatalf("expected retry_ready status, got %q", retried.Status)
	}
	if retried.Attempts != 1 {
		t.Fatalf("expected attempts to remain 1 after first failure, got %d", retried.Attempts)
	}
	if retried.NextAttemptAt == nil {
		t.Fatalf("expected next attempt timestamp to be set")
	}

	record, claimed, err = deliveryStore.Claim(
		ctx,
		"github",
		"delivery-1",
		[]byte(`{"ok":true}`),
		time.Minute,
	)
	if err != nil {
		t.Fatalf("claim before retry window should not error: %v", err)
	}
	if claimed {
		t.Fatalf("expected retry-ready delivery to remain unavailable until retry window")
	}

	if err := deliveryStore.Fail(ctx, retried.ProviderID+":delivery-1:invalid", fmt.Errorf("bad"), time.Now().UTC(), 3); err == nil {
		t.Fatalf("expected invalid claim id to fail")
	}
	if _, err := client.DB().NewRaw(
		"UPDATE service_webhook_deliveries SET next_attempt_at = ? WHERE provider_id = ? AND delivery_id = ?",
		time.Now().UTC().Add(-time.Second),
		"github",
		"delivery-1",
	).Exec(ctx); err != nil {
		t.Fatalf("set retry-ready window to elapsed: %v", err)
	}

	record, claimed, err = deliveryStore.Claim(
		ctx,
		"github",
		"delivery-1",
		[]byte(`{"ok":true}`),
		time.Minute,
	)
	if err != nil {
		t.Fatalf("claim with expired lease window: %v", err)
	}
	if !claimed {
		t.Fatalf("expected retry-ready delivery to be claimable after retry window")
	}
	if record.Attempts != 2 {
		t.Fatalf("expected attempts to increment on re-claim, got %d", record.Attempts)
	}
	if err := deliveryStore.Complete(ctx, record.ClaimID); err != nil {
		t.Fatalf("complete delivery: %v", err)
	}

	processed, err := deliveryStore.Get(ctx, "github", "delivery-1")
	if err != nil {
		t.Fatalf("get processed delivery: %v", err)
	}
	if processed.Status != "processed" {
		t.Fatalf("expected processed status, got %q", processed.Status)
	}
	if processed.NextAttemptAt != nil {
		t.Fatalf("expected next attempt timestamp to be cleared")
	}
}

func TestWebhookDeliveryStore_MarksDeadAfterMaxAttempts(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	deliveryStore, err := sqlstore.NewWebhookDeliveryStore(client.DB())
	if err != nil {
		t.Fatalf("new webhook delivery store: %v", err)
	}

	first, claimed, err := deliveryStore.Claim(ctx, "github", "delivery-dead", []byte(`{"ok":true}`), time.Minute)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if !claimed {
		t.Fatalf("expected first claim to succeed")
	}
	if err := deliveryStore.Fail(ctx, first.ClaimID, fmt.Errorf("temporary"), time.Now().UTC().Add(-time.Second), 2); err != nil {
		t.Fatalf("first fail: %v", err)
	}

	second, claimed, err := deliveryStore.Claim(ctx, "github", "delivery-dead", []byte(`{"ok":true}`), time.Minute)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if !claimed {
		t.Fatalf("expected second claim to succeed")
	}
	if err := deliveryStore.Fail(ctx, second.ClaimID, fmt.Errorf("terminal"), time.Now().UTC(), 2); err != nil {
		t.Fatalf("second fail: %v", err)
	}

	dead, err := deliveryStore.Get(ctx, "github", "delivery-dead")
	if err != nil {
		t.Fatalf("get dead delivery: %v", err)
	}
	if dead.Status != "dead" {
		t.Fatalf("expected dead delivery status, got %q", dead.Status)
	}
	if dead.Attempts != 2 {
		t.Fatalf("expected attempts=2 at dead state, got %d", dead.Attempts)
	}

	_, claimed, err = deliveryStore.Claim(ctx, "github", "delivery-dead", []byte(`{"ok":true}`), time.Minute)
	if err != nil {
		t.Fatalf("claim dead delivery: %v", err)
	}
	if claimed {
		t.Fatalf("expected dead delivery to remain non-claimable")
	}
}

func TestWebhookDeliveryStore_ReclaimsExpiredProcessingLease(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	deliveryStore, err := sqlstore.NewWebhookDeliveryStore(client.DB())
	if err != nil {
		t.Fatalf("new webhook delivery store: %v", err)
	}

	first, claimed, err := deliveryStore.Claim(ctx, "github", "delivery-lease", []byte(`{"ok":true}`), 5*time.Millisecond)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if !claimed {
		t.Fatalf("expected first claim to succeed")
	}

	_, claimed, err = deliveryStore.Claim(ctx, "github", "delivery-lease", []byte(`{"ok":true}`), time.Minute)
	if err != nil {
		t.Fatalf("claim during active lease: %v", err)
	}
	if claimed {
		t.Fatalf("expected active processing lease to block re-claim")
	}

	time.Sleep(20 * time.Millisecond)
	second, claimed, err := deliveryStore.Claim(ctx, "github", "delivery-lease", []byte(`{"ok":true}`), time.Minute)
	if err != nil {
		t.Fatalf("claim after lease expiry: %v", err)
	}
	if !claimed {
		t.Fatalf("expected lease-expired processing delivery to be re-claimable")
	}
	if second.Attempts != first.Attempts+1 {
		t.Fatalf("expected attempts to increment on lease-recovery claim, got %d", second.Attempts)
	}
}

func TestOutboxStore_ClaimAckRetryLifecycle(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	outboxStore, err := sqlstore.NewOutboxStore(client.DB())
	if err != nil {
		t.Fatalf("new outbox store: %v", err)
	}

	event := core.LifecycleEvent{
		ID:         "evt_outbox_1",
		Name:       "connection.refresh_failed",
		ProviderID: "github",
		ScopeType:  "user",
		ScopeID:    "usr_outbox",
		Source:     "worker",
		OccurredAt: time.Now().UTC().Add(-2 * time.Minute),
		Payload:    map[string]any{"status": "warn"},
		Metadata:   map[string]any{"request_id": "req_1"},
	}
	if err := outboxStore.Enqueue(ctx, event); err != nil {
		t.Fatalf("enqueue event: %v", err)
	}

	claimed, err := outboxStore.ClaimBatch(ctx, 10)
	if err != nil {
		t.Fatalf("claim batch: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("expected 1 claimed event, got %d", len(claimed))
	}
	if claimed[0].ID != event.ID {
		t.Fatalf("expected event id %q, got %q", event.ID, claimed[0].ID)
	}
	if claimed[0].Metadata[core.MetadataKeyOutboxAttempts] != 0 {
		t.Fatalf("expected initial attempts metadata to be 0")
	}

	if err := outboxStore.Retry(ctx, event.ID, errors.New("transient"), time.Now().UTC().Add(-time.Second)); err != nil {
		t.Fatalf("retry event: %v", err)
	}

	reclaimed, err := outboxStore.ClaimBatch(ctx, 10)
	if err != nil {
		t.Fatalf("re-claim batch: %v", err)
	}
	if len(reclaimed) != 1 {
		t.Fatalf("expected 1 re-claimed event, got %d", len(reclaimed))
	}
	if reclaimed[0].Metadata[core.MetadataKeyOutboxAttempts] != 1 {
		t.Fatalf("expected attempts metadata=1 after retry")
	}

	if err := outboxStore.Ack(ctx, event.ID); err != nil {
		t.Fatalf("ack event: %v", err)
	}
	claimedAfterAck, err := outboxStore.ClaimBatch(ctx, 10)
	if err != nil {
		t.Fatalf("claim after ack: %v", err)
	}
	if len(claimedAfterAck) != 0 {
		t.Fatalf("expected no claimed events after ack, got %d", len(claimedAfterAck))
	}

	failedEvent := core.LifecycleEvent{
		ID:         "evt_outbox_2",
		Name:       "sync.failed",
		ProviderID: "github",
		ScopeType:  "user",
		ScopeID:    "usr_outbox",
		Source:     "worker",
		OccurredAt: time.Now().UTC(),
	}
	if err := outboxStore.Enqueue(ctx, failedEvent); err != nil {
		t.Fatalf("enqueue failed event: %v", err)
	}
	if _, err := outboxStore.ClaimBatch(ctx, 10); err != nil {
		t.Fatalf("claim failed event: %v", err)
	}
	if err := outboxStore.Retry(ctx, failedEvent.ID, errors.New("terminal"), time.Time{}); err != nil {
		t.Fatalf("mark failed event: %v", err)
	}

	var status string
	var attempts int
	if err := client.DB().NewRaw(
		"SELECT status, attempts FROM service_lifecycle_outbox WHERE event_id = ?",
		failedEvent.ID,
	).Scan(ctx, &status, &attempts); err != nil {
		t.Fatalf("load failed event row: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected failed status, got %q", status)
	}
	if attempts != 1 {
		t.Fatalf("expected failed event attempts=1, got %d", attempts)
	}
}

func TestNotificationDispatchStore_IdempotencyLedger(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	ledger, err := sqlstore.NewNotificationDispatchStore(client.DB())
	if err != nil {
		t.Fatalf("new notification dispatch store: %v", err)
	}

	record := core.NotificationDispatchRecord{
		EventID:        "evt_notify_1",
		Projector:      "go-notifications",
		DefinitionCode: "services.connection.failed",
		RecipientKey:   "user:usr_1",
		IdempotencyKey: "dispatch-key-1",
		Status:         "sent",
		Metadata:       map[string]any{"channel": "email"},
	}
	if err := ledger.Record(ctx, record); err != nil {
		t.Fatalf("record dispatch: %v", err)
	}

	seen, err := ledger.Seen(ctx, record.IdempotencyKey)
	if err != nil {
		t.Fatalf("seen: %v", err)
	}
	if !seen {
		t.Fatalf("expected idempotency key to be tracked")
	}

	// Duplicate idempotency keys should be treated as idempotent no-op.
	if err := ledger.Record(ctx, record); err != nil {
		t.Fatalf("record duplicate dispatch: %v", err)
	}

	var count int
	if err := client.DB().NewRaw(
		"SELECT COUNT(*) FROM service_notification_dispatches WHERE idempotency_key = ?",
		record.IdempotencyKey,
	).Scan(ctx, &count); err != nil {
		t.Fatalf("count dispatch rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one dispatch row for idempotency key, got %d", count)
	}
}

func TestActivityStore_OperationalRetentionAndQuery(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	activityStore, err := sqlstore.NewActivityStore(client.DB())
	if err != nil {
		t.Fatalf("new activity store: %v", err)
	}
	repoFactory, err := sqlstore.NewRepositoryFactoryFromPersistence(client)
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}
	filterConnection, err := repoFactory.ConnectionStore().Create(ctx, core.CreateConnectionInput{
		ProviderID:        "github",
		Scope:             core.ScopeRef{Type: "user", ID: "usr_activity"},
		ExternalAccountID: "acct_activity",
		Status:            core.ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create filter connection: %v", err)
	}

	baseMeta := map[string]any{
		"provider_id": "github",
		"scope_type":  "user",
		"scope_id":    "usr_activity",
	}
	oldCreatedAt := time.Now().UTC().Add(-48 * time.Hour)
	for i := 0; i < 3; i++ {
		entry := core.ServiceActivityEntry{
			ID:        fmt.Sprintf("act_old_%d", i),
			Actor:     "system",
			Action:    "connection.refresh",
			Object:    fmt.Sprintf("connection:conn_%d", i),
			Channel:   "services.lifecycle",
			Status:    core.ServiceActivityStatusOK,
			Metadata:  baseMeta,
			CreatedAt: oldCreatedAt.Add(time.Duration(i) * time.Minute),
		}
		if err := activityStore.Record(ctx, entry); err != nil {
			t.Fatalf("record old entry %d: %v", i, err)
		}
	}
	for i := 0; i < 3; i++ {
		entry := core.ServiceActivityEntry{
			ID:      fmt.Sprintf("act_new_%d", i),
			Actor:   "webhook",
			Action:  "webhook.received",
			Object:  fmt.Sprintf("subscription:sub_%d", i),
			Channel: "services.lifecycle",
			Status:  core.ServiceActivityStatusWarn,
			Metadata: map[string]any{
				"provider_id": "github",
				"scope_type":  "user",
				"scope_id":    "usr_activity",
			},
			CreatedAt: time.Now().UTC().Add(time.Duration(i) * time.Minute),
		}
		if i == 1 {
			entry.Metadata["connection_id"] = filterConnection.ID
		}
		if err := activityStore.Record(ctx, entry); err != nil {
			t.Fatalf("record new entry %d: %v", i, err)
		}
	}

	page, err := activityStore.List(ctx, core.ServicesActivityFilter{
		ProviderID: "github",
		ScopeType:  "user",
		ScopeID:    "usr_activity",
		Page:       1,
		PerPage:    10,
	})
	if err != nil {
		t.Fatalf("list activity entries: %v", err)
	}
	if page.Total != 6 {
		t.Fatalf("expected total=6 before prune, got %d", page.Total)
	}

	connectionPage, err := activityStore.List(ctx, core.ServicesActivityFilter{
		ProviderID:  "github",
		ScopeType:   "user",
		ScopeID:     "usr_activity",
		Connections: []string{filterConnection.ID},
		Page:        1,
		PerPage:     10,
	})
	if err != nil {
		t.Fatalf("list activity entries by connection: %v", err)
	}
	if connectionPage.Total != 1 {
		t.Fatalf("expected total=1 when filtering by connection, got %d", connectionPage.Total)
	}

	deleted, err := activityStore.Prune(ctx, core.ActivityRetentionPolicy{
		TTL:    24 * time.Hour,
		RowCap: 2,
	})
	if err != nil {
		t.Fatalf("prune activity entries: %v", err)
	}
	if deleted < 4 {
		t.Fatalf("expected at least four deleted entries, got %d", deleted)
	}

	page, err = activityStore.List(ctx, core.ServicesActivityFilter{
		ProviderID: "github",
		ScopeType:  "user",
		ScopeID:    "usr_activity",
		Page:       1,
		PerPage:    10,
	})
	if err != nil {
		t.Fatalf("list after prune: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("expected total=2 after prune row cap, got %d", page.Total)
	}
}

func TestSyncCursorStore_AdvanceAtomicCompareAndSwap(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	cursorStore, err := sqlstore.NewSyncCursorStore(client.DB())
	if err != nil {
		t.Fatalf("new sync cursor store: %v", err)
	}
	repoFactory, err := sqlstore.NewRepositoryFactoryFromPersistence(client)
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}
	connection, err := repoFactory.ConnectionStore().Create(ctx, core.CreateConnectionInput{
		ProviderID:        "github",
		Scope:             core.ScopeRef{Type: "user", ID: "usr_cursor"},
		ExternalAccountID: "acct_cursor",
		Status:            core.ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	seeded, err := cursorStore.Upsert(ctx, core.UpsertSyncCursorInput{
		ConnectionID: connection.ID,
		ProviderID:   "github",
		ResourceType: "drive.file",
		ResourceID:   "file_1",
		Cursor:       "c1",
		Status:       "active",
	})
	if err != nil {
		t.Fatalf("seed cursor: %v", err)
	}
	if seeded.Cursor != "c1" {
		t.Fatalf("expected seeded cursor c1")
	}

	syncedAt := time.Now().UTC()
	advanced, err := cursorStore.Advance(ctx, core.AdvanceSyncCursorInput{
		ConnectionID:   connection.ID,
		ProviderID:     "github",
		ResourceType:   "drive.file",
		ResourceID:     "file_1",
		ExpectedCursor: "c1",
		Cursor:         "c2",
		LastSyncedAt:   &syncedAt,
		Status:         "active",
	})
	if err != nil {
		t.Fatalf("advance cursor: %v", err)
	}
	if advanced.Cursor != "c2" {
		t.Fatalf("expected cursor to advance to c2, got %q", advanced.Cursor)
	}

	_, err = cursorStore.Advance(ctx, core.AdvanceSyncCursorInput{
		ConnectionID:   connection.ID,
		ProviderID:     "github",
		ResourceType:   "drive.file",
		ResourceID:     "file_1",
		ExpectedCursor: "stale",
		Cursor:         "c3",
		Status:         "active",
	})
	if !errors.Is(err, core.ErrSyncCursorConflict) {
		t.Fatalf("expected sync cursor conflict, got %v", err)
	}

	current, err := cursorStore.Get(ctx, connection.ID, "drive.file", "file_1")
	if err != nil {
		t.Fatalf("get current cursor: %v", err)
	}
	if current.Cursor != "c2" {
		t.Fatalf("expected cursor to remain c2 after conflict, got %q", current.Cursor)
	}
}

func TestSyncOrchestrator_PersistsCheckpointAndResume(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	repoFactory, err := sqlstore.NewRepositoryFactoryFromPersistence(client)
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}
	connection, err := repoFactory.ConnectionStore().Create(ctx, core.CreateConnectionInput{
		ProviderID:        "github",
		Scope:             core.ScopeRef{Type: "user", ID: "usr_sync"},
		ExternalAccountID: "acct_sync",
		Status:            core.ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	cursorStore, err := sqlstore.NewSyncCursorStore(client.DB())
	if err != nil {
		t.Fatalf("new sync cursor store: %v", err)
	}
	_, err = cursorStore.Upsert(ctx, core.UpsertSyncCursorInput{
		ConnectionID: connection.ID,
		ProviderID:   "github",
		ResourceType: "drive.file",
		ResourceID:   "file_sync",
		Cursor:       "checkpoint_seed",
		Status:       "active",
	})
	if err != nil {
		t.Fatalf("seed cursor: %v", err)
	}

	syncJobStore, err := sqlstore.NewSyncJobStore(client.DB())
	if err != nil {
		t.Fatalf("new sync job store: %v", err)
	}
	orchestrator := servicesync.NewOrchestrator(syncJobStore, cursorStore)

	job, err := orchestrator.StartBootstrap(ctx, core.BootstrapRequest{
		ConnectionID: connection.ID,
		ProviderID:   "github",
		ResourceType: "drive.file",
		ResourceID:   "file_sync",
	})
	if err != nil {
		t.Fatalf("start bootstrap: %v", err)
	}
	if job.Checkpoint != "checkpoint_seed" {
		t.Fatalf("expected checkpoint from persisted cursor")
	}

	job, err = orchestrator.SaveCheckpoint(ctx, job.ID, "checkpoint_next", map[string]any{"page": 2})
	if err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	if job.Checkpoint != "checkpoint_next" {
		t.Fatalf("expected checkpoint update to persist")
	}

	nextAttempt := time.Now().UTC().Add(5 * time.Minute)
	job, err = orchestrator.Fail(ctx, job.ID, fmt.Errorf("temporary"), &nextAttempt)
	if err != nil {
		t.Fatalf("fail job: %v", err)
	}
	if job.Status != core.SyncJobStatusFailed {
		t.Fatalf("expected failed job status")
	}

	if err := orchestrator.Resume(ctx, job.ID); err != nil {
		t.Fatalf("resume job: %v", err)
	}
	stored, err := syncJobStore.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get resumed job: %v", err)
	}
	if stored.Status != core.SyncJobStatusQueued {
		t.Fatalf("expected queued status after resume, got %q", stored.Status)
	}
	if stored.Checkpoint != "checkpoint_next" {
		t.Fatalf("expected checkpoint durability across resume")
	}
}

func TestInstallationStore_UpsertListAndStatusTransitions(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	repoFactory, err := sqlstore.NewRepositoryFactoryFromPersistence(client)
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}
	installationStore := repoFactory.InstallationStore()
	if installationStore == nil {
		t.Fatalf("expected installation store from repository factory")
	}

	now := time.Now().UTC().Truncate(time.Second)
	installation, err := installationStore.Upsert(ctx, core.UpsertInstallationInput{
		ProviderID:  "github",
		Scope:       core.ScopeRef{Type: "org", ID: "org_install_1"},
		InstallType: "marketplace_app",
		Status:      core.InstallationStatusActive,
		GrantedAt:   &now,
		Metadata:    map[string]any{"installer": "admin_1"},
	})
	if err != nil {
		t.Fatalf("upsert installation: %v", err)
	}
	if installation.ID == "" {
		t.Fatalf("expected persisted installation id")
	}
	if installation.Status != core.InstallationStatusActive {
		t.Fatalf("expected active status, got %q", installation.Status)
	}

	stored, err := installationStore.Get(ctx, installation.ID)
	if err != nil {
		t.Fatalf("get installation: %v", err)
	}
	if stored.InstallType != "marketplace_app" {
		t.Fatalf("expected install type marketplace_app, got %q", stored.InstallType)
	}
	if stored.Metadata["installer"] != "admin_1" {
		t.Fatalf("expected installer metadata")
	}

	listed, err := installationStore.ListByScope(ctx, "github", core.ScopeRef{Type: "org", ID: "org_install_1"})
	if err != nil {
		t.Fatalf("list installations by scope: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected exactly one installation, got %d", len(listed))
	}

	if err := installationStore.UpdateStatus(ctx, installation.ID, string(core.InstallationStatusSuspended), "quota exhausted"); err != nil {
		t.Fatalf("update installation status suspended: %v", err)
	}
	suspended, err := installationStore.Get(ctx, installation.ID)
	if err != nil {
		t.Fatalf("get suspended installation: %v", err)
	}
	if suspended.Status != core.InstallationStatusSuspended {
		t.Fatalf("expected suspended status, got %q", suspended.Status)
	}
	if suspended.Metadata["status_reason"] != "quota exhausted" {
		t.Fatalf("expected status reason metadata")
	}

	updated, err := installationStore.Upsert(ctx, core.UpsertInstallationInput{
		ProviderID:  "github",
		Scope:       core.ScopeRef{Type: "org", ID: "org_install_1"},
		InstallType: "marketplace_app",
		Status:      core.InstallationStatusActive,
		Metadata:    map[string]any{"installer": "admin_2"},
	})
	if err != nil {
		t.Fatalf("upsert installation second pass: %v", err)
	}
	if updated.ID != installation.ID {
		t.Fatalf("expected upsert to update existing installation id, got %q want %q", updated.ID, installation.ID)
	}
	if updated.Metadata["installer"] != "admin_2" {
		t.Fatalf("expected metadata refresh on upsert")
	}
}

func TestRateLimitStateStore_PersistsAndSupportsPolicyFlow(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	repoFactory, err := sqlstore.NewRepositoryFactoryFromPersistence(client)
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}
	store := repoFactory.RateLimitStateStore()
	if store == nil {
		t.Fatalf("expected rate-limit state store from repository factory")
	}

	key := core.RateLimitKey{
		ProviderID: "github",
		ScopeType:  "org",
		ScopeID:    "org_rl_1",
		BucketKey:  "api",
	}
	if _, err := store.Get(ctx, key); !errors.Is(err, servicesratelimit.ErrStateNotFound) {
		t.Fatalf("expected state not found error, got %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	retryAfter := 15 * time.Second
	throttledUntil := now.Add(retryAfter)
	if err := store.Upsert(ctx, servicesratelimit.State{
		Key:            key,
		Limit:          5000,
		Remaining:      0,
		ResetAt:        ptrTime(now.Add(30 * time.Second)),
		RetryAfter:     &retryAfter,
		ThrottledUntil: &throttledUntil,
		LastStatus:     429,
		Attempts:       2,
		UpdatedAt:      now,
		Metadata:       map[string]any{"endpoint": "issues"},
	}); err != nil {
		t.Fatalf("upsert rate-limit state: %v", err)
	}

	stored, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("get rate-limit state: %v", err)
	}
	if stored.Limit != 5000 || stored.Remaining != 0 {
		t.Fatalf("unexpected persisted quota numbers: %#v", stored)
	}
	if stored.Attempts != 2 || stored.LastStatus != 429 {
		t.Fatalf("expected attempts/last status persistence, got %#v", stored)
	}
	if stored.RetryAfter == nil || *stored.RetryAfter != retryAfter {
		t.Fatalf("expected retry_after=%s, got %+v", retryAfter, stored.RetryAfter)
	}
	if stored.Metadata["endpoint"] != "issues" {
		t.Fatalf("expected metadata endpoint")
	}

	policy := servicesratelimit.NewAdaptivePolicy(store)
	policy.Now = func() time.Time { return now }
	beforeErr := policy.BeforeCall(ctx, key)
	var throttledErr servicesratelimit.ThrottledError
	if !errors.As(beforeErr, &throttledErr) {
		t.Fatalf("expected throttled error from persisted state, got %v", beforeErr)
	}

	if err := policy.AfterCall(ctx, key, core.ProviderResponseMeta{
		StatusCode: 200,
		Headers: map[string]string{
			"X-RateLimit-Limit":     "5000",
			"X-RateLimit-Remaining": "4999",
		},
	}); err != nil {
		t.Fatalf("policy after-call success transition: %v", err)
	}
	updated, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("get updated rate-limit state: %v", err)
	}
	if updated.Attempts != 0 || updated.ThrottledUntil != nil {
		t.Fatalf("expected throttle state reset after success, got %#v", updated)
	}
}

func TestSubscriptionLifecycle_RenewAndCancel_Integration(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	repoFactory, err := sqlstore.NewRepositoryFactoryFromPersistence(client)
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}
	subscriptionStore, err := sqlstore.NewSubscriptionStore(client.DB())
	if err != nil {
		t.Fatalf("new subscription store: %v", err)
	}

	provider := &integrationProvider{
		id: "github",
		subscribeResponse: core.SubscriptionResult{
			ChannelID:            "channel_1",
			RemoteSubscriptionID: "remote_1",
			ExpiresAt:            ptrTime(time.Now().UTC().Add(30 * time.Minute)),
			Metadata:             map[string]any{"lease": "initial"},
		},
		renewSubscriptionResponse: core.SubscriptionResult{
			ChannelID:            "channel_1",
			RemoteSubscriptionID: "remote_2",
			ExpiresAt:            ptrTime(time.Now().UTC().Add(60 * time.Minute)),
			Metadata:             map[string]any{"lease": "renewed"},
		},
	}
	registry := core.NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	svc, err := core.NewService(core.Config{},
		core.WithRegistry(registry),
		core.WithConnectionStore(repoFactory.ConnectionStore()),
		core.WithSubscriptionStore(subscriptionStore),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	connection, err := repoFactory.ConnectionStore().Create(ctx, core.CreateConnectionInput{
		ProviderID:        "github",
		Scope:             core.ScopeRef{Type: "user", ID: "usr_sub"},
		ExternalAccountID: "acct_sub",
		Status:            core.ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	subscription, err := svc.Subscribe(ctx, core.SubscribeRequest{
		ConnectionID: connection.ID,
		ResourceType: "drive.file",
		ResourceID:   "file_sub",
		CallbackURL:  "https://app.example/webhooks/github",
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if subscription.RemoteSubscriptionID != "remote_1" {
		t.Fatalf("expected remote_1 subscription id")
	}

	renewed, err := svc.RenewSubscription(ctx, core.RenewSubscriptionRequest{
		SubscriptionID: subscription.ID,
	})
	if err != nil {
		t.Fatalf("renew subscription: %v", err)
	}
	if renewed.RemoteSubscriptionID != "remote_2" {
		t.Fatalf("expected remote_2 subscription id")
	}

	if err := svc.CancelSubscription(ctx, core.CancelSubscriptionRequest{
		SubscriptionID: renewed.ID,
		Reason:         "manual revoke",
	}); err != nil {
		t.Fatalf("cancel subscription: %v", err)
	}
	stored, err := subscriptionStore.Get(ctx, renewed.ID)
	if err != nil {
		t.Fatalf("load cancelled subscription: %v", err)
	}
	if stored.Status != core.SubscriptionStatusCancelled {
		t.Fatalf("expected cancelled subscription status, got %q", stored.Status)
	}
}

func TestWebhookTriggeredSync_DedupeAndCursorAdvance_Integration(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	repoFactory, err := sqlstore.NewRepositoryFactoryFromPersistence(client)
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}
	connection, err := repoFactory.ConnectionStore().Create(ctx, core.CreateConnectionInput{
		ProviderID:        "github",
		Scope:             core.ScopeRef{Type: "user", ID: "usr_webhook"},
		ExternalAccountID: "acct_webhook",
		Status:            core.ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	subscriptionStore, err := sqlstore.NewSubscriptionStore(client.DB())
	if err != nil {
		t.Fatalf("new subscription store: %v", err)
	}
	_, err = subscriptionStore.Upsert(ctx, core.UpsertSubscriptionInput{
		ConnectionID: connection.ID,
		ProviderID:   "github",
		ResourceType: "drive.file",
		ResourceID:   "file_webhook",
		ChannelID:    "channel_webhook",
		CallbackURL:  "https://app.example/webhooks/github",
		Status:       core.SubscriptionStatusActive,
	})
	if err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	cursorStore, err := sqlstore.NewSyncCursorStore(client.DB())
	if err != nil {
		t.Fatalf("new sync cursor store: %v", err)
	}
	_, err = cursorStore.Upsert(ctx, core.UpsertSyncCursorInput{
		ConnectionID: connection.ID,
		ProviderID:   "github",
		ResourceType: "drive.file",
		ResourceID:   "file_webhook",
		Cursor:       "cursor_1",
		Status:       "active",
	})
	if err != nil {
		t.Fatalf("seed cursor: %v", err)
	}

	syncJobStore, err := sqlstore.NewSyncJobStore(client.DB())
	if err != nil {
		t.Fatalf("new sync job store: %v", err)
	}
	orchestrator := servicesync.NewOrchestrator(syncJobStore, cursorStore)

	deliveryStore, err := sqlstore.NewWebhookDeliveryStore(client.DB())
	if err != nil {
		t.Fatalf("new webhook delivery store: %v", err)
	}
	handler := webhookSyncHandler{
		subscriptions: subscriptionStore,
		cursors:       cursorStore,
		orchestrator:  orchestrator,
	}
	processor := serviceswebhooks.NewProcessor(nil, deliveryStore, handler)

	req := core.InboundRequest{
		ProviderID: "github",
		Headers: map[string]string{
			"X-Channel-ID": "channel_webhook",
		},
		Metadata: map[string]any{
			"delivery_id": "delivery_sync_1",
			"next_cursor": "cursor_2",
		},
	}
	result, err := processor.Process(ctx, req)
	if err != nil {
		t.Fatalf("process webhook: %v", err)
	}
	if !result.Accepted {
		t.Fatalf("expected webhook processing accepted")
	}

	updatedCursor, err := cursorStore.Get(ctx, connection.ID, "drive.file", "file_webhook")
	if err != nil {
		t.Fatalf("load updated cursor: %v", err)
	}
	if updatedCursor.Cursor != "cursor_2" {
		t.Fatalf("expected cursor advance to cursor_2, got %q", updatedCursor.Cursor)
	}

	var firstRunCount int
	if err := client.DB().NewRaw(
		"SELECT COUNT(*) FROM service_sync_jobs WHERE connection_id = ? AND mode = ?",
		connection.ID,
		string(core.SyncJobModeIncremental),
	).Scan(ctx, &firstRunCount); err != nil {
		t.Fatalf("count sync jobs: %v", err)
	}
	if firstRunCount != 1 {
		t.Fatalf("expected one incremental sync job after first delivery, got %d", firstRunCount)
	}

	second, err := processor.Process(ctx, req)
	if err != nil {
		t.Fatalf("process duplicate webhook: %v", err)
	}
	if second.Metadata["deduped"] != true {
		t.Fatalf("expected duplicate webhook to be deduped")
	}
	var dedupedCount int
	if err := client.DB().NewRaw(
		"SELECT COUNT(*) FROM service_sync_jobs WHERE connection_id = ? AND mode = ?",
		connection.ID,
		string(core.SyncJobModeIncremental),
	).Scan(ctx, &dedupedCount); err != nil {
		t.Fatalf("count sync jobs after duplicate: %v", err)
	}
	if dedupedCount != 1 {
		t.Fatalf("expected no new sync job on duplicate delivery, got %d", dedupedCount)
	}
}

func TestCursorInvalidationRecoveryAndResumableBackfill_Integration(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	repoFactory, err := sqlstore.NewRepositoryFactoryFromPersistence(client)
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}
	connection, err := repoFactory.ConnectionStore().Create(ctx, core.CreateConnectionInput{
		ProviderID:        "github",
		Scope:             core.ScopeRef{Type: "user", ID: "usr_backfill"},
		ExternalAccountID: "acct_backfill",
		Status:            core.ConnectionStatusActive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	cursorStore, err := sqlstore.NewSyncCursorStore(client.DB())
	if err != nil {
		t.Fatalf("new sync cursor store: %v", err)
	}
	_, err = cursorStore.Upsert(ctx, core.UpsertSyncCursorInput{
		ConnectionID: connection.ID,
		ProviderID:   "github",
		ResourceType: "drive.file",
		ResourceID:   "file_backfill",
		Cursor:       "cursor_a",
		Status:       "active",
	})
	if err != nil {
		t.Fatalf("seed cursor: %v", err)
	}
	_, err = cursorStore.Advance(ctx, core.AdvanceSyncCursorInput{
		ConnectionID:   connection.ID,
		ProviderID:     "github",
		ResourceType:   "drive.file",
		ResourceID:     "file_backfill",
		ExpectedCursor: "cursor_a",
		Cursor:         "cursor_b",
		Status:         "active",
	})
	if err != nil {
		t.Fatalf("advance cursor to cursor_b: %v", err)
	}
	_, err = cursorStore.Advance(ctx, core.AdvanceSyncCursorInput{
		ConnectionID:   connection.ID,
		ProviderID:     "github",
		ResourceType:   "drive.file",
		ResourceID:     "file_backfill",
		ExpectedCursor: "cursor_a",
		Cursor:         "cursor_c",
		Status:         "active",
	})
	if !errors.Is(err, core.ErrSyncCursorConflict) {
		t.Fatalf("expected cursor conflict on stale advance, got %v", err)
	}

	_, err = cursorStore.Upsert(ctx, core.UpsertSyncCursorInput{
		ConnectionID: connection.ID,
		ProviderID:   "github",
		ResourceType: "drive.file",
		ResourceID:   "file_backfill",
		Cursor:       "cursor_rebootstrap",
		Status:       "active",
		Metadata: map[string]any{
			"recovery": "invalidation",
		},
	})
	if err != nil {
		t.Fatalf("recover cursor via bootstrap baseline: %v", err)
	}

	syncJobStore, err := sqlstore.NewSyncJobStore(client.DB())
	if err != nil {
		t.Fatalf("new sync job store: %v", err)
	}
	orchestrator := servicesync.NewOrchestrator(syncJobStore, cursorStore)

	from := time.Now().UTC().Add(-48 * time.Hour)
	to := time.Now().UTC().Add(-24 * time.Hour)
	job, err := orchestrator.StartBackfill(ctx, core.BackfillRequest{
		ConnectionID: connection.ID,
		ProviderID:   "github",
		ResourceType: "drive.file",
		ResourceID:   "file_backfill",
		From:         &from,
		To:           &to,
	})
	if err != nil {
		t.Fatalf("start backfill: %v", err)
	}
	if job.Mode != core.SyncJobModeBackfill {
		t.Fatalf("expected backfill mode")
	}

	job, err = orchestrator.SaveCheckpoint(ctx, job.ID, "page_10", map[string]any{"window": "historic"})
	if err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	if job.Checkpoint != "page_10" {
		t.Fatalf("expected checkpoint page_10")
	}

	nextAttempt := time.Now().UTC().Add(15 * time.Minute)
	job, err = orchestrator.Fail(ctx, job.ID, fmt.Errorf("temporary backfill failure"), &nextAttempt)
	if err != nil {
		t.Fatalf("fail backfill job: %v", err)
	}
	if job.Status != core.SyncJobStatusFailed {
		t.Fatalf("expected failed backfill status")
	}

	if err := orchestrator.Resume(ctx, job.ID); err != nil {
		t.Fatalf("resume backfill job: %v", err)
	}
	stored, err := syncJobStore.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("load resumed backfill job: %v", err)
	}
	if stored.Status != core.SyncJobStatusQueued {
		t.Fatalf("expected queued status after backfill resume")
	}
	if stored.Checkpoint != "page_10" {
		t.Fatalf("expected backfill checkpoint preserved across resume")
	}
}

func TestService_GrantLifecyclePermissionAndRefreshIdempotency_Integration(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newSQLiteClient(t)
	defer cleanup()

	repoFactory, err := sqlstore.NewRepositoryFactoryFromPersistence(client)
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}
	grantStore, err := sqlstore.NewGrantStore(client.DB())
	if err != nil {
		t.Fatalf("new grant store: %v", err)
	}

	provider := &integrationProvider{
		id: "github",
		completeResponse: core.CompleteAuthResponse{
			ExternalAccountID: "acct_int",
			Credential: core.ActiveCredential{
				TokenType:       "bearer",
				RequestedScopes: []string{"repo:read", "repo:write"},
				GrantedScopes:   []string{"repo:read", "repo:write"},
				Refreshable:     true,
			},
			RequestedGrants: []string{"repo:read", "repo:write"},
			GrantedGrants:   []string{"repo:read", "repo:write"},
		},
		refreshResponse: core.RefreshResult{
			Credential: core.ActiveCredential{
				TokenType:       "bearer",
				RequestedScopes: []string{"repo:read", "repo:write"},
				GrantedScopes:   []string{"repo:read"},
				Refreshable:     true,
			},
			GrantedGrants: []string{"repo:read"},
		},
		capabilities: []core.CapabilityDescriptor{{
			Name:           "repo.write",
			RequiredGrants: []string{"repo:write"},
			DeniedBehavior: core.CapabilityDeniedBehaviorBlock,
		}},
	}
	registry := core.NewProviderRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	svc, err := core.NewService(core.Config{},
		core.WithRegistry(registry),
		core.WithConnectionStore(repoFactory.ConnectionStore()),
		core.WithCredentialStore(repoFactory.CredentialStore()),
		core.WithSecretProvider(newServiceSecretProvider(t)),
		core.WithGrantStore(grantStore),
		core.WithOAuthStateStore(core.NewMemoryOAuthStateStore(time.Minute)),
		core.WithConnectionLocker(core.NewMemoryConnectionLocker()),
		core.WithRefreshBackoffScheduler(core.ExponentialBackoffScheduler{Initial: 0, Max: 0}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	beginResp, err := svc.Connect(ctx, core.ConnectRequest{
		ProviderID:      "github",
		Scope:           core.ScopeRef{Type: "user", ID: "usr_int"},
		RedirectURI:     "https://app.example/callback",
		RequestedGrants: []string{"repo:read", "repo:write"},
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	completed, err := svc.CompleteCallback(ctx, core.CompleteAuthRequest{
		ProviderID:  "github",
		Scope:       core.ScopeRef{Type: "user", ID: "usr_int"},
		Code:        "code-int",
		State:       beginResp.State,
		RedirectURI: "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("complete callback: %v", err)
	}

	allowedBefore, err := svc.InvokeCapability(ctx, core.InvokeCapabilityRequest{
		ProviderID: "github",
		Scope:      core.ScopeRef{Type: "user", ID: "usr_int"},
		Capability: "repo.write",
	})
	if err != nil {
		t.Fatalf("invoke capability before refresh: %v", err)
	}
	if !allowedBefore.Allowed {
		t.Fatalf("expected capability allowed before grant downgrade")
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, refreshErr := svc.Refresh(ctx, core.RefreshRequest{
				ProviderID:   "github",
				ConnectionID: completed.Connection.ID,
			})
			errCh <- refreshErr
		}()
	}
	wg.Wait()
	close(errCh)

	successCount := 0
	lockCount := 0
	for refreshErr := range errCh {
		if refreshErr == nil {
			successCount++
			continue
		}
		if strings.Contains(strings.ToLower(refreshErr.Error()), "service_refresh_locked") {
			lockCount++
			continue
		}
		t.Fatalf("refresh failed: %v", refreshErr)
	}
	if successCount != 1 || lockCount != 1 {
		t.Fatalf("expected one refresh success and one lock conflict, got success=%d lock=%d", successCount, lockCount)
	}

	activeCredential, err := repoFactory.CredentialStore().GetActiveByConnection(ctx, completed.Connection.ID)
	if err != nil {
		t.Fatalf("get active credential: %v", err)
	}
	if activeCredential.Version != 2 {
		t.Fatalf("expected exactly one credential rotation, got version %d", activeCredential.Version)
	}

	allowedAfter, err := svc.InvokeCapability(ctx, core.InvokeCapabilityRequest{
		ProviderID: "github",
		Scope:      core.ScopeRef{Type: "user", ID: "usr_int"},
		Capability: "repo.write",
	})
	if err != nil {
		t.Fatalf("invoke capability after refresh: %v", err)
	}
	if allowedAfter.Allowed {
		t.Fatalf("expected capability blocked after grant downgrade")
	}

	var expandedCount int
	if err := client.DB().NewRaw(
		"SELECT COUNT(*) FROM service_grant_events WHERE connection_id = ? AND event_type = ?",
		completed.Connection.ID,
		core.GrantEventExpanded,
	).Scan(ctx, &expandedCount); err != nil {
		t.Fatalf("count expanded events: %v", err)
	}
	if expandedCount == 0 {
		t.Fatalf("expected at least one expanded grant event")
	}

	var downgradedCount int
	if err := client.DB().NewRaw(
		"SELECT COUNT(*) FROM service_grant_events WHERE connection_id = ? AND event_type = ?",
		completed.Connection.ID,
		core.GrantEventDowngraded,
	).Scan(ctx, &downgradedCount); err != nil {
		t.Fatalf("count downgraded events: %v", err)
	}
	if downgradedCount == 0 {
		t.Fatalf("expected at least one downgraded grant event")
	}
}

func newSQLiteClient(t *testing.T) (*persistence.Client, func()) {
	t.Helper()

	dsn := fmt.Sprintf(
		"file:services-test-%d?mode=memory&cache=shared&_foreign_keys=on",
		time.Now().UnixNano(),
	)
	sqlDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	cfg := testPersistenceConfig{
		driver: "sqlite3",
		server: dsn,
	}
	client, err := persistence.New(cfg, sqlDB, sqlitedialect.New())
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("new persistence client: %v", err)
	}

	ctx := context.Background()
	_, err = servicemigrations.Register(ctx, func(_ context.Context, dialect string, _ string, fsys fs.FS) error {
		if dialect != servicemigrations.DialectSQLite {
			return nil
		}
		client.RegisterSQLMigrations(fsys)
		return nil
	}, servicemigrations.WithValidationTargets(servicemigrations.DialectSQLite))
	if err != nil {
		_ = client.Close()
		t.Fatalf("register migrations: %v", err)
	}
	if err := client.Migrate(ctx); err != nil {
		_ = client.Close()
		t.Fatalf("migrate: %v", err)
	}

	return client, func() {
		_ = client.Close()
	}
}

type stubConnectionStore struct{}

func (stubConnectionStore) Create(context.Context, core.CreateConnectionInput) (core.Connection, error) {
	return core.Connection{}, nil
}
func (stubConnectionStore) Get(context.Context, string) (core.Connection, error) {
	return core.Connection{}, nil
}
func (stubConnectionStore) FindByScope(context.Context, string, core.ScopeRef) ([]core.Connection, error) {
	return nil, nil
}
func (stubConnectionStore) UpdateStatus(context.Context, string, string, string) error {
	return nil
}

type stubCredentialStore struct{}

func (stubCredentialStore) SaveNewVersion(context.Context, core.SaveCredentialInput) (core.Credential, error) {
	return core.Credential{}, nil
}
func (stubCredentialStore) GetActiveByConnection(context.Context, string) (core.Credential, error) {
	return core.Credential{}, nil
}
func (stubCredentialStore) RevokeActive(context.Context, string, string) error {
	return nil
}

type integrationProvider struct {
	id                        string
	completeResponse          core.CompleteAuthResponse
	refreshResponse           core.RefreshResult
	capabilities              []core.CapabilityDescriptor
	subscribeResponse         core.SubscriptionResult
	renewSubscriptionResponse core.SubscriptionResult
	subscribeErr              error
	renewErr                  error
	cancelErr                 error
	cancelCount               int
}

func (p *integrationProvider) ID() string                    { return p.id }
func (p *integrationProvider) AuthKind() string              { return "oauth2" }
func (p *integrationProvider) SupportedScopeTypes() []string { return []string{"user", "org"} }
func (p *integrationProvider) Capabilities() []core.CapabilityDescriptor {
	return append([]core.CapabilityDescriptor(nil), p.capabilities...)
}

func (p *integrationProvider) BeginAuth(_ context.Context, req core.BeginAuthRequest) (core.BeginAuthResponse, error) {
	return core.BeginAuthResponse{
		URL:             "https://example.com/oauth",
		State:           req.State,
		RequestedGrants: append([]string(nil), req.RequestedGrants...),
	}, nil
}

func (p *integrationProvider) CompleteAuth(_ context.Context, _ core.CompleteAuthRequest) (core.CompleteAuthResponse, error) {
	return p.completeResponse, nil
}

func (p *integrationProvider) Refresh(_ context.Context, _ core.ActiveCredential) (core.RefreshResult, error) {
	return p.refreshResponse, nil
}

func (p *integrationProvider) Subscribe(_ context.Context, _ core.SubscribeRequest) (core.SubscriptionResult, error) {
	if p.subscribeErr != nil {
		return core.SubscriptionResult{}, p.subscribeErr
	}
	return p.subscribeResponse, nil
}

func (p *integrationProvider) RenewSubscription(_ context.Context, _ core.RenewSubscriptionRequest) (core.SubscriptionResult, error) {
	if p.renewErr != nil {
		return core.SubscriptionResult{}, p.renewErr
	}
	return p.renewSubscriptionResponse, nil
}

func (p *integrationProvider) CancelSubscription(_ context.Context, _ core.CancelSubscriptionRequest) error {
	p.cancelCount++
	return p.cancelErr
}

func (p *integrationProvider) Signer() core.Signer {
	return integrationSigner{}
}

type integrationSigner struct{}

func (integrationSigner) Sign(_ context.Context, req *http.Request, _ core.ActiveCredential) error {
	req.Header.Set("X-Signed-Integration", "true")
	return nil
}

type webhookSyncHandler struct {
	subscriptions core.SubscriptionStore
	cursors       core.SyncCursorStore
	orchestrator  *servicesync.Orchestrator
}

func (h webhookSyncHandler) Handle(ctx context.Context, req core.InboundRequest) (core.InboundResult, error) {
	channelID := req.Headers["X-Channel-ID"]
	subscription, err := h.subscriptions.GetByChannelID(ctx, req.ProviderID, channelID)
	if err != nil {
		return core.InboundResult{}, err
	}

	currentCursor, err := h.cursors.Get(ctx, subscription.ConnectionID, subscription.ResourceType, subscription.ResourceID)
	if err != nil {
		currentCursor = core.SyncCursor{
			ConnectionID: subscription.ConnectionID,
			ProviderID:   subscription.ProviderID,
			ResourceType: subscription.ResourceType,
			ResourceID:   subscription.ResourceID,
			Cursor:       "",
		}
	}

	nextCursor := strings.TrimSpace(fmt.Sprint(req.Metadata["next_cursor"]))
	job, err := h.orchestrator.StartIncremental(
		ctx,
		subscription.ConnectionID,
		subscription.ProviderID,
		subscription.ResourceType,
		subscription.ResourceID,
		map[string]any{"source": "webhook"},
	)
	if err != nil {
		return core.InboundResult{}, err
	}

	_, err = h.cursors.Advance(ctx, core.AdvanceSyncCursorInput{
		ConnectionID:   subscription.ConnectionID,
		ProviderID:     subscription.ProviderID,
		ResourceType:   subscription.ResourceType,
		ResourceID:     subscription.ResourceID,
		ExpectedCursor: currentCursor.Cursor,
		Cursor:         nextCursor,
		Status:         "active",
		LastSyncedAt:   ptrTime(time.Now().UTC()),
		Metadata:       map[string]any{"source": "webhook"},
	})
	if err != nil {
		return core.InboundResult{}, err
	}
	if _, err := h.orchestrator.SaveCheckpoint(ctx, job.ID, nextCursor, map[string]any{"source": "webhook"}); err != nil {
		return core.InboundResult{}, err
	}

	return core.InboundResult{
		Accepted:   true,
		StatusCode: 202,
		Metadata: map[string]any{
			"job_id": job.ID,
		},
	}, nil
}

func ptrTime(value time.Time) *time.Time {
	out := value.UTC()
	return &out
}

func newServiceSecretProvider(t *testing.T) core.SecretProvider {
	t.Helper()
	provider, err := servicessecurity.NewAppKeySecretProviderFromString(
		"integration-test-secret-key",
		servicessecurity.WithKeyID("integration-test-key"),
		servicessecurity.WithVersion(1),
	)
	if err != nil {
		t.Fatalf("new secret provider: %v", err)
	}
	return provider
}
