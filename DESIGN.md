---
name: Calendar
description: A clear calendar atlas for service operators
colors:
  primary: '#2b2d42'
  ink: '#2b2d42'
  canvas: '#edf2f4'
  sidebar: '#e9e7e2'
  surface: '#ffffff'
  muted: '#515b6c'
  line: '#ccc5b9'
typography:
  heading:
    fontFamily: 'Manrope Variable, sans-serif'
    fontWeight: 650
    fontSize: 'clamp(23px, 2.1vw, 33px)'
    letterSpacing: '-0.9px'
rounded:
  control: '8px'
  chip: '4px'
  dialog: '16px'
---

# Calendar Design System

## Overview

An operating interface, not a marketing dashboard. The primary artifact is the
calendar grid. A quiet left navigation rail and selected-day agenda provide
context without competing with the dates. English copy is direct and friendly.

## Colors

The supplied palette is steel #8D99AE, cool white #EDF2F4, red #EF233C, navy
#2B2D42, blue #5AA9E6, yellow #FFE45E, warm gray #403D39, taupe #CCC5B9, and
charcoal #252422. Light mode uses navy actions on cool surfaces; dark mode uses
yellow #FFE45E actions on neutral charcoal (#242424 canvas, #2E2E2E surfaces).
Yellow highlights dates and selections, red marks
alerts and small brand details. Derived shades preserve text contrast. Six stable,
hash-selected event colors use bright mint, lemon, rose, lavender, peach, and aqua
fills with readable dark text. Dark-mode group labels and dots use matching light
colors. Group names remain visible; color is never the only identifier.

Semantic CSS color tokens provide light and dark palettes; dark mode does not
invert the light palette. Native controls follow the resolved color scheme.
The top-right theme icon cycles System (default), Light, Dark, then System on each
click, with no menu. Its accessible label names the current and next modes. Preferences are
stored under `calendar.theme`, applied before app startup, and synchronized across
tabs. System follows live OS changes; explicit choices override them. Unavailable
storage never prevents switching themes.

## Typography

Manrope Variable is self-hosted through the bundled fontsource package. Period
headings are medium-large and dense calendar labels are compact. Date numerals
are tabular. Long event names truncate only in the grid; the agenda and editor
provide the full content.

## Layout

The desktop shell has a sticky, viewport-height 224px navigation rail that scrolls
independently when its contents exceed the available height. The main calendar shares a bordered
surface with a 247px agenda. At 1000px the agenda moves below; at 700px navigation
becomes a dismissible drawer and month cells use event dots. The year view uses
twelve mini-calendars; the day view uses the same time grid as the week. No sample data
is included in the production UI.

The navbar has no fixed height and uses `0 24px` padding. Page content uses
`8px 24px` padding at every breakpoint; both toolbar and search row have a 4px
bottom margin. Mobile footer spacing keeps the floating create button clear.

The week view uses a full-width, Monday-first time grid with 30-minute slots and
the selected-day agenda below. Day headers and all-day events stay visible while
scrolling. Narrow screens scroll the week horizontally. Clicking a slot or
dragging within one day opens the editor with that range; touch uses tap-to-create
and native scrolling. Arrow keys navigate slots. The day grid uses one column and
the same range-selection behavior. Month cells support dragging across dates,
including row and month boundaries, to create an inclusive all-day range. Date
buttons still select the agenda on click; event buttons still open existing events.
Month cells also have direct add buttons for keyboard and touch use. Escape cancels
an active drag without opening the editor.

## Elevation & Depth

The calendar is flat and bounded by a fine neutral border. Soft, offset shadows
are reserved for the modal, notifications, mobile create button, and open drawer.

## Shapes

Controls use compact rounded rectangles. Today and mini-calendar selections use
circles. Event chips use small corners rather than pill-shaped cards.

## Components

Native dialogs protect editing and deletion focus. All form controls have visible
labels. Save/delete requests expose pending and error states; destructive deletion
requires confirmation. Empty, loading and API-error states remain distinct.
Lucide provides the icon vocabulary. Reduced-motion preferences disable animations.

The event editor separates date and time fields, preserves duration when moving
the start, and presents inclusive all-day end dates while saving exclusive API ends.
New events default to the calendar's local zone so the selected hour stays
consistent. UTC remains available in the selector. Existing events retain their
event zone.

## Do's and Don'ts

- Keep month/week/day/year controls and creation actions directly discoverable.
- Keep recurrence editing explicitly series-wide and API end dates exclusive.
- Use event-zone dates for all-day entries and browser-zone times for timed entries.
- Do not add fabricated metrics, decorative charts, or nested dashboard cards.
- Protect keyboard focus and mobile dismissal whenever navigation or dialogs change.
