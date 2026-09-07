import assert from 'node:assert/strict';
import { chromium, expect } from '@playwright/test';
import { createServer } from 'vite';

const server = await createServer({ server: { port: 0, host: '127.0.0.1' } });
let browser;
try {
  await server.listen();
  browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, colorScheme: 'dark' });
  const page = await context.newPage();
  const errors = [];
  let fixtures = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.route('**/v1/**', (route) => route.fulfill({ json: { payload: fixtures } }));
  const base = `http://127.0.0.1:${server.httpServer.address().port}/calendar/`;
  let release;
  const blocked = new Promise((resolve) => {
    release = resolve;
  });
  await page.route('**/src/main.ts', async (route) => {
    await blocked;
    await route.continue();
  });
  await page.goto(base, { waitUntil: 'commit' });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  assert.equal(
    await page.locator('html').evaluate((el) => getComputedStyle(el).backgroundColor),
    'rgb(36, 36, 36)',
    'Dark canvas before app startup',
  );
  release();
  await page.locator('.calendar-toolbar h1').waitFor();
  await page.unroute('**/src/main.ts');

  const trigger = () => page.getByRole('button', { name: /^Theme:/ });
  const choose = async (theme) => {
    for (
      let i = 0;
      i < 3 && (await page.locator('html').getAttribute('data-theme-preference')) !== theme.toLowerCase();
      i++
    )
      await trigger().click();
    await expect(page.locator('html')).toHaveAttribute('data-theme-preference', theme.toLowerCase());
    await expect(page.getByRole('menu')).toHaveCount(0);
  };
  await expect(trigger()).toHaveAccessibleName('Theme: System. Switch to Light');
  await page.emulateMedia({ colorScheme: 'light' });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await choose('Dark');
  assert.equal(await page.evaluate(() => localStorage.getItem('calendar.theme')), 'dark');
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(trigger()).toHaveAccessibleName('Theme: Dark. Switch to System');
  await choose('Light');
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await choose('System');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await trigger().press('Enter');
  await expect(trigger()).toHaveAccessibleName('Theme: Light. Switch to Dark');
  await trigger().press('Space');
  await expect(trigger()).toHaveAccessibleName('Theme: Dark. Switch to System');
  await trigger().press('Enter');
  await expect(trigger()).toHaveAccessibleName('Theme: System. Switch to Light');
  await expect(trigger()).toBeFocused();
  await expect(page.getByRole('menu')).toHaveCount(0);

  const tab = await page.context().newPage();
  await tab.route('**/v1/**', (route) => route.fulfill({ json: { payload: [] } }));
  await tab.goto(base);
  await choose('Light');
  await expect(tab.locator('html')).toHaveAttribute('data-theme', 'light');
  await tab.close();

  fixtures = await page.evaluate(() =>
    Array.from({ length: 6 }, (_, index) => {
      const start = new Date();
      start.setHours(9 + index, 0, 0, 0);
      return {
        id: `theme-${index}`,
        name: ['Planning', 'Design review', 'Focus time', 'Team lunch', 'Release check', 'Workshop'][index],
        description: '',
        event_group: `Group ${index}`,
        date_from: start.toISOString(),
        date_to: new Date(start.getTime() + 3600000).toISOString(),
        tz: 'UTC',
        all_day: false,
        disabled: false,
        rrule: '',
      };
    }),
  );
  await page.getByRole('button', { name: 'Refresh calendar', exact: true }).click();
  await page.locator('.agenda-event').first().waitFor();

  for (const theme of ['Dark', 'Light']) {
    await choose(theme);
    await expect(page.locator('.brand')).toHaveText('calendar');
    assert.equal(await page.locator('.brand').evaluate(el => getComputedStyle(el).color), 'rgb(239, 35, 60)');
    assert.equal(
      await page.locator('html').evaluate((el) => getComputedStyle(el).getPropertyValue('--pine').trim()),
      theme === 'Dark' ? '#ffe45e' : '#2b2d42',
    );
    for (const mode of ['Month', 'Week', 'Day', 'Year']) {
      await page.getByRole('button', { name: mode, exact: true }).click();
      assert.equal(
        await page.locator('.calendar-layout').evaluate((el) => getComputedStyle(el).backgroundColor),
        theme === 'Dark' ? 'rgb(46, 46, 46)' : 'rgb(255, 255, 255)',
      );
    }
  }
  await page.getByRole('button', { name: 'Month', exact: true }).click();
  for (const theme of ['Dark', 'Light']) {
    await choose(theme);
    await expect
      .poll(() =>
        page
          .locator('.agenda-event h3')
          .first()
          .evaluate((el) => getComputedStyle(el).color),
      )
      .toBe(theme === 'Dark' ? 'rgb(237, 242, 244)' : 'rgb(43, 45, 66)');
    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 900 });
      const navbar = await page.locator('.topbar').evaluate((el) => {
        const style = getComputedStyle(el);
        return {
          left: style.paddingLeft,
          right: style.paddingRight,
          top: style.paddingTop,
          bottom: style.paddingBottom,
          height: el.getBoundingClientRect().height,
        };
      });
      assert.equal(navbar.left, '24px');
      assert.equal(navbar.right, '24px');
      assert.equal(navbar.top, '0px');
      assert.equal(navbar.bottom, '0px');
      assert(navbar.height < 55, 'Navbar should size to its content with compact vertical padding');
      assert.equal(
        await page.locator('.page-content').evaluate((el) => getComputedStyle(el).padding),
        '8px 24px',
      );
      for (const selector of ['.search-line', '.calendar-toolbar'])
        assert.equal(await page.locator(selector).evaluate((el) => getComputedStyle(el).marginBottom), '4px');
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      if (process.env.SCREENSHOT_DIR)
        await page.screenshot({
          path: `${process.env.SCREENSHOT_DIR}/calendar-${theme.toLowerCase()}-${width}.png`,
          fullPage: true,
        });
      if (width > 700) await page.getByRole('button', { name: 'New event', exact: true }).click();
      else await page.getByRole('button', { name: 'Create event', exact: true }).click();
      await page.getByRole('dialog').waitFor();
      await page.getByLabel('All-day event').uncheck();
      assert.equal(
        await page
          .getByLabel('Start time', { exact: true })
          .evaluate((el) => getComputedStyle(el).colorScheme),
        theme.toLowerCase(),
      );
      assert.equal(
        await page.getByRole('dialog').evaluate((el) => getComputedStyle(el).backgroundColor),
        theme === 'Dark' ? 'rgb(46, 46, 46)' : 'rgb(255, 255, 255)',
      );
      if (process.env.SCREENSHOT_DIR)
        await page.screenshot({
          path: `${process.env.SCREENSHOT_DIR}/calendar-${theme.toLowerCase()}-editor-${width}.png`,
          fullPage: true,
        });
      await page.getByRole('button', { name: 'Cancel', exact: true }).click();
    }
  }

  await page.setViewportSize({ width: 1440, height: 550 });
  await page.evaluate(() => window.scrollTo(0, 200));
  assert(await page.evaluate(() => window.scrollY > 0));
  assert.equal(await page.locator('.sidebar').evaluate((el) => el.getBoundingClientRect().top), 0);
  assert.equal(await page.locator('.sidebar').evaluate((el) => el.getBoundingClientRect().height), 550);
  const sidebarScroll = await page.locator('.sidebar').evaluate((el) => {
    el.scrollTop = 200;
    return el.scrollTop;
  });
  assert(sidebarScroll > 0, 'Short-screen sidebar must scroll independently');

  const unavailable = await browser.newPage({ colorScheme: 'dark' });
  unavailable.on('pageerror', (error) => errors.push(error.message));
  await unavailable.addInitScript(() => {
    Object.defineProperty(window, 'localStorage', {
      get() {
        throw new DOMException('Blocked', 'SecurityError');
      },
    });
  });
  await unavailable.route('**/v1/**', (route) => route.fulfill({ json: { payload: [] } }));
  await unavailable.goto(base);
  await expect(
    unavailable.getByRole('button', { name: 'Theme: System. Switch to Light', exact: true }),
  ).toBeVisible();
  await unavailable.getByRole('button', { name: /^Theme:/ }).click();
  await expect(unavailable.locator('html')).toHaveAttribute('data-theme', 'light');
  const invalid = await browser.newPage({ colorScheme: 'dark' });
  await invalid.addInitScript(() => localStorage.setItem('calendar.theme', 'invalid'));
  await invalid.route('**/v1/**', (route) => route.fulfill({ json: { payload: [] } }));
  await invalid.goto(base);
  await expect(
    invalid.getByRole('button', { name: 'Theme: System. Switch to Light', exact: true }),
  ).toBeVisible();
  await expect(invalid.locator('html')).toHaveAttribute('data-theme', 'dark');
  assert.deepEqual(errors, []);
  console.log(
    'PASS: prepaint palette, system changes, saved overrides, cross-tab sync, click/keyboard cycling without menu, all views, mobile/editor and blocked/invalid storage',
  );
} finally {
  await browser?.close();
  await server.close();
}
