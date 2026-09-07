import { dateInput, toInstant } from './calendar';

// Shift calendar dates independently of the browser's zone and DST transitions.
export function shiftEditorDate(value: string, days: number) {
  const date = new Date(`${value}T12:00:00Z`);
  date.setUTCDate(date.getUTCDate() + days);
  return date.toISOString().slice(0, 10);
}

export function moveEditorEnd(start: string, end: string, next: string, zone: string, allDay: boolean) {
  if (allDay) {
    const days = (Date.parse(`${end}T12:00:00Z`) - Date.parse(`${start}T12:00:00Z`)) / 86400000;
    return shiftEditorDate(next, Math.max(0, days));
  }
  const duration = Date.parse(toInstant(end, zone)) - Date.parse(toInstant(start, zone));
  if (duration <= 0) return end;
  return dateInput(new Date(Date.parse(toInstant(next, zone)) + duration).toISOString(), zone, false);
}
