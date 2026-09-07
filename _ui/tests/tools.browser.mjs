import assert from 'node:assert/strict';
import { chromium, expect } from '@playwright/test';
import { createServer } from 'vite';

const server = await createServer({ server: { port: 0, host: '127.0.0.1' } });
let browser;
try {
  await server.listen();
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [],
    requests = [],
    writes = [];
  page.on('pageerror', (e) => errors.push(e.message));
  const entity = 'Team, R&D + /';
  let relations = Array.from({ length: 201 }, (_, i) => ({
    entity: i ? `Entity ${i}` : entity,
    event_group: 'Work, R&D',
    event_id: null,
  }));
  let failImport = true;
  let failAssignment = true;
  relations[1] = { entity: 'Combined entity', event_group: 'Work, R&D', event_id: 'outside/disabled' };
  const event = {
    id: 'outside/disabled',
    name: 'Archived planning',
    event_group: 'Work, R&D',
    date_from: '2020-01-01T09:00:00Z',
    date_to: '2020-01-01T10:00:00Z',
    tz: 'UTC',
    disabled: true,
    all_day: false,
    rrule: '',
    description: '',
  };
  await page.addInitScript(() => Object.defineProperty(navigator, 'clipboard', { value: undefined }));
  await page.route('**/v1/**', (route) => {
    const req = route.request(),
      url = new URL(req.url());
    requests.push(url);
    if (url.pathname.endsWith('/relations')) {
      if (req.method() === 'POST') {
        if (failAssignment)
          return route.fulfill({
            status: 503,
            json: { message: { text: 'Assignment service unavailable' } },
          });
        const row = req.postDataJSON();
        writes.push(row);
        relations.push({ event_group: null, event_id: null, ...row });
        return route.fulfill({ json: { payload: [] } });
      }
      if (req.method() === 'DELETE') {
        assert.equal(url.searchParams.get('_exact'), 'true');
        relations = relations.filter(
          (r) =>
            !(
              r.entity === url.searchParams.get('entity') &&
              r.event_group === url.searchParams.get('event_group') &&
              r.event_id === url.searchParams.get('event_id')
            ),
        );
        return route.fulfill({ status: 200, body: '' });
      }
      const offset = Number(url.searchParams.get('_offset'));
      if (!relations.length)
        return route.fulfill({ status: 404, json: { message: { text: 'no relations found' } } });
      return route.fulfill({ json: { payload: relations.slice(offset, offset + 200) } });
    }
    if (url.pathname.endsWith('/ics')) {
      if (req.method() === 'POST') {
        assert(req.headers()['content-type'].includes('multipart/form-data'));
        assert(req.postData().includes('Europe/Istanbul'));
        assert(req.postData().includes('Imported group'));
        assert.equal(url.searchParams.get('event_group'), 'Imported group');
        assert.equal(url.searchParams.get('tz'), 'Europe/Istanbul');
        return route.fulfill(
          failImport
            ? { status: 400, json: { message: { text: 'Unsupported exception' } } }
            : { json: { payload: [] } },
        );
      }
      return route.fulfill({ contentType: 'text/calendar', body: 'BEGIN:VCALENDAR\r\nEND:VCALENDAR' });
    }
    return route.fulfill({ json: { payload: url.pathname.endsWith('/events') ? [event] : [] } });
  });
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/calendar/`);
  await expect(page.getByLabel('Entity', { exact: true })).toBeEnabled();
  assert(requests.some((u) => u.pathname.endsWith('/relations') && u.searchParams.get('_offset') === '200'));
  await page.getByLabel('Entity', { exact: true }).selectOption(entity);
  await expect
    .poll(() =>
      requests.some((u) => u.pathname.endsWith('/occurrences') && u.searchParams.get('entity') === entity),
    )
    .toBe(true);
  await page.getByRole('button', { name: 'Calendar tools', exact: true }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await page.getByRole('button', { name: 'Remove assignment for Work, R&D', exact: true }).click();
  await expect(page.getByText('No assignments for this entity.', { exact: false })).toBeVisible();
  await page.getByLabel('Entity name', { exact: true }).fill('New, entity');
  await page.getByLabel('Assignment group').fill('Work, R&D');
  await page.getByRole('button', { name: 'Add assignment', exact: true }).click();
  await expect(page.getByRole('alert')).toHaveText('Assignment service unavailable');
  await expect(page.getByLabel('Entity name', { exact: true })).toHaveValue('New, entity');
  failAssignment = false;
  await page.getByRole('button', { name: 'Add assignment', exact: true }).click();
  await expect(page.getByText('Group: Work, R&D', { exact: true })).toBeVisible();
  assert.deepEqual(writes[0], { entity: 'New, entity', event_group: 'Work, R&D' });
  await page.getByLabel('Assign', { exact: true }).selectOption('event');
  await page.getByLabel('Find an event').fill('Archived');
  await page.getByLabel('Assignment event').selectOption(event.id);
  await page.getByRole('button', { name: 'Add assignment', exact: true }).click();
  await expect(page.getByText('Event: Archived planning', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: `Remove assignment for ${event.id}`, exact: true }).click();
  await expect(page.getByText('Event: Archived planning', { exact: true })).toHaveCount(0);
  await page.getByLabel('Entity name', { exact: true }).fill('Combined entity');
  await expect(page.getByText('Combined rule.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Remove assignment for Work, R&D', exact: true }).click();
  await expect(page.getByText('No assignments for this entity.', { exact: false })).toBeVisible();
  assert(
    requests.some(
      (u) =>
        u.searchParams.get('_exact') === 'true' &&
        u.searchParams.get('event_group') === 'Work, R&D' &&
        u.searchParams.get('event_id') === event.id,
    ),
  );
  await page.getByRole('button', { name: 'Import & export', exact: true }).click();
  await page.getByLabel('Export group scope').selectOption('Work, R&D');
  await page.getByLabel('Download year (optional)').fill('2026');
  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Download ICS', exact: true }).click();
  assert((await download).suggestedFilename().endsWith('.ics'));
  await page.getByRole('button', { name: 'Copy subscription URL', exact: true }).click();
  await expect(page.getByText('Clipboard unavailable.', { exact: false })).toBeVisible();
  const subscription = new URL(await page.getByLabel('Subscription URL (rolling years)').inputValue());
  assert.equal(subscription.searchParams.get('entity[eq]'), entity);
  assert.equal(subscription.searchParams.get('event_group[eq]'), 'Work, R&D');
  assert.equal(subscription.searchParams.has('year'), false);
  await page.getByLabel('ICS file (up to 9 MiB)').setInputFiles({
    name: 'events.ics',
    mimeType: 'text/calendar',
    buffer: Buffer.from('BEGIN:VCALENDAR\r\nEND:VCALENDAR'),
  });
  await page.getByLabel('Import group (optional)').fill('Imported group');
  await page.getByLabel('Import time zone (optional)').fill('Europe/Istanbul');
  await page.getByRole('button', { name: 'Import file', exact: true }).click();
  await expect(page.getByRole('alert')).toHaveText('Unsupported exception');
  failImport = false;
  await page.getByRole('button', { name: 'Import file', exact: true }).click();
  await expect(page.getByText('Import completed.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(page.getByLabel('Entity', { exact: true })).toHaveValue(entity);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Toggle calendar filters' }).click();
  await page.getByRole('button', { name: 'Calendar tools', exact: true }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  assert(await page.getByRole('dialog').evaluate((el) => el.scrollWidth <= el.clientWidth));
  if (process.env.SCREENSHOT_DIR)
    await page.screenshot({ path: `${process.env.SCREENSHOT_DIR}/tools-mobile.png`, fullPage: true });
  await page.getByRole('button', { name: 'Import & export', exact: true }).click();
  assert(await page.getByRole('dialog').evaluate((el) => el.scrollWidth <= el.clientWidth));
  if (process.env.SCREENSHOT_DIR) {
    await page.screenshot({ path: `${process.env.SCREENSHOT_DIR}/tools-import-mobile.png`, fullPage: true });
    await page.getByLabel('Subscription URL (rolling years)').scrollIntoViewIfNeeded();
    await page.screenshot({ path: `${process.env.SCREENSHOT_DIR}/tools-export-mobile.png`, fullPage: true });
  }
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await page.getByRole('button', { name: 'Close filters', exact: true }).click();
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.getByRole('button', { name: 'Calendar tools', exact: true }).click();
  if (process.env.SCREENSHOT_DIR)
    await page.screenshot({ path: `${process.env.SCREENSHOT_DIR}/tools-desktop.png`, fullPage: true });
  await page.getByRole('button', { name: 'Import & export', exact: true }).click();
  if (process.env.SCREENSHOT_DIR) {
    await page.screenshot({ path: `${process.env.SCREENSHOT_DIR}/tools-import-desktop.png`, fullPage: true });
    await page.getByLabel('Subscription URL (rolling years)').scrollIntoViewIfNeeded();
    await page.screenshot({ path: `${process.env.SCREENSHOT_DIR}/tools-export-desktop.png`, fullPage: true });
  }
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  relations = [];
  await page.getByRole('button', { name: 'Refresh calendar', exact: true }).click();
  await expect(page.getByText('No entities yet.', { exact: false })).toBeVisible();
  await expect(page.getByLabel('Entity', { exact: true })).toHaveValue(entity);
  assert.deepEqual(errors, []);
  console.log(
    'PASS: paginated entities, literal filtering, group/event assignments, exact deletion, retained selection, import errors/retry, download scope, rolling subscription clipboard fallback, mobile tools',
  );
} finally {
  await browser?.close();
  await server.close();
}
