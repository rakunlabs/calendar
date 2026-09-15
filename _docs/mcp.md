# AI Access (MCP)

Calendar speaks the [Model Context Protocol](https://modelcontextprotocol.io),
so an AI client can read the schedule, look for free time, and — unless the
endpoint is read-only — create and change events. Tools call the same service
the REST API uses, so recurrence, time zone, and concurrency rules are identical.

The endpoint is streamable HTTP at `<base_path>/mcp`. With no `base_path` that is
`http://localhost:8080/mcp`; with `base_path: /calendar` it is:

```
http://localhost:8080/calendar/mcp
```

::: danger No Authentication
The MCP endpoint has no authentication, exactly like the UI and REST API, and it
grants the same authority: anything an AI client reaches can read and, by
default, rewrite every calendar. Restrict it at deployment, set `read_only`, or
disable it. It is not scoped by the entity filter.
:::

## Configuration

```yaml
mcp:
  enabled: true # default; set to false to remove the endpoint entirely
  read_only: false # true publishes only the three read tools
```

With `read_only: true` the write tools are not registered, so a client cannot
call them even if it knows their names.

## Connect a Client

Most clients accept an HTTP MCP server entry:

```json
{
  "mcpServers": {
    "calendar": {
      "type": "http",
      "url": "http://localhost:8080/calendar/mcp",
      "headers": { "X-User": "ai-assistant" }
    }
  }
}
```

`X-User` is the optional audit label stored in `Updated by`. It is not a login
and grants nothing; when it is absent, writes are recorded as `mcp`.

## Tools

| Tool | Mode | Purpose |
| --- | --- | --- |
| `list_occurrences` | read | Everything scheduled in a date range, with recurring events expanded |
| `list_events` | read | Search stored events by name, group, or entity; returns ids and repeat rules |
| `find_free_time` | read | Open slots of a given length within working hours |
| `create_event` | write | Create a timed or all-day event, optionally repeating |
| `update_event` | write | Change supplied fields of an event; edits the whole series |
| `cancel_occurrence` | write | Cancel one occurrence, leaving the rest of the series |
| `delete_event` | write | Delete events by id, including every occurrence |

### Dates and Times

Supply an IANA `time_zone` such as `Europe/Istanbul`; it defaults to UTC. Timed
values accept RFC3339 (`2026-09-15T09:00:00+03:00`) or a wall-clock timestamp
(`2026-09-15T09:00:00`) read in that zone. A wall-clock time that does not exist
because of a daylight-saving transition is rejected rather than silently moved.

All-day events use plain dates, and `end_date` is the **inclusive** last day:
`start_date: 2026-09-21` with `end_date: 2026-09-23` is a three-day event.
Occurrences are reported the same way, so a value read from `list_occurrences`
can be passed straight back to a write tool.

### Free Time

`find_free_time` merges overlapping meetings, treats all-day events as busy for
their whole span, and skips weekends unless `include_weekends` is set. Working
hours default to `09:00`–`18:00` and are applied on the wall clock, so a slot
keeps its local time across a daylight-saving change.

### Limits

A range covers at most **400 days** and 20,000 occurrences; exceeding either
returns an error rather than a truncated calendar. `list_occurrences` returns
200 occurrences by default and reports `truncated` when more exist.

## Recurring Events

`update_event` edits the series. To remove a single occurrence, call
`cancel_occurrence` with the `id` and the `occurrence_start` reported by
`list_occurrences`; cancelling the same occurrence twice is harmless. Calendar
rejects series timing changes while exceptions exist, so remove the exceptions
first. Rescheduling one occurrence is not exposed as a tool — use the UI or the
REST API.

## Example Requests

- "What is on the team calendar next week?" → `list_occurrences`
- "Find an hour for a design review before Friday" → `find_free_time`, then `create_event`
- "Move the Monday standup to 10:00" → `list_events`, then `update_event`
- "Skip next Tuesday's standup" → `list_occurrences`, then `cancel_occurrence`
