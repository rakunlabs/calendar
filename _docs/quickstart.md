# Getting Started

Calendar runs as a Go service with an embedded browser UI.
**PostgreSQL is required**, including when running a prebuilt binary. Node.js and
pnpm are needed only when building or developing the UI from source.

## Install

### Binary

1. Download the archive for your operating system and architecture from the [latest release](https://github.com/rakunlabs/calendar/releases/latest).
2. Extract the binary and provision a PostgreSQL database and schema.
3. Create a configuration file using the example below, setting both database connection strings.
4. Start the extracted binary with the configuration path:

```sh
CONFIG_FILE=/path/to/calendar.yaml ./calendar
```

Open `http://localhost:8080/calendar/`. The service applies pending database
migrations before starting the HTTP server; the migration connection needs
permission to create and alter the application's tables.

## Configuration

Set `CONFIG_FILE` to the configuration path, or use
`calendar.[toml|yaml|yml|json]` in the current working directory.
The following example matches the local development database:

```yaml
log_level: info
port: 8080

db_type: pgx
db_datasource: postgres://postgres@localhost:5432/postgres?sslmode=disable
db_schema: public

migrate:
  db_datasource: postgres://postgres@localhost:5432/postgres?sslmode=disable
  db_type: pgx
  db_schema: public
  db_table: calendar_migrations
```

The application and migration connections are configured separately; neither
datasource has a default. They can use different database credentials, but must
point to the same application database and schema. Use password authentication
and an appropriate TLS configuration for your PostgreSQL deployment rather than
copying the local connection strings into production.

## Run From Source

From the repository root, with Go matching `go.mod`, Docker with Compose,
Node.js 22, and the pnpm version pinned in `_ui/package.json` installed:

```sh
make env
make run
```

`make env` starts PostgreSQL on port 5432 using `env/docker-compose.yaml`.
The checked-in `calendar.yaml` configures both connections for that database.
`make run` installs UI dependencies with the frozen lockfile, builds the embedded
UI, and runs `go run cmd/calendar/main.go`. Startup also runs migrations.
Wait for the database to be ready before starting the service.

Open `http://localhost:8080/calendar/`. For frontend development, run
`make ui-dev` in a second terminal and open `http://localhost:5173/calendar/`;
Vite proxies API requests to the Go service on port 8080. See the
[UI README](https://github.com/rakunlabs/calendar/blob/main/_ui/README.md)
for build and test commands.

::: warning Development Database
The Compose database uses `POSTGRES_HOST_AUTH_METHOD=trust` and publishes port
5432. It is for local development only; do not expose it to untrusted networks.
`make env-down` runs `docker compose down --volumes` and removes the local
database volumes and their data. It is not a data-preserving stop command.
:::

## Before Sharing

Calendar adds no authentication to the UI or API. Protect both with deployment-level
access controls. `Updated by` sends an optional `X-User` audit label, not a login
or verified identity. Entity filters and subscription URLs do not enforce authorization.

Continue with the [User Guide](/ui-guide) to create events, assign calendars,
and choose the scope of ICS downloads and subscriptions.
