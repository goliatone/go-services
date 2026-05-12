package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	services "github.com/goliatone/go-services"
	_ "github.com/mattn/go-sqlite3"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"github.com/uptrace/bun/dialect/feature"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/schema"
)

func TestRequiredSQLTables_ReturnsDefensiveSortedCopy(t *testing.T) {
	tables := RequiredSQLTables()
	if !slices.IsSorted(tables) {
		t.Fatalf("expected sorted tables, got %v", tables)
	}
	if !slices.Contains(tables, "service_connections") {
		t.Fatalf("expected service_connections in required SQL tables")
	}

	tables[0] = "mutated"
	next := RequiredSQLTables()
	if next[0] == "mutated" {
		t.Fatalf("expected defensive copy")
	}
}

func TestRequiredOAuthStorageTables_ReturnsDefensiveSubset(t *testing.T) {
	oauthTables := RequiredOAuthStorageTables()
	if !slices.IsSorted(oauthTables) {
		t.Fatalf("expected sorted OAuth tables, got %v", oauthTables)
	}

	oauthTables[0] = "mutated"
	if RequiredOAuthStorageTables()[0] == "mutated" {
		t.Fatalf("expected defensive copy")
	}

	full := RequiredSQLTables()
	for _, table := range RequiredOAuthStorageTables() {
		if !slices.Contains(full, table) {
			t.Fatalf("expected OAuth table %q to be in full table list", table)
		}
	}
}

func TestNormalizeDialect_Aliases(t *testing.T) {
	tests := map[string]string{
		"pg":         DialectPostgres,
		"postgres":   DialectPostgres,
		"postgresql": DialectPostgres,
		" sqlite ":   DialectSQLite,
		"sqlite3":    DialectSQLite,
	}
	for input, expected := range tests {
		t.Run(input, func(t *testing.T) {
			actual, err := normalizeDialect(input)
			if err != nil {
				t.Fatalf("normalize dialect: %v", err)
			}
			if actual != expected {
				t.Fatalf("expected %q, got %q", expected, actual)
			}
		})
	}
}

func TestVerifySQLSchemaSQLite_UnmigratedReportsMissingTables(t *testing.T) {
	db := openTestBunSQLite(t, "unmigrated")

	err := VerifySQLSchema(context.Background(), db)
	if err == nil {
		t.Fatalf("expected missing table error")
	}
	if !strings.Contains(err.Error(), "missing required tables:") ||
		!strings.Contains(err.Error(), "service_connections") ||
		!strings.Contains(err.Error(), "service_sync_job_idempotency") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifySQLSchemaSQLite_MigratedPasses(t *testing.T) {
	db := openTestBunSQLite(t, "migrated-full")
	applySQLiteMigrations(t, db.DB)

	if err := VerifySQLSchema(context.Background(), db); err != nil {
		t.Fatalf("verify SQL schema: %v", err)
	}
}

func TestRequiredSQLTables_MatchesSQLiteMigratedServiceTables(t *testing.T) {
	db := openTestBunSQLite(t, "migrated-drift")
	applySQLiteMigrations(t, db.DB)

	rows, err := db.QueryContext(
		context.Background(),
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name LIKE 'service_%' ORDER BY name`,
	)
	if err != nil {
		t.Fatalf("query service tables: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var created []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("scan service table: %v", err)
		}
		created = append(created, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read service tables: %v", err)
	}

	expected := RequiredSQLTables()
	if !slices.Equal(created, expected) {
		t.Fatalf("expected migrated service tables to match RequiredSQLTables\ncreated:  %v\nexpected: %v", created, expected)
	}
}

func TestVerifyOAuthStorageSchemaSQLite_MigratedPasses(t *testing.T) {
	db := openTestBunSQLite(t, "migrated-oauth")
	applySQLiteMigrations(t, db.DB)

	if err := VerifyOAuthStorageSchema(context.Background(), db); err != nil {
		t.Fatalf("verify OAuth storage schema: %v", err)
	}
}

func TestVerifyOAuthStorageSchemaSQLite_UnmigratedReportsMissingTables(t *testing.T) {
	db := openTestBunSQLite(t, "unmigrated-oauth")

	err := VerifyOAuthStorageSchema(context.Background(), db)
	if err == nil {
		t.Fatalf("expected missing table error")
	}
	if !strings.Contains(err.Error(), "service_connections") ||
		!strings.Contains(err.Error(), "service_grant_snapshots") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifySQLSchema_RejectsNilDB(t *testing.T) {
	err := VerifySQLSchema(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "database handle is nil") {
		t.Fatalf("expected nil database error, got %v", err)
	}
}

func TestVerifySQLSchema_RejectsUnsupportedDialect(t *testing.T) {
	sqlDB, err := sql.Open("sqlite3", "file:unsupported-dialect?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db := bun.NewDB(sqlDB, newUnsupportedTestDialect(dialect.MySQL))
	t.Cleanup(func() { _ = db.Close() })

	err = VerifySQLSchema(context.Background(), db)
	if err == nil || !strings.Contains(err.Error(), `unsupported dialect "mysql"`) {
		t.Fatalf("expected unsupported dialect error, got %v", err)
	}
}

func openTestBunSQLite(t *testing.T, name string) *bun.DB {
	t.Helper()

	sqlDB, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=memory&cache=shared&_foreign_keys=on", name))
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	db := bun.NewDB(sqlDB, sqlitedialect.New())
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func applySQLiteMigrations(t *testing.T, db *sql.DB) {
	t.Helper()

	root := services.GetCoreMigrationsFS()
	sqliteMigrations, err := fs.Sub(root, "data/sql/migrations/sqlite")
	if err != nil {
		t.Fatalf("resolve sqlite migrations: %v", err)
	}
	files, err := fs.Glob(sqliteMigrations, "*.up.sql")
	if err != nil {
		t.Fatalf("glob sqlite migrations: %v", err)
	}
	slices.Sort(files)

	for _, file := range files {
		if err := execSQLMigration(context.Background(), db, sqliteMigrations, filepath.Clean(file)); err != nil {
			t.Fatalf("apply migration %s: %v", file, err)
		}
	}
}

type unsupportedTestDialect struct {
	schema.BaseDialect

	name   dialect.Name
	tables *schema.Tables
}

func newUnsupportedTestDialect(name dialect.Name) *unsupportedTestDialect {
	d := &unsupportedTestDialect{name: name}
	d.tables = schema.NewTables(d)
	return d
}

func (d *unsupportedTestDialect) Init(*sql.DB) {}

func (d *unsupportedTestDialect) Name() dialect.Name {
	return d.name
}

func (d *unsupportedTestDialect) Features() feature.Feature {
	return 0
}

func (d *unsupportedTestDialect) Tables() *schema.Tables {
	return d.tables
}

func (d *unsupportedTestDialect) OnTable(*schema.Table) {}

func (d *unsupportedTestDialect) IdentQuote() byte {
	return '`'
}

func (d *unsupportedTestDialect) AppendSequence(b []byte, _ *schema.Table, _ *schema.Field) []byte {
	return b
}

func (d *unsupportedTestDialect) DefaultVarcharLen() int {
	return 0
}

func (d *unsupportedTestDialect) DefaultSchema() string {
	return ""
}

func (d *unsupportedTestDialect) AppendTime(b []byte, tm time.Time) []byte {
	return d.BaseDialect.AppendTime(b, tm)
}
