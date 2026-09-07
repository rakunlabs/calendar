import assert from 'node:assert/strict';
import { chromium, expect } from '@playwright/test';
import { createServer } from 'vite';

// Read-only smoke check against CALENDAR_API_URL (default localhost:8080).
// No fixtures are written to the running service.
const server = await createServer({ server: { port: 0, host: '127.0.0.1' } });
let browser;
try {
  await server.listen();
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/calendar/`);
  await expect(page.getByLabel('Entity', { exact: true })).toBeEnabled();
  await expect(page.getByRole('alert')).toHaveCount(0);
  const response = await page.request.get(
    new URL(
      'v1/occurrences?from=2026-09-01T00:00:00Z&to=2026-10-01T00:00:00Z&entity=Read-only%2C%20smoke%20%26%20%2B',
      page.url(),
    ).href,
  );
  assert.equal(response.status(), 200);
  assert(Array.isArray((await response.json()).payload));
  await page.getByRole('button', { name: 'Calendar tools', exact: true }).click();
  await page.getByRole('button', { name: 'Import & export', exact: true }).click();
  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Download ICS', exact: true }).click();
  assert.equal(await (await download).failure(), null);
  const subscription = await page.getByLabel('Subscription URL (rolling years)').inputValue();
  const ics = await page.request.get(subscription);
  assert.equal(ics.status(), 200);
  assert((await ics.text()).includes('BEGIN:VCALENDAR'));
  assert.deepEqual(errors, []);
  console.log(
    'PASS: live read-only catalog, literal occurrence filter, ICS download and subscription endpoint; no service data modified',
  );
} finally {
  await browser?.close();
  await server.close();
}
