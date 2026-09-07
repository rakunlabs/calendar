---
layout: doc
prev: false
next: false
---

# Calendar 🗓️

Calendar is a self-hosted calendar and holiday service with an embedded management
UI, a Go API, and iCalendar (ICS) import, export, and subscriptions. Use it to plan
team events, maintain recurring holidays, and build calendars for different entities.

![Calendar month view with group filters and a selected-day agenda](/screenshots/calendar-month-1440.png)

## What You Can Do

- Browse month, week, day, and year views, with a selected-day agenda and mobile layout.
- Create all-day and timed events with explicit time zones, recurring schedules, and special holiday rules.
- Group events and assign whole groups or individual events to named entities.
- Import ICS files or share enabled events through scoped downloads and subscription URLs.
- Run a single Go binary with PostgreSQL; the UI needs no separate frontend server.

## Start Using Calendar

Follow the [Quickstart](/quickstart) to configure PostgreSQL and run the service.
Then open the [User Guide](/ui-guide) for event editing, filters, assignments,
and calendar sharing. Developers can find frontend commands and checks in the
[UI README](https://github.com/rakunlabs/calendar/blob/main/_ui/README.md).

::: warning Protect Your Deployment
Calendar does not provide authentication for the UI or API. Put access controls
in front of both before exposing the service. The optional `X-User` audit label,
entity filters, and subscription URLs are not authorization mechanisms.
:::

---

Report bugs and request improvements on [GitHub](https://github.com/rakunlabs/calendar/issues).
