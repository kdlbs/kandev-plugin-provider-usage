# Provider Usage — kandev plugin

Shows **subscription utilization** for your agent providers: how much of each
rate-limit window (5-hour / weekly / monthly …) is used, with reset times.
Data comes from the [codexbar](https://github.com/steipete/codexbar) CLI, so it
covers ~60 providers (Claude, Codex/OpenAI, Gemini, Copilot, Cursor, Grok, …).

Two surfaces:

- **Settings → Provider Usage** — a page listing utilization for every provider
  codexbar can read on this machine, plus the ones it can't (with why).
- **Chat-bar icon** — a gauge in the composer showing utilization for the
  provider backing the **current session's** agent; hover for the full
  per-window breakdown. The inline number is the busiest window's percentage,
  colored green / amber / red by your thresholds.

Everything is computed backend-side; the UI is a dumb renderer.

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
