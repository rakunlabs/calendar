import assert from 'node:assert/strict';
import { chromium, expect } from '@playwright/test';
import { createServer } from 'vite';

// Isolated interaction checks: no requests reach a calendar service.
const server = await createServer({ server: { port: 0, host: '127.0.0.1' } });
let browser;
try {
  await server.listen();
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1050 }, timezoneId: 'Europe/Istanbul' });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  const requests = [];
  const writes = [];
  let fixtures = [];
  await page.route('**/v1/**', route => {
    if (new URL(route.request().url()).pathname.endsWith('/relations')) return route.fulfill({ json: { payload: [] } });
    requests.push(route.request().url());
    if (route.request().method() === 'POST') {
      const saved = route.request().postDataJSON();
      writes.push(saved);
      return route.fulfill({ json: { payload: saved } });
    }
    return route.fulfill({ json: { payload: fixtures } });
  });
  const base = `http://127.0.0.1:${server.httpServer.address().port}/calendar/`;
  await page.goto(base);
  await page.locator('.calendar-toolbar h1').waitFor();
  for (const width of [1440, 1649, 1651, 1920]) {
    await page.setViewportSize({ width, height: 1050 });
    const surface = await page.locator('.calendar-surface').boundingBox();
    const agenda = await page.locator('.agenda').boundingBox();
    assert.equal(agenda.width, 247, `Empty agenda width at ${width}`);
    assert.equal(agenda.height, surface.height, `Blank space below empty agenda at ${width}`);
    assert.equal(await page.locator('.month-cell').first().evaluate(el => el.clientHeight), 109);
  }
  await page.setViewportSize({ width: 1440, height: 1050 });
  await expect(page.locator('.topbar').getByRole('button', { name: 'Create event', exact: true })).toHaveCount(0);
  await expect(page.getByText('Your calendar workspace', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'New event', exact: true }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  const cancel = () => page.getByRole('button', { name: 'Cancel', exact: true }).click();
  const beginDrag = async (from, to, blank = false) => {
    await from.scrollIntoViewIfNeeded();
    const first = await from.boundingBox();
    const last = await to.boundingBox();
    assert(first && last, 'Drag endpoints must be visible');
    await page.mouse.move(first.x + first.width / 2, first.y + (blank ? first.height - 8 : first.height / 2));
    await page.mouse.down();
    await page.mouse.move(last.x + last.width / 2, last.y + (blank ? last.height - 8 : last.height / 2), { steps: 8 });
  };
  const checkCancellation = async (from, to, selected, blank = false) => {
    for (const reason of ['Escape', 'blur', 'pointercancel']) {
      await beginDrag(from, to, blank);
      await expect(selected).not.toHaveCount(0);
      if (reason === 'Escape') await page.keyboard.press('Escape');
      else await page.evaluate(reason => window.dispatchEvent(
        reason === 'pointercancel' ? new PointerEvent(reason) : new Event(reason),
      ), reason);
      await expect(selected).toHaveCount(0);
      await page.mouse.up();
      await expect(page.getByRole('dialog')).toHaveCount(0);
    }
  };
  assert.equal(await page.getByText('Your time, in view.', { exact: true }).count(), 0);
  assert.equal(await page.getByText('A little perspective on everything coming up.', { exact: true }).count(), 0);
  await page.locator('.month-add').nth(10).click();
  await page.getByRole('dialog').waitFor();
  assert.equal(await page.getByLabel('Time zone', { exact: true }).inputValue(), 'Europe/Istanbul');
  assert.equal(await page.getByLabel('Start date', { exact: true }).inputValue(), await page.getByLabel('End date', { exact: true }).inputValue());
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  const cells = page.locator('.month-cell');
  const cell = index => page.locator(`[data-month-index="${index}"]`);
  const dates = await cells.evaluateAll(elements => elements.map(el => el.dataset.date));
  const lastInMonth = await cells.evaluateAll(elements => elements.findLastIndex(el => !el.classList.contains('other-month')));
  const rangeEnd = lastInMonth + 2;
  const rangeStart = rangeEnd - 8;
  assert.notEqual(Math.floor(rangeStart / 7), Math.floor(rangeEnd / 7));
  assert.notEqual(dates[rangeStart].slice(0, 7), dates[rangeEnd].slice(0, 7));
  for (const reverse of [false, true]) {
    await beginDrag(reverse ? cell(rangeEnd).locator('.day-select') : cell(rangeStart),
      reverse ? cell(rangeStart) : cell(rangeEnd), !reverse);
    await expect(page.locator('.month-cell.range-selected')).toHaveCount(9);
    assert.deepEqual(await page.locator('.month-cell.range-selected').evaluateAll(elements => elements.map(el => el.dataset.date)), dates.slice(rangeStart, rangeEnd + 1));
    await page.mouse.up();
    await page.getByRole('dialog').waitFor();
    assert.equal(await page.getByLabel('Start date', { exact: true }).inputValue(), dates[rangeStart]);
    assert.equal(await page.getByLabel('End date', { exact: true }).inputValue(), dates[rangeEnd]);
    await expect(page.getByLabel('All-day event')).toBeChecked();
    assert.equal(await page.getByLabel('Time zone', { exact: true }).inputValue(), 'Europe/Istanbul');
    await page.getByLabel('Event name', { exact: true }).fill('Month range regression');
    await Promise.all([
      page.waitForResponse(res => res.request().method() === 'POST' && res.url().includes('/v1/events')),
      page.getByRole('dialog').getByRole('button', { name: 'Create event', exact: true }).click(),
    ]);
    await expect(page.getByRole('dialog')).toHaveCount(0);
    const exclusiveEnd = new Date(`${dates[rangeEnd]}T00:00:00+03:00`);
    exclusiveEnd.setUTCDate(exclusiveEnd.getUTCDate() + 1);
    assert.equal(writes.at(-1).date_from, new Date(`${dates[rangeStart]}T00:00:00+03:00`).toISOString());
    assert.equal(writes.at(-1).date_to, exclusiveEnd.toISOString());
    assert.equal(writes.at(-1).all_day, true);
  }
  await checkCancellation(cell(10), cell(12), page.locator('.month-cell.range-selected'), true);
  await cell(10).click({ position: { x: 50, y: 70 } });
  await page.getByRole('dialog').waitFor();
  assert.equal(await page.getByLabel('Start date', { exact: true }).inputValue(), dates[10]);
  assert.equal(await page.getByLabel('End date', { exact: true }).inputValue(), dates[10]);
  await expect(page.getByLabel('All-day event')).toBeChecked();
  await cancel();
  await cell(11).locator('.day-select').click();
  await expect(cell(11)).toHaveClass(/selected-day/);
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.locator('.month-cell.range-selected')).toHaveCount(0);
  await page.getByRole('button', { name: 'Week', exact: true }).click();
  await page.locator('.week-scroll').waitFor();
  assert.equal(await page.locator('.week-slot').count(), 336);
  const heading = await page.locator('.calendar-toolbar h1').textContent();
  await page.getByRole('button', { name: 'Next week', exact: true }).click();
  assert.notEqual(await page.locator('.calendar-toolbar h1').textContent(), heading);
  await page.getByRole('button', { name: 'Previous week', exact: true }).click();
  assert.equal(await page.locator('.calendar-toolbar h1').textContent(), heading);

  const slot = (day, time) => page.locator(`[data-week-slot="${time}"][data-day="${day}"]`);
  await slot(1, 19).click();
  await page.getByRole('dialog').waitFor();
  assert.equal(await page.getByLabel('Start time', { exact: true }).inputValue(), '09:30');
  assert.equal(await page.getByLabel('End time', { exact: true }).inputValue(), '10:00');
  assert.equal(await page.getByLabel('Time zone', { exact: true }).inputValue(), 'Europe/Istanbul');
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();

  for (const [from, to] of [[20, 23], [23, 20]]) {
    await beginDrag(slot(2, from), slot(2, to));
    await expect(page.locator('.range-selected')).toHaveCount(4);
    await page.mouse.up();
    await page.getByRole('dialog').waitFor();
    assert.equal(await page.getByLabel('Start time', { exact: true }).inputValue(), '10:00');
    assert.equal(await page.getByLabel('End time', { exact: true }).inputValue(), '12:00');
    await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  }
  await slot(0, 18).focus();
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('Enter');
  await page.getByRole('dialog').waitFor();
  assert.equal(await page.getByLabel('Start time', { exact: true }).inputValue(), '09:30');
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await slot(5, 18).focus();
  await expect(slot(5, 18)).toHaveAttribute('tabindex', '0');
  await page.getByRole('button', { name: 'Day', exact: true }).click();
  await expect(page.locator('.week-slot')).toHaveCount(48);
  await expect(page.locator('.week-slot:not([data-day="0"])')).toHaveCount(0);
  const focusable = page.locator('.week-slot[tabindex="0"]');
  await expect(focusable).toHaveCount(1);
  await focusable.focus();
  await page.keyboard.press('ArrowRight');
  await expect(slot(0, 18)).toBeFocused();
  await page.keyboard.press('ArrowLeft');
  await page.keyboard.press('ArrowDown');
  await expect(slot(0, 19)).toBeFocused();
  await page.keyboard.press('Enter');
  await page.getByRole('dialog').waitFor();
  assert.equal(await page.getByLabel('Start time', { exact: true }).inputValue(), '09:30');
  assert.equal(await page.getByLabel('End time', { exact: true }).inputValue(), '10:00');
  await cancel();
  for (const [from, to] of [[20, 23], [23, 20]]) {
    await beginDrag(slot(0, from), slot(0, to));
    await expect(page.locator('.week-slot.range-selected')).toHaveCount(4);
    await page.mouse.up();
    await page.getByRole('dialog').waitFor();
    assert.equal(await page.getByLabel('Start time', { exact: true }).inputValue(), '10:00');
    assert.equal(await page.getByLabel('End time', { exact: true }).inputValue(), '12:00');
    assert.equal(await page.getByLabel('Start date', { exact: true }).inputValue(), await page.getByLabel('End date', { exact: true }).inputValue());
    await cancel();
  }
  await checkCancellation(slot(0, 20), slot(0, 23), page.locator('.week-slot.range-selected'));
  await page.getByRole('button', { name: 'Week', exact: true }).click();
  fixtures = await page.evaluate(() => {
    const monday = new Date();
    monday.setDate(monday.getDate() - (monday.getDay() + 6) % 7);
    monday.setHours(0, 0, 0, 0);
    return [
      ['Planning', 0, 9, 0, 10.5, false],
      ['Overlapping review with a long event title', 0, 9.5, 0, 11, false],
      ['Overnight maintenance', 1, 23, 2, 9, false],
      ['Team holiday', 3, 0, 4, 0, true],
    ].map(([name, fromDay, fromHour, toDay, toHour, allDay], index) => {
      const from = new Date(monday);
      from.setDate(from.getDate() + fromDay);
      from.setMinutes(fromHour * 60);
      const to = new Date(monday);
      to.setDate(to.getDate() + toDay);
      to.setMinutes(toHour * 60);
      return { id: String(index), name, description: '', event_group: 'Team', date_from: from.toISOString(), date_to: to.toISOString(), tz: 'Europe/Istanbul', all_day: allDay, rrule: '', disabled: false };
    });
  });
  await page.getByRole('button', { name: 'Today', exact: true }).click();
  await page.getByRole('button', { name: 'Refresh calendar', exact: true }).click();
  await page.locator('.week-event').first().waitFor();
  assert.equal(await page.locator('.week-event').count(), 4);
  const overlapping = page.locator('.week-column').first().locator('.week-event');
  const firstEvent = await overlapping.nth(0).boundingBox();
  const secondEvent = await overlapping.nth(1).boundingBox();
  assert(firstEvent.x + firstEvent.width <= secondEvent.x, 'Overlapping events need separate columns');
  await overlapping.nth(1).click();
  await page.getByRole('dialog').waitFor();
  assert.equal(await page.getByLabel('Event name', { exact: true }).inputValue(), fixtures[1].name);
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Month', exact: true }).click();
  const chip = page.locator('.month-cell .event-chip').filter({ hasText: 'Planning' }).first();
  await expect(chip).toBeEnabled();
  const chipBox = await chip.boundingBox();
  await page.mouse.move(chipBox.x + chipBox.width / 2, chipBox.y + chipBox.height / 2);
  await page.mouse.down();
  await expect(page.locator('.month-cell.range-selected')).toHaveCount(0);
  await page.mouse.up();
  await page.getByRole('dialog').waitFor();
  assert.equal(await page.getByLabel('Event name', { exact: true }).inputValue(), 'Planning');
  await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeVisible();
  await cancel();
  await chip.locator('..').locator('..').locator('.day-select').click();
  if (process.env.SCREENSHOT_DIR) {
    const dismiss = page.getByRole('button', { name: 'Dismiss notification' });
    if (await dismiss.isVisible()) await dismiss.click();
    await page.mouse.move(0, 0);
  }
  for (const mode of ['Month', 'Day']) {
    await page.getByRole('button', { name: mode, exact: true }).click();
    for (const width of [1440, 1649, 1651, 1920, 390, 320]) {
      await page.setViewportSize({ width, height: 900 });
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${mode} overflow at ${width}`);
      assert(await page.locator('.agenda').evaluate(el => el.scrollWidth <= el.clientWidth), `${mode} agenda horizontal overflow at ${width}`);
      if (mode === 'Month' && width >= 1440) {
        assert.equal((await page.locator('.agenda').boundingBox()).width, 247);
        assert.equal(await page.locator('.event-chip').first().evaluate(el => getComputedStyle(el).fontSize), '9px');
      }
      if (process.env.SCREENSHOT_DIR)
        await page.screenshot({ path: `${process.env.SCREENSHOT_DIR}/calendar-${mode.toLowerCase()}-${width}.png`, fullPage: true });
    }
  }
  await page.getByRole('button', { name: 'Week', exact: true }).click();
  for (const width of [1440, 1649, 1651, 1920, 768, 390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `Overflow at ${width}`);
    if (process.env.SCREENSHOT_DIR && [1440, 390].includes(width))
      await page.screenshot({ path: `${process.env.SCREENSHOT_DIR}/calendar-week-${width}.png`, fullPage: true });
  }
  const mobile = await browser.newPage({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });
  mobile.on('pageerror', error => errors.push(error.message));
  await mobile.route('**/v1/**', route => route.fulfill({ json: { payload: [] } }));
  await mobile.goto(base);
  await mobile.locator('.month-add').nth(10).tap();
  await mobile.getByRole('dialog').waitFor();
  await expect(mobile.getByLabel('All-day event')).toBeChecked();
  await mobile.getByRole('button', { name: 'Cancel', exact: true }).tap();
  await mobile.getByRole('button', { name: 'Week', exact: true }).click();
  await mobile.locator('[data-week-slot="18"][data-day="0"]').tap();
  await mobile.getByRole('dialog').waitFor();
  assert.equal(await mobile.getByLabel('Start time', { exact: true }).inputValue(), '09:00');
  await mobile.getByRole('button', { name: 'Cancel', exact: true }).tap();
  await mobile.getByRole('button', { name: 'Day', exact: true }).tap();
  await expect(mobile.locator('.week-slot')).toHaveCount(48);
  await mobile.locator('[data-week-slot="19"][data-day="0"]').tap();
  await mobile.getByRole('dialog').waitFor();
  assert.equal(await mobile.getByLabel('Start time', { exact: true }).inputValue(), '09:30');
  assert.equal(await mobile.getByLabel('End time', { exact: true }).inputValue(), '10:00');
  await mobile.getByRole('button', { name: 'Cancel', exact: true }).tap();
  assert.deepEqual(errors, []);
  assert(requests.some(url => url.includes('/occurrences?')));
  for (const width of [390, 320]) {
    await mobile.setViewportSize({ width, height: 844 });
    assert(await mobile.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `Day page overflow at ${width}`);
    await expect.poll(async () => mobile.locator('.week-scroll').evaluate(el => el.scrollWidth - el.clientWidth), {
      message: `Day grid horizontal overflow at ${width}px (scrollWidth - clientWidth)`,
    }).toBeLessThanOrEqual(0);
  }
  // Groups must include later catalog pages, even when no events occur in this view.
  await page.setViewportSize({ width: 1440, height: 1050 });
  fixtures = Array.from({ length: 20 }, (_, index) => ({
    ...fixtures[0], id: `busy-${index}`, name: `Busy agenda ${index} ${'long-title'.repeat(8)}`,
    description: 'Full notes remain available in the event editor. '.repeat(4),
  }));
  await page.getByRole('button', { name: 'Month', exact: true }).click();
  await page.getByRole('button', { name: 'Refresh calendar', exact: true }).click();
  await page.locator('.month-cell').filter({ has: page.locator('.event-chip') }).first().locator('.day-select').click();
  await expect(page.locator('.agenda-event')).toHaveCount(20);
  for (const width of [1440, 1649, 1651, 1920, 390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `Busy agenda page overflow at ${width}`);
    const agenda = await page.locator('.agenda').evaluate(el => {
      el.scrollTop = el.scrollHeight;
      return { horizontal: el.scrollWidth - el.clientWidth, height: el.clientHeight, scrollTop: el.scrollTop };
    });
    assert.equal(agenda.horizontal, 0, `Busy agenda horizontal overflow at ${width}`);
    const row = page.locator('.agenda-event').first();
    const rowBox = await row.boundingBox();
    const timeBox = await row.locator('.agenda-time').boundingBox();
    const titleBox = await row.locator('h3').boundingBox();
    assert(rowBox.height >= 44 && rowBox.height <= 48, `Compact accessible row height at ${width}`);
    assert(timeBox.x < titleBox.x, `Time precedes title at ${width}`);
    assert(Math.abs(timeBox.y + timeBox.height / 2 - titleBox.y - titleBox.height / 2) < 2, `Time and title align at ${width}`);
    await expect(row.locator('p')).toHaveCount(0);
    if (width >= 1440) {
      assert(agenda.height <= 740, `Busy agenda remains bounded at ${width}`);
      assert(agenda.scrollTop > 0, `Busy agenda can scroll vertically at ${width}`);
    }
  }
  await page.setViewportSize({ width: 1440, height: 1050 });
  await page.locator('.agenda-event').first().focus();
  await page.keyboard.press('Enter');
  await expect(page.getByLabel('Notes', { exact: false })).toHaveValue(fixtures[0].description);
  await cancel();
  const buttonMargin = await page.locator('.new-event-sidebar').evaluate(el => getComputedStyle(el).margin);
  assert.equal(buttonMargin, '8px 0px');
  let catalogFails = true;
  const catalog = Array.from({ length: 201 }, (_, index) => ({
    id: String(index), name: `Catalog ${index}`, event_group: index === 200 ? 'Later page group' : 'Team',
    date_from: '2020-01-01T00:00:00Z', date_to: '2020-01-02T00:00:00Z',
    tz: 'UTC', all_day: true, rrule: '', disabled: index === 200, description: '',
  }));
  await page.unroute('**/v1/**');
  await page.route('**/v1/**', route => {
    const url = new URL(route.request().url());
    requests.push(url.href);
    if (url.pathname.endsWith('/relations')) return route.fulfill({ json: { payload: [] } });
    if (url.pathname.endsWith('/occurrences')) return route.fulfill({ json: { payload: [] } });
    if (catalogFails) return route.fulfill({ status: 503, json: { message: { text: 'Service unavailable.' } } });
    const offset = Number(url.searchParams.get('_offset'));
    const limit = Number(url.searchParams.get('_limit'));
    return route.fulfill({ json: { payload: catalog.slice(offset, offset + limit) } });
  });
  await page.reload();
  await expect(page.locator('.sidebar').getByRole('alert')).toContainText('Could not load groups');
  await expect(page.getByText('Groups appear when you add your first event.')).toHaveCount(0);
  await page.getByRole('button', { name: 'New event', exact: true }).click();
  await expect(page.getByRole('dialog').getByRole('alert')).toContainText('Service unavailable.');
  catalogFails = false;
  await page.getByRole('dialog').getByRole('button', { name: 'Retry groups' }).click();
  await expect(page.getByLabel('Calendar group', { exact: true }).locator('option')).toHaveCount(3);
  await page.getByLabel('Calendar group', { exact: true }).selectOption('Later page group');
  await expect(page.getByLabel('Calendar group', { exact: true })).toHaveValue('Later page group');
  await cancel();
  await expect(page.locator('.sidebar').getByRole('checkbox', { name: /Later page group/ })).toBeVisible();
  assert(requests.some(url => url.includes('_offset=200')));
  assert.deepEqual(errors, []);
  console.log('PASS: calendar interactions/mobile overflow; paginated groups including disabled/out-of-range events, catalog failure and editor retry.');
} finally {
  await browser?.close();
  await server.close();
}
