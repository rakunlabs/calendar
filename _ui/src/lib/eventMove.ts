import { differenceInCalendarDays } from 'date-fns';
import type { CalendarEvent } from './api';
import { dateInput, toInstant } from './calendar';
import { shiftEditorDate } from './editorDates';
import { editOccurrence, editSeries, editorTimingZone, hasExceptions } from './recurrence';

export function movedEvent(master: CalendarEvent, occurrence: CalendarEvent, target: Date) {
  if (editorTimingZone(occurrence, master).locked)
    throw new Error(
      'This imported time zone cannot be moved in the browser. Edit it in the source calendar.',
    );
  const edited = { ...occurrence, event_group: master.event_group };
  if (occurrence.all_day) {
    const from = dateInput(occurrence.date_from, occurrence.tz || 'UTC', true);
    const to = dateInput(occurrence.date_to, occurrence.tz || 'UTC', true);
    const days = differenceInCalendarDays(target, new Date(`${from}T12:00:00`));
    edited.date_from = toInstant(shiftEditorDate(from, days), occurrence.tz || 'UTC');
    edited.date_to = toInstant(shiftEditorDate(to, days), occurrence.tz || 'UTC');
  } else {
    const duration = Date.parse(occurrence.date_to) - Date.parse(occurrence.date_from);
    edited.date_from = target.toISOString();
    edited.date_to = new Date(target.getTime() + duration).toISOString();
  }
  if (
    Date.parse(edited.date_from) === Date.parse(occurrence.date_from) &&
    Date.parse(edited.date_to) === Date.parse(occurrence.date_to)
  )
    return master;
  if (occurrence.recurrence_id && (master.rrule || hasExceptions(master)))
    return editOccurrence(master, occurrence.recurrence_id, 'edit', edited, occurrence);
  return editSeries(master, { ...master, date_from: edited.date_from, date_to: edited.date_to });
}
