import { addDays, startOfDay, startOfMonth, startOfWeek, endOfMonth, format } from 'date-fns';
import { formatInTimeZone, fromZonedTime } from 'date-fns-tz';
import type { CalendarEvent } from './api';

export type View = 'month' | 'week' | 'day' | 'year';
export const weekdays = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
export const dayKey = (date: Date) => format(date, 'yyyy-MM-dd');
export const groupName = (event: CalendarEvent) => event.event_group || 'Ungrouped';
export const localZone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';

export function safeZone(zone: string) {
  try {
    new Intl.DateTimeFormat('en', { timeZone: zone });
    return zone;
  } catch {
    return 'UTC';
  }
}

export function monthDays(date: Date, fixed = true) {
  const first = startOfWeek(startOfMonth(date), { weekStartsOn: 1 });
  const count = fixed
    ? 42
    : Math.ceil((endOfMonth(date).getDate() + ((startOfMonth(date).getDay() + 6) % 7)) / 7) * 7;
  return Array.from({ length: count }, (_, i) => addDays(first, i));
}

export function viewRange(date: Date, view: View): [Date, Date] {
  if (view === 'year') return [new Date(date.getFullYear(), 0, 1), new Date(date.getFullYear() + 1, 0, 1)];
  if (view === 'day') return [startOfDay(date), addDays(startOfDay(date), 1)];
  if (view === 'week') {
    const from = startOfWeek(date, { weekStartsOn: 1 });
    return [from, addDays(from, 7)];
  }
  const days = monthDays(date);
  return [days[0], addDays(days[41], 1)];
}

export function occursOn(event: CalendarEvent, day: Date) {
  // All-day entries follow their event-zone dates, not the browser's UTC offset.
  if (event.all_day) {
    const zone = safeZone(event.tz || 'UTC');
    const key = dayKey(day);
    return (
      formatInTimeZone(event.date_from, zone, 'yyyy-MM-dd') <= key &&
      formatInTimeZone(event.date_to, zone, 'yyyy-MM-dd') > key
    );
  }
  return new Date(event.date_from) < addDays(startOfDay(day), 1) && new Date(event.date_to) > startOfDay(day);
}

export function colorFor(group: string): number {
  let hash = 0;
  for (const char of group) hash = (hash * 31 + char.charCodeAt(0)) | 0;
  return Math.abs(hash) % 6;
}

export function dateInput(value: string, zone: string, allDay: boolean) {
  return formatInTimeZone(value, safeZone(zone), allDay ? 'yyyy-MM-dd' : "yyyy-MM-dd'T'HH:mm:ss");
}

export function toInstant(value: string, zone: string) {
  const instant = fromZonedTime(value.length === 10 ? `${value}T00:00` : value, zone);
  if (Number.isNaN(instant.getTime())) throw new Error('Enter a valid date and IANA time zone.');
  // Nonexistent wall-clock times during the spring DST transition must not shift silently.
  const expected = value.length === 16 ? `${value}:00` : value;
  if (dateInput(instant.toISOString(), zone, value.length === 10) !== expected)
    throw new Error('This time does not exist in the selected time zone (daylight saving transition).');
  return instant.toISOString();
}

export function timeLabel(event: CalendarEvent) {
  if (event.all_day) return 'All day';
  return format(new Date(event.date_from), 'HH:mm');
}
