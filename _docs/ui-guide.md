# User Guide

Open `/calendar/` on your running service, or follow the [Quickstart](/quickstart)
to start a local instance. Calendar combines an event editor, calendar views,
entity assignments, and ICS tools in one interface.

## Find Your Events

Use **Month**, **Week**, **Day**, or **Year** to change the view. The navigation
arrows move through the selected view; **Today** returns to the current date.
Select a date in the calendar or mini-calendar to see its agenda.

![Month view with calendar groups and the selected-day agenda](/screenshots/calendar-month-1440.png)

Week and day views place timed events on a time grid, with all-day events above it.
Select an event in the calendar or agenda to open its editor.

![Week view showing timed events and an all-day row](/screenshots/calendar-week-1440.png)

- **Search events** matches event names, notes, and groups in the loaded calendar range.
- **My calendars** toggles group visibility without changing stored events.
- **Show disabled events** reveals disabled events in the UI; it does not enable them or include them in ICS exports.
- **Entity** requests events assigned to that entity from the server. Search and group visibility are local view controls, not additional access restrictions.
- **Refresh calendar** reloads data after changes made elsewhere.

On mobile, use **Toggle calendar filters** to open navigation and filters. Month
cells show event dots; tap a date to read its agenda and use the create button to
add an event.

<img src="/screenshots/calendar-month-390.png" width="390" alt="Mobile month view with event dots and a selected-day agenda" />

## Create and Edit Events

1. Choose **New event**, a date's add button, or a time slot in week/day view. On desktop, dragging across dates or time slots can prefill a range.
2. Enter an event name, choose an existing **Calendar group** or enter a new group name, and optionally add notes.
3. Choose all-day or timed scheduling, set the start and end, and confirm the **Time zone**.
4. Choose a **Repeat** preset if needed, then save. Use **Additional settings** for the disabled flag and optional **Updated by** audit label.

To change an event, select it and save your changes. To remove it, choose delete
in the editor and confirm. Disabling an event keeps it stored and editable while
excluding it from ICS exports.

### Dates and Time Zones

Timed events display in your browser's time zone; the editor uses the event's
explicit time zone to interpret the entered date and time. All-day events retain
their dates in the event's own zone instead of shifting with the browser zone.

The all-day editor's **End date is inclusive**: for a one-day event, use the same
start and end date. The UI converts it to the following day's midnight for storage.
The API and ICS use **exclusive end timestamps**. For example, an all-day event
on September 7 ends at September 8, 00:00 in its event zone. A timed event ending
at 10:00 does not occupy the next time slot beginning at 10:00.

### Recurring Events

Repeat options include daily, weekly, monthly, and yearly schedules, plus special
holiday presets. Existing advanced RRULE/FUNC rules are retained when editing
unless you replace the repeat selection. Occurrences are expanded by the server.

::: warning Whole-Series Changes
Editing or deleting a recurring occurrence changes or deletes the entire stored
series, not just that occurrence. There is no single-occurrence edit workflow.
:::

## Groups and Entities

A **group** is a label on an event, such as `Holidays` or `Team`. It organizes
the calendar's colors and visibility. An **entity** is a named collection built
from assignments to groups or individual events, such as an office or team calendar.
Entities are not users, accounts, or permission boundaries.

In **Calendar tools > Assignments**:

1. Choose an entity or enter a new entity name.
2. Assign either a group or an individual event, then add the assignment.
3. Review the entity's assignments and remove individual assignments when no longer needed.

A group assignment includes matching events, including events later added to that
group. An individual-event assignment includes only that event. Entity names come
from existing assignments; there is no separate entity registry. Removing an
assignment does not delete its events. Older combined assignments are shown as
group **OR** event; removing one removes that exact assignment.

Creating or importing events does not automatically assign them to the selected
entity. If a new event is missing under an entity filter, assign its group or the
event itself, or switch to all entities. The assignment picker includes disabled
events and events outside the current calendar window. Large catalogs have a
20,000-row browser limit; use the API to manage datasets beyond that limit.

## Import, Download, and Subscribe

Open **Calendar tools > Import & export**.

![Calendar tools showing download scope and subscription controls](/screenshots/tools-export-desktop.png)

### Import an ICS File

Choose an `.ics` file up to 9 MiB, optionally set an import group, time zone, and
audit label, then choose **Import file**. Imports do not create entity assignments.
Use the Assignments tab afterward if the imported events belong in an entity calendar.

### Download a Calendar

Choose **Export entity** and **Export group scope**, then **Download ICS**.
Only enabled events matching **both** scope controls are exported. Choosing all
entities means all events, including events without assignments; choosing all
groups applies no group restriction.

The export controls are independent of calendar search, hidden groups, and
**Show disabled events**. Review the scope before sharing: a filtered screen is
not a preview of the download's contents.

The optional **Download year** selects event series, not an exact clipping of the
visible calendar range. Without a year, the default window is the previous year,
current year, and next two years. An ICS download is a snapshot, not an automatically
updating subscription.

### Subscribe From Another Calendar

Choose the export scope, then **Copy subscription URL** and add it as a calendar
subscription in your calendar client. If clipboard access is unavailable, select
and copy the displayed URL manually. Subscriptions use the same entity/group scope
but never include the download year, retaining the rolling default window.

The URL must be reachable by the subscribing calendar provider. A localhost URL
will not work for a remote provider. Refresh timing depends on that provider;
updates may not appear immediately.

## Deployment Security

The UI and API provide **no built-in authentication**. Protect both at deployment
with appropriate network restrictions or an authentication layer. The optional
`Updated by` / audit label is sent as `X-User`; it is caller-supplied metadata,
not a verified identity, login, or authorization check.

Entity filters and subscription URLs do not restrict access. A subscription URL
is not an authorization token. When protecting the service, also plan how your
calendar provider will reach and authenticate to the feed without exposing
management endpoints or other calendars.
