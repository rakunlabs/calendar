# Calendar UI

Svelte 5, Tailwind CSS 4, TypeScript and Vite. Use **pnpm** (version pinned in
`package.json`); do not mix package managers.

```sh
pnpm --dir _ui install --frozen-lockfile
pnpm --dir _ui dev
```

Open `http://localhost:5173/`. Vite proxies `/v1` to
`http://localhost:8080/calendar/v1` (and also accepts `/calendar/v1`).
Set `CALENDAR_API_URL` when the API runs elsewhere.

```sh
make ui-build
go run ./cmd/calendar
```

The service serves the compiled UI at `/calendar/` by default; `/` is outside that mount.
The server's `base_path` config changes the UI, API and Swagger mount together.
Leading/trailing slashes are normalized; `/` mounts at the root without a redirect.
No UI rebuild specific to that path is needed.
The frontend uses `base: './'` and relative API paths. A reverse proxy can mount
the UI at another directory as long as its `v1` API is exposed alongside it.
Directory URLs must end with `/`; subscription links resolve against that directory.
`_ui/embed.go` embeds `dist`, so **build the UI before Go builds, tests or Swagger
generation**. Dist is generated, not committed. Make targets and CI build it.

## Features

- Month, week, day and year views, selected-day agenda and mini-calendar navigation.
- Create, edit and delete events; recurring events offer single-occurrence or
  whole-series scope. Cancel one occurrence, reset an override, or restore a
  cancellation from the series exceptions list (also reachable from the sidebar).
- Group filtering, search, all-day and timed events, and disabled event visibility.
- Secondly through yearly presets (sub-daily for timed events only). Existing advanced RRULE/FUNC rules
  are retained when editing. Occurrences are expanded on the server, not in JS.
- Explicit time zones for editing; timed events display in the browser zone.
  All-day dates retain the event's own time zone. End timestamps are exclusive.
- An optional `Updated by` label sends `X-User`; this is an audit label, not login.
- Entity filtering is server-side; search, group visibility and disabled visibility
  remain local view controls. Entity names come from assignments, not a registry.
- Calendar tools manages group and individual-event assignments, including new
  entity names and disabled/off-window events. Combined legacy rules are displayed
  as group OR event and removed using their exact tuple, never an entity-wide delete.
- ICS import accepts files up to 9 MiB, with optional group, time zone and audit
  label. Import does not automatically assign events to an entity. The current
  handler reads options from query parameters, so the client sends them there as
  well as in multipart fields.
- ICS downloads and absolute subscription URLs have explicit entity/group scope,
  independent of local filters. Commas remain literal using explicit equality
  operators. Disabled events are always excluded. Downloads may select a year of
  event series; subscriptions omit the year to retain the rolling default window.
- The subscription URL remains selectable when clipboard access is unavailable.
  It must be reachable by the subscribing calendar provider and does not grant
  authorization. Assignment and event catalogs each have a 20,000-row browser cap.

The management UI adds no authentication. Protect it and the API at deployment.
The `/calendar/v1/occurrences` API accepts RFC3339 `from` and `to` parameters,
with an exclusive end and a maximum 400-day range / 20,000 returned instances.
Expansion supports SECONDLY through YEARLY RRULE frequencies and existing FUNC
holiday rules, subject to a work budget as well as the result limit. Limit failures
are errors, not partial results.

## Recurrence Editing

The editor fetches the master and sends versioned PUTs with its `updated_at`.
Recurrence-aware writes with a missing/stale version return HTTP 409; reload before
retrying. Occurrence results carry the master `id`, original `recurrence_id`, and
`is_override`; moved starts must not replace the original recurrence identity.
Single-occurrence changes replace/cancel an entry in the master's JSON
`recurrence.overrides`, not a separate event row. The recursive DTO is
`Event -> Recurrence -> OccurrenceOverride -> Event`.

Legacy PUTs omitting `recurrence` preserve stored metadata, including exclusions,
additions, overrides, lexical dates and timezone definitions. If the version is
also absent the service uses the version just read, with a database concurrency
check. The UI sends metadata explicitly and preserves advanced rules on
details-only edits. Series timing/rule changes are blocked while overrides,
EXDATE, or RDATE exist; remove exceptions in a separate update first.

Switching scope discards unsaved form edits. Reset/restore saves immediately and
closes the form without saving other edits. EXDATE/RDATE (including PERIOD) are
supported through ICS/API and preserved by the UI, but there are no controls to
author/remove them. Only single/series scope is available, not
`RANGE=THISANDFUTURE`. Zero-duration timed events are unsupported.

Imported DURATION survives details-only edits; changing timing replaces it with
explicit start/end metadata. Weeks/days are nominal calendar units applied before
exact hours/minutes/seconds, so `P1D` and `PT24H` can differ across DST. Leap second
`60` falls back to `59` for evaluation while retained lexical metadata preserves
`60` for export. Do not normalize untouched metadata through JavaScript dates.

The server supports custom VTIMEZONE STANDARD/DAYLIGHT definitions with DTSTART,
RDATE and a YEARLY RRULE subset (BYMONTH/BYMONTHDAY/BYDAY/BYSETPOS, INTERVAL, COUNT,
UTC UNTIL) over civil years 1..9999. Browser-unsupported zones and events carrying
imported definitions have read-only timing controls with UTC fallback display,
including recognized names whose imported rules may differ. Exports preserve custom definitions and generate explicit grouped RDATE
transitions for IANA zones over that range; DST definitions can be hundreds of
KiB. Conflicting definitions or custom/IANA scope collisions fail explicitly.
These capabilities are not a claim of complete RFC 5545 support.

Migration `internal/adapter/repository/migrations/04_recurrence.sql` runs
automatically at service startup and adds nullable JSONB metadata without
replacing existing events or relations. The domain's recurrence database
Value/Scan methods delegate to `types.JSON`; existing rows retain NULL metadata
until recurrence data is written. See the root README for the API contract.

## Checks

```sh
pnpm --dir _ui check
pnpm --dir _ui test
pnpm --dir _ui test:ui
pnpm --dir _ui test:basepath
pnpm --dir _ui build
go test -race ./...
```

The UI interaction suite uses mocked HTTP responses in Chromium, including
assignment pagination, exact deletes, multipart imports and ICS downloads.
Set `SCREENSHOT_DIR` when running `node tests/tools.browser.mjs` from `_ui` to
capture the tools dialog at desktop and mobile sizes. Live-service tests are
separate; mocked checks do not verify external calendar-provider connectivity.
Run `node tests/tools.live.browser.mjs` from `_ui` for a read-only browser smoke
check against the running service. It loads catalogs, queries occurrences, and
downloads ICS without creating or deleting service data.
