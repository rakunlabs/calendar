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
	db := container.Sqlx()

	for _, version := range []int{0, 1, 2} {
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
				for i, file := range []string{"01_events.sql", "02_relations.sql"}[:version] {
					content, err := migrationFS.ReadFile("migrations/" + file)
					require.NoError(t, err)
					_, err = tx.ExecContext(t.Context(), string(content))
					require.NoError(t, err)
					_, err = tx.ExecContext(t.Context(), `INSERT INTO calendar_migrations (path, version, migrated_on) VALUES ('/', $1, '2020-01-01Z')`, i+1)
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
				require.NoError(t, db.Select(&versions, "SELECT version FROM "+history+" WHERE path = '/' ORDER BY version"))
				require.Equal(t, []int{1, 2}, versions)
				var count int
				require.NoError(t, db.Get(&count, "SELECT count(*) FROM "+history))
				require.Equal(t, 2, count)
				require.NoError(t, db.Get(&count, "SELECT count(*) FROM "+quotedSchema+".calendar_events"))
				require.NoError(t, db.Get(&count, "SELECT count(*) FROM "+quotedSchema+".calendar_relations"))
				if version > 0 {
					var comment string
					require.NoError(t, db.Get(&comment, `SELECT col_description($1::regclass, attnum) FROM pg_attribute WHERE attrelid = $1::regclass AND attname = 'rrule'`, schema+".calendar_events"))
					require.Equal(t, "do not replay", comment)
					var migratedOn time.Time
					require.NoError(t, db.Get(&migratedOn, "SELECT migrated_on FROM "+history+" WHERE path = '/' AND version = 1"))
					require.True(t, migratedOn.Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)))
				}
			}
		})
	}
}
