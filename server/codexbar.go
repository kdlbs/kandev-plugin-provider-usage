package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kandev/kandev/pkg/pluginsdk"
)

// Command sources, reported in InstallStatus.Source so the UI can explain where
// the codexbar invocation came from.
const (
	sourceSettings = "settings" // operator-configured command
	sourcePath     = "path"     // codexbar binary found on PATH
	sourceDownload = "download" // pinned per-platform binary downloaded + cached
)

// Failure stages, reported in InstallStatus.Stage so the UI can say whether the
// plugin never got a command at all, or got one that didn't run.
const (
	stageResolve = "resolve" // no runnable command (unsupported platform, failed download, ...)
	stageProbe   = "probe"   // a command exists, but `--version` failed
)

// resolvedCommand is the argv the plugin will run codexbar with, plus where that
// argv came from. Err is set when resolution itself failed (unsupported platform
// or a failed download); probeInstall/runUsage surface it as a degraded status.
// CacheDir is the auto-download cache root, carried for the download source so a
// failure hint can name the directory to clear.
type resolvedCommand struct {
	Argv     []string
	Source   string
	CacheDir string
	Err      error
}

// commandDisplay renders the resolved argv for humans
// ("/usr/local/bin/codexbar"), or "" when resolution never produced one.
func commandDisplay(cmd resolvedCommand) string {
	return strings.Join(cmd.Argv, " ")
}

// runner executes a command and returns its stdout — exec.CommandContext in
// production (see newPlugin), injected for tests.
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// InstallStatus reports whether the resolved command actually runs, and as what
// version. Embedded in every payload so the UI can render setup guidance from
// the same shape it always reads.
type InstallStatus struct {
	// Command is the resolved argv joined for display, "" when resolution failed.
	Command string `json:"command,omitempty"`
	// Source is where the command came from: "settings", "path" or "download".
	Source    string `json:"source"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	// Error is the raw cause, verbatim (exit status + stderr, HTTP status, ...).
	Error string `json:"error,omitempty"`
	// Stage is where it broke: "resolve" or "probe". Empty when installed.
	Stage string `json:"stage,omitempty"`
	// Hint is the operator-facing next step for Error, rendered under it on the
	// Settings card. Empty when the error speaks for itself.
	Hint string `json:"hint,omitempty"`
}

// probeInstall checks the resolved command works by running `--version` and
// parsing the version out of its output ("CodexBar 0.45.2"). A failure at either
// stage is annotated with an actionable hint rather than left as a bare error.
func probeInstall(ctx context.Context, cmd resolvedCommand, run runner) InstallStatus {
	status := InstallStatus{Command: commandDisplay(cmd), Source: cmd.Source}
	if cmd.Err != nil {
		status.Stage = stageResolve
		status.Error = cmd.Err.Error()
		status.Hint = installHint(cmd, cmd.Err)
		return status
	}
	out, err := run(ctx, cmd.Argv[0], append(argvTail(cmd), "--version")...)
	if err != nil {
		status.Stage = stageProbe
		status.Error = err.Error()
		status.Hint = installHint(cmd, err)
		return status
	}
	status.Installed = true
	status.Version = parseVersion(string(out))
	return status
}

// settingName is how the codexbar command field is labelled in the Settings
// form, so hints can point at the exact input to edit.
const settingName = `"codexbar · CLI command"`

// installHint turns a resolution or probe failure into the single next step the
// operator can take. Download failures are classified by installError kind;
// everything else is a command that exists but didn't run, which differs by
// where that command came from.
func installHint(cmd resolvedCommand, err error) string {
	var ierr *installError
	if errors.As(err, &ierr) {
		return downloadHint(ierr)
	}
	switch cmd.Source {
	case sourceSettings:
		return fmt.Sprintf(
			"kandev couldn't run %s, the command set in %s. Check that the path exists and is executable, "+
				"or clear the field to let the plugin download codexbar itself.",
			commandDisplay(cmd), settingName)
	case sourcePath:
		return fmt.Sprintf(
			"%s was found on PATH but didn't answer `--version`. Set a full path to a working codexbar in %s, "+
				"or remove the broken binary from PATH.",
			commandDisplay(cmd), settingName)
	case sourceDownload:
		return fmt.Sprintf(
			"The downloaded codexbar at %s didn't answer `--version` — it may be quarantined or truncated. "+
				"Delete %s and re-check to download it again, or set your own codexbar path in %s.",
			commandDisplay(cmd), cacheDirDisplay(cmd), settingName)
	}
	return ""
}

// downloadHint is the per-kind guidance for a failed auto-download.
func downloadHint(err *installError) string {
	switch err.Kind {
	case installErrUnsupported:
		return fmt.Sprintf(
			"No prebuilt codexbar CLI is published for this platform, so nothing can be downloaded here. "+
				"Install codexbar yourself and set its full path in %s.", settingName)
	case installErrDownload:
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Sprintf(
				"The one-time download of codexbar v%s ran out of time and was discarded. It retries on the next "+
					"refresh — press re-check. If this host can't reach github.com, install codexbar yourself and "+
					"set its path in %s.", err.Version, settingName)
		}
		return fmt.Sprintf(
			"The one-time download of codexbar v%s from %s failed. Check this host's outbound access to "+
				"github.com (proxy, firewall, air-gapped install), then press re-check — or install codexbar "+
				"yourself and set its path in %s.", err.Version, downloadTarget(err), settingName)
	case installErrChecksum:
		return fmt.Sprintf(
			"The downloaded archive didn't match the SHA-256 pinned for codexbar v%s, so it was discarded "+
				"instead of run. Press re-check to retry; if it keeps failing, install codexbar yourself and set "+
				"its path in %s.", err.Version, settingName)
	case installErrUnpack:
		return fmt.Sprintf(
			"The download couldn't be unpacked into %s. Check that directory's permissions and free space, "+
				"or set your own codexbar path in %s.", err.Path, settingName)
	}
	return ""
}

// downloadTarget names what the download hint should point at: the full asset
// URL, unless the raw error already spells it out one line above — then just the
// host, so the card doesn't print the same URL twice.
func downloadTarget(err *installError) string {
	if err.URL == "" || strings.Contains(err.Err.Error(), err.URL) {
		return "github.com"
	}
	return err.URL
}

// cacheDirDisplay names the auto-download cache root, falling back to a generic
// description when the command didn't come from the download path.
func cacheDirDisplay(cmd resolvedCommand) string {
	if cmd.CacheDir != "" {
		return cmd.CacheDir
	}
	return "the plugin's codexbar cache"
}

// parseVersion extracts "0.45.2" from codexbar's `--version` output
// ("CodexBar 0.45.2"). Empty when no version token is found.
func parseVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.EqualFold(fields[0], "codexbar") {
			return fields[1]
		}
	}
	return ""
}

// argvTail returns the command arguments after the binary (empty for a bare
// binary path). Copied so callers can append without mutating cmd.Argv.
func argvTail(cmd resolvedCommand) []string {
	if len(cmd.Argv) <= 1 {
		return []string{}
	}
	return append([]string{}, cmd.Argv[1:]...)
}

// sourceOAuth is codexbar's fast usage source: a direct HTTP call to the
// provider's usage endpoint, versus shelling out to the agent CLI (which can
// take many seconds for e.g. claude). Not every provider supports it.
const sourceOAuth = "oauth"

// runUsageFast fetches a single provider's usage, trying the fast oauth source
// first and falling back to codexbar's default (auto) source when oauth returns
// no usable usage — an unsupported provider, or the oauth endpoint erroring or
// being rate-limited. `all` is a sweep, not a single provider, so it skips the
// fast path.
func runUsageFast(ctx context.Context, cmd resolvedCommand, run runner, provider string) ([]cbEntry, error) {
	if provider == "" || provider == providersAll {
		return runUsage(ctx, cmd, run, provider)
	}
	if fast, err := runUsageSource(ctx, cmd, run, provider, sourceOAuth); err == nil && hasUsage(fast) {
		return fast, nil
	}
	return runUsage(ctx, cmd, run, provider)
}

// runUsage runs `codexbar usage --provider <p> --format json` with codexbar's
// default (auto) source and returns the decoded provider entries. provider may
// be "all".
func runUsage(ctx context.Context, cmd resolvedCommand, run runner, provider string) ([]cbEntry, error) {
	return runUsageSource(ctx, cmd, run, provider, "")
}

// runUsageSource runs a usage query, optionally forcing codexbar's --source.
//
// codexbar exits non-zero when a requested provider is unavailable (e.g. not
// signed in) yet still writes a valid JSON array carrying per-provider `error`
// objects to stdout. So the parsed stdout wins: a non-zero exit is only a hard
// failure when stdout can't be parsed into entries (e.g. the binary is missing).
func runUsageSource(ctx context.Context, cmd resolvedCommand, run runner, provider, source string) ([]cbEntry, error) {
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	args := append(argvTail(cmd), "usage", "--provider", provider, "--format", "json", "--no-color")
	if source != "" {
		args = append(args, "--source", source)
	}
	out, runErr := run(ctx, cmd.Argv[0], args...)
	entries, parseErr := parseCodexbarUsage(out)
	if parseErr == nil {
		return entries, nil
	}
	if runErr != nil {
		return nil, fmt.Errorf("running %s: %w", cmd.Argv[0], runErr)
	}
	return nil, parseErr
}

// hasUsage reports whether any entry carries a usage payload (vs. only errors).
func hasUsage(entries []cbEntry) bool {
	for _, e := range entries {
		if e.Usage != nil {
			return true
		}
	}
	return false
}

// providerMatch pairs a codexbar provider id with the lowercase substrings that,
// when found in a session's agent/model strings, identify that provider. Order
// matters: the first match wins.
type providerMatch struct {
	provider string
	needles  []string
}

var providerMatches = []providerMatch{
	{"claude", []string{"claude", "anthropic", "sonnet", "opus", "haiku", "fable"}},
	{"codex", []string{"codex", "openai", "gpt", "o1", "o3", "o4"}},
	{"gemini", []string{"gemini", "antigravity"}},
	{"copilot", []string{"copilot"}},
	{"cursor", []string{"cursor"}},
	{"grok", []string{"grok", "xai"}},
	// codexbar splits opencode into two ids — `opencode` (web/browser cookies
	// only) and `opencodego` (the Go CLI: local SQLite + optional web overlay).
	// The plugin polls only opencodego (no cookies needed) and surfaces it as a
	// single "OpenCode" provider; both spellings map to it.
	{"opencodego", []string{"opencode", "opencodego"}},
	{"augment", []string{"augment", "auggie"}},
	{"amp", []string{"amp"}},
}

// providerForSession maps a kandev session to a codexbar provider id using the
// agent profile name, display name, and model. Returns "" when nothing matches.
func providerForSession(s pluginsdk.Session) string {
	hay := strings.ToLower(strings.Join([]string{
		s.AgentProfileName, s.AgentDisplayName, s.Model,
	}, " "))
	for _, m := range providerMatches {
		for _, n := range m.needles {
			if strings.Contains(hay, n) {
				return m.provider
			}
		}
	}
	return ""
}
