import type { CalendarEvent } from './api';
import { occursOn } from './calendar';

// Reserve one lane across the entire visible span before placing single-day entries.
export function eventLanes(events: CalendarEvent[], days: Date[]) {
  const lanes: (CalendarEvent | null)[][] = days.map(() => []);
  const spans = events.filter(event => event.all_day).map(event => ({
    event, indices: days.flatMap((day, index) => occursOn(event, day) ? [index] : []),
  })).filter(span => span.indices.length)
    .sort((a, b) => a.indices[0] - b.indices[0] || b.indices.length - a.indices.length || a.event.name.localeCompare(b.event.name));
  for (const { event, indices } of spans) {
    let lane = 0;
    while (indices.some(index => lanes[index][lane])) lane++;
    for (const index of indices) {
      while (lanes[index].length <= lane) lanes[index].push(null);
      lanes[index][lane] = event;
    }
  }
  days.forEach((day, index) => {
    for (const event of events.filter(event => !event.all_day && occursOn(event, day))) {
      const lane = lanes[index].indexOf(null);
      if (lane < 0) lanes[index].push(event);
      else lanes[index][lane] = event;
    }
  });
  return lanes;
}
