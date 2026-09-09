# Codex manual reset screenshots

Captured on September 9, 2026 for PR #22 using an authenticated Codex account.

The production Go runner, collector, and overview webhook were exercised with
CodexBar CLI 0.45.2 (`oauth` source). The response contained three available
manual reset credits, expiring on September 21, October 4, and October 5, 2026.
No reset credits were redeemed.

The screenshots show this PR's unmodified UI bundle loaded through Kandev's
actual plugin host. Playwright replayed the normalized live webhook response in
the browser; this was not a packaged-plugin installation. The existing Kandev
instance's plugin configuration and settings were left unchanged.

- `codex-manual-resets-light.png`: collapsed desktop summary, light theme.
- `codex-manual-resets-dark.png`: collapsed desktop summary, dark theme.
- `codex-manual-resets-mobile.png`: collapsed provider row in the phone Status drawer.
- `codex-manual-resets-light-expanded.png`: expanded desktop inventory, light theme.
- `codex-manual-resets-dark-expanded.png`: expanded desktop inventory, dark theme.
- `codex-manual-resets-mobile-expanded.png`: expanded reset details after scrolling
  the phone Status drawer so every expiry is visible.

Chromium used a 1440 × 1000 desktop viewport and a 390 × 844 touch viewport,
both at 2× device scale. Captures are cropped to the relevant panel or row.
Browser assertions checked the three-credit count and nearest expiry while
collapsed; every exact local timestamp and ascending expiry order when expanded;
and absence of horizontal overflow in both states. Enter and Space opened and
closed the desktop disclosure; tapping did the same on the phone. The summary
has a 44 px tap target. The phone provider icon stays aligned with its name, and
the expanded inventory is reachable through the drawer's normal scrolling.
All assertions passed without browser errors. Credentials, account identifiers,
and raw CodexBar output are not included.
