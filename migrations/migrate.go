package migrations

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

//go:embed postgres/*.sql sqlite/*.sql mysql/*.sql
var embedMigrations embed.FS

// MigrateUp applies all pending migrations for the given dialect.
func MigrateUp(db *sql.DB, dialect string) error {
	goose.SetBaseFS(embedMigrations)
	if err := goose.SetDialect(gooseDialect(dialect)); err != nil {
		return fmt.Errorf("failed to set dialect: %w", err)
	}
	if err := goose.Up(db, migrationDir(dialect)); err != nil {
		return fmt.Errorf("failed to run migrations up: %w", err)
	}
	return nil
}

// MigrateDown rolls back all migrations for the given dialect.
func MigrateDown(db *sql.DB, dialect string) error {
	goose.SetBaseFS(embedMigrations)
	if err := goose.SetDialect(gooseDialect(dialect)); err != nil {
		return fmt.Errorf("failed to set dialect: %w", err)
	}
	if err := goose.DownTo(db, migrationDir(dialect), 0); err != nil {
		return fmt.Errorf("failed to run migrations down: %w", err)
	}
	return nil
}

func gooseDialect(dialect string) string {
	if dialect == "sqlite" {
		return "sqlite3"
	}
	return dialect
}

func migrationDir(dialect string) string {
	if dialect == "sqlite" || dialect == "sqlite3" {
		return "sqlite"
	}
	return dialect
}
