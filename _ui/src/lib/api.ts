export interface CalendarEvent {
  id: string;
  name: string;
  description: string;
  event_group: string | null;
  date_from: string;
  date_to: string;
  tz: string;
  all_day: boolean;
  rrule: string;
  disabled: boolean;
  updated_at?: string;
  updated_by?: string;
}

interface Envelope<T> {
  payload: T;
  message?: { text?: string; error?: string };
}
const base = `${import.meta.env.BASE_URL}v1`;

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(`${base}${path}`, init);
  const text = await response.text();
  let body: Envelope<T> | undefined;
  try {
    body = text ? JSON.parse(text) : undefined;
  } catch {
    /* Handled as an invalid API response below. */
  }
  if (!response.ok) {
    if (response.status === 404 && path.startsWith('/events?')) return [] as T;
    throw new Error(
      body?.message?.error ||
        body?.message?.text ||
        `Request failed (${response.status}). Check the calendar service.`,
    );
  }
  if (text && !body) throw new Error('The service returned an invalid response. Please retry.');
  return body?.payload as T;
}

export async function getEvents(signal?: AbortSignal): Promise<CalendarEvent[]> {
  const result: CalendarEvent[] = [];
  for (let offset = 0; offset < 20000; offset += 200) {
    const page = await request<CalendarEvent[]>(`/events?_limit=200&_offset=${offset}&_sort=id`, { signal });
    result.push(...page);
    if (page.length < 200) return result;
  }
  throw new Error(
    'This calendar exceeds the 20,000-event browser limit. Use the API to narrow your dataset.',
  );
}

export function getOccurrences(from: Date, to: Date, signal?: AbortSignal) {
  const search = new URLSearchParams({ from: from.toISOString(), to: to.toISOString() });
  return request<CalendarEvent[]>(`/occurrences?${search}`, { signal });
}

export function saveEvent(event: CalendarEvent, existing: boolean, user: string) {
  return request(existing ? `/events/${encodeURIComponent(event.id)}` : '/events', {
    method: existing ? 'PUT' : 'POST',
    headers: { 'Content-Type': 'application/json', 'X-User': user },
    body: JSON.stringify(event),
  });
}

export function deleteEvent(id: string) {
  return request(`/events/${encodeURIComponent(id)}`, { method: 'DELETE' });
}
