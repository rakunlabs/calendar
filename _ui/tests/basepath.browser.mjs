import assert from 'node:assert/strict';
import { once } from 'node:events';
import { readFile, realpath } from 'node:fs/promises';
import { createServer } from 'node:http';
import { extname, isAbsolute, relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium, expect } from '@playwright/test';
import { build } from 'vite';

const root = fileURLToPath(new URL('../', import.meta.url));
// Use the actual production config, not a base override or a possibly stale dist.
await build({ root });
const dist = await realpath(resolve(root, 'dist'));
const mime = {
  '.html': 'text/html',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.woff2': 'font/woff2',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.ico': 'image/x-icon',
};
const entity = 'Team, R&D + /';
const group = 'Work, R&D';
const event = {
  id: 'basepath-event', name: 'Base path fixture', event_group: group,
  date_from: '2026-01-01T09:00:00Z', date_to: '2026-01-01T10:00:00Z',
  tz: 'UTC', disabled: false, all_day: false, rrule: '', description: '',
};
let browser;
try {
  browser = await chromium.launch({ headless: true });
  for (const prefix of ['/', '/calendar/', '/tools/team/calendar/']) {
    for (const entry of ['', 'index.html']) {
      const requests = [];
      const errors = [];
      // Only this mount exists: no SPA fallback or root API can mask bad URLs.
      const server = createServer(async (req, res) => {
        requests.push(req.url);
        try {
          const url = new URL(req.url, 'http://localhost');
          const pathname = decodeURIComponent(url.pathname);
          if (req.method !== 'GET' || !pathname.startsWith(prefix)) {
            res.writeHead(404).end();
            return;
          }
          const path = pathname.slice(prefix.length);
          const payloads = {
            'v1/events': [event],
            'v1/relations': [{ entity, event_group: group, event_id: null }],
            'v1/occurrences': [],
          };
          if (Object.hasOwn(payloads, path)) {
            res.writeHead(200, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ payload: payloads[path] }));
            return;
          }
          if (path === 'v1/ics') {
            res.writeHead(200, { 'Content-Type': 'text/calendar' });
            res.end('BEGIN:VCALENDAR\r\nVERSION:2.0\r\nEND:VCALENDAR\r\n');
            return;
          }
          if (path.includes('\0') || path.includes('\\') || path.split('/').includes('..')) {
            res.writeHead(400).end();
            return;
          }
          // realpath also prevents symlinks from escaping the build directory.
          const file = await realpath(resolve(dist, path || 'index.html'));
          const local = relative(dist, file);
          if (local === '..' || local.startsWith(`..${sep}`) || isAbsolute(local)) {
            res.writeHead(403).end();
            return;
          }
          const body = await readFile(file);
          res.writeHead(200, {
            'Content-Type': mime[extname(file)] || 'application/octet-stream',
            'Cache-Control': 'no-store',
          });
          res.end(body);
        } catch (error) {
          if (!['ENOENT', 'ENOTDIR', 'EISDIR'].includes(error.code) && !(error instanceof URIError)) {
            errors.push(`Static server: ${error.message}`);
          }
          res.writeHead(404).end();
        }
      });
      let context;
      try {
        server.listen(0, '127.0.0.1');
        await once(server, 'listening');
        const origin = `http://127.0.0.1:${server.address().port}`;
        const address = `${origin}${prefix}${entry}`;
        context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
        await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin });
        const page = await context.newPage();
        const assets = [];
        page.on('pageerror', error => errors.push(error.message));
        page.on('console', message => {
          if (message.type() === 'error') errors.push(message.text());
        });
        page.on('requestfailed', request => errors.push(`${request.url()}: ${request.failure()?.errorText}`));
        page.on('response', response => {
          if (response.status() >= 400) errors.push(`${response.status()}: ${response.url()}`);
          if (['script', 'stylesheet', 'font'].includes(response.request().resourceType())) {
            assets.push(response);
          }
        });
        assert.equal((await page.goto(address)).status(), 200);
        await expect(page.locator('.calendar-toolbar h1')).toBeVisible();
        await expect(page.getByLabel('Entity', { exact: true })).toBeEnabled();
        await expect.poll(() => ['events', 'relations', 'occurrences'].every(endpoint =>
          requests.some(url => new URL(url, origin).pathname === `${prefix}v1/${endpoint}`),
        )).toBe(true);
        await page.evaluate(() => document.fonts.ready);
        for (const type of ['script', 'stylesheet', 'font']) {
          assert(assets.some(response => response.request().resourceType() === type), `${address}: missing ${type}`);
        }
        for (const response of assets) {
          assert(response.ok(), `Asset failed: ${response.url()}`);
          assert(new URL(response.url()).pathname.startsWith(`${prefix}assets/`), `Wrong asset prefix: ${response.url()}`);
        }
        assert.equal(await page.locator('a.brand').evaluate(link => link.href), `${origin}${prefix}`);
        assert.equal(
          await page.getByRole('link', { name: 'API documentation' }).evaluate(link => link.href),
          `${origin}${prefix}swagger/index.html`,
        );

        await page.getByRole('button', { name: 'Calendar tools', exact: true }).click();
        await page.getByRole('button', { name: 'Import & export', exact: true }).click();
        const subscription = page.getByLabel('Subscription URL (rolling years)');
        await expect(subscription).toHaveValue(`${origin}${prefix}v1/ics`);
        await page.getByLabel('Export entity', { exact: true }).selectOption(entity);
        await page.getByLabel('Export group scope').selectOption(group);
        await page.getByLabel('Download year (optional)').fill('2026');
        const expected = new URL(`${origin}${prefix}v1/ics`);
        expected.searchParams.set('entity[eq]', entity);
        expected.searchParams.set('event_group[eq]', group);
        await expect(subscription).toHaveValue(expected.href);
        await page.getByRole('button', { name: 'Copy subscription URL', exact: true }).click();
        await expect(page.getByText('Subscription URL copied.', { exact: true })).toBeVisible();
        assert.equal(await page.evaluate(() => navigator.clipboard.readText()), expected.href);
        // Exercise the displayed-URL fallback as well as the real clipboard path.
        await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', { value: undefined }));
        await page.getByRole('button', { name: 'Copy subscription URL', exact: true }).click();
        await expect(page.getByText('Clipboard unavailable.', { exact: false })).toBeVisible();
        await expect(subscription).toBeVisible();
        await expect(subscription).toHaveValue(expected.href);
        const download = page.waitForEvent('download');
        await page.getByRole('button', { name: 'Download ICS', exact: true }).click();
        assert((await download).suggestedFilename().endsWith('.ics'));
        expected.searchParams.set('year', '2026');
        assert(requests.includes(`${expected.pathname}${expected.search}`), 'ICS download must use the mounted API');
        await expect(page.getByRole('alert')).toHaveCount(0);
        for (const request of requests) {
          assert(new URL(request, origin).pathname.startsWith(prefix), `Request escaped ${prefix}: ${request}`);
        }
        assert.deepEqual(errors, [], address);
        console.log(`PASS: ${prefix}${entry} production assets, API, links, ICS clipboard/fallback/download`);
      } finally {
        await context?.close();
        await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
      }
    }
  }
} finally {
  await browser?.close();
}
