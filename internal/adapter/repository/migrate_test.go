package repository

import (
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rakunlabs/calendar/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/worldline-go/test/container/containerpostgres"
)

func TestMigrateDBEmptyDatasource(t *testing.T) {
	require.EqualError(t, MigrateDB(t.Context(), &config.Config{}), "migrate database datasource is empty")
}

func TestMigrateDB(t *testing.T) {
	container := containerpostgres.New(t)
	defer container.Stop(t)
	db := container.Sql()

	for _, version := range []int{0, 1, 2, 3} {
		t.Run(fmt.Sprintf("existing_version_%d", version), func(t *testing.T) {
			schema := fmt.Sprintf("migration_test_%d", version)
			quotedSchema := pgx.Identifier{schema}.Sanitize()
			_, err := db.ExecContext(t.Context(), "CREATE SCHEMA "+quotedSchema)
			require.NoError(t, err)
			t.Cleanup(func() {
				_, err := db.Exec("DROP SCHEMA " + quotedSchema + " CASCADE")
				require.NoError(t, err)
			})
			cfg := &config.Config{Migrate: config.Migrate{
				DBType: "pgx", DBDatasource: container.DSN(), DBSchema: schema, DBTable: "calendar_migrations",
			}}
			history := quotedSchema + ".calendar_migrations"
			if version > 0 {
				tx, err := db.BeginTx(t.Context(), nil)
				require.NoError(t, err)
				defer tx.Rollback()
				_, err = tx.ExecContext(t.Context(), "SET LOCAL search_path = "+quotedSchema)
				require.NoError(t, err)
				// Exact history DDL and paths written by igmigrator v2.4.0.
				_, err = tx.ExecContext(t.Context(), `CREATE TABLE calendar_migrations (
					path VARCHAR(1000) NOT NULL DEFAULT '/',
					version INT,
					migrated_on TIMESTAMPTZ NOT NULL DEFAULT NOW(),
					PRIMARY KEY (path, version)
				)`)
				require.NoError(t, err)
				for i, file := range []string{"01_events.sql", "02_relations.sql", "03_relation_uniqueness.sql"}[:version] {
					content, err := migrationFS.ReadFile("migrations/" + file)
					require.NoError(t, err)
					_, err = tx.ExecContext(t.Context(), string(content))
					require.NoError(t, err)
					_, err = tx.ExecContext(t.Context(), `INSERT INTO calendar_migrations (path, version, migrated_on) VALUES ('/', $1, '2020-01-01Z')`, i+1)
					require.NoError(t, err)
				}
				if version == 2 {
					_, err = tx.ExecContext(t.Context(), `
						INSERT INTO calendar_events (id, name, date_from, date_to) VALUES ('e', 'event', NOW(), NOW());
						INSERT INTO calendar_relations (entity, event_group, event_id, updated_at, updated_by) VALUES
						('x', 'g', NULL, '2020-01-01Z', 'old'),
						('x', 'g', NULL, '2026-01-01Z', 'latest'),
						('x', 'g', NULL, NULL, 'unknown'),
						('x', NULL, 'e', '2020-01-01Z', 'old'),
						('x', NULL, 'e', '2026-01-01Z', 'latest'),
						('x', NULL, NULL, '2020-01-01Z', 'old'),
						('x', NULL, NULL, '2026-01-01Z', 'latest'),
						('x', 'g', 'e', '2026-01-01Z', 'latest')`)
					require.NoError(t, err)
				}
				if version != 2 {
					_, err = tx.ExecContext(t.Context(), `INSERT INTO calendar_events (id, name, date_from, date_to) VALUES ('e', 'event', NOW(), NOW())`)
					require.NoError(t, err)
				}
				// The shipped SQL is mostly idempotent; this comment detects replay.
				_, err = tx.ExecContext(t.Context(), `COMMENT ON COLUMN calendar_events.rrule IS 'do not replay'`)
				require.NoError(t, err)
				require.NoError(t, tx.Commit())
			}

			for range 2 {
				require.NoError(t, MigrateDB(t.Context(), cfg))
				var versions []int
				rows, err := db.QueryContext(t.Context(), "SELECT version FROM "+history+" WHERE path = '/' ORDER BY version")
				require.NoError(t, err)
				defer rows.Close()
				for rows.Next() {
					var version int
					require.NoError(t, rows.Scan(&version))
					versions = append(versions, version)
				}
				require.NoError(t, rows.Err())
				require.NoError(t, rows.Close())
				require.Equal(t, []int{1, 2, 3, 4}, versions)
				var count int
				require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+history).Scan(&count))
				require.Equal(t, 4, count)
				require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+quotedSchema+".calendar_events").Scan(&count))
				require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+quotedSchema+".calendar_relations").Scan(&count))
				if version == 2 {
					require.Equal(t, 4, count)
					require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+quotedSchema+".calendar_relations WHERE updated_by = 'latest'").Scan(&count))
					require.Equal(t, 4, count)
				}
				if version > 0 {
					require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+quotedSchema+".calendar_events WHERE id = 'e' AND recurrence IS NULL").Scan(&count))
					require.Equal(t, 1, count)
					var comment string
					require.NoError(t, db.QueryRowContext(t.Context(), `SELECT col_description($1::regclass, attnum) FROM pg_attribute WHERE attrelid = $1::regclass AND attname = 'rrule'`, schema+".calendar_events").Scan(&comment))
					require.Equal(t, "do not replay", comment)
					var migratedOn time.Time
					require.NoError(t, db.QueryRowContext(t.Context(), "SELECT migrated_on FROM "+history+" WHERE path = '/' AND version = 1").Scan(&migratedOn))
					require.True(t, migratedOn.Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)))
				}
			}
		})
	}
}
