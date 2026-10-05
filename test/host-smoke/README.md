# Built-package host smoke

Use this procedure after `make build` and `make verify-package-host`. Install
the archive into an isolated Kandev host profile and test the installed bundle
in a browser. Do not use a personal Kandev profile, database, credential, or
provider account.

## Local checks

Run the focused provider UI tests and packaging checks from the plugin root:

```sh
node --test test/bundle.test.mjs test/topbar.test.mjs
make check-format vet test verify-package-host
```

Use the repository's local SDK checkout at the immutable revision in
`.kandev-sdk-ref`; the Makefile deliberately runs `plugin-pack` from the SDK
module. Set `NODE` to the selected Node executable if it is not on `PATH`, and
use a task-owned `TMPDIR` for disposable builds.

## Disposable host and fixtures

Build and verify the host-platform archive, then upload that exact archive in
**Settings → Plugins → Install plugin → Upload**. Use a disposable host with its
own `HOME`, `KANDEV_HOME_DIR`, data directory, database, ports, and temporary
directory. Keep the archive and host version in the test record. Verify
`/health` reports the tested host version and verify the installed plugin
version before opening the browser.

Configure the plugin for these fixtures:

| Setting | Value |
| --- | --- |
| `codexbar_command` | Absolute path to `test/host-smoke/fake-codexbar.sh` |
| `codexbar_providers` | `all` |
| `display_pill_providers` | `all` |
| `display_status_bar_mode` | `both` |
| Cursor auth cookie | Synthetic value for the fake account only |
| Cursor team | `30677937` (`Provider Usage smoke team`) |

The fake CodexBar command reads only `usage.json`, accepts CodexBar's
`--provider` filter, and makes no account, credential, or network requests. It
returns a deliberately long Codex percentage so the toolbar's text containment
can be checked. The Cursor dashboard fixture
`fake-cursor-dashboard.py` is an HTTPS CONNECT proxy which accepts only
`cursor.com:443` and `api2.cursor.sh:443`. Configure the disposable host to
trust its task-local test CA and route its proxy through that fixture. The
fixture returns fake user `42424242`, two synthetic teams, fake usage/spend,
and one controlled usage event. Its JSONL log omits cookies and authorization
headers. Never route real provider credentials through this fixture.

The mode file accepts `default`, `daily-503`, `excessive-events`,
`mismatch-team`, and `mismatch-user`. Change it between explicit **Refresh
usage** actions. The optional daily-spend request must remain bounded to page
1, page size 100, the selected team, and the current fake user. A failed,
oversized, or mismatched response must leave the quota visible and show daily
spend as unreported. The selected team must show only the current fake user's
spend, not another synthetic member's spend. Check the daily amount expires at
00:00 UTC and is not presented as today's amount after midnight.

## Browser coverage

Use desktop Chromium and a phone-sized browser context with a coarse pointer,
touch enabled, and a 390 × 844 CSS-pixel viewport. Use one E2E worker. Record
the browser version and whether the phone context is emulated; do not describe
emulation as a physical-device test.

Check all of these on the actual installed archive:

- The main top bar and a task session's `chat-top-bar` each register one
  trigger. The Action host renders the host Action; the supported older host
  renders the legacy Button fallback. The localized label is registered from
  plugin-local English and pt-PT resources, with a safe English fallback when
  host i18n is absent.
- On the Action host, the trigger fits host-owned geometry, has a 44px minimum
  touch target in the mobile Plugins menu, and contains the long metric without
  page or toolbar horizontal overflow. Keyboard focus opens the existing rich
  panel; Space closes it and Enter opens it. Touch activation opens the same
  panel. The Action has an empty tooltip so it does not duplicate that panel.
- The rich panel retains provider order, selected-provider state, thresholds,
  reset details, and Codex reserve/deficit values. Verify provider absent,
  loading, error, and long-value states.
- Select the fake Cursor team. Verify selected-team quota and daily-spend copy
  in the top bar, settings details, and host Status drawer; exercise the
  optional daily-spend failure modes and UTC-midnight expiry above.
- Disable the plugin in **Settings → Plugins**. Its Action and contributed
  metric disappear. Re-enable it and verify exactly one Action returns.
- Enable the host app Status bar and check the existing `app-status-bar-right`
  multi-provider strip and phone Status drawer. These remain read-only rich
  status surfaces; they are not migrated into Actions.

## Release validation record

Stable-host validation has been completed against the released Kandev `v0.97.0`
runtime, tag commit
`e43881c7555372897b57ec51c705f1e05da43c40`. The published Linux x64 full
runtime archive was checked against its release checksum, reports `v0.97.0`
from `--version`, and reports `v0.97.0` from `/health`. That release is 194
commits after the plugin's immutable SDK/API source pin
`570600439036e81f8e9e1c63f15c4abce8a6c846`; it contains the Action API but is
not an exact-pin runtime test. The SDK source pin and existing minimum-host
compatibility policy remain unchanged.

The v0.97.0 smoke used desktop Chromium 154.0.8037.97 and Chromium emulation
at 390 × 844 CSS pixels with touch and coarse-pointer enabled. The latter is
not a physical handset test. The older-host Button fallback was separately
checked on the released `v0.96.0` runtime. Exact PR head/base, runtime asset
and installed plugin archive digests, commands, CI results, and remaining
limits are recorded in the validation notes on PR #30. The stable-host
validation condition is satisfied for that recorded head and package; the PR
remains a draft pending an explicit merge instruction.
