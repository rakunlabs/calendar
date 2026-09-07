# Calendar UI

Svelte 5, Tailwind CSS 4, TypeScript and Vite. Use **pnpm** (version pinned in
`package.json`); do not mix package managers.

```sh
pnpm --dir _ui install --frozen-lockfile
pnpm --dir _ui dev
```

Open `http://localhost:5173/calendar/`. Vite proxies `/calendar/v1` to
`http://localhost:8080`. Set `CALENDAR_API_URL` when the API runs elsewhere.

```sh
make ui-build
go run ./cmd/calendar
```

The service serves the compiled UI at `/calendar/`; `/` redirects there.
`_ui/embed.go` embeds `dist`, so **build the UI before Go builds, tests or Swagger
generation**. Dist is generated, not committed. Make targets and CI build it.

## Features

- Month, day and year views, selected-day agenda and mini-calendar navigation.
- Create, edit and delete events; edits/deletions affect the entire recurring series.
- Group filtering, search, all-day and timed events, and disabled event visibility.
- Daily, weekly, monthly and yearly presets. Existing advanced RRULE/FUNC rules
  are retained when editing. Occurrences are expanded on the server, not in JS.
- Explicit time zones for editing; timed events display in the browser zone.
  All-day dates retain the event's own time zone. End timestamps are exclusive.
- An optional `Updated by` label sends `X-User`; this is an audit label, not login.

The management UI adds no authentication. Protect it and the API at deployment.
The `/calendar/v1/occurrences` API accepts RFC3339 `from` and `to` parameters,
with an exclusive end and a maximum 400-day range / 20,000 returned instances.
Expansion supports daily through yearly RRULE frequencies and existing FUNC
holiday rules. Existing ICS and holiday APIs remain unchanged.

## Checks

```sh
pnpm --dir _ui check
pnpm --dir _ui test
pnpm --dir _ui build
go test -race ./...
```
