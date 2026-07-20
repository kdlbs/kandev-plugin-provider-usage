# kandev-provider-usage

A [kandev](https://github.com/kdlbs/kandev) plugin that shows **subscription
utilization** for your agent providers — how much of each rate-limit window
(5-hour / weekly / monthly …) is used, with reset times — right in the session
top bar. Data comes from the [codexbar](https://github.com/steipete/codexbar)
CLI (covers ~60 providers: Claude, Codex/OpenAI, Gemini, Copilot, Cursor,
Grok, …) plus Augment's own Analytics API.

## Screenshots

A pill in the session top bar (`chat-top-bar` slot) shows the providers you
pick, each as an icon + %:

![Top-bar pill](https://raw.githubusercontent.com/kdlbs/kandev-provider-usage/0cbed6cfb38b0e642a26158392b71f44170e686d/topbar-pill.png)

Hover it to open a panel that cycles through every provider — tabs across the
top, starting with the one behind the **current session** — with rate-limit
windows as bars, reset countdowns, and pace:

![Provider panel — Claude](https://raw.githubusercontent.com/kdlbs/kandev-provider-usage/0cbed6cfb38b0e642a26158392b71f44170e686d/panel-claude.png)

Augment shows month-to-date consumption against a budget, plus average/day and
a projected month-end total:

![Provider panel — Augment](https://raw.githubusercontent.com/kdlbs/kandev-provider-usage/0cbed6cfb38b0e642a26158392b71f44170e686d/panel-augment.png)

Operator settings (**Settings → Plugins → Provider Usage**), generated from the
manifest's `config_schema` and grouped by source:

![Settings page](https://raw.githubusercontent.com/kdlbs/kandev-provider-usage/0cbed6cfb38b0e642a26158392b71f44170e686d/settings.png)

## What it does

- **One surface**: a component in the `chat-top-bar` plugin slot (kandev ≥
  [#1827](https://github.com/kdlbs/kandev/pull/1827)) — a pill showing the
  providers you configure (icon + %), each real brand mark rendered
  monochrome. Hover to open a panel that cycles through every provider (tabs,
  hover or click to switch), opening on the one behind the current session.
- Each provider's panel shows its rate-limit windows as thin bars (calm indigo
  normally, warming to amber/coral only when high — never a hard red), a
  reset countdown, plan/source badges, and codexbar's pace summary
  ("48% in reserve").
- **Augment** is a special case: codexbar can't read it off macOS, so it's
  fetched directly from Augment's Analytics API. Shows month-to-date
  consumption as a used-of-budget bar (defaulting to a 2.5M-credit budget for
  credit plans so it always renders a bar), plus average-per-day and a
  projected month-end total.
- **A background poller** refreshes a single snapshot of every provider every
  few minutes (configurable); the pill and panel always serve that snapshot
  instantly — codexbar/Augment only run on the timer or an explicit
  **Refresh**. Each refresh tries codexbar's fast `oauth` source first, falling
  back to the agent CLI only when needed.
- **No cookies / web login required**: utilization is read from your local
  provider credentials (`~/.claude`, `~/.codex`, …) for the OAuth agents.

## codexbar distribution

codexbar ships per-platform release binaries (no npm/npx). The plugin resolves
the CLI in this order:

1. the **codexbar command** you set in Settings (a path, e.g.
   `/usr/local/bin/codexbar`);
2. a `codexbar` binary on `PATH`;
3. otherwise it **downloads a pinned build once**, verifies it against a
   bundled SHA-256, and caches it under `~/.config/kandev-provider-usage`
   (macOS and Linux only — codexbar has no Windows build, so set an explicit
   path there).

## Settings

Settings → Plugins → Provider Usage (generated from the manifest
`config_schema`, grouped by source):

| Key                       | Meaning                                                                                                     |
| -------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `augment_api_token`        | Augment Analytics service-account token (stored as a secret — masked in the UI). Enables the Augment card.  |
| `augment_email`             | Your Augment org email, used to filter Analytics to your usage.                                             |
| `augment_monthly_budget`    | Budget for the used-of-budget %. Empty = a per-user Analytics override, else a 2,500,000-credit default.   |
| `augment_resource`          | `credits` (default) or `usd` — which metric your Augment plan bills.                                        |
| `codexbar_command`          | Explicit codexbar command. Empty = auto-detect / auto-download.                                             |
| `codexbar_poll_minutes`     | Background refresh interval (default 5, minimum 1).                                                          |
| `codexbar_providers`        | Comma-separated provider ids to poll. Empty = curated local-credential set; `"all"` = full sweep (slower).  |
| `display_pill_providers`    | Providers shown in the top-bar pill, comma-separated. Tokens: `current`, `all`, or explicit ids. Empty = current session's provider only. |
| `display_threshold_warn`    | A window at/above this % turns amber (default 75).                                                            |
| `display_threshold_high`    | A window at/above this % turns red/coral (default 90).                                                        |

Saving settings restarts the plugin, so changes take effect immediately; the
panel also has a **Refresh** button for an on-demand update.

## Layout

- `manifest.yaml` — three GET webhooks (`status`, `providers`, `overview`), the
  UI bundle, the `api_read: ["sessions"]` capability (to resolve the current
  session's agent → provider server-side), and the grouped `config_schema`.
- `server/` — Go backend half (`pluginsdk.Plugin`), spawned by kandev over the
  gRPC plugin contract. Runs a background poller that shells out to codexbar
  (and, when configured, calls Augment's Analytics API directly) and caches a
  snapshot; webhooks serve that snapshot instantly.
- `ui/bundle.js` — hand-written, no-build ES module using the shared host React
  instance and `host.ui` components, plus inlined real brand-mark SVGs
  (monochrome). It only renders the backend payload.

## Develop

Requires a sibling checkout of the kandev monorepo at `../kandev` (see the
`replace` directive in `go.mod`).

```sh
make test           # go test ./server/... (codexbar/Augment calls are injected — no network needed)
make package-host   # tarball for this machine only (fast iteration)
make package         # tarball for all 5 supported platforms
```

Install the tarball via Settings → Plugins → Install plugin (upload), or:

```sh
curl -F package=@kandev-provider-usage-0.1.0.tar.gz \
  http://localhost:8080/api/plugins/install
```

## Release

Tag `vX.Y.Z`; `.github/workflows/release.yml` verifies (`fmt`/`vet`/`test`),
cross-compiles all platforms, and publishes the tarball + `checksums.txt` as a
GitHub Release, which the kandev marketplace resolves.
