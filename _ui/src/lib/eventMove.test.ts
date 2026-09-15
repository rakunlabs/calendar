import { describe, expect, it } from 'vitest';
import type { CalendarEvent } from './api';
import { movedEvent } from './eventMove';
import { dateInput } from './calendar';

const event: CalendarEvent = {
  id: 'event',
  name: 'Meeting',
  description: '',
  event_group: 'Team',
  date_from: '2026-09-15T09:00:00Z',
  date_to: '2026-09-15T10:30:00Z',
  all_day: false,
  tz: 'UTC',
  rrule: '',
  disabled: false,
  updated_at: '2026-09-01T00:00:00Z',
};
describe('event moves', () => {
  it('preserves duration, version and metadata when crossing midnight', () => {
    const master = { ...event, recurrence: { start: { value: '20260915T090000Z' }, duration: 'PT90M' } };
    const result = movedEvent(master, master, new Date('2026-09-18T23:30:00Z'));
    expect(result.date_to).toBe('2026-09-19T01:00:00.000Z');
    expect(result.updated_at).toBe(event.updated_at);
    expect(result.recurrence?.end?.value).toBe('20260919T010000Z');
    expect(result.recurrence?.duration).toBeUndefined();
    expect(master.date_from).toBe(event.date_from);
  });
  it('moves only the selected recurrence, keeping original identity on repeated moves', () => {
    const master = {
      ...event,
      rrule: 'RRULE:FREQ=DAILY',
      recurrence: { exdates: [{ value: '20260917T090000Z' }] },
    };
    const occurrence = { ...event, recurrence_id: { value: '20260916T090000Z' } };
    const first = movedEvent(master, occurrence, new Date('2026-09-18T11:30:00Z'));
    const replacement = first.recurrence!.overrides![0].event!;
    const second = movedEvent(
      first,
      { ...replacement, recurrence_id: occurrence.recurrence_id },
      new Date('2026-09-19T12:00:00Z'),
    );
    expect(second.recurrence?.overrides).toHaveLength(1);
    expect(second.recurrence?.overrides?.[0].recurrence_id).toEqual(occurrence.recurrence_id);
    expect(second.recurrence?.exdates).toEqual(master.recurrence.exdates);
    expect(second.date_from).toBe(master.date_from);
  });
  it('preserves all-day calendar length across DST', () => {
    const allDay = {
      ...event,
      all_day: true,
      tz: 'America/New_York',
      date_from: '2026-03-06T05:00:00Z',
      date_to: '2026-03-09T04:00:00Z',
    };
    const result = movedEvent(allDay, allDay, new Date(2026, 2, 10));
    expect(dateInput(result.date_from, allDay.tz, true)).toBe('2026-03-10');
    expect(dateInput(result.date_to, allDay.tz, true)).toBe('2026-03-13');
  });
  it('refuses unsupported imported zones', () => {
    const imported = { ...event, recurrence: { timezones: ['custom definition'] } };
    expect(() => movedEvent(imported, event, new Date())).toThrow(/time zone/);
  });
});
