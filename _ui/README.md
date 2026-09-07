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

The service serves the compiled UI at `/calendar/`; `/` redirects there.
The frontend uses `base: './'` and relative API paths. A reverse proxy can mount
the UI at another directory as long as its `v1` API is exposed alongside it.
Directory URLs must end with `/`; subscription links resolve against that directory.
`_ui/embed.go` embeds `dist`, so **build the UI before Go builds, tests or Swagger
generation**. Dist is generated, not committed. Make targets and CI build it.

## Features

- Month, week, day and year views, selected-day agenda and mini-calendar navigation.
- Create, edit and delete events; edits/deletions affect the entire recurring series.
- Group filtering, search, all-day and timed events, and disabled event visibility.
- Daily, weekly, monthly and yearly presets. Existing advanced RRULE/FUNC rules
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
Expansion supports daily through yearly RRULE frequencies and existing FUNC
holiday rules. Existing ICS and holiday APIs remain unchanged.

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
