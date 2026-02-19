package migrations

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	services "github.com/goliatone/go-services"
	_ "github.com/mattn/go-sqlite3"
)

func TestFilesystems_ReturnsPostgresAndSQLite(t *testing.T) {
	filesystems, err := Filesystems()
	if err != nil {
		t.Fatalf("filesystems: %v", err)
	}
	if len(filesystems) != 2 {
		t.Fatalf("expected 2 filesystems, got %d", len(filesystems))
	}

	var postgresFound bool
	var sqliteFound bool
	for _, entry := range filesystems {
		matches, globErr := fs.Glob(entry.FS, "*.up.sql")
		if globErr != nil {
			t.Fatalf("glob %s: %v", entry.Dialect, globErr)
		}
		if len(matches) == 0 {
			t.Fatalf("expected %s migration files, got none", entry.Dialect)
		}
		switch entry.Dialect {
		case DialectPostgres:
			postgresFound = true
		case DialectSQLite:
			sqliteFound = true
		}
	}

	if !postgresFound {
		t.Fatalf("expected postgres filesystem")
	}
	if !sqliteFound {
		t.Fatalf("expected sqlite filesystem")
	}
}

func TestRegister_UsesValidationTargets(t *testing.T) {
	var calls []string
	_, err := Register(context.Background(), func(_ context.Context, dialect string, _ string, _ fs.FS) error {
		calls = append(calls, dialect)
		return nil
	}, WithValidationTargets(DialectSQLite))
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 registration call, got %d", len(calls))
	}
	if calls[0] != DialectSQLite {
		t.Fatalf("expected sqlite registration, got %q", calls[0])
	}
}

func TestRateLimitStateUniquenessMigrationPair_ExistsForBothDialects(t *testing.T) {
	root := services.GetCoreMigrationsFS()
	paths := []string{
		"data/sql/migrations/00004_services_rate_limit_state_uniqueness.up.sql",
		"data/sql/migrations/00004_services_rate_limit_state_uniqueness.down.sql",
		"data/sql/migrations/sqlite/00004_services_rate_limit_state_uniqueness.up.sql",
		"data/sql/migrations/sqlite/00004_services_rate_limit_state_uniqueness.down.sql",
	}
	for _, migrationPath := range paths {
		content, err := fs.ReadFile(root, migrationPath)
		if err != nil {
			t.Fatalf("read migration %s: %v", migrationPath, err)
		}
		if strings.TrimSpace(string(content)) == "" {
			t.Fatalf("expected migration %s to have SQL content", migrationPath)
		}
	}
}

func TestFormDataFoundationMigrationPair_ExistsForBothDialects(t *testing.T) {
	root := services.GetCoreMigrationsFS()
	paths := []string{
		"data/sql/migrations/00005_services_form_data_foundation.up.sql",
		"data/sql/migrations/00005_services_form_data_foundation.down.sql",
		"data/sql/migrations/sqlite/00005_services_form_data_foundation.up.sql",
		"data/sql/migrations/sqlite/00005_services_form_data_foundation.down.sql",
	}
	for _, migrationPath := range paths {
		content, err := fs.ReadFile(root, migrationPath)
		if err != nil {
			t.Fatalf("read migration %s: %v", migrationPath, err)
		}
		if strings.TrimSpace(string(content)) == "" {
			t.Fatalf("expected migration %s to have SQL content", migrationPath)
		}
	}
}

func TestSyncJobIdempotencyMigrationPair_ExistsForBothDialects(t *testing.T) {
	root := services.GetCoreMigrationsFS()
	paths := []string{
		"data/sql/migrations/00006_services_sync_job_idempotency.up.sql",
		"data/sql/migrations/00006_services_sync_job_idempotency.down.sql",
		"data/sql/migrations/sqlite/00006_services_sync_job_idempotency.up.sql",
		"data/sql/migrations/sqlite/00006_services_sync_job_idempotency.down.sql",
	}
	for _, migrationPath := range paths {
		content, err := fs.ReadFile(root, migrationPath)
		if err != nil {
			t.Fatalf("read migration %s: %v", migrationPath, err)
		}
		if strings.TrimSpace(string(content)) == "" {
			t.Fatalf("expected migration %s to have SQL content", migrationPath)
		}
	}
}

func TestSQLiteRateLimitStateUniquenessMigration_ApplyAndRollback(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:migrations-rate-limit-uniqueness?mode=memory&cache=shared&_foreign_keys=on")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer func() { _ = db.Close() }()

	root := services.GetCoreMigrationsFS()
	sqliteMigrations, err := fs.Sub(root, "data/sql/migrations/sqlite")
	if err != nil {
		t.Fatalf("resolve sqlite migrations: %v", err)
	}

	baseUps := []string{
		"00001_services_core_schema.up.sql",
		"00002_services_credential_payload_codec.up.sql",
		"00003_services_grant_snapshots.up.sql",
	}
	for _, migration := range baseUps {
		if err := execSQLMigration(context.Background(), db, sqliteMigrations, migration); err != nil {
			t.Fatalf("apply base migration %s: %v", migration, err)
		}
	}

	insertStatement := `
		INSERT INTO service_rate_limit_state (
			id,
			provider_id,
			scope_type,
			scope_id,
			bucket_key,
			"limit",
			remaining,
			metadata,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	rows := [][]any{
		{"dup-old", "github", "org", "org_1", "api", 5000, 4999, "{}", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"},
		{"dup-new", "github", "org", "org_1", "api", 5000, 4500, "{}", "2026-02-01T00:00:00Z", "2026-02-01T00:00:00Z"},
		{"tie-b", "github", "org", "org_2", "api", 5000, 4900, "{}", "2026-02-01T00:00:00Z", "2026-02-01T00:00:00Z"},
		{"tie-a", "github", "org", "org_2", "api", 5000, 4800, "{}", "2026-02-01T00:00:00Z", "2026-02-01T00:00:00Z"},
	}
	for _, row := range rows {
		if _, err := db.ExecContext(context.Background(), insertStatement, row...); err != nil {
			t.Fatalf("insert seed row %v: %v", row[0], err)
		}
	}

	if err := execSQLMigration(
		context.Background(),
		db,
		sqliteMigrations,
		"00004_services_rate_limit_state_uniqueness.up.sql",
	); err != nil {
		t.Fatalf("apply uniqueness migration up: %v", err)
	}

	var count int
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM service_rate_limit_state WHERE provider_id=? AND scope_type=? AND scope_id=? AND bucket_key=?`,
		"github",
		"org",
		"org_1",
		"api",
	).Scan(&count); err != nil {
		t.Fatalf("count deduped org_1 rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected org_1 dedupe count=1, got %d", count)
	}

	var winningID string
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT id FROM service_rate_limit_state WHERE provider_id=? AND scope_type=? AND scope_id=? AND bucket_key=?`,
		"github",
		"org",
		"org_1",
		"api",
	).Scan(&winningID); err != nil {
		t.Fatalf("select winning org_1 row: %v", err)
	}
	if winningID != "dup-new" {
		t.Fatalf("expected org_1 winner dup-new (latest updated_at), got %q", winningID)
	}

	if err := db.QueryRowContext(
		context.Background(),
		`SELECT id FROM service_rate_limit_state WHERE provider_id=? AND scope_type=? AND scope_id=? AND bucket_key=?`,
		"github",
		"org",
		"org_2",
		"api",
	).Scan(&winningID); err != nil {
		t.Fatalf("select winning org_2 row: %v", err)
	}
	if winningID != "tie-a" {
		t.Fatalf("expected org_2 winner tie-a (id ASC tie-breaker), got %q", winningID)
	}

	if _, err := db.ExecContext(
		context.Background(),
		insertStatement,
		"dup-after-up",
		"github",
		"org",
		"org_1",
		"api",
		5000,
		4000,
		"{}",
		"2026-03-01T00:00:00Z",
		"2026-03-01T00:00:00Z",
	); err == nil {
		t.Fatalf("expected unique index violation after up migration")
	}

	if err := execSQLMigration(
		context.Background(),
		db,
		sqliteMigrations,
		"00004_services_rate_limit_state_uniqueness.down.sql",
	); err != nil {
		t.Fatalf("apply uniqueness migration down: %v", err)
	}

	if _, err := db.ExecContext(
		context.Background(),
		insertStatement,
		"dup-after-down",
		"github",
		"org",
		"org_1",
		"api",
		5000,
		3500,
		"{}",
		"2026-04-01T00:00:00Z",
		"2026-04-01T00:00:00Z",
	); err != nil {
		t.Fatalf("expected duplicate insert to succeed after down migration: %v", err)
	}
}

func TestSQLiteFormDataFoundationMigration_ApplyAndRollback(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:migrations-form-data-foundation?mode=memory&cache=shared&_foreign_keys=on")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer func() { _ = db.Close() }()

	root := services.GetCoreMigrationsFS()
	sqliteMigrations, err := fs.Sub(root, "data/sql/migrations/sqlite")
	if err != nil {
		t.Fatalf("resolve sqlite migrations: %v", err)
	}

	baseUps := []string{
		"00001_services_core_schema.up.sql",
		"00002_services_credential_payload_codec.up.sql",
		"00003_services_grant_snapshots.up.sql",
		"00004_services_rate_limit_state_uniqueness.up.sql",
	}
	for _, migration := range baseUps {
		if err := execSQLMigration(context.Background(), db, sqliteMigrations, migration); err != nil {
			t.Fatalf("apply base migration %s: %v", migration, err)
		}
	}

	if err := execSQLMigration(
		context.Background(),
		db,
		sqliteMigrations,
		"00005_services_form_data_foundation.up.sql",
	); err != nil {
		t.Fatalf("apply form data foundation migration up: %v", err)
	}

	requiredTables := []string{
		"service_mapping_specs",
		"service_sync_bindings",
		"service_identity_bindings",
		"service_sync_conflicts",
		"service_sync_checkpoints",
		"service_sync_change_log",
	}
	for _, tableName := range requiredTables {
		var count int
		if err := db.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`,
			tableName,
		).Scan(&count); err != nil {
			t.Fatalf("query sqlite_master for %s: %v", tableName, err)
		}
		if count != 1 {
			t.Fatalf("expected table %s to exist after up migration", tableName)
		}
	}

	if err := execSQLMigration(
		context.Background(),
		db,
		sqliteMigrations,
		"00005_services_form_data_foundation.down.sql",
	); err != nil {
		t.Fatalf("apply form data foundation migration down: %v", err)
	}

	var count int
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`,
		"service_mapping_specs",
	).Scan(&count); err != nil {
		t.Fatalf("query sqlite_master after down migration: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected service_mapping_specs to be dropped after down migration")
	}
}

func TestSQLiteSyncJobIdempotencyMigration_ApplyAndRollback(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:migrations-sync-job-idempotency?mode=memory&cache=shared&_foreign_keys=on")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer func() { _ = db.Close() }()

	root := services.GetCoreMigrationsFS()
	sqliteMigrations, err := fs.Sub(root, "data/sql/migrations/sqlite")
	if err != nil {
		t.Fatalf("resolve sqlite migrations: %v", err)
	}

	baseUps := []string{
		"00001_services_core_schema.up.sql",
		"00002_services_credential_payload_codec.up.sql",
		"00003_services_grant_snapshots.up.sql",
		"00004_services_rate_limit_state_uniqueness.up.sql",
		"00005_services_form_data_foundation.up.sql",
	}
	for _, migration := range baseUps {
		if err := execSQLMigration(context.Background(), db, sqliteMigrations, migration); err != nil {
			t.Fatalf("apply base migration %s: %v", migration, err)
		}
	}

	var activeIndexCount int
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`,
		"uq_service_sync_jobs_active",
	).Scan(&activeIndexCount); err != nil {
		t.Fatalf("query active sync job index before up: %v", err)
	}
	if activeIndexCount != 1 {
		t.Fatalf("expected uq_service_sync_jobs_active before up migration")
	}

	if err := execSQLMigration(
		context.Background(),
		db,
		sqliteMigrations,
		"00006_services_sync_job_idempotency.up.sql",
	); err != nil {
		t.Fatalf("apply sync job idempotency migration up: %v", err)
	}

	if err := db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`,
		"uq_service_sync_jobs_active",
	).Scan(&activeIndexCount); err != nil {
		t.Fatalf("query active sync job index after up: %v", err)
	}
	if activeIndexCount != 0 {
		t.Fatalf("expected uq_service_sync_jobs_active to be dropped after up migration")
	}

	if _, err := db.ExecContext(
		context.Background(),
		`INSERT INTO service_connections
			(id, provider_id, scope_type, scope_id, external_account_id, status, last_error, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"conn_migration_1",
		"github",
		"org",
		"org_migration_1",
		"acct_migration_1",
		"active",
		"",
		"2026-01-01T00:00:00Z",
		"2026-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	if _, err := db.ExecContext(
		context.Background(),
		`INSERT INTO service_sync_jobs
			(id, connection_id, provider_id, mode, checkpoint, status, attempts, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"job_migration_1",
		"conn_migration_1",
		"github",
		"full",
		"",
		"queued",
		0,
		"{}",
		"2026-01-01T00:00:00Z",
		"2026-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("insert sync job: %v", err)
	}
	if _, err := db.ExecContext(
		context.Background(),
		`INSERT INTO service_sync_job_idempotency
			(id, scope_type, scope_id, provider_id, connection_id, mode, idempotency_key, sync_job_id, requested_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"idem_migration_1",
		"org",
		"org_migration_1",
		"github",
		"conn_migration_1",
		"full",
		"idem_key_1",
		"job_migration_1",
		"wizard",
		"2026-01-01T00:00:00Z",
		"2026-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("insert sync job idempotency claim: %v", err)
	}

	if _, err := db.ExecContext(
		context.Background(),
		`INSERT INTO service_sync_job_idempotency
			(id, scope_type, scope_id, provider_id, connection_id, mode, idempotency_key, sync_job_id, requested_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"idem_migration_2",
		"org",
		"org_migration_1",
		"github",
		"conn_migration_1",
		"full",
		"idem_key_1",
		"job_migration_1",
		"wizard",
		"2026-01-01T00:01:00Z",
		"2026-01-01T00:01:00Z",
	); err == nil {
		t.Fatalf("expected unique idempotency tuple violation after up migration")
	}

	if err := execSQLMigration(
		context.Background(),
		db,
		sqliteMigrations,
		"00006_services_sync_job_idempotency.down.sql",
	); err != nil {
		t.Fatalf("apply sync job idempotency migration down: %v", err)
	}

	var idempotencyTableCount int
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`,
		"service_sync_job_idempotency",
	).Scan(&idempotencyTableCount); err != nil {
		t.Fatalf("query idempotency table after down: %v", err)
	}
	if idempotencyTableCount != 0 {
		t.Fatalf("expected service_sync_job_idempotency to be dropped after down migration")
	}
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`,
		"uq_service_sync_jobs_active",
	).Scan(&activeIndexCount); err != nil {
		t.Fatalf("query active sync job index after down: %v", err)
	}
	if activeIndexCount != 1 {
		t.Fatalf("expected uq_service_sync_jobs_active to be restored after down migration")
	}
}

func execSQLMigration(ctx context.Context, db *sql.DB, fsys fs.FS, filename string) error {
	content, err := fs.ReadFile(fsys, filepath.Clean(filename))
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, string(content))
	return err
}
