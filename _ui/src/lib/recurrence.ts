import type { CalendarDate, CalendarEvent } from './api';
import { dateInput, localZone, safeZone } from './calendar';

// DTOs are JSON data; this also accepts Svelte's reactive proxies.
const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value));

export function hasExceptions(event: CalendarEvent) {
  const r = event.recurrence;
  return !!(r?.overrides?.length || r?.exdates?.length || r?.rdates?.length);
}

export function sameRecurrenceID(a: CalendarDate, b: CalendarDate) {
  return (
    a.value === b.value &&
    (a.tzid || '') === (b.tzid || '') &&
    (a.type || 'DATE-TIME') === (b.type || 'DATE-TIME')
  );
}

export function editorTimingZone(event: CalendarEvent | null, master: CalendarEvent | null) {
  const previous =
    event?.recurrence_id &&
    master?.recurrence?.overrides?.find((o) => sameRecurrenceID(o.recurrence_id, event.recurrence_id!))
      ?.event;
  const sources = [event, master, previous];
  // Intl recognizing a TZID does not prove equivalence to an imported VTIMEZONE.
  // Conservatively lock when definitions are present; do not interpret ICS in the browser.
  const imported = sources.some((e) => e?.recurrence?.timezones?.length);
  const unsupported = sources.some((e) =>
    [e?.tz, e?.recurrence?.start?.tzid, e?.recurrence?.end?.tzid].some(
      (zone) => zone && safeZone(zone) !== zone,
    ),
  );
  const locked = imported || unsupported;
  return { locked, zone: locked ? 'UTC' : event?.tz || localZone };
}

// Only changed timing is re-encoded: a notes edit must preserve DURATION and leap seconds.
export function editSeries(original: CalendarEvent | null, edited: CalendarEvent): CalendarEvent {
  const result = clone(edited);
  delete result.recurrence_id;
  delete result.is_override;
  if (original && original.event_group !== result.event_group) {
    for (const override of result.recurrence?.overrides || []) {
      if (override.event) override.event.event_group = result.event_group;
    }
  }
  const changed =
    !original ||
    original.all_day !== edited.all_day ||
    original.tz !== edited.tz ||
    Date.parse(original.date_from) !== Date.parse(edited.date_from) ||
    Date.parse(original.date_to) !== Date.parse(edited.date_to);
  if (original && hasExceptions(original) && (changed || original.rrule !== edited.rrule))
    throw new Error(
      'Series timing and repeat rules cannot change while exceptions exist. Reset exceptions first.',
    );
  if (changed && result.recurrence) {
    const lexical = (value: string): CalendarDate => ({
      value:
        dateInput(value, edited.tz || 'UTC', edited.all_day)
          .replaceAll('-', '')
          .replaceAll(':', '') + (!edited.all_day && edited.tz === 'UTC' ? 'Z' : ''),
      ...(edited.all_day ? { type: 'DATE' } : edited.tz && edited.tz !== 'UTC' ? { tzid: edited.tz } : {}),
    });
    result.recurrence.start = lexical(edited.date_from);
    result.recurrence.end = lexical(edited.date_to);
    delete result.recurrence.duration;
  }
  return result;
}

export function editOccurrence(
  master: CalendarEvent,
  id: CalendarDate,
  action: 'edit' | 'cancel' | 'reset',
  edited?: CalendarEvent,
  occurrence?: CalendarEvent,
): CalendarEvent {
  const result = clone(master);
  delete result.recurrence_id;
  delete result.is_override;
  result.recurrence ??= {};
  const overrides = (result.recurrence.overrides ??= []);
  const index = overrides.findIndex((o) => sameRecurrenceID(o.recurrence_id, id));
  const previous = index < 0 ? undefined : overrides[index].event;
  const identity = clone(index < 0 ? id : overrides[index].recurrence_id);
  if (index >= 0) overrides.splice(index, 1);
  if (action === 'cancel') overrides.push({ recurrence_id: identity, cancelled: true });
  if (action === 'edit') {
    if (!edited) throw new Error('Missing occurrence details.');
    const event = clone(edited);
    delete event.recurrence;
    if (
      previous?.recurrence &&
      previous.tz === event.tz &&
      previous.all_day === event.all_day &&
      Date.parse(previous.date_from) === Date.parse(event.date_from) &&
      Date.parse(previous.date_to) === Date.parse(event.date_to)
    ) {
      const { start, end, duration, timezones } = previous.recurrence;
      event.recurrence = clone({ start, end, duration, timezones });
    }
    // The backend has already resolved RDATE precedence and lexical timing.
    // Never reconstruct effective timing from the master's recurrence graph.
    if (
      !previous &&
      occurrence &&
      occurrence.recurrence &&
      occurrence.tz === event.tz &&
      occurrence.all_day === event.all_day &&
      Date.parse(occurrence.date_from) === Date.parse(event.date_from) &&
      Date.parse(occurrence.date_to) === Date.parse(event.date_to)
    ) {
      const { start, end, duration } = occurrence.recurrence;
      event.recurrence = clone({ start, end, duration });
    }
    delete event.recurrence_id;
    delete event.is_override;
    event.rrule = '';
    event.event_group = master.event_group;
    overrides.push({ recurrence_id: identity, event });
  }
  return result;
}
