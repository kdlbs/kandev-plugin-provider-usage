# Built-package host smoke

Use this procedure after `make build` and `make verify-package`. It tests the
archive installed in a disposable Kandev profile. The Action-path smoke used
Kandev host revision `359b5ffdbb6e25592bc3a46d88db1dbf94ff103c`, which is 30
commits after and descends from the SDK/API source pin
`570600439036e81f8e9e1c63f15c4abce8a6c846`. That host contains the Action API,
but this smoke does not validate the exact pinned host revision at runtime.
Use the stable Kandev `v0.96.0` release for the older-host Button fallback.
The plugin's existing runtime compatibility floor is unchanged; the SDK pin
is a build input, not a new minimum version. No stable release containing PR
#3943 has been validated, so the release hold remains in effect.

Run each host in a disposable E2E worker with its temporary `HOME`,
`KANDEV_HOME_DIR`, and test database. Do not use a personal Kandev profile.
Install `kandev-provider-usage-*.tar.gz` through **Settings → Plugins → Install
plugin → Upload**. In the plugin settings, set:

| Setting | Value |
| --- | --- |
| `codexbar_command` | Absolute path to `test/host-smoke/fake-codexbar.sh` |
| `codexbar_providers` | `codex` |
| `display_pill_providers` | `codex` |
| `display_status_bar_mode` | `both` |

The fake command reads only `test/host-smoke/usage.json`. It returns a
synthetic `1234567890%` Codex value and makes no account, credential, or
network requests.

Check both registered top-bar surfaces on desktop. The main page shows one
trigger. A task session shows one trigger in `chat-top-bar`. On the Action
host, the trigger uses the host Action, fits the toolbar, and clips the long
percentage inside the host-owned text box. Its accessible label is localized
when host i18n is available. The Action tooltip is suppressed so it does not
duplicate the existing rich hover/focus panel. Focus opens that panel; Space
closes it and Enter opens it. On v0.96.0, the trigger uses the legacy Button
and keeps its provider pill.

Disable and re-enable the plugin in **Settings → Plugins**. The trigger must
disappear while disabled and return once when enabled again. At a phone-sized
viewport, open the Plugins section in the app menu. The trigger must be at
least 44 × 44 px, the long value must not cause horizontal page overflow, and
a tap must open the provider panel without showing a tooltip.

The optional `app-status-bar-right` contribution remains the existing
read-only multi-provider strip. Check its phone drawer and provider details
separately; it is outside the Action migration.
