import { afterEach, expect, it, vi } from 'vitest';
import { deleteEvent, getEvents, getOccurrences } from './api';

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
