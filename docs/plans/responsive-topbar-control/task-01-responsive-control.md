---
id: "01-responsive-control"
title: "Responsive Provider Usage topbar control"
status: done
wave: 1
depends_on: []
plan: "plan.md"
spec: "../../specs/responsive-topbar-control/spec.md"
---

# Task 01: Responsive Provider Usage topbar control

## Acceptance

- The Provider Usage topbar control is 28px tall on desktop and 44px tall on
  phones; icon-only mode is square in both modes.
- The responsive rule is scoped to the plugin control and does not alter the
  usage panel or global status-bar presentation.

## Verification

- `node --test test/bundle.test.mjs`
- `node --check ui/bundle.js`
- `make test`

## Files likely touched

- `ui/bundle.js`
- `test/bundle.test.mjs`

## Dependencies and risks

None. The plugin has no existing stylesheet injection helper, so initialization
and teardown must avoid duplicate style nodes when the host reloads the bundle.

## Output contract

Record the changed files, test output, commit, pushed branch, and PR URL in the
primary task conversation.
