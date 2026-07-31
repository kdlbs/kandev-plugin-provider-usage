---
status: building
created: 2026-07-31
---

# Responsive Topbar Control

## Why

Provider Usage's session-topbar control is shorter than adjacent Kandy and native controls on desktop and phone. It needs consistent viewport-specific geometry without changing the usage information it surfaces.

## What

- In the desktop session topbar, Provider Usage renders as a 28px-tall control, matching native metric chips and Kandy.
- Below Kandev's 640px phone breakpoint, the Provider Usage control has a 44px interactive height; its icon-only presentation is 44px square.
- Resizing across the breakpoint updates the rendered target without requiring a page reload.
- The percentage pill, provider selection, hover panel, click behavior, and accessible label remain unchanged.

## Scenarios

- **GIVEN** a desktop-width session topbar, **WHEN** Provider Usage renders with or without percentage content, **THEN** its interactive control is 28px tall.
- **GIVEN** a phone-width session topbar, **WHEN** Provider Usage renders icon-only, **THEN** its interactive control is 44px tall and wide.
- **GIVEN** the viewport crosses the phone breakpoint, **WHEN** the topbar remains mounted, **THEN** Provider Usage adopts the target size for the new viewport.

## Out of scope

- Changing provider polling, data sources, or percentage formatting.
- Changing host-owned topbar controls.
