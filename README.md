# Provider Usage — kandev plugin

Shows **subscription utilization** for your agent providers: how much of each
rate-limit window (5-hour / weekly / monthly …) is used, with reset times.
Data comes from the [codexbar](https://github.com/steipete/codexbar) CLI, so it
covers ~60 providers (Claude, Codex/OpenAI, Gemini, Copilot, Cursor, Grok, …).

**One surface:** a gauge in the **session top bar** (the `chat-top-bar` plugin
slot, kandev ≥ #1827). Hover it to open a panel that cycles through **every**
provider — prev/next arrows and dot anchors — starting with the provider that
backs the **current session**. Each provider shows its rate-limit windows as
bars (green / amber / red by your thresholds), reset countdowns, plan/source
badges, and Augment's raw monthly consumption. The gauge also shows the current
provider's busiest-window % inline.

Everything is computed backend-side (one `overview` webhook returns the warm
snapshot of all providers plus the resolved current-session provider); the UI is
a dumb renderer.

## Augment (Analytics API)

codexbar can't read Augment off macOS, so Augment is fetched directly from its
**Analytics API** (`api.augmentcode.com`) when you configure an
`augment_api_token` (an Analytics service-account token) and your
`augment_email`. It shows your **month-to-date consumption** (credits or USD),
as a used-of-budget **percentage** when a budget is known — set
`augment_monthly_budget`, or the plugin auto-detects a per-user budget override.
With no budget it just shows the raw amount ("959,232 credits this month").

## How it stays fast

A background poller refreshes a single snapshot of every provider's utilization
every few minutes (configurable). Both surfaces serve that snapshot **instantly**
— codexbar only runs on the timer or when you hit **Refresh**. Each refresh
tries codexbar's fast `oauth` source first (a quick HTTP call) and falls back to
the agent CLI only when needed, so a full refresh is ~1s rather than tens of
seconds.

## How it gets codexbar

codexbar ships per-platform release binaries (no npm/npx). The plugin resolves
the CLI in this order:

1. the **codexbar command** you set in _Settings → Plugins → Provider Usage_
   (a path, e.g. `/usr/local/bin/codexbar`);
2. a `codexbar` binary on `PATH`;
3. otherwise it **downloads a pinned build once**, verifies it against a
   bundled SHA-256, and caches it under `~/.config/kandev-provider-usage`
   (macOS and Linux only — on Windows, set an explicit path).

Utilization is read from your **local** provider credentials/logs (e.g.
`~/.claude`, `~/.codex`); no cookies or web login required for the OAuth agents.

## Settings

| Key                     | Meaning                                                                                         |
| ----------------------- | ----------------------------------------------------------------------------------------------- |
| `command`               | Explicit codexbar command. Empty = auto-detect / auto-download.                                 |
| `providers`             | Comma-separated codexbar provider ids to poll. Empty = curated local set; `"all"` = full sweep. |
| `poll_interval_minutes` | Background refresh interval (default 5, minimum 1).                                              |
| `warn_threshold`        | A window at/above this % turns amber (default 75).                                               |
| `high_threshold`        | A window at/above this % turns red (default 90).                                                 |
| `augment_api_token`     | Augment Analytics service-account token (enables the Augment card).                             |
| `augment_email`         | Your Augment org email, used to filter Analytics to your usage.                                 |
| `augment_resource`      | `credits` (default) or `usd` — which metric your Augment plan bills.                            |
| `augment_monthly_budget`| Optional monthly budget for a used-of-budget %. Empty = auto-detect / raw amount.               |

The pages have a **Refresh** button for an immediate update, and saving settings
restarts the plugin (so changes take effect immediately).

## Develop

Requires a sibling checkout of the kandev monorepo at `../kandev` (the
`replace` directive in `go.mod` points there for the plugin SDK).

```bash
make test          # go test ./server/...
make package-host  # build + pack for this platform only (fast)
make package       # cross-compile all 5 platforms + checksums.txt

# Install into a running kandev instance:
curl -F package=@kandev-provider-usage-0.1.0.tar.gz \
  http://localhost:<port>/api/plugins/install
```

Then enable it in _Settings → Plugins_ (needs the `plugins` feature flag).

## Release

Tag `vX.Y.Z`; `.github/workflows/release.yml` cross-compiles all platforms and
publishes the tarball + `checksums.txt` as a GitHub Release, which the kandev
marketplace resolves.
