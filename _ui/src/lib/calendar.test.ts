import { describe, expect, it } from 'vitest';
import { monthDays, occursOn, toInstant, viewRange, colorFor } from './calendar';
import type { CalendarEvent } from './api';

const event: CalendarEvent = {
  id: 'one',
  name: 'Holiday',
  description: '',
  event_group: null,
  date_from: '2026-03-29T00:00:00+01:00',
  date_to: '2026-03-30T00:00:00+02:00',
  tz: 'Europe/Amsterdam',
  all_day: true,
  rrule: '',
  disabled: false,
};

describe('calendar dates', () => {
  it('builds a Monday-first six-week month including adjacent dates', () => {
    const days = monthDays(new Date(2026, 1, 15));
    expect(days).toHaveLength(42);
    expect(days[0].getDay()).toBe(1);
    expect(days[0].getMonth()).toBe(0);
    expect(days.filter((d) => d.getMonth() === 1)).toHaveLength(28);
  });
  it('uses exclusive all-day ends and preserves event-zone dates through DST', () => {
    expect(occursOn(event, new Date(2026, 2, 29))).toBe(true);
    expect(occursOn(event, new Date(2026, 2, 30))).toBe(false);
    expect(occursOn(event, new Date(2026, 2, 28))).toBe(false);
  });
  it('converts local form times to UTC without losing the selected zone', () => {
    expect(toInstant('2026-07-01T09:00', 'Europe/Amsterdam')).toBe('2026-07-01T07:00:00.000Z');
    expect(toInstant('2026-01-01', 'Europe/Amsterdam')).toBe('2025-12-31T23:00:00.000Z');
    expect(() => toInstant('2026-03-29T02:30', 'Europe/Amsterdam')).toThrow('does not exist');
    expect(() => toInstant('2026-01-01', 'Invalid/Zone')).toThrow();
  });
  it('bounds year requests including leap days', () => {
    const [from, to] = viewRange(new Date(2024, 5, 1), 'year');
    expect(from.getFullYear()).toBe(2024);
    expect(to.getFullYear()).toBe(2025);
    expect(to.getMonth()).toBe(0);
  });
  it('bounds weeks Monday to Monday across year boundaries', () => {
    const [from, to] = viewRange(new Date(2026, 0, 1), 'week');
    expect(from).toEqual(new Date(2025, 11, 29));
    expect(to).toEqual(new Date(2026, 0, 5));
    expect(viewRange(new Date(2026, 0, 4), 'week')).toEqual([from, to]);
  });
  it('keeps group colors stable', () => {
    expect(colorFor('Team')).toBe(colorFor('Team'));
    expect(colorFor('Holidays')).toBeLessThan(6);
  });
});
