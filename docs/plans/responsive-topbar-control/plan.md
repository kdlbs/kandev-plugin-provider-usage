---
spec: docs/specs/responsive-topbar-control/spec.md
created: 2026-07-31
status: completed
---

# Implementation Plan: Responsive Provider Usage Topbar Control

## Overview

Add a plugin-owned responsive rule for the Provider Usage session-topbar
control. Normalize its desktop height to the 28px native topbar rhythm and
give phone users a 44px touch target below the 640px breakpoint, preserving the
existing percentage pill, provider selection, and hover panel.

## UI

- Add a small injected stylesheet in `ui/bundle.js` scoped to
  `#provider-usage-topbar`.
- Keep the icon-only control square and let the percentage pill retain its
  content-driven width while sharing the same responsive height.

## Tests

- Extend `test/bundle.test.mjs` to initialize the plugin with a test registry
  and assert the emitted stylesheet contains desktop and phone dimensions and
  the 640px media boundary.
- Run `node --test test/bundle.test.mjs`, `node --check ui/bundle.js`, and
  `make test`.

## Task

- [x] [task-01-responsive-control](task-01-responsive-control.md) — completed
