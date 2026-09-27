# Built-package host smoke

Use this procedure after `make build` and `make verify-package`. It tests the
archive installed in a disposable Kandev profile. Use a host checkout built at
the pinned API commit in `.kandev-sdk-ref` for the Action path. Use the stable
Kandev `v0.96.0` release for the older-host Button fallback. Both hosts keep
the plugin's existing runtime compatibility floor; the SDK pin is a build
input, not a new minimum version.

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
trigger. A task session shows one trigger in `chat-top-bar`. On the pinned
Action host, the trigger uses the host Action, fits the toolbar, and clips the
long percentage inside the host-owned text box. Focus opens the provider
panel; Space closes it and Enter opens it. On v0.96.0, the trigger uses the
legacy Button and keeps its provider pill.

Disable and re-enable the plugin in **Settings → Plugins**. The trigger must
disappear while disabled and return once when enabled again. At a phone-sized
viewport, open the Plugins section in the app menu. The trigger must be at
least 44 × 44 px, the long value must not cause horizontal page overflow, and
a tap must open the provider panel without showing a tooltip.

The optional `app-status-bar-right` contribution remains the existing
read-only multi-provider strip. Check its phone drawer and provider details
separately; it is outside the Action migration.
