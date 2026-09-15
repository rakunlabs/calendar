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
  page.on('pageerror', error => errors.push(error.message));
  const monday = new Date();
  monday.setUTCDate(monday.getUTCDate() - (monday.getUTCDay() + 6) % 7);
  monday.setUTCHours(0, 0, 0, 0);
  const date = offset => { const d = new Date(monday); d.setUTCDate(d.getUTCDate() + offset); return d.toISOString().slice(0, 10); };
  let master = { id: 'trip', name: 'Team trip', description: '', event_group: 'Team',
    date_from: `${date(1)}T00:00:00Z`, date_to: `${date(4)}T00:00:00Z`, tz: 'UTC', all_day: true,
    rrule: '', disabled: false, updated_at: '2026-01-01T00:00:00Z' };
  // Earlier overlapping event reserves lane zero, so the trip must retain lane one afterwards.
  const overlap = { ...master, id: 'other', name: 'Earlier holiday', date_from: `${date(0)}T00:00:00Z`, date_to: `${date(2)}T00:00:00Z` };
  const writes = [];
  await page.route('**/v1/**', route => {
    const request = route.request();
    if (request.method() === 'PUT') {
      master = request.postDataJSON(); writes.push(master);
      return route.fulfill({ json: { payload: master } });
    }
    return route.fulfill({ json: { payload: request.url().includes('/relations') ? [] : [master, overlap] } });
  });
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/calendar/`);
  const chip = (day, view) => page.locator(`${view === 'Month' ? '.month-cell' : '.all-day-drop'}[data-drop-date="${date(day)}"] .event-chip`).filter({ hasText: 'Team trip' });
  const checkConnection = async view => {
    const a = await chip(1, view).boundingBox();
    const b = await chip(2, view).boundingBox();
    const c = await chip(3, view).boundingBox();
    assert(Math.abs(a.y - b.y) < 1 && Math.abs(b.y - c.y) < 1, `${view}: stable lanes across overlapping dates`);
    assert(Math.abs(a.x + a.width - b.x) < 1 && Math.abs(b.x + b.width - c.x) < 1, `${view}: connected bars have no gap`);
    await expect(chip(2, view).locator('.event-resize-handle:not([hidden])')).toHaveCount(0);
    await expect(chip(1, view).locator('.all-day-resize-left')).toBeVisible();
    await expect(chip(3, view).locator('.all-day-resize-right')).toBeVisible();
  };
  const resize = async (view, edgeDay, side, targetDay, cancellation) => {
    const event = chip(edgeDay, view);
    await event.scrollIntoViewIfNeeded();
    await event.hover();
    const a = await event.locator(`.all-day-resize-${side}`).boundingBox();
    const target = page.locator(`${view === 'Month' ? '.month-cell' : '.all-day-drop'}[data-drop-date="${date(targetDay)}"]`);
    const b = await target.boundingBox();
    await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2);
    await page.mouse.down();
    await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2, { steps: 8 });
    await expect(page.locator('.all-day-resize-preview')).not.toHaveCount(0);
    await expect(page.locator('.event-drag-preview, .month-cell.range-selected, .all-day-selected')).toHaveCount(0);
    if (cancellation === 'Escape') await page.keyboard.press('Escape');
    else if (cancellation) await page.evaluate(reason => window.dispatchEvent(reason === 'pointercancel' ? new PointerEvent(reason) : new Event(reason)), cancellation);
    await page.mouse.up();
    await expect(page.locator('.all-day-resize-preview, .event-drag-status')).toHaveCount(0);
    await expect(page.getByRole('dialog')).toHaveCount(0);
  };
  await expect(chip(1, 'Month')).toBeEnabled();
  await checkConnection('Month');
  await page.getByRole('button', { name: 'Week', exact: true }).click();
  await checkConnection('Week');
  for (const reason of ['Escape', 'blur', 'pointercancel']) await resize('Week', 3, 'right', 5, reason);
  assert.equal(writes.length, 0);
  await resize('Week', 3, 'right', 5);
  await expect.poll(() => writes.length).toBe(1);
  assert.equal(master.date_from, `${date(1)}T00:00:00.000Z`);
  assert.equal(master.date_to, `${date(6)}T00:00:00.000Z`);
  await expect(chip(5, 'Week')).toBeEnabled();
  await resize('Week', 1, 'left', 0);
  await expect.poll(() => writes.length).toBe(2);
  assert.equal(master.date_from, `${date(0)}T00:00:00.000Z`);
  assert.equal(master.date_to, `${date(6)}T00:00:00.000Z`);
  await page.getByRole('button', { name: 'Month', exact: true }).click();
  await expect(chip(5, 'Month')).toBeEnabled();
  await resize('Month', 5, 'right', 8);
  await expect.poll(() => writes.length).toBe(3);
  assert.equal(master.date_to, `${date(9)}T00:00:00.000Z`);
  await expect(chip(6, 'Month').getByLabel('Continues on next week')).toBeVisible();
  await expect(chip(7, 'Month').getByLabel('Continues from previous week')).toBeVisible();
  await expect(chip(8, 'Month')).toBeEnabled();
  await resize('Month', 0, 'left', 2);
  await expect.poll(() => writes.length).toBe(4);
  assert.equal(master.date_from, `${date(2)}T00:00:00.000Z`);
  assert.equal(master.date_to, `${date(9)}T00:00:00.000Z`);
  await expect(chip(0, 'Month')).toHaveCount(0);
  await expect(chip(8, 'Month')).toBeEnabled();
  await resize('Month', 8, 'right', 0);
  await expect.poll(() => writes.length).toBe(5);
  assert.equal(master.date_to, `${date(3)}T00:00:00.000Z`, 'At least one calendar day remains');
  assert.deepEqual(errors, []);
  console.log('PASS: connected all-day lanes, horizontal resizing, week boundaries, cancellation and one-day minimum.');
} finally {
  await browser?.close();
  await server.close();
}
