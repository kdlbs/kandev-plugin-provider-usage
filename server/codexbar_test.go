package main

import (
	"context"
	"errors"
	"testing"

	"github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/stretchr/testify/require"
)

func TestParseVersion(t *testing.T) {
	require.Equal(t, "0.45.2", parseVersion("CodexBar 0.45.2\n"))
	require.Equal(t, "0.45.2", parseVersion("noise\nCodexBar 0.45.2 (build)\n"))
	require.Equal(t, "", parseVersion("no version here"))
}

func TestProviderForSession(t *testing.T) {
	cases := []struct {
		name string
		sess pluginsdk.Session
		want string
	}{
		{"claude by profile", pluginsdk.Session{AgentProfileName: "Claude Code"}, "claude"},
		{"claude by model", pluginsdk.Session{Model: "claude-opus-4-8"}, "claude"},
		{"claude by fable model", pluginsdk.Session{Model: "claude-fable-5"}, "claude"},
		{"codex by display", pluginsdk.Session{AgentDisplayName: "Codex"}, "codex"},
		{"codex by gpt model", pluginsdk.Session{Model: "gpt-5-codex"}, "codex"},
		{"gemini", pluginsdk.Session{AgentDisplayName: "Gemini CLI"}, "gemini"},
		{"copilot", pluginsdk.Session{AgentProfileName: "GitHub Copilot"}, "copilot"},
		{"cursor", pluginsdk.Session{AgentDisplayName: "Cursor Agent"}, "cursor"},
		{"grok", pluginsdk.Session{Model: "grok-4"}, "grok"},
		{"unknown", pluginsdk.Session{AgentDisplayName: "Mystery"}, ""},
		{"empty", pluginsdk.Session{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, providerForSession(c.sess))
		})
	}
}

// TestRunUsage_NonZeroExitStillParses covers codexbar's behaviour: an
// unavailable provider exits non-zero but still writes a valid JSON array with
// an `error` entry to stdout. The parsed stdout must win.
func TestRunUsage_NonZeroExitStillParses(t *testing.T) {
	run := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("[" + sampleCursorError + "]"), errors.New("exit status 1")
	}
	entries, err := runUsage(context.Background(), resolvedCommand{Argv: []string{"codexbar"}}, run, "cursor")
	require.NoError(t, err, "non-zero exit with valid JSON is not a hard failure")
	require.Len(t, entries, 1)
	require.NotNil(t, entries[0].Error)
}

func TestRunUsage_HardFailureWhenNoOutput(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("exec: \"codexbar\": not found")
	}
	_, err := runUsage(context.Background(), resolvedCommand{Argv: []string{"codexbar"}}, run, "claude")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestRunUsage_ResolutionError(t *testing.T) {
	cmd := resolvedCommand{Source: sourceDownload, Err: errors.New("no prebuilt CLI")}
	_, err := runUsage(context.Background(), cmd, nil, "claude")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no prebuilt")
}

func TestProbeInstall_ResolutionError(t *testing.T) {
	cmd := resolvedCommand{Source: sourceDownload, Err: errors.New("no prebuilt CLI")}
	status := probeInstall(context.Background(), cmd, nil)
	require.False(t, status.Installed)
	require.Contains(t, status.Error, "no prebuilt")
}
