package migrations

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/uptrace/bun"
)

var requiredSQLTables = []string{
	"service_activity_entries",
	"service_connections",
	"service_credentials",
	"service_events",
	"service_grant_events",
	"service_grant_snapshots",
	"service_identity_bindings",
	"service_installations",
	"service_lifecycle_outbox",
	"service_mapping_specs",
	"service_notification_dispatches",
	"service_rate_limit_state",
	"service_subscriptions",
	"service_sync_bindings",
	"service_sync_change_log",
	"service_sync_checkpoints",
	"service_sync_conflicts",
	"service_sync_cursors",
	"service_sync_job_idempotency",
	"service_sync_jobs",
	"service_webhook_deliveries",
}

var requiredOAuthStorageTables = []string{
	"service_connections",
	"service_credentials",
	"service_grant_events",
	"service_grant_snapshots",
}

func RequiredSQLTables() []string {
	return append([]string(nil), requiredSQLTables...)
}

func RequiredOAuthStorageTables() []string {
	return append([]string(nil), requiredOAuthStorageTables...)
}

func VerifySQLSchema(ctx context.Context, db *bun.DB) error {
	return verifyRequiredTables(ctx, db, requiredSQLTables)
}

func VerifyOAuthStorageSchema(ctx context.Context, db *bun.DB) error {
	return verifyRequiredTables(ctx, db, requiredOAuthStorageTables)
}

func verifyRequiredTables(ctx context.Context, db *bun.DB, required []string) error {
	if db == nil {
		return fmt.Errorf("migrations: database handle is nil")
	}

	dialect, err := normalizeDialect(db.Dialect().Name().String())
	if err != nil {
		return err
	}

	existing, err := existingTables(ctx, db, dialect)
	if err != nil {
		return err
	}

	missing := make([]string, 0)
	for _, table := range required {
		if _, ok := existing[table]; !ok {
			missing = append(missing, table)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	sort.Strings(missing)
	return fmt.Errorf("migrations: missing required tables: %s", strings.Join(missing, ", "))
}

func existingTables(ctx context.Context, db *bun.DB, dialect string) (map[string]struct{}, error) {
	var query string
	switch dialect {
	case DialectSQLite:
		query = `SELECT name FROM sqlite_master WHERE type = 'table'`
	case DialectPostgres:
		query = `
			SELECT table_name
			FROM information_schema.tables
			WHERE table_schema = current_schema()
				AND table_type = 'BASE TABLE'
		`
	default:
		return nil, fmt.Errorf("migrations: unsupported dialect %q", dialect)
	}

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("migrations: query %s tables: %w", dialect, err)
	}
	defer func() { _ = rows.Close() }()

	tables := make(map[string]struct{})
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, fmt.Errorf("migrations: scan %s table: %w", dialect, err)
		}
		tables[table] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrations: read %s tables: %w", dialect, err)
	}

	return tables, nil
}

func normalizeDialect(dialect string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(dialect)) {
	case "pg", "postgres", "postgresql":
		return DialectPostgres, nil
	case "sqlite", "sqlite3":
		return DialectSQLite, nil
	default:
		return "", fmt.Errorf("migrations: unsupported dialect %q", dialect)
	}
}
