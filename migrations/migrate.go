package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed postgres/*.sql sqlite/*.sql mysql/*.sql
var embedMigrations embed.FS

// NewProvider creates a new goose.Provider configured for stripeflow migrations.
func NewProvider(db *sql.DB, dialect string) (*goose.Provider, error) {
	gooseDialect, err := parseDialect(dialect)
	if err != nil {
		return nil, err
	}
	fsys, err := fs.Sub(embedMigrations, migrationDir(dialect))
	if err != nil {
		return nil, fmt.Errorf("failed to get migration subdirectory: %w", err)
	}
	return goose.NewProvider(gooseDialect, db, fsys,
		goose.WithTableName("stripeflow_goose_db_version"),
	)
}

// MigrateUp applies all pending migrations for the given dialect.
func MigrateUp(db *sql.DB, dialect string) error {
	provider, err := NewProvider(db, dialect)
	if err != nil {
		return fmt.Errorf("failed to create migration provider: %w", err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		return fmt.Errorf("failed to run migrations up: %w", err)
	}
	return nil
}

// MigrateDown rolls back all migrations for the given dialect.
func MigrateDown(db *sql.DB, dialect string) error {
	provider, err := NewProvider(db, dialect)
	if err != nil {
		return fmt.Errorf("failed to create migration provider: %w", err)
	}
	if _, err := provider.DownTo(context.Background(), 0); err != nil {
		return fmt.Errorf("failed to run migrations down: %w", err)
	}
	return nil
}

func parseDialect(dialect string) (goose.Dialect, error) {
	switch dialect {
	case "postgres":
		return goose.DialectPostgres, nil
	case "mysql":
		return goose.DialectMySQL, nil
	case "sqlite", "sqlite3":
		return goose.DialectSQLite3, nil
	default:
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}
}

func migrationDir(dialect string) string {
	if dialect == "sqlite" || dialect == "sqlite3" {
		return "sqlite"
	}
	return dialect
}
