package migrations

import (
	"context"
	"io/fs"
	"testing"
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
