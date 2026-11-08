# Public SDK migration validation

Recorded on 2026-11-05 and completed on 2026-11-08, Linux amd64,
Go 1.26.6 and Node 24.19.0. Base: `main` at
`75ee761991f96ee77b0cbdd9623b35dc7b562f5c`.

## Dependency and compatibility evidence

The repository had no `AGENTS.md`, npm package, lockfile, frontend SDK import,
recipe subproject, or direct generated-protocol import. The hand-written
`ui/bundle.js` and all declared surfaces remain unchanged. API v1, plugin
version 0.12.0, author, capabilities, webhooks, agent tool and UI registrations
are preserved. No plugin release or catalog version was changed.

The verified public release is
[pluginsdk/v0.1.0](https://github.com/kdlbs/kandev/releases/tag/pluginsdk/v0.1.0),
source `f0e2e774cfe2df8a0d15e4c21dea7b48580e38fb`. The downloaded Go SDK's
`sdk-release.json` says SDK 0.1.0, host_release 0.99.0,
min_kandev_version 0.99.0 and api_versions [1, 2]. The manifest uses that
SDK-wide floor. SDK versions remain independent of plugin and host versions.

The baseline `go test ./server/...` failed because
`../kandev/apps/backend` did not exist. After migration, a fresh local Git
clone under `.build/migration/standalone-root/plugin` had neither a Kandev
sibling nor a tooling checkout. Validation used an initially empty module
cache with `GOWORK=off`, `GOPROXY=https://proxy.golang.org` and
`GOSUMDB=sum.golang.org`:

```sh
go mod download
go mod tidy
git diff --exit-code -- go.mod go.sum
go list -m all
go mod download -json github.com/kdlbs/kandev/pluginsdk@v0.1.0
make sdk-check check-format vet test build
git status --porcelain
```

All passed; module files stayed unchanged and the clone stayed clean. The
selected graph contains `github.com/kdlbs/kandev/pluginsdk v0.1.0`, with no
backend module or replacement. Public resolution returned the source commit
above, `h1:Pov5zE2gGf53FslhpcoGgx6E3nu6kjizT69x3MlEEEs=` for the module and
`h1:V0Y6IoJvwa+OuZdfgD3Ck2EgQut7S3jSu8OLQRo7xxs=` for go.mod.

`make sdk-check` runs:

```sh
go run github.com/kdlbs/kandev/pluginsdk/cmd/kandev-sdk-check@v0.1.0 --manifest manifest.yaml
```

Result: `SDK 0.1.0: compatible (minimum Kandev 0.99.0)`.
`make vet` runs `go vet ./server/...`; `make test` runs the actual Go server
suite, all 98 Node tests, `node --check ui/bundle.js`, package-verifier tests
and release-version tests. `make build` builds the server. There is no npm SDK
consumer to install and no frontend typecheck script to claim as passed.

## Packaging evidence

Packaging still requires backend-owned `plugin-pack`. `.kandev-tools-ref`
pins `f0e2e774cfe2df8a0d15e4c21dea7b48580e38fb`; its separate sparse checkout
contains `apps/backend` and root `pluginsdk`. It does not provide the plugin's
SDK dependency. From the standalone clone:

```sh
CGO_ENABLED=0 make verify-package-host KANDEV_TOOLS=/absolute/path/to/pinned/kandev-tools
CGO_ENABLED=0 GOMAXPROCS=2 make verify-package KANDEV_TOOLS=/absolute/path/to/pinned/kandev-tools
tar -tzf kandev-provider-usage-0.12.0.tar.gz
```

Both package checks passed, including exact inventory and every generated
internal checksum. Full inventory: manifest.yaml, ui/bundle.js, checksums.txt,
and the declared server binaries for linux-amd64, linux-arm64, darwin-amd64,
darwin-arm64 and windows-amd64.exe. Full archive SHA-256:
`d0629b7a2b9b8fc8d75811e4e30ead9935f832ae60c1c567f7d786e833ab54d8`.

The executor inherited `CGO_ENABLED=1`, which made an initial arm64 cross-build
use the amd64 assembler and fail. Explicit `CGO_ENABLED=0` passed all targets.
The new packager normalizes YAML and filters host-only manifest executables;
the old verifier rejected that valid form. Added normalized full/host fixtures
failed first, then passed after fixing the verifier, alongside its existing
negative cases. The full-mode check still requires all five declarations.

## Released minimum-host smoke

Downloaded the published `v0.99.0` Linux x64 runtime and checksum:

```sh
gh release download v0.99.0 --repo kdlbs/kandev \
  --pattern kandev-linux-x64.tar.gz --pattern kandev-linux-x64.tar.gz.sha256
sha256sum -c kandev-linux-x64.tar.gz.sha256
tar -xzf kandev-linux-x64.tar.gz
kandev/bin/kandev --version --json
```

Checksum passed; runtime archive SHA-256:
`a60656b3dbce2c663965270b22b81301e568ab39c9906f331280b4eda84294bb`.
The binary reports version v0.99.0, commit
`f0e2e774cfe2df8a0d15e4c21dea7b48580e38fb`.
Started it with `env -i`, a minimal PATH, task-owned HOME/XDG/TMPDIR and
KANDEV_HOME_DIR, `KANDEV_SERVER_HOST=127.0.0.1`, authentication disabled and
`run --headless --port 43981`. A clean environment is necessary: the executor's
inherited KANDEV_BUNDLE_DIR/KANDEV_VERSION otherwise select its own host.
`/health` reports `v0.99.0`.

Installed host archive `kandev-provider-usage-0.12.0.tar.gz` (SHA-256
`ce1681bbadd1d6c84901c2de48c4941bb490c491e58e65f3d2050f92016618b4`)
through `POST /api/plugins/install`. It became active. Config used the absolute
`test/host-smoke/fake-codexbar.sh`, providers `all`, pill providers `all` and
status mode `both`. No real provider account or personal profile was used.

```sh
npm install --prefix .build/browser-tools --no-save --package-lock=false \
  esbuild@0.25.12 react@19.2.3 react-dom@19.2.3 @playwright/test@1.61.1
SMOKE_BASE_URL=http://127.0.0.1:43981 node test/host-smoke/sdk-migration.mjs
PLUGIN_UI_BUNDLE="$PWD/.build/verify-host/ui/bundle.js" node test/host-smoke/auth-recovery.mjs
PLUGIN_UI_BUNDLE="$PWD/.build/verify-host/ui/bundle.js" node test/host-smoke/provider-panel.mjs
```

All passed using Chromium 149.0.7827.55. The Nix executor additionally needed
LD_LIBRARY_PATH pointing to installed Chromium shared libraries. The installed
smoke passed 19 HTTP/lifecycle checks: provider/status/discovery/session/overview
webhooks, Host state write, malformed input and method rejection, config
validation, disable/re-enable with browser reload and config preservation.
Installed UI checks passed for the desktop top bar/provider panel and custom
provider settings on desktop and 390 × 844 touch emulation without document
overflow. The two synthetic React fixtures passed both viewports, including
status drawer, tab interactions, Copilot credits and auth-recovery guidance.
An earlier provider-panel run timed out on a detached tab; the same unchanged
fixture passed on recheck. Phone coverage is emulation, not a physical device.

Built `cmd/mock-agent` from the pinned tooling source, placed that test binary
beside the downloaded runtime and restarted the disposable host with
`KANDEV_MOCK_AGENT=only`. Started a synthetic local task with the mock profile
and local executor; it reached WAITING_FOR_INPUT after an actual agent turn.
Used that task's agentctl endpoint (port 41022), retaining the MCP session header:

```sh
SMOKE_MCP_URL=http://127.0.0.1:41022/mcp python3 test/host-smoke/sdk-mcp.py
```

Passed initialize, tools/list (exactly one
`kandev_kandev_provider_usage_get_provider_usage`), tools/call with structured
schema v1 and instance-wide scope, and error serialization for a forbidden
workspace_id argument. This is local task/MCP/gRPC routing with a synthetic
model and provider; it is not live vendor telemetry. SSH, native macOS Keychain,
physical phones, other operating-system runtimes, authenticated-host roles and
plugin version upgrades were not exercised. Cross-platform binary compilation
and package inspection do not certify those runtime paths.
