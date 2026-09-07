# Calendar 🗓️

[![License](https://img.shields.io/github/license/rakunlabs/calendar?color=blue&style=flat-square)](https://raw.githubusercontent.com/rakunlabs/calendar/main/LICENSE)
[![Coverage](https://img.shields.io/sonar/coverage/worldline-go_calendar?logo=sonarcloud&server=https%3A%2F%2Fsonarcloud.io&style=flat-square)](https://sonarcloud.io/summary/overall?id=worldline-go_calendar)
[![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/rakunlabs/calendar/test.yml?branch=main&logo=github&style=flat-square&label=ci)](https://github.com/rakunlabs/calendar/actions)
[![Web](https://img.shields.io/badge/web-document-blueviolet?style=flat-square)](https://rakunlabs.github.io/calendar/)

This service that provides information about holidays in a given country or special code.

## Development

Use `make` to show help and create env, run tests, etc. Building the embedded UI
requires Node.js 22 and pnpm (the version is pinned in `_ui/package.json`).

```sh
# Run compose-file to open postgresql in local
make env
# Start the service, it default reads `calendar.[toml|yaml|yml|json]` or use `CONFIG_FILE` env value for file path.
make run
```

Open **http://localhost:8080/calendar/** for the calendar management UI. It has
month, day, and year views, event creation/editing/deletion, recurrence, and group
filters. The Svelte 5 + Tailwind CSS app is embedded in the Go binary and served
through Ada's folder handler; no frontend runtime server is required in production.

For UI development, run `make ui-dev` alongside the Go service. Vite's development
URL is http://localhost:5173/calendar/ and API calls are proxied to port 8080.
See [`_ui/README.md`](_ui/README.md) for build, test and timezone details.

## References

- https://datatracker.ietf.org/doc/html/rfc5545
- https://en.wikipedia.org/wiki/List_of_tz_database_time_zones
- https://en.wikipedia.org/wiki/ISO_3166-1_alpha-3
- https://www.thunderbird.net/en-US/calendar/holidays/
- https://calendars.icloud.com/holidays/tr_tr.ics/
- https://github.com/ics-tools/viewer
