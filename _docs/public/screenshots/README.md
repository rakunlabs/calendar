# Documentation Screenshots

These screenshots show the actual UI with synthetic browser-test data. No running
API or database is needed to regenerate them.

From `_ui`, after installing dependencies and Playwright Chromium:

```sh
pnpm install --frozen-lockfile
pnpm exec playwright install chromium
SCREENSHOT_DIR=/tmp/calendar-captures node tests/calendar.browser.mjs
SCREENSHOT_DIR=/tmp/calendar-captures node tests/tools.browser.mjs
```

Copy only these captures into this directory, then build the documentation with
`pnpm --dir _docs docs:build` from the repository root:

- `calendar-month-1440.png`
- `calendar-week-1440.png`
- `calendar-month-390.png`
- `tools-export-desktop.png`

The calendar test uses the current week and the Europe/Istanbul time zone, so
dates will change when captures are refreshed. Review the images before publishing.
