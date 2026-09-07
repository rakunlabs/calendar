import { afterEach, expect, it, vi } from 'vitest';
import {
  deleteEvent,
  getEvents,
  getOccurrences,
  getRelations,
  addRelation,
  deleteRelation,
  importICS,
  downloadICS,
  icsURL,
  maxImportBytes,
} from './api';

afterEach(() => vi.unstubAllGlobals());

it('treats the events API empty-list 404 as an empty calendar', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue(new Response('{"message":{"text":"no events found"}}', { status: 404 })),
  );
  expect(await getEvents()).toEqual([]);
});

it('continues paging without depending on the count envelope', async () => {
  const page = Array.from({ length: 200 }, (_, i) => ({ id: String(i) }));
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(new Response(JSON.stringify({ payload: page })))
    .mockResolvedValueOnce(new Response('{"payload":[]}'));
  vi.stubGlobal('fetch', fetch);
  expect(await getEvents()).toHaveLength(200);
  expect(fetch.mock.calls[1][0]).toContain('_offset=200');
});

it('propagates service failures instead of displaying an empty calendar', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue(new Response('{"message":{"text":"Unavailable"}}', { status: 503 })),
  );
  await expect(getOccurrences(new Date(), new Date())).rejects.toThrow('Unavailable');
});

it('accepts the empty successful event deletion response', async () => {
  const fetch = vi.fn().mockResolvedValue(new Response(null, { status: 200 }));
  vi.stubGlobal('fetch', fetch);
  await expect(deleteEvent('a/b')).resolves.toBeUndefined();
  expect(fetch.mock.calls[0][0]).toContain('/events/a%2Fb');
});

it('pages all relations, accepts 404, and forwards cancellation', async () => {
  const signal = new AbortController().signal;
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          payload: Array.from({ length: 200 }, () => ({ entity: 'A', event_group: 'G', event_id: null })),
        }),
      ),
    )
    .mockResolvedValueOnce(new Response('', { status: 404 }));
  vi.stubGlobal('fetch', fetch);
  expect(await getRelations(signal)).toHaveLength(200);
  expect(fetch.mock.calls[1][0]).toContain('_offset=200&_sort=entity,event_group,event_id');
  expect(fetch.mock.calls[0][1].signal).toBe(signal);
  const controller = new AbortController();
  controller.abort();
  await expect(getRelations(controller.signal)).rejects.toThrow();
});

it('keeps special characters literal and deletes only exact target tuples', async () => {
  const fetch = vi.fn().mockImplementation(() => Promise.resolve(new Response('')));
  vi.stubGlobal('fetch', fetch);
  const entity = 'R&D, + / %';
  await getOccurrences(new Date(), new Date(), undefined, entity);
  expect(new URL(fetch.mock.calls[0][0], 'https://example.com').searchParams.get('entity')).toBe(entity);
  for (const targets of [
    { event_group: 'G,&', event_id: null },
    { event_group: null, event_id: 'id/+,' },
    { event_group: 'G,&', event_id: 'id/+,' },
  ]) {
    await addRelation({ entity, ...targets }, 'Tester');
    const write = fetch.mock.lastCall![1];
    expect(write.headers['X-User']).toBe('Tester');
    expect(JSON.parse(write.body)).not.toHaveProperty(
      Object.values(targets).includes(null)
        ? targets.event_id === null
          ? 'event_id'
          : 'event_group'
        : 'unused',
    );
    await deleteRelation({ entity, ...targets });
    const params = new URL(fetch.mock.lastCall![0], 'https://example.com').searchParams;
    expect(params.get('_exact')).toBe('true');
    expect(params.get('entity')).toBe(entity);
    expect(params.get('event_group')).toBe(targets.event_group);
    expect(params.get('event_id')).toBe(targets.event_id);
  }
  expect(() => deleteRelation({ entity, event_group: null, event_id: null })).toThrow();
});

it('imports multipart fields without setting content type, and surfaces size/server errors', async () => {
  const fetch = vi.fn().mockResolvedValue(new Response(''));
  vi.stubGlobal('fetch', fetch);
  const file = new File(['BEGIN:VCALENDAR'], 'events.ics');
  await importICS(file, 'Group', 'Europe/Istanbul', 'Tester');
  const init = fetch.mock.calls[0][1];
  expect(init.body).toBeInstanceOf(FormData);
  expect(init.body.get('file').name).toBe('events.ics');
  expect(init.body.get('event_group')).toBe('Group');
  expect(init.body.get('tz')).toBe('Europe/Istanbul');
  const params = new URL(fetch.mock.calls[0][0], 'https://example.com').searchParams;
  expect(params.get('event_group')).toBe('Group');
  expect(params.get('tz')).toBe('Europe/Istanbul');
  expect(init.headers).not.toHaveProperty('Content-Type');
  expect(() => importICS(new File([new Uint8Array(maxImportBytes + 1)], 'large.ics'))).toThrow('9 MiB');
  fetch.mockResolvedValue(new Response('{"message":{"text":"Too large"}}', { status: 413 }));
  await expect(importICS(file)).rejects.toThrow('Too large');
});

it('handles an entirely empty relations catalog and bounds pagination', async () => {
  const fetch = vi.fn().mockResolvedValueOnce(new Response('', { status: 404 }));
  vi.stubGlobal('fetch', fetch);
  expect(await getRelations()).toEqual([]);
  fetch.mockImplementation(() =>
    Promise.resolve(
      new Response(
        JSON.stringify({
          payload: Array.from({ length: 200 }, () => ({ entity: 'A', event_group: 'G', event_id: null })),
        }),
      ),
    ),
  );
  await expect(getRelations()).rejects.toThrow('20,000-row');
  expect(fetch).toHaveBeenCalledTimes(101);
});

it('builds absolute rolling literal ICS scope and handles blob/error downloads', async () => {
  vi.stubGlobal('document', { baseURI: 'https://calendar.example/calendar/' });
  const url = new URL(icsURL('A,B & +', 'G,H'));
  expect(url.origin).toBe('https://calendar.example');
  expect(url.pathname).toBe('/calendar/v1/ics');
  expect(url.searchParams.get('entity[eq]')).toBe('A,B & +');
  expect(url.searchParams.get('event_group[eq]')).toBe('G,H');
  expect(url.searchParams.has('year')).toBe(false);
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(new Response('BEGIN:VCALENDAR'))
    .mockResolvedValueOnce(new Response('Bad gateway', { status: 502 }));
  vi.stubGlobal('fetch', fetch);
  expect(await (await downloadICS('', '', '2026')).text()).toBe('BEGIN:VCALENDAR');
  expect(fetch.mock.calls[0][0]).toContain('year=2026');
  await expect(downloadICS()).rejects.toThrow('502');
});

it.each(['/', '/calendar/', '/tools/team/calendar/', '/tools/team/calendar/index.html'])(
  'resolves API and subscription paths relative to %s',
  async (path) => {
    const baseURI = `https://calendar.example${path}`;
    vi.stubGlobal('document', { baseURI });
    const fetch = vi.fn().mockResolvedValue(new Response('{"payload":[]}'));
    vi.stubGlobal('fetch', fetch);
    await getOccurrences(new Date(), new Date());
    const expected = `${new URL('.', baseURI).pathname}v1/`;
    expect(new URL(fetch.mock.calls[0][0], baseURI).pathname).toBe(`${expected}occurrences`);
    expect(new URL(icsURL()).pathname).toBe(`${expected}ics`);
  },
);
