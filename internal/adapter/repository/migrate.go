package repository

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jmoiron/sqlx"
	"github.com/rakunlabs/muz"

	"github.com/rakunlabs/calendar/internal/config"
)

//go:embed migrations/*
var migrationFS embed.FS

func MigrateDB(ctx context.Context, cfg *config.Config) error {
	if cfg.Migrate.DBDatasource == "" {
		return fmt.Errorf("migrate database datasource is empty")
	}

	db, err := sqlx.ConnectContext(ctx, cfg.Migrate.DBType, cfg.Migrate.DBDatasource)
	if err != nil {
		return fmt.Errorf("migrate database connect: %w", err)
	}

	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migrations: %w", err)
	}
	defer tx.Rollback()

	if schema := strings.TrimSpace(cfg.Migrate.DBSchema); schema != "" {
		if _, err := tx.ExecContext(ctx, "SET LOCAL search_path = "+pgx.Identifier{schema}.Sanitize()); err != nil {
			return fmt.Errorf("set migration schema: %w", err)
		}
	}
	table := strings.TrimSpace(cfg.Migrate.DBTable)
	if table == "" {
		table = "calendar_migrations"
	}
	table = pgx.Identifier{table}.Sanitize()
	// Serialize creation as well as execution, including on a fresh database.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext(current_schema()), hashtext($1))`, table); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	// Keep igmigrator's shipped history schema and '/'-rooted paths. muz's
	// SQLDriver uses incompatible columns and would replay existing migrations.
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+table+` (
		path VARCHAR(1000) NOT NULL DEFAULT '/',
		version INT,
		migrated_on TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (path, version)
	)`); err != nil {
		return fmt.Errorf("create migration history: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "LOCK TABLE "+table+" IN ACCESS EXCLUSIVE MODE"); err != nil {
		return fmt.Errorf("lock migration history: %w", err)
	}

	migration := muz.Migrate{FS: migrationFS, Path: "migrations", Extension: ".sql", Skip: []string{"**/archive/**"}}
	for directory, err := range migration.Migrations() {
		if err != nil {
			return fmt.Errorf("read migrations: %w", err)
		}
		migrationPath := path.Join("/", directory.Dir)
		var previous sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT MAX(version) FROM "+table+" WHERE path = $1", migrationPath).Scan(&previous); err != nil {
			return fmt.Errorf("read migration history: %w", err)
		}
		for _, file := range directory.Files {
			if int64(file.Version) <= previous.Int64 {
				continue
			}
			content, err := directory.ReadFile(file.Path)
			if err != nil {
				return fmt.Errorf("read migration %s: %w", file.Path, err)
			}
			if _, err := tx.ExecContext(ctx, string(content)); err != nil {
				return fmt.Errorf("run migration %s: %w", file.Path, err)
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO "+table+" (path, version) VALUES ($1, $2)", migrationPath, file.Version); err != nil {
				return fmt.Errorf("record migration %s: %w", file.Path, err)
			}
			slog.InfoContext(ctx, "ran migration", "path", migrationPath, "previous_version", previous.Int64, "new_version", file.Version)
			previous.Int64 = int64(file.Version)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}
