package migrations

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	services "github.com/goliatone/go-services"
)

const (
	DialectPostgres = "postgres"
	DialectSQLite   = "sqlite"
)

type FilesystemSpec struct {
	Dialect string
	Path    string
	FS      fs.FS
}

type Registration struct {
	SourceLabel       string
	ValidationTargets []string
	Filesystems       []FilesystemSpec
}

type RegisterFunc func(ctx context.Context, dialect string, sourceLabel string, fsys fs.FS) error

type Option func(*Registration)

func WithDialectSourceLabel(label string) Option {
	return func(r *Registration) {
		trimmed := strings.TrimSpace(label)
		if trimmed != "" {
			r.SourceLabel = trimmed
		}
	}
}

func WithValidationTargets(targets ...string) Option {
	return func(r *Registration) {
		if len(targets) == 0 {
			return
		}
		next := make([]string, 0, len(targets))
		for _, target := range targets {
			trimmed := strings.TrimSpace(strings.ToLower(target))
			if trimmed == "" {
				continue
			}
			next = append(next, trimmed)
		}
		if len(next) == 0 {
			return
		}
		r.ValidationTargets = dedupe(next)
	}
}

func WithFilesystems(filesystems ...FilesystemSpec) Option {
	return func(r *Registration) {
		if len(filesystems) == 0 {
			return
		}
		copied := make([]FilesystemSpec, 0, len(filesystems))
		for _, fsys := range filesystems {
			dialect := strings.TrimSpace(strings.ToLower(fsys.Dialect))
			if dialect == "" || fsys.FS == nil {
				continue
			}
			copied = append(copied, FilesystemSpec{
				Dialect: dialect,
				Path:    fsys.Path,
				FS:      fsys.FS,
			})
		}
		if len(copied) == 0 {
			return
		}
		r.Filesystems = copied
	}
}

func Filesystems(sources ...fs.FS) ([]FilesystemSpec, error) {
	root := services.GetCoreMigrationsFS()
	if len(sources) > 0 && sources[0] != nil {
		root = sources[0]
	}

	base, basePath, err := migrationsRoot(root)
	if err != nil {
		return nil, err
	}
	sqliteFS, err := fs.Sub(base, "sqlite")
	if err != nil {
		return nil, fmt.Errorf("migrations: resolve sqlite filesystem: %w", err)
	}

	filesystems := []FilesystemSpec{
		{
			Dialect: DialectPostgres,
			Path:    basePath,
			FS:      base,
		},
		{
			Dialect: DialectSQLite,
			Path:    pathJoin(basePath, "sqlite"),
			FS:      sqliteFS,
		},
	}

	for _, fsys := range filesystems {
		matches, globErr := fs.Glob(fsys.FS, "*.up.sql")
		if globErr != nil {
			return nil, fmt.Errorf("migrations: glob %s %s: %w", fsys.Dialect, fsys.Path, globErr)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("migrations: %s filesystem %q has no *.up.sql files", fsys.Dialect, fsys.Path)
		}
	}

	return filesystems, nil
}

func Register(ctx context.Context, registerFn RegisterFunc, opts ...Option) (Registration, error) {
	reg := Registration{
		SourceLabel:       "go-services",
		ValidationTargets: []string{DialectPostgres, DialectSQLite},
	}

	filesystems, err := Filesystems()
	if err != nil {
		return reg, err
	}
	reg.Filesystems = filesystems

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&reg)
	}

	if len(reg.ValidationTargets) == 0 {
		return reg, fmt.Errorf("migrations: validation targets are required")
	}
	if strings.TrimSpace(reg.SourceLabel) == "" {
		return reg, fmt.Errorf("migrations: source label is required")
	}
	if len(reg.Filesystems) == 0 {
		return reg, fmt.Errorf("migrations: filesystems are required")
	}
	if registerFn == nil {
		return reg, fmt.Errorf("migrations: register function is required")
	}

	targets := dedupe(reg.ValidationTargets)
	for _, fsys := range reg.Filesystems {
		if !slices.Contains(targets, fsys.Dialect) {
			continue
		}
		if fsys.FS == nil {
			return reg, fmt.Errorf("migrations: filesystem for %s is nil", fsys.Dialect)
		}
		if err := registerFn(ctx, fsys.Dialect, reg.SourceLabel, fsys.FS); err != nil {
			return reg, fmt.Errorf("migrations: register %s (%s): %w", fsys.Dialect, fsys.Path, err)
		}
	}

	return reg, nil
}

func migrationsRoot(root fs.FS) (fs.FS, string, error) {
	sub, err := fs.Sub(root, "data/sql/migrations")
	if err == nil {
		return sub, "data/sql/migrations", nil
	}

	entries, readErr := fs.ReadDir(root, ".")
	if readErr == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
				return root, ".", nil
			}
		}
	}

	return nil, "", fmt.Errorf("migrations: data/sql/migrations not found: %w", err)
}

func dedupe(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(strings.ToLower(value))
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func pathJoin(base string, suffix string) string {
	if base == "." {
		return suffix
	}
	return strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(suffix, "/")
}
