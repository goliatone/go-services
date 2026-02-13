package services

import (
	"embed"
	"io/fs"
)

// migrationsFS contains the full go-services SQL migration tree, including
// dialect alternatives under data/sql/migrations/sqlite.
//
//go:embed data/sql/migrations/*.sql data/sql/migrations/sqlite/*.sql
var migrationsFS embed.FS

// GetMigrationsFS returns the full embedded migration tree.
func GetMigrationsFS() fs.FS {
	return migrationsFS
}

// GetCoreMigrationsFS returns the default core services schema migration tree.
//
// v1 intentionally ships only core migrations; segmented getters are deferred.
func GetCoreMigrationsFS() fs.FS {
	return migrationsFS
}
