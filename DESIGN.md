---
name: Calendar
description: A clear calendar atlas for service operators
colors:
  pine: '#185e49'
  ink: '#243b37'
  canvas: '#f7f9f8'
  sidebar: '#f1f5f2'
  surface: '#ffffff'
  muted: '#697a75'
  line: '#e3eae6'
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

Pine green identifies primary actions, today, and selected dates. Neutral surfaces
provide separation without elevated cards. Six stable, hash-selected group colors
are paired with textual group names; color is never the only identifier.

## Typography

Manrope Variable is self-hosted through the bundled fontsource package. Period
headings are medium-large and dense calendar labels are compact. Date numerals
are tabular. Long event names truncate only in the grid; the agenda and editor
provide the full content.

## Layout

The desktop shell has a 224px navigation rail. The main calendar shares a bordered
surface with a 247px agenda. At 1000px the agenda moves below; at 700px navigation
becomes a dismissible drawer and month cells use event dots. The year view uses
twelve mini-calendars; the day view uses a scrollable hourly list. No sample data
is included in the production UI.

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

## Do's and Don'ts

- Keep month/day/year controls and creation actions directly discoverable.
- Keep recurrence editing explicitly series-wide and end dates explicitly exclusive.
- Use event-zone dates for all-day entries and browser-zone times for timed entries.
- Do not add fabricated metrics, decorative charts, or nested dashboard cards.
- Protect keyboard focus and mobile dismissal whenever navigation or dialogs change.
