import assert from 'node:assert/strict';
import { chromium } from '@playwright/test';
import { createServer } from 'vite';
import { readFile } from 'node:fs/promises';

// Mount only the editor; API writes are intercepted and never reach the service.
const server = await createServer({
  base: '/',
  server: { port: 0, host: '127.0.0.1' },
  plugins: [
    {
      name: 'isolated-editor-test',
      configureServer(server) {
        server.middlewares.use('/editor-test', (_req, res) => {
          res.setHeader('Content-Type', 'text/html');
          res.end(
            '<html><head><meta name="viewport" content="width=device-width, initial-scale=1"></head><body><script type="module" src="/__editor_entry.js"></script></body></html>',
          );
        });
      },
      resolveId(id) {
        if (id === '/__editor_entry.js') return '\0editor-test';
      },
      load(id) {
        if (id !== '\0editor-test') return;
        return `
        import { mount, unmount } from 'svelte';
        import Editor from '/src/EventEditor.svelte';
        import '/src/app.css';
        let editor;
        window.openEditor = async (props = {}) => {
          if (editor) await unmount(editor);
          editor = mount(Editor, { target: document.body, props: {
            event: null, groups: [], onclose() {}, onsaved() {}, ...props,
            day: new Date(props.day || '2026-09-07T09:30:00-04:00'),
            endDay: props.endDay ? new Date(props.endDay) : undefined,
          }});
        };
      `;
      },
    },
  ],
});
let browser;
try {
  await server.listen();
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({
    timezoneId: 'America/New_York',
    viewport: { width: 1440, height: 1050 },
  });
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  let saved;
  await page.route('**/v1/events**', (route) => {
    saved = route.request().postDataJSON();
    return route.fulfill({ json: { payload: saved } });
  });
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/editor-test`);
  await page.waitForFunction(() => !!window.openEditor);
  const open = async (props = {}) => {
    await page.evaluate((props) => window.openEditor(props), props);
    await page.getByRole('dialog').waitFor();
  };
  const value = (label) => page.getByLabel(label, { exact: true }).inputValue();
  const fill = (label, value) => page.getByLabel(label, { exact: true }).fill(value);
  const save = async (existing = false) => {
    saved = undefined;
    await fill('Event name', 'Editor test');
    const response = page.waitForResponse('**/v1/events**');
    await page.getByRole('button', { name: existing ? 'Save changes' : 'Create event', exact: true }).click();
    await response;
    return saved;
  };

  await open();
  assert.equal(await value('Time zone'), 'America/New_York');
  assert.equal(await value('Start date'), '2026-09-07');
  assert.equal(await value('End date'), '2026-09-07');
  assert.equal((await save()).date_to, '2026-09-08T04:00:00.000Z');
  await page.getByLabel('Time zone', { exact: true }).selectOption('UTC');
  assert.equal((await save()).date_to, '2026-09-08T00:00:00.000Z');
  await page.getByLabel('All-day event').uncheck();
  assert.equal(await value('Start time'), '09:00');
  assert.equal(await value('End time'), '10:00');

  await open({ day: '2026-10-30T00:00:00-04:00', endDay: '2026-11-02T00:00:00-05:00' });
  assert.equal(await page.getByLabel('All-day event').isChecked(), true);
  assert.equal(await value('Time zone'), 'America/New_York');
  assert.equal(await value('Start date'), '2026-10-30');
  assert.equal(await value('End date'), '2026-11-02');
  const range = await save();
  assert.equal(range.all_day, true);
  assert.equal(range.date_from, '2026-10-30T04:00:00.000Z');
  assert.equal(range.date_to, '2026-11-03T05:00:00.000Z');

  await open({ hour: 9.5, endDay: '2026-09-07T11:00:00-04:00' });
  assert.equal(await value('Time zone'), 'America/New_York');
  assert.equal(await value('Start time'), '09:30');
  assert.equal(await value('End time'), '11:00');
  await fill('Start time', '23:30');
  await page.getByLabel('End time', { exact: true }).focus();
  assert.equal(await value('End date'), '2026-09-08');
  assert.equal(await value('End time'), '01:00');
  assert.equal((await save()).date_from, '2026-09-08T03:30:00.000Z');

  await open({ hour: 23.5, day: '2026-09-07T23:30:00-04:00', endDay: '2026-09-08T00:00:00-04:00' });
  await page.getByLabel('All-day event').check();
  assert.equal(await value('End date'), '2026-09-07');
  await page.getByLabel('All-day event').uncheck();
  assert.equal(await value('End date'), '2026-09-08');
  assert.equal(await value('End time'), '00:00');

  await open({
    event: {
      id: 'existing',
      name: 'Existing',
      tz: 'Pacific/Auckland',
      all_day: true,
      date_from: '2026-09-06T12:00:00Z',
      date_to: '2026-09-09T12:00:00Z',
    },
  });
  assert.equal(await value('Time zone'), 'Pacific/Auckland');
  assert.equal(await value('End date'), '2026-09-09');
  await fill('Start date', '2026-09-08');
  await page.getByLabel('End date', { exact: true }).focus();
  assert.equal(await value('End date'), '2026-09-10');
  assert.equal((await save(true)).date_to, '2026-09-10T12:00:00.000Z');
  await page.getByLabel('All-day event').uncheck();
  assert.equal(await value('End date'), '2026-09-10');

  const existing = {
    id: 'existing', name: 'Existing', event_group: 'Team', tz: 'UTC', all_day: true,
    date_from: '2026-01-01T00:00:00Z', date_to: '2026-01-02T00:00:00Z',
  };
  await open({ groups: ['Team', 'Holidays', 'Ungrouped'], event: existing });
  const groupSelect = page.getByLabel('Calendar group', { exact: true });
  assert.deepEqual(await groupSelect.locator('option').allTextContents(), ['No group', 'Holidays', 'Team', 'Ungrouped']);
  await groupSelect.selectOption('Ungrouped');
  assert.equal((await save(true)).event_group, 'Ungrouped');
  await groupSelect.selectOption('');
  assert.equal((await save(true)).event_group, null);
  await page.getByLabel('Enter a group name').check();
  await fill('Group name', 'New calendar');
  assert.equal((await save(true)).event_group, 'New calendar');
  await page.getByLabel('Enter a group name').uncheck();
  await groupSelect.selectOption('Holidays');
  assert.equal((await save(true)).event_group, 'Holidays');

  const supported = [...(await readFile('../pkg/ical/special/func.go', 'utf8')).matchAll(/"([A-Z]+)":/g)].map(match => match[1]);
  const repeat = page.getByLabel('Repeat', { exact: true });
  assert.deepEqual(await repeat.locator('optgroup option').evaluateAll(options => options.map(option => option.value)), supported.map(name => `FUNC:${name}`));
  for (const name of supported) {
    await repeat.selectOption(`FUNC:${name}`);
    assert.equal((await save(true)).rrule, `FUNC:${name}`);
  }
  for (const rrule of ['FUNC:EASTERSUNDAY', 'FUNC:GoodFriday', 'RRULE:FREQ=WEEKLY;COUNT=8\nFUNC:EasterMonday FUNC:WHITMONDAY']) {
    await open({ event: { ...existing, rrule } });
    assert.equal((await save(true)).rrule, rrule, 'Unchanged recurrence must remain byte-for-byte intact');
    await repeat.selectOption('FUNC:ASCENSIONDAY');
    assert.equal((await save(true)).rrule, 'FUNC:ASCENSIONDAY');
    if (rrule !== 'FUNC:EASTERSUNDAY') {
      await repeat.selectOption('custom');
      assert.equal((await save(true)).rrule, rrule, 'Returning to the existing rule restores mixed rules');
    }
  }
  const timed = {
    ...existing, all_day: false, date_from: '2026-01-01T09:30:00Z', date_to: '2026-01-03T15:00:00Z',
    rrule: 'RRULE:FREQ=WEEKLY;COUNT=8\nFUNC:EasterMonday FUNC:WHITMONDAY',
  };
  await open({ event: timed });
  let result = await save(true);
  assert.equal(result.all_day, false);
  assert.equal(result.date_to, '2026-01-03T15:00:00.000Z');
  await repeat.selectOption('FUNC:GOODFRIDAY');
  assert.equal(await page.getByLabel('All-day event').isChecked(), true);
  assert.equal(await page.getByLabel('All-day event').isDisabled(), true);
  assert.equal(await page.getByLabel('Start time', { exact: true }).count(), 0);
  assert.equal(await page.getByLabel('End time', { exact: true }).count(), 0);
  assert.equal(await page.getByLabel('Start date', { exact: true }).isDisabled(), true);
  assert.equal(await page.getByLabel('End date', { exact: true }).isDisabled(), true);
  assert.match(await page.locator('#event-special-help').textContent(), /one full calendar day/);
  result = await save(true);
  assert.equal(result.rrule, 'FUNC:GOODFRIDAY');
  assert.equal(result.all_day, true);
  assert.equal(result.date_from, '2026-01-01T00:00:00.000Z');
  assert.equal(result.date_to, '2026-01-02T00:00:00.000Z');
  await repeat.selectOption('custom');
  result = await save(true);
  assert.equal(result.rrule, timed.rrule);
  assert.equal(result.all_day, false);
  assert.equal(result.date_from, '2026-01-01T09:30:00.000Z');
  assert.equal(result.date_to, '2026-01-03T15:00:00.000Z');
  await open({ hour: 9.5 });
  await repeat.selectOption('FUNC:EASTERSUNDAY');
  result = await save();
  assert.equal(result.all_day, true);
  assert.equal(result.date_from, '2026-09-07T04:00:00.000Z');
  assert.equal(result.date_to, '2026-09-08T04:00:00.000Z');
  await open({ groupsLoading: true });
  assert.equal(await page.getByRole('status').textContent(), 'Loading groups...');
  await open({ groupsError: 'Service unavailable.' });
  assert.match(await page.getByRole('alert').textContent(), /Could not load groups/);
  assert.equal(await page.getByRole('button', { name: 'Retry groups' }).isEnabled(), true);
  await open({ groups: ['Team', 'Holidays'], event: { ...existing, rrule: 'FUNC:EASTERSUNDAY' } });
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 844 });
    assert(
      await page.getByRole('dialog').evaluate((el) => el.scrollWidth <= el.clientWidth),
      `Editor overflow at ${width}px`,
    );
    await groupSelect.selectOption('Holidays');
    await repeat.selectOption('FUNC:WHITMONDAY');
    assert.equal((await save(true)).rrule, 'FUNC:WHITMONDAY');
    if (process.env.SCREENSHOT_DIR) {
      await page.getByRole('dialog').evaluate(el => { el.scrollTop = 0; });
      await page.screenshot({ path: `${process.env.SCREENSHOT_DIR}/editor-special-${width}.png`, fullPage: true });
    }
  }
  assert.deepEqual(errors, []);
  console.log(
    'PASS: editor dates/DST, groups/new names/no group, all supported FUNC presets, lossless custom/mixed recurrence, loading/errors, desktop/mobile selection and overflow.',
  );
} finally {
  await browser?.close();
  await server.close();
}
