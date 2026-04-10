package migrations

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

//go:embed postgres/*.sql sqlite/*.sql mysql/*.sql
var embedMigrations embed.FS

// MigrateUp applies all pending migrations for the given dialect
func MigrateUp(db *sql.DB, dialect string) error {
	goose.SetBaseFS(embedMigrations)
	if err := goose.SetDialect(dialect); err != nil {
		return fmt.Errorf("failed to set dialect: %w", err)
	}

	dir := dialect
	if dialect == "sqlite3" {
		dir = "sqlite"
	}

	if err := goose.Up(db, dir); err != nil {
		return fmt.Errorf("failed to run migrations up: %w", err)
	}

	return nil
}

// MigrateDown rolls back all migrations for the given dialect
func MigrateDown(db *sql.DB, dialect string) error {
	goose.SetBaseFS(embedMigrations)
	if err := goose.SetDialect(dialect); err != nil {
		return fmt.Errorf("failed to set dialect: %w", err)
	}

	dir := dialect
	if dialect == "sqlite3" {
		dir = "sqlite"
	}

	if err := goose.DownTo(db, dir, 0); err != nil {
		return fmt.Errorf("failed to run migrations down: %w", err)
	}

	return nil
}
