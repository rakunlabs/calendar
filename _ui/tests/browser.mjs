import { chromium } from '@playwright/test';
import { spawn } from 'node:child_process';
import { mkdir } from 'node:fs/promises';
import { resolve } from 'node:path';
import assert from 'node:assert/strict';

// Run against an isolated development DB. Fixtures are real API records and are
// removed in finally. Never point this test at a production calendar.
const base = process.env.CALENDAR_BASE_URL || 'http://localhost:18080';
const service = process.env.CALENDAR_BINARY ? spawn(process.env.CALENDAR_BINARY, [], { env: process.env, stdio: 'inherit' }) : null;
const fixtureIDs = [];
const token = `ui-test-${Date.now()}`;
let browser;

async function api(path, method = 'GET', body) {
  const response = await fetch(`${base}/calendar/v1${path}`, { method, headers: { 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
  const text = await response.text();
  assert(response.ok, `${method} ${path}: ${response.status} ${text}`);
  return text ? JSON.parse(text) : null;
}

try {
  for (let i = 0; i < 60; i++) {
    try { if ((await fetch(`${base}/calendar/`)).ok) break; } catch { /* Wait for service startup. */ }
    if (i === 59) throw new Error('Calendar did not start');
    await new Promise(resolve => setTimeout(resolve, 500));
  }
  const today = new Date();
  const fixtures = [
    ['Weekly planning', 'Team', 0, 9, 'Align on priorities for the week ahead.', 'RRULE:FREQ=WEEKLY;COUNT=4'],
    ['Design review', 'Product', 0, 11, 'A fresh perspective on the next release.', ''],
    ['Time to focus', 'Personal', 0, 14, 'A quiet hour to do your best work.', ''],
    ['Sprint kickoff', 'Team', 2, 10, '', ''],
    ['Release checkpoint', 'Product', 4, 13, '', ''],
    ['A day away', 'Personal', 7, 9, '', ''],
    ['Team lunch', 'Team', 10, 12, '', ''],
    ['Product workshop', 'Product', -3, 10, '', ''],
    ['Project handover', 'Team', 13, 11, '', ''],
    ['Monthly reflection', 'Personal', 18, 16, '', ''],
  ].map(([name, group, delta, hour, description, rrule], index) => {
    const date = new Date(today.getFullYear(), today.getMonth(), today.getDate() + delta, hour);
    const id = `${token}-${index}`;
    fixtureIDs.push(id);
    return { id, name, event_group: group, description, date_from: date.toISOString(), date_to: new Date(date.getTime() + 3600000).toISOString(), tz: Intl.DateTimeFormat().resolvedOptions().timeZone, all_day: false, disabled: false, rrule };
  });
  await api('/events', 'POST', fixtures);
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1050 } });
  const errors = [];
  page.on('pageerror', e => { errors.push(e.message); console.error('Browser error:', e.message); });
  await page.goto(`${base}/calendar/`);
  await page.locator('.calendar-toolbar h1').waitFor();
  assert.equal(await page.getByText('Your time, in view.', { exact: true }).count(), 0);
  assert.equal(await page.locator('.workspace-avatar').count(), 0);
  await page.locator('.agenda-events').waitFor();
  await page.getByRole('button', { name: 'New event', exact: true }).click();
  await page.waitForTimeout(150);
  if (await page.locator('dialog').count() === 0) throw new Error(`Editor did not mount: ${errors.join('; ')}`);
  await page.getByLabel('Event name', { exact: true }).fill(token);
  await page.getByLabel('Enter a group name').check();
  await page.getByLabel('Group name', { exact: true }).fill('Test');
  await page.locator('dialog').getByRole('button', { name: 'Create event', exact: true }).click();
  await page.locator('dialog').waitFor({ state: 'detached' });
  await page.getByLabel('Search events', { exact: true }).fill(token);
  await page.locator('.agenda-event').filter({ hasText: token }).click();
  await page.getByLabel('Event name', { exact: true }).fill(`${token}-updated`);
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await page.locator('dialog').waitFor({ state: 'detached' });
  await page.locator('.agenda-event').filter({ hasText: `${token}-updated` }).click();
  await page.getByRole('button', { name: 'Delete', exact: true }).click();
  await page.getByRole('button', { name: 'Delete permanently', exact: true }).click();
  await page.locator('dialog').waitFor({ state: 'detached' });
  await page.getByLabel('Search events', { exact: true }).fill('');
  await page.getByRole('button', { name: 'Day', exact: true }).click();
  await page.locator('.single-day').waitFor();
  await page.locator('[data-week-slot="20"][data-day="0"]').click();
  assert.equal(await page.getByLabel('Start time', { exact: true }).inputValue(), '10:00');
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Year', exact: true }).click();
  assert.equal(await page.locator('.year-month').count(), 12);
  await page.getByRole('button', { name: 'Month', exact: true }).click();
  await page.locator('.agenda-event').first().waitFor();
  await page.getByRole('button', { name: 'Refresh calendar', exact: true }).waitFor();
  await page.waitForFunction(() => document.querySelector('.calendar-layout')?.getAttribute('aria-busy') === 'false');
  if (await page.getByRole('button', { name: 'Dismiss notification', exact: true }).isVisible()) await page.getByRole('button', { name: 'Dismiss notification', exact: true }).click();
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'Desktop overflow');
  const captures = resolve('../.impeccable/review');
  await mkdir(captures, { recursive: true });
  await page.screenshot({ path: `${captures}/desktop.png`, fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'Mobile overflow');
  await page.screenshot({ path: `${captures}/mobile.png`, fullPage: true });
  await page.getByRole('button', { name: 'Toggle calendar filters', exact: true }).click();
  await page.getByRole('checkbox', { name: /Team/ }).uncheck();
  await page.getByRole('button', { name: 'Close filters', exact: true }).click();
  await page.getByRole('button', { name: 'Create event', exact: true }).click();
  await page.getByRole('dialog').waitFor();
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'Mobile editor overflow');
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  assert.deepEqual(errors, [], `Browser errors: ${errors.join('\n')}`);
  console.log('PASS: real API create/edit/delete, month/day/year, timed creation, mobile filters/editor, overflow and browser errors.');
} finally {
  await browser?.close();
  for (const id of fixtureIDs) { try { await api(`/events/${encodeURIComponent(id)}`, 'DELETE'); } catch {} }
  service?.kill('SIGTERM');
}
