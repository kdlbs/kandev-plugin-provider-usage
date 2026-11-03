# CodexBar 0.83.0 fixtures

The asset fixture records official release filenames and SHA-256 sidecars.
Each downloaded archive matched its sidecar on November 3, 2026.

The Copilot error fixtures and login help came from the verified Linux x86_64
musl executable. The commands used a disposable home, a missing CodexBar settings
file, and an environment without provider credentials. Both usage commands
returned exit code 1. Their JSON remains parseable.

```sh
CodexBarCLI config login --help
CodexBarCLI usage --provider copilot --format json --no-color
CodexBarCLI usage --provider copilot --format json --no-color --source oauth
```

The usage-success fixture uses synthetic values with the supported JSON shape.
It covers Cursor windows, Copilot usage, and a simultaneous provider error.
It is not a capture from a live authorized account.

The Copilot credit fixture uses synthetic values with the v0.83.0 detail-row format.
It includes the numeric `usageValue` and `progress.used`/`progress.total` fields.
The total represents a configured allowance. The fixture does not establish
an enforced GitHub quota or a default allowance for any plan.

Sources:

- [Official release and assets](https://github.com/steipete/CodexBar/releases/tag/v0.83.0)
- [Cursor session format](https://github.com/steipete/CodexBar/blob/v0.83.0/Sources/CodexBarCore/Providers/Cursor/CursorAppAuth.swift)
- [Safe Storage decryption](https://github.com/steipete/CodexBar/blob/v0.83.0/Sources/CodexBarCore/ChromiumSafeStorage.swift)
- [Copilot device login](https://github.com/steipete/CodexBar/blob/v0.83.0/docs/cli-configuration.md#copilot-device-login)
- [Copilot credit details](https://github.com/steipete/CodexBar/blob/v0.83.0/Sources/CodexBarCore/Providers/Copilot/CopilotUsageFetcher.swift)
