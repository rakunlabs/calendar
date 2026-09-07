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
const base = './v1';

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
    if (response.status === 404 && (path.startsWith('/events?') || path.startsWith('/relations?')))
      return [] as T;
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

export function getOccurrences(from: Date, to: Date, signal?: AbortSignal, entity?: string) {
  const search = new URLSearchParams({ from: from.toISOString(), to: to.toISOString() });
  if (entity) search.set('entity', entity);
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

export interface Relation {
  entity: string;
  event_group: string | null;
  event_id: string | null;
  updated_at?: string;
  updated_by?: string;
}

export async function getRelations(signal?: AbortSignal): Promise<Relation[]> {
  const result: Relation[] = [];
  for (let offset = 0; offset < 20000; offset += 200) {
    signal?.throwIfAborted();
    const page = await request<Relation[]>(
      `/relations?_limit=200&_offset=${offset}&_sort=entity,event_group,event_id`,
      { signal },
    );
    result.push(...page);
    if (page.length < 200) return result;
  }
  throw new Error('Assignments exceed the 20,000-row browser limit. Use the API to manage this dataset.');
}

function relationTargets(relation: Relation) {
  if (!relation.entity.trim() || (!relation.event_group && !relation.event_id))
    throw new Error('Choose an entity and at least one assignment target.');
  return {
    entity: relation.entity,
    ...(relation.event_group !== null ? { event_group: relation.event_group } : {}),
    ...(relation.event_id !== null ? { event_id: relation.event_id } : {}),
  };
}

export function addRelation(relation: Relation, user = '') {
  return request('/relations', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-User': user },
    body: JSON.stringify(relationTargets(relation)),
  });
}

export function deleteRelation(relation: Relation) {
  const search = new URLSearchParams({ _exact: 'true', ...relationTargets(relation) });
  return request(`/relations?${search}`, { method: 'DELETE' });
}

export function icsURL(entity = '', group = '', year = '') {
  const search = new URLSearchParams();
  if (entity) search.set('entity[eq]', entity);
  if (group) search.set('event_group[eq]', group);
  if (year) search.set('year', year);
  return new URL(`${base}/ics${search.size ? `?${search}` : ''}`, document.baseURI).href;
}

export async function downloadICS(entity = '', group = '', year = '') {
  const response = await fetch(icsURL(entity, group, year));
  if (!response.ok) {
    const body = await response.json().catch(() => null);
    throw new Error(
      body?.message?.error || body?.message?.text || `Download failed (${response.status}). Please retry.`,
    );
  }
  return response.blob();
}

// Leave room for multipart headers under the service's 10 MiB request limit.
export const maxImportBytes = 9 * 1024 * 1024;
export function importICS(file: File, group = '', tz = '', user = '') {
  if (file.size > maxImportBytes) throw new Error('Choose an ICS file smaller than 9 MiB.');
  const body = new FormData();
  body.append('file', file);
  if (group.trim()) body.append('event_group', group.trim());
  if (tz.trim()) body.append('tz', tz.trim());
  // The current handler reads options from the query; retain multipart fields too.
  const search = new URLSearchParams();
  if (group.trim()) search.set('event_group', group.trim());
  if (tz.trim()) search.set('tz', tz.trim());
  return request(`/ics${search.size ? `?${search}` : ''}`, {
    method: 'POST',
    headers: { 'X-User': user },
    body,
  });
}
