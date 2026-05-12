package migrations

import (
	"io/fs"
	"strings"
	"testing"
)

func TestSourceDescriptorForDialect_Postgres(t *testing.T) {
	descriptor, err := SourceDescriptorForDialect(DialectPostgres)
	if err != nil {
		t.Fatalf("source descriptor: %v", err)
	}

	if descriptor.Name != SourceLabel || descriptor.Key != SourceLabel || descriptor.Label != SourceLabel {
		t.Fatalf("expected canonical source identifiers, got name=%q key=%q label=%q", descriptor.Name, descriptor.Key, descriptor.Label)
	}
	if descriptor.Dialect != DialectPostgres {
		t.Fatalf("expected postgres dialect, got %q", descriptor.Dialect)
	}
	if descriptor.Root == nil {
		t.Fatalf("expected root filesystem")
	}
	if len(descriptor.ValidationTargets) != 1 || descriptor.ValidationTargets[0] != DialectPostgres {
		t.Fatalf("expected postgres-only validation target, got %v", descriptor.ValidationTargets)
	}
	if matches, err := fs.Glob(descriptor.Root, "*.up.sql"); err != nil || len(matches) == 0 {
		t.Fatalf("expected postgres migration files, matches=%v err=%v", matches, err)
	}
}

func TestSourceDescriptorForDialect_SQLite(t *testing.T) {
	descriptor, err := SourceDescriptorForDialect(DialectSQLite)
	if err != nil {
		t.Fatalf("source descriptor: %v", err)
	}

	if descriptor.Dialect != DialectSQLite {
		t.Fatalf("expected sqlite dialect, got %q", descriptor.Dialect)
	}
	if len(descriptor.ValidationTargets) != 1 || descriptor.ValidationTargets[0] != DialectSQLite {
		t.Fatalf("expected sqlite-only validation target, got %v", descriptor.ValidationTargets)
	}
	if matches, err := fs.Glob(descriptor.Root, "*.up.sql"); err != nil || len(matches) == 0 {
		t.Fatalf("expected sqlite migration files, matches=%v err=%v", matches, err)
	}
}

func TestSourceDescriptorForDialect_Aliases(t *testing.T) {
	tests := map[string]string{
		"pg":         DialectPostgres,
		"postgresql": DialectPostgres,
		"sqlite3":    DialectSQLite,
	}
	for input, expected := range tests {
		t.Run(input, func(t *testing.T) {
			descriptor, err := SourceDescriptorForDialect(input)
			if err != nil {
				t.Fatalf("source descriptor: %v", err)
			}
			if descriptor.Dialect != expected {
				t.Fatalf("expected %q, got %q", expected, descriptor.Dialect)
			}
		})
	}
}

func TestSourceDescriptorForDialect_RejectsUnknownDialect(t *testing.T) {
	_, err := SourceDescriptorForDialect("mysql")
	if err == nil {
		t.Fatalf("expected unknown dialect error")
	}
	if !strings.Contains(err.Error(), `unsupported dialect "mysql"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSourceDescriptorForDialect_ValidationTargetsAreDefensive(t *testing.T) {
	descriptor, err := SourceDescriptorForDialect("pg")
	if err != nil {
		t.Fatalf("source descriptor: %v", err)
	}
	descriptor.ValidationTargets[0] = "mutated"

	next, err := SourceDescriptorForDialect("pg")
	if err != nil {
		t.Fatalf("source descriptor: %v", err)
	}
	if next.ValidationTargets[0] == "mutated" {
		t.Fatalf("expected validation targets to be defensive")
	}
}
