# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Svelte 5 and Tailwind CSS in `_ui`, built with Vite and pnpm. The Go calendar service
embeds the production dist and serves it with Ada's folder handler.

## Users and Purpose

An English-language management interface for operators of the calendar service.
Users view events by month, day, and year, create and edit events, remove events,
filter by event group, and work with recurring events.

## Capabilities and Constraints

Use the existing `/calendar/v1` API and real service data. Date ranges have an
inclusive start and exclusive end. Existing events may carry time zones and
recurrence rules. Do not invent events or persist changes only in the browser.
No authentication is provided by this UI; deployment access control remains
the operator's responsibility.

## Brand Commitments

The user selected an English interface with a wide calendar, left mini-calendar
and group filters, and a selected-day agenda on the right. A light background
and dark green accent were confirmed. Mobile layouts place the agenda below.
