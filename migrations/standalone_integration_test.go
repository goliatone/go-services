package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	servicemigrations "github.com/goliatone/go-services/migrations"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

func TestStandaloneMigrationsSQLiteApplyRollbackReapply(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", "file:"+filepath.Join(t.TempDir(), "services.db")+"?_fk=1")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if err := applyTrack(ctx, db, servicemigrations.DialectSQLite, true); err != nil {
		t.Fatalf("apply sqlite migrations: %v", err)
	}
	assertTableExistsSQLite(t, db, "service_connections")
	assertTableExistsSQLite(t, db, "service_sync_job_idempotency")
	assertTableExistsSQLite(t, db, "service_mapping_specs")

	if err := applyTrack(ctx, db, servicemigrations.DialectSQLite, false); err != nil {
		t.Fatalf("rollback sqlite migrations: %v", err)
	}
	assertTableNotExistsSQLite(t, db, "service_connections")
	assertTableNotExistsSQLite(t, db, "service_sync_job_idempotency")
	assertTableNotExistsSQLite(t, db, "service_mapping_specs")

	if err := applyTrack(ctx, db, servicemigrations.DialectSQLite, true); err != nil {
		t.Fatalf("reapply sqlite migrations: %v", err)
	}
	assertTableExistsSQLite(t, db, "service_connections")
	assertTableExistsSQLite(t, db, "service_sync_job_idempotency")
	assertTableExistsSQLite(t, db, "service_mapping_specs")
}

func TestStandaloneMigrationsPostgresApplyRollbackReapply(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("GO_SERVICES_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set GO_SERVICES_TEST_POSTGRES_DSN to run postgres migration integration test")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	schemaName := fmt.Sprintf("goservices_mig_%d_%d", time.Now().UnixNano(), rand.Intn(10000))
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA "`+schemaName+`"`); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `SET search_path TO public`)
		_, _ = db.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS "`+schemaName+`" CASCADE`)
	})
	if _, err := db.ExecContext(ctx, `SET search_path TO "`+schemaName+`"`); err != nil {
		t.Fatalf("set search path: %v", err)
	}

	if err := applyTrack(ctx, db, servicemigrations.DialectPostgres, true); err != nil {
		t.Fatalf("apply postgres migrations: %v", err)
	}
	assertTableExistsPostgres(t, db, schemaName, "service_connections")
	assertTableExistsPostgres(t, db, schemaName, "service_sync_job_idempotency")
	assertTableExistsPostgres(t, db, schemaName, "service_mapping_specs")

	if err := applyTrack(ctx, db, servicemigrations.DialectPostgres, false); err != nil {
		t.Fatalf("rollback postgres migrations: %v", err)
	}
	assertTableNotExistsPostgres(t, db, schemaName, "service_connections")
	assertTableNotExistsPostgres(t, db, schemaName, "service_sync_job_idempotency")
	assertTableNotExistsPostgres(t, db, schemaName, "service_mapping_specs")

	if err := applyTrack(ctx, db, servicemigrations.DialectPostgres, true); err != nil {
		t.Fatalf("reapply postgres migrations: %v", err)
	}
	assertTableExistsPostgres(t, db, schemaName, "service_connections")
	assertTableExistsPostgres(t, db, schemaName, "service_sync_job_idempotency")
	assertTableExistsPostgres(t, db, schemaName, "service_mapping_specs")
}

func applyTrack(ctx context.Context, db *sql.DB, dialect string, up bool) error {
	filesystem, err := filesystemForDialect(dialect)
	if err != nil {
		return err
	}

	pattern := "*.up.sql"
	if !up {
		pattern = "*.down.sql"
	}
	files, err := fs.Glob(filesystem, pattern)
	if err != nil {
		return fmt.Errorf("glob %s migrations: %w", dialect, err)
	}
	sort.Strings(files)
	if !up {
		for i, j := 0, len(files)-1; i < j; i, j = i+1, j-1 {
			files[i], files[j] = files[j], files[i]
		}
	}

	for _, filename := range files {
		script, err := fs.ReadFile(filesystem, filepath.Clean(filename))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", filename, err)
		}
		if _, err := db.ExecContext(ctx, string(script)); err != nil {
			return fmt.Errorf("exec migration %s: %w", filename, err)
		}
	}

	return nil
}

func filesystemForDialect(dialect string) (fs.FS, error) {
	filesystems, err := servicemigrations.Filesystems()
	if err != nil {
		return nil, err
	}
	for _, spec := range filesystems {
		if spec.Dialect == dialect {
			return spec.FS, nil
		}
	}
	return nil, fmt.Errorf("dialect %q filesystem not found", dialect)
}

func assertTableExistsSQLite(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
	if err != nil || name != table {
		t.Fatalf("expected sqlite table %q to exist, err=%v", table, err)
	}
}

func assertTableNotExistsSQLite(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
	if err == nil {
		t.Fatalf("expected sqlite table %q to be absent", table)
	}
}

func assertTableExistsPostgres(t *testing.T, db *sql.DB, schema, table string) {
	t.Helper()
	var exists bool
	err := db.QueryRow(
		`SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = $1 AND table_name = $2
		)`,
		schema,
		table,
	).Scan(&exists)
	if err != nil || !exists {
		t.Fatalf("expected postgres table %s.%s to exist, err=%v", schema, table, err)
	}
}

func assertTableNotExistsPostgres(t *testing.T, db *sql.DB, schema, table string) {
	t.Helper()
	var exists bool
	err := db.QueryRow(
		`SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = $1 AND table_name = $2
		)`,
		schema,
		table,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("query postgres table existence %s.%s: %v", schema, table, err)
	}
	if exists {
		t.Fatalf("expected postgres table %s.%s to be absent", schema, table)
	}
}
