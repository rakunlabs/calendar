# Calendar 🗓️

[![License](https://img.shields.io/github/license/rakunlabs/calendar?color=blue&style=flat-square)](https://raw.githubusercontent.com/rakunlabs/calendar/main/LICENSE)
[![Coverage](https://img.shields.io/sonar/coverage/rakunlabs_calendar?logo=sonarcloud&server=https%3A%2F%2Fsonarcloud.io&style=flat-square)](https://sonarcloud.io/summary/overall?id=rakunlabs_calendar)
[![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/rakunlabs/calendar/test.yml?branch=main&logo=github&style=flat-square&label=ci)](https://github.com/rakunlabs/calendar/actions)
[![Web](https://img.shields.io/badge/web-document-blueviolet?style=flat-square)](https://rakunlabs.github.io/calendar/)

Calendar is a self-hosted calendar and holiday service with a browser-based management
UI, a Go API, and iCalendar (ICS) support. Plan events, maintain recurring holidays,
organize calendars for different entities, and share feeds with calendar clients.

![Calendar month view with group filters and a selected-day agenda](_docs/public/screenshots/calendar-month-1440.png)

## Features

- Month, week, day, and year views with a selected-day agenda and mobile layout.
- Create, edit, and delete all-day or timed events with explicit time zones.
- Secondly through yearly recurrence, plus special holiday rules.
- ICS/API recurrence sets with EXDATE, RDATE (including PERIOD), and same-UID
  occurrence overrides; the UI can edit or cancel one occurrence or edit a series.
- Organize events into groups and assign groups or individual events to entities.
- Import ICS files, download calendars, and copy subscription URLs with explicit scope.
- An embedded UI served by the Go binary; no frontend runtime server is needed in production.

**Start here:** [Quickstart](https://rakunlabs.github.io/calendar/quickstart) ·
[User guide](https://rakunlabs.github.io/calendar/ui-guide) ·
[Releases](https://github.com/rakunlabs/calendar/releases/latest) ·
[UI development](_ui/README.md)

PostgreSQL is required. The service runs database migrations on startup.
Migration `04_recurrence.sql` automatically adds a nullable `recurrence` JSONB
column. Existing events and relations are preserved; existing events start with
NULL metadata. Recurrence metadata uses `github.com/worldline-go/types.JSON` for
database serialization and scanning, stored atomically with the master event.
The UI and API have **no built-in authentication**: protect them at deployment.
The optional `Updated by` / `X-User` value is an audit label, not a verified identity
or an access control mechanism. Entity filters and subscription URLs do not restrict access.

## Recurrence and API Contract

Calendar implements a supported subset of iCalendar, not complete RFC 5545 support.
ICS import/export supports EXDATE, RDATE dates/date-times and PERIOD values
(start/end or start/DURATION), and detached `RECURRENCE-ID` overrides and
`STATUS:CANCELLED` exceptions sharing the master's UID. These are stored in one
master row, not competing rows with the same UID.

The event JSON `recurrence` object contains optional `start`, `end`, `duration`,
`exdates`, `rdates`, `overrides`, and raw `VTIMEZONE` strings in `timezones`.
Calendar dates use `{ "value": "20260908T090000Z" }` with optional `tzid` and
`type` (`DATE` or `DATE-TIME`); values retain their ICS lexical representation.
Each override has an original `recurrence_id` and either `cancelled: true` or an
`event` replacement. Expanded occurrences retain the master `id` and expose
`recurrence_id` and `is_override`; a moved occurrence's identity is its original
scheduled start, not its replacement start. Fetch the master before editing it;
do not PUT an expanded occurrence as though it were the series.
An occurrence's `recurrence` contains only its effective start/end or duration,
not the master's full exception set or timezone definitions.

Recurrence-aware `PUT /calendar/v1/events/{id}` updates must include the master's
latest `updated_at`. Missing or stale versions return HTTP **409 Conflict**;
reload the master before retrying. Legacy PUTs that omit `recurrence` preserve
stored metadata; if they also omit `updated_at`, the server uses the version it
just read and still checks for concurrent writes. Omitting metadata does not
clear exceptions. While exceptions exist, changing series dates, all-day status,
timezone, duration, or repeat rule is rejected. Remove exceptions in a separate
versioned update before changing series timing.

Positive ICS `DURATION` supports weeks, days, hours, minutes, and seconds (not
months or years). Weeks/days are nominal calendar days, applied before exact
hours/minutes/seconds: `P1D` can differ from `PT24H` across daylight-saving changes.
All-day spans also retain nominal day lengths. `DTEND` and `DURATION` are mutually
exclusive. Zero-duration timed events and `RANGE=THISANDFUTURE` are not supported.
Leap second `60` is evaluated as second `59`, while retained lexical dates and
rules preserve the original value for export.

IANA zones and calendar-scoped custom `VTIMEZONE` definitions are supported.
Custom STANDARD/DAYLIGHT observances accept DTSTART, RDATE and a YEARLY RRULE
subset with BYMONTH/BYMONTHDAY/BYDAY/BYSETPOS, INTERVAL, COUNT, and UTC UNTIL.
Unsupported observance rules fail explicitly. Custom transitions cover civil
years **1..9999**, not an unbounded timeline. Export preserves custom definitions
and emits missing IANA definitions as explicit grouped RDATE transitions over
that range, so DST zone definitions can be hundreds of KiB. Conflicting custom
definitions, or a custom/IANA TZID scope collision across exported events, fail
instead of silently changing another event's timezone.

Server expansion supports SECONDLY through YEARLY RRULE frequencies and FUNC
holiday rules, with a bounded expansion-work budget. High-frequency or expensive
rules can exceed that budget even in a valid window. The occurrences endpoint
accepts RFC3339 `from`/`to`, an exclusive end, at most **400 days** per request and
**20,000 results**; exceeding limits returns an error, not a truncated calendar.
Ordinary ICS series exports select by effective occurrence without materializing
every instance; FUNC/multiple-rule sets may be materialized for the selected years.
The UI offers secondly through yearly presets (sub-daily for timed events only) and single-occurrence or
whole-series scope. EXDATE/RDATE authoring and removal require ICS or the API.

## Development

Run these commands from the repository root. You need Go matching `go.mod`,
Docker with Compose, Node.js 22, and pnpm (the version is pinned in `_ui/package.json`).
Use `make` to list available targets.

```sh
# Start the local PostgreSQL development database on port 5432.
make env
# Install/build the UI and run Go using the checked-in calendar.yaml.
make run
```

Open **http://localhost:8080/calendar/**. Configuration is read from
`calendar.[toml|yaml|yml|json]` in the current directory, or the path in `CONFIG_FILE`.

Set `base_path` to mount the UI, API, and Swagger under another path:

```yaml
base_path: /calendar
```

The default is `/calendar`. `calendar`, `/calendar`, `calendar/`, and `/calendar/`
are equivalent; surrounding whitespace is trimmed. Nested paths such as
`/tools/team/calendar` work too. Use `/` to serve at the root. The UI directory
redirect keeps its trailing slash so relative API and asset URLs resolve correctly.

> The Compose database uses trust authentication and is for local development only.
> Do not expose it to untrusted networks. `make env-down` runs Compose with
> `--volumes`, removing the local database volumes and their data.

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
