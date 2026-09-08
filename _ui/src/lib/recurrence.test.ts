import { describe, expect, it } from 'vitest';
import type { CalendarEvent } from './api';
import { editOccurrence, editSeries, editorTimingZone } from './recurrence';
import { dateInput, occursOn, toInstant } from './calendar';
import { moveEditorEnd } from './editorDates';

const master: CalendarEvent = {
  id: 'series',
  name: 'Standup',
  description: '',
  event_group: 'Team',
  date_from: '2026-09-01T09:00:17Z',
  date_to: '2026-09-01T09:30:19Z',
  tz: 'UTC',
  all_day: false,
  rrule: 'RRULE:FREQ=DAILY',
  disabled: false,
  updated_at: '2026-08-01T12:00:00Z',
  recurrence: {
    start: { value: '20260901T090017Z' },
    duration: 'PT30M2S',
    timezones: ['original definition'],
  },
};
const id = { value: '20260902T090017Z' };

describe('recurrence edits', () => {
  it('keeps nominal duration and the occurrence start for a details-only exception', () => {
    const occurrence = {
      ...master,
      date_from: '2026-09-02T09:00:17Z',
      date_to: '2026-09-02T09:30:19Z',
      recurrence: { start: id, duration: 'PT30M2S' },
    };
    const result = editOccurrence(master, id, 'edit', { ...occurrence, name: 'Renamed' }, occurrence);
    expect(result.recurrence?.overrides?.[0].event?.recurrence).toEqual({ start: id, duration: 'PT30M2S' });
    const regrouped = editSeries(result, { ...result, event_group: 'New team' });
    expect(regrouped.recurrence?.overrides?.[0].event?.event_group).toBe('New team');
    expect(result.recurrence?.overrides?.[0].event?.event_group).toBe('Team');
  });

  it('sends the full master and immutable identity when moving an occurrence', () => {
    const before = JSON.stringify(master);
    const occurrence = {
      ...master,
      date_from: '2026-09-03T10:00:17Z',
      date_to: '2026-09-03T10:30:19Z',
      recurrence_id: id,
      is_override: true,
      event_group: 'Other',
    };
    const result = editOccurrence(master, id, 'edit', occurrence);
    expect(result.date_from).toBe(master.date_from);
    expect(result.updated_at).toBe(master.updated_at);
    expect(result.recurrence?.overrides?.[0]).toEqual({
      recurrence_id: id,
      event: {
        ...occurrence,
        recurrence: undefined,
        recurrence_id: undefined,
        is_override: undefined,
        rrule: '',
        event_group: 'Team',
      },
    });
    const moved = editOccurrence(result, id, 'edit', { ...occurrence, date_from: '2026-09-04T10:00:17Z' });
    expect(moved.recurrence?.overrides).toHaveLength(1);
    expect(moved.recurrence?.overrides?.[0].recurrence_id).toEqual(id);
    expect(JSON.stringify(master)).toBe(before);
    expect(occurrence.recurrence).toBe(master.recurrence);
  });

  it('uses effective period timing for equivalent UTC/TZID RDATEs with a plain date first', () => {
    const utc = { value: '20260902T090017Z' };
    const zoned = { value: '20260902T110017', tzid: 'Europe/Amsterdam' };
    const original = {
      ...master,
      recurrence: {
        ...master.recurrence,
        duration: 'PT1H',
        rdates: [{ start: utc }, { start: zoned, duration: 'PT2H' }],
      },
    };
    const occurrence = {
      ...master,
      date_from: '2026-09-02T09:00:17Z',
      date_to: '2026-09-02T11:00:17Z',
      recurrence: { start: zoned, duration: 'PT2H' },
    };
    const before = JSON.stringify({ original, occurrence });
    const result = editOccurrence(original, utc, 'edit', { ...occurrence, name: 'Renamed' }, occurrence);
    expect(result.recurrence?.overrides?.[0].event?.recurrence).toEqual(occurrence.recurrence);
    expect(result.recurrence?.overrides?.[0].recurrence_id).toEqual(utc);
    expect(result.recurrence?.overrides?.[0].event?.date_to).toBe(occurrence.date_to);
    expect(JSON.stringify({ original, occurrence })).toBe(before);
  });

  it('preserves effective lexical DTEND leap seconds without attaching a master graph', () => {
    const timing = { start: { value: '20260901T090017Z' }, end: { value: '20260901T100060Z' } };
    const original = {
      ...master,
      date_to: '2026-09-01T10:00:59Z',
      recurrence: { ...timing, timezones: ['original definition'], exdates: [id], rdates: [{ start: id }] },
    };
    const occurrence = { ...original, recurrence: timing };
    const result = editOccurrence(
      original,
      timing.start,
      'edit',
      { ...occurrence, name: 'Renamed' },
      occurrence,
    );
    const event = result.recurrence?.overrides?.[0].event;
    expect(event?.recurrence).toEqual(timing);
    expect(event?.date_from).toBe(occurrence.date_from);
    expect(event?.date_to).toBe(occurrence.date_to);
    expect(result.recurrence?.timezones).toEqual(original.recurrence.timezones);
  });

  it('cancels and resets one exception without deleting the master or other metadata', () => {
    const withDates = {
      ...master,
      recurrence: {
        ...master.recurrence,
        exdates: [{ value: '20260905T090017Z' }],
        rdates: [{ start: { value: '20260906T090017Z' }, duration: 'PT1H' }],
      },
    };
    const cancelled = editOccurrence(withDates, id, 'cancel');
    expect(cancelled.recurrence?.overrides).toEqual([{ recurrence_id: id, cancelled: true }]);
    expect(withDates.recurrence).not.toHaveProperty('overrides');
    const reset = editOccurrence(cancelled, id, 'reset');
    expect(reset.recurrence).toEqual({ ...withDates.recurrence, overrides: [] });
    expect(cancelled.recurrence?.overrides).toHaveLength(1);
  });

  it('preserves DURATION, lexical leap seconds and imported metadata for name-only edits', () => {
    const leap = {
      ...master,
      recurrence: { ...master.recurrence, start: { value: '20260901T090060Z', type: 'DATE-TIME' } },
    };
    const renamed = editSeries(leap, { ...leap, name: 'New name' });
    expect(renamed.recurrence).toEqual(leap.recurrence);
    expect(renamed.recurrence).not.toBe(leap.recurrence);
    expect(leap.name).toBe('Standup');
  });

  it('synchronizes lexical timing on series edits and explicitly replaces duration with end', () => {
    const result = editSeries(master, {
      ...master,
      date_from: '2026-09-04T10:00:17Z',
      date_to: '2026-09-04T10:30:19Z',
    });
    expect(result.recurrence?.start).toEqual({ value: '20260904T100017Z' });
    expect(result.recurrence?.end).toEqual({ value: '20260904T103019Z' });
    expect(result.recurrence?.duration).toBeUndefined();
    expect(master.recurrence?.duration).toBe('PT30M2S');
  });

  it('blocks timing/rule edits with exceptions but allows name and group edits', () => {
    const original = editOccurrence(master, id, 'cancel');
    expect(() => editSeries(original, { ...original, rrule: '' })).toThrow('exceptions');
    expect(() => editSeries(original, { ...original, date_from: '2026-09-01T09:00:18Z' })).toThrow(
      'exceptions',
    );
    expect(editSeries(original, { ...original, name: 'Renamed', event_group: 'New group' }).event_group).toBe(
      'New group',
    );
  });

  it('preserves timing-only metadata on an unchanged imported override', () => {
    const detached = {
      ...master,
      rrule: '',
      recurrence: { start: { value: '20260902T090060Z' }, duration: 'PT30M' },
    };
    const original = {
      ...master,
      recurrence: { ...master.recurrence, overrides: [{ recurrence_id: id, event: detached }] },
    };
    expect(
      editOccurrence(original, id, 'edit', { ...detached, name: 'Renamed' }).recurrence?.overrides?.[0].event
        ?.recurrence,
    ).toEqual(detached.recurrence);
  });
});

describe('seconds and unsupported zones', () => {
  const definition =
    'BEGIN:VTIMEZONE\r\nTZID:Europe/Amsterdam\r\nBEGIN:STANDARD\r\nTZOFFSETFROM:+0400\r\nTZOFFSETTO:+0400\r\nDTSTART:19700101T000000\r\nEND:STANDARD\r\nEND:VTIMEZONE';
  it('locks recognized IANA names when the master supplies raw timezone definitions', () => {
    const original = { ...master, tz: 'Europe/Amsterdam', recurrence: { timezones: [definition] } };
    const occurrence = { ...original, recurrence: { start: id, duration: 'PT1H' } };
    expect(editorTimingZone(occurrence, original)).toEqual({ locked: true, zone: 'UTC' });
    expect(editorTimingZone(original, original)).toEqual({ locked: true, zone: 'UTC' });
  });
  it('finds selected override definitions even when its response contains slim timing only', () => {
    const detached = { ...master, tz: 'Europe/Amsterdam', recurrence: { timezones: [definition] } };
    const original = { ...master, recurrence: { overrides: [{ recurrence_id: id, event: detached }] } };
    const occurrence = { ...detached, recurrence_id: id, is_override: true, recurrence: { start: id } };
    expect(editorTimingZone(occurrence, original)).toEqual({ locked: true, zone: 'UTC' });
  });
  it('allows IANA timing only when no imported definitions are present', () => {
    const original = { ...master, tz: 'Europe/Amsterdam', recurrence: { start: id } };
    expect(editorTimingZone(original, original)).toEqual({ locked: false, zone: 'Europe/Amsterdam' });
    expect(editorTimingZone({ ...original, tz: 'Custom/Imported' }, original)).toEqual({
      locked: true,
      zone: 'UTC',
    });
  });
  it('round-trips seconds and retains them when moving', () => {
    expect(dateInput(master.date_from, 'UTC', false)).toBe('2026-09-01T09:00:17');
    expect(toInstant('2026-09-01T09:00:17', 'UTC')).toBe('2026-09-01T09:00:17.000Z');
    expect(
      moveEditorEnd('2026-09-01T09:00:17', '2026-09-01T09:30:19', '2026-09-02T10:00:23', 'UTC', false),
    ).toBe('2026-09-02T10:30:25');
  });
  it('displays custom imported zones in UTC without crashing', () => {
    expect(dateInput(master.date_from, 'Custom/Imported', false)).toBe('2026-09-01T09:00:17');
    expect(() =>
      occursOn({ ...master, tz: 'Custom/Imported', all_day: true }, new Date('2026-09-01T12:00:00')),
    ).not.toThrow();
  });
});
