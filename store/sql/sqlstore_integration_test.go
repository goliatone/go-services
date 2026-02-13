package sqlstore_test

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"time"

	persistence "github.com/goliatone/go-persistence-bun"
	"github.com/goliatone/go-services/core"
	servicemigrations "github.com/goliatone/go-services/migrations"
	sqlstore "github.com/goliatone/go-services/store/sql"
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

	secondCredential, err := credentialStore.SaveNewVersion(ctx, core.SaveCredentialInput{
		ConnectionID:      connection.ID,
		EncryptedPayload:  []byte("cipher-v2"),
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
