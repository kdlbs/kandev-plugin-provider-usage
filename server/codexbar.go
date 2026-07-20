package main

import (
	"context"
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

// resolvedCommand is the argv the plugin will run codexbar with, plus where that
// argv came from. Err is set when resolution itself failed (unsupported platform
// or a failed download); probeInstall/runUsage surface it as a degraded status.
type resolvedCommand struct {
	Argv   []string
	Source string
	Err    error
}

// commandDisplay renders the resolved argv for humans ("/usr/local/bin/codexbar").
func commandDisplay(cmd resolvedCommand) string {
	if len(cmd.Argv) == 0 {
		return "codexbar"
	}
	return strings.Join(cmd.Argv, " ")
}

// runner executes a command and returns its stdout — exec.CommandContext in
// production (see newPlugin), injected for tests.
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// InstallStatus reports whether the resolved command actually runs, and as what
// version. Embedded in every payload so the UI can render setup guidance from
// the same shape it always reads.
type InstallStatus struct {
	// Command is the resolved argv joined for display.
	Command string `json:"command"`
	// Source is where the command came from: "settings", "path" or "download".
	Source    string `json:"source"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

// probeInstall checks the resolved command works by running `--version` and
// parsing the version out of its output ("CodexBar 0.45.2").
func probeInstall(ctx context.Context, cmd resolvedCommand, run runner) InstallStatus {
	status := InstallStatus{Command: commandDisplay(cmd), Source: cmd.Source}
	if cmd.Err != nil {
		status.Error = cmd.Err.Error()
		return status
	}
	out, err := run(ctx, cmd.Argv[0], append(argvTail(cmd), "--version")...)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Installed = true
	status.Version = parseVersion(string(out))
	return status
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

// runUsage runs `codexbar usage --provider <p> --format json` and returns the
// decoded provider entries. provider may be "all".
//
// codexbar exits non-zero when a requested provider is unavailable (e.g. not
// signed in) yet still writes a valid JSON array carrying per-provider `error`
// objects to stdout. So the parsed stdout wins: a non-zero exit is only a hard
// failure when stdout can't be parsed into entries (e.g. the binary is missing).
func runUsage(ctx context.Context, cmd resolvedCommand, run runner, provider string) ([]cbEntry, error) {
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	args := append(argvTail(cmd), "usage", "--provider", provider, "--format", "json", "--no-color")
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
	{"opencode", []string{"opencode"}},
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
