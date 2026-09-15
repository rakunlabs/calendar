import assert from 'node:assert/strict';
import { chromium, expect } from '@playwright/test';
import { createServer } from 'vite';

const server = await createServer({ server: { port: 0, host: '127.0.0.1' } });
let browser;
try {
  await server.listen();
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1050 }, timezoneId: 'UTC' });
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  const today = new Date();
  const date = today.toISOString().slice(0, 10);
  let master = { id: 'local', name: 'Movable meeting', description: '', event_group: 'Team',
    date_from: `${date}T09:00:00Z`, date_to: `${date}T10:00:00Z`, tz: 'UTC', all_day: false,
    rrule: '', disabled: false, updated_at: '2026-01-01T00:00:00Z' };
  const writes = [];
  let feedRequests = 0;
  let failMove = false;
  let failFeed = false;
  await page.route('**/v1/**', async route => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    if (path.endsWith('/subscriptions/occurrences')) {
      feedRequests++;
      assert.equal(req.postDataJSON().url, 'https://example.com/team.ics');
      if (failFeed) return route.fulfill({ status: 502, json: { message: { error: 'Feed unavailable' } } });
      return route.fulfill({ json: { payload: [{ ...master, id: 'local', name: 'Remote meeting' }] } });
    }
    if (req.method() === 'PUT') {
      writes.push(req.postDataJSON());
      if (failMove) return route.fulfill({ status: 409, json: { message: { error: 'Event changed. Refresh before retrying.' } } });
      master = req.postDataJSON();
      return route.fulfill({ json: { payload: master } });
    }
    return route.fulfill({ json: { payload: path.endsWith('/relations') ? [] : [master] } });
  });
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/calendar/`);
  await expect(page.locator('.month-cell .event-chip').first()).toBeEnabled();
  const source = page.locator('.month-cell .event-chip').first();
  const destination = page.locator(`[data-date="${date}"]`).locator('xpath=following-sibling::*[1]');
  const nextDate = await destination.getAttribute('data-date');
  const drag = async (from, to, cancel = false) => {
    await from.scrollIntoViewIfNeeded();
    const a = await from.boundingBox();
    const b = await to.boundingBox();
    await page.mouse.move(a.x + a.width / 2, a.y + 5);
    await page.mouse.down();
    await page.mouse.move(b.x + b.width / 2, b.y + 5, { steps: 10 });
    await expect(page.locator('.event-drag-preview')).toBeVisible();
    if (cancel) await page.keyboard.press('Escape');
    await page.mouse.up();
  };
  await drag(source, destination, true);
  assert.equal(writes.length, 0);
  await drag(source, destination);
  await expect.poll(() => writes.length).toBe(1);
  assert.equal(writes[0].date_from, `${nextDate}T09:00:00.000Z`);
  assert.equal(writes[0].date_to, `${nextDate}T10:00:00.000Z`);
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.locator('.month-cell .event-chip').first()).toBeEnabled();
  await page.getByRole('button', { name: 'Day', exact: true }).click();
  await expect(page.locator('.week-event')).toBeEnabled();
  const slot = page.locator('[data-week-slot="21"]'); // 10:30
  await drag(page.locator('.week-event'), slot);
  await expect.poll(() => writes.length).toBe(2);
  assert.equal(writes[1].date_from, `${nextDate}T10:30:00.000Z`);
  assert.equal(writes[1].date_to, `${nextDate}T11:30:00.000Z`);
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.locator('.week-event')).toBeEnabled();
  failMove = true;
  await drag(page.locator('.week-event'), page.locator('[data-week-slot="24"]'));
  await expect(page.getByText('Event changed. Refresh before retrying.')).toBeVisible();
  assert.equal(master.date_from, `${nextDate}T10:30:00.000Z`);

  // A multi-day all-day event grabbed on its second day retains its full span.
  failMove = false;
  const plusDays = (date, days) => { const d = new Date(`${date}T00:00:00Z`); d.setUTCDate(d.getUTCDate() + days); return d.toISOString().slice(0, 10); };
  master = {...master, all_day: true, date_from: `${date}T00:00:00Z`, date_to: `${plusDays(date, 3)}T00:00:00Z`};
  await page.getByRole('button', { name: 'Month', exact: true }).click();
  await page.getByRole('button', { name: 'Refresh calendar', exact: true }).click();
  const secondDay = page.locator(`[data-date="${plusDays(date, 1)}"] .event-chip`);
  await expect(secondDay).toBeEnabled();
  await drag(secondDay, page.locator(`[data-date="${plusDays(date, 4)}"]`));
  await expect.poll(() => writes.length).toBe(4);
  assert.equal(master.date_from, `${plusDays(date, 3)}T00:00:00.000Z`);
  assert.equal(master.date_to, `${plusDays(date, 6)}T00:00:00.000Z`);
  // Restore a timed fixture for the read-only subscription checks below.
  master = {...master, all_day: false, date_from: `${plusDays(date, 3)}T09:00:00Z`, date_to: `${plusDays(date, 3)}T10:00:00Z`};
  await page.getByRole('button', { name: 'Day', exact: true }).click();
  await expect(page.locator('.week-event')).toBeEnabled();

  await page.getByRole('button', { name: 'Add subscription', exact: true }).click();
  await page.getByLabel('Calendar name', { exact: true }).fill('External team');
  await page.getByLabel('ICS / webcal URL').fill('webcal://example.com/team.ics');
  await page.getByRole('button', { name: 'Subscribe', exact: true }).click();
  await expect(page.locator('.week-event', { hasText: 'Remote meeting' })).toBeVisible();
  await page.locator('.week-event', { hasText: 'Remote meeting' }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.getByText(/Read-only subscription; edit in the source calendar/)).toBeVisible();
  const count = writes.length;
  const remote = page.locator('.week-event', { hasText: 'Remote meeting' });
  const box = await remote.boundingBox();
  await page.mouse.move(box.x + 5, box.y + 5);
  await page.mouse.down();
  await page.mouse.move(box.x + 5, box.y + 100, { steps: 8 });
  await page.mouse.up();
  assert.equal(writes.length, count);
  await page.reload();
  await expect(page.getByRole('checkbox', { name: 'External team' })).toBeChecked();
  await expect.poll(() => feedRequests).toBeGreaterThan(1);
  await page.getByRole('checkbox', { name: 'External team' }).uncheck();
  await expect(page.locator('.month-cell .event-chip', { hasText: 'Remote meeting' })).toHaveCount(0);
  failFeed = true;
  await page.getByRole('checkbox', { name: 'External team' }).check();
  await expect(page.getByText('External team: Feed unavailable')).toBeVisible();
  await page.getByRole('button', { name: 'Unsubscribe from External team' }).click();
  await expect(page.getByRole('checkbox', { name: 'External team' })).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole('checkbox', { name: 'External team' })).toHaveCount(0);
  assert.deepEqual(errors, []);
  console.log('Event moves and subscriptions: passed');
} finally {
  await browser?.close();
  await server.close();
}
