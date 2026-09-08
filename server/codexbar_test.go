package main

import (
	"context"
	"errors"
	"sync/atomic"
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
		{"opencode by profile", pluginsdk.Session{AgentProfileName: "OpenCode"}, "opencodego"},
		{"opencodego by display", pluginsdk.Session{AgentDisplayName: "OpenCode Go"}, "opencodego"},
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

func TestRunUsageFast_OAuthHitSkipsFallback(t *testing.T) {
	var calls int32
	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		require.Contains(t, args, "oauth", "fast path forces --source oauth")
		return []byte("[" + sampleCodexEntry + "]"), nil
	}
	entries, err := runUsageFast(context.Background(), resolvedCommand{Argv: []string{"codexbar"}}, run, "codex")
	require.NoError(t, err)
	require.True(t, hasUsage(entries))
	require.Equal(t, int32(1), calls, "oauth returned usage -> no default-source fallback")
}

func TestRunUsageFast_FallsBackWhenOAuthEmpty(t *testing.T) {
	var calls int32
	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 { // oauth attempt: rate-limited error, no usage
			return []byte("[" + sampleCursorError + "]"), errors.New("exit status 3")
		}
		return []byte("[" + sampleClaudeInner() + "]"), nil // default source: usage
	}
	entries, err := runUsageFast(context.Background(), resolvedCommand{Argv: []string{"codexbar"}}, run, "claude")
	require.NoError(t, err)
	require.True(t, hasUsage(entries))
	require.Equal(t, int32(2), calls, "oauth had no usage -> fell back to default source")
}

func TestRunUsageFast_AllSkipsFastPath(t *testing.T) {
	var calls int32
	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		require.NotContains(t, args, "oauth", "the sweep does not force a source")
		return []byte("[" + sampleCodexEntry + "]"), nil
	}
	_, err := runUsageFast(context.Background(), resolvedCommand{Argv: []string{"codexbar"}}, run, providersAll)
	require.NoError(t, err)
	require.Equal(t, int32(1), calls)
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
	require.Equal(t, stageResolve, status.Stage)
	require.Empty(t, status.Command, "resolution never produced a command to show")
}

// TestProbeInstall_ProbeErrorCarriesHint covers the common misconfiguration: a
// configured command that doesn't run. The Settings card gets the raw error AND
// the field to fix.
func TestProbeInstall_ProbeErrorCarriesHint(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("fork/exec /opt/codexbar: no such file or directory")
	}
	cmd := resolvedCommand{Argv: []string{"/opt/codexbar"}, Source: sourceSettings}
	status := probeInstall(context.Background(), cmd, run)

	require.False(t, status.Installed)
	require.Equal(t, stageProbe, status.Stage)
	require.Equal(t, "/opt/codexbar", status.Command)
	require.Contains(t, status.Error, "no such file or directory")
	require.Contains(t, status.Hint, "/opt/codexbar")
	require.Contains(t, status.Hint, settingName, "names the setting to edit")
}

func TestInstallHint_ByDownloadFailure(t *testing.T) {
	cmd := resolvedCommand{Source: sourceDownload, CacheDir: "/home/u/.config/kandev-provider-usage"}
	cases := []struct {
		name string
		err  error
		want []string
	}{
		{
			"unsupported platform",
			&installError{Kind: installErrUnsupported, Err: errors.New("no prebuilt CLI for linux-386")},
			[]string{"No prebuilt codexbar CLI is published for this platform", settingName},
		},
		{
			"download failed",
			&installError{Kind: installErrDownload, URL: "https://example.test/cli.tar.gz", Version: pinnedVersion, Err: errors.New("unexpected status 403")},
			[]string{"https://example.test/cli.tar.gz", "github.com", pinnedVersion},
		},
		{
			// The raw error already ends in the URL, so the hint says "github.com"
			// instead of repeating it.
			"download failure that already names the url",
			&installError{
				Kind: installErrDownload,
				URL:  "https://example.test/cli.tar.gz",
				Err:  errors.New("downloading codexbar: unexpected status 403 fetching https://example.test/cli.tar.gz"),
			},
			[]string{"from github.com failed"},
		},
		{
			"download timed out",
			&installError{Kind: installErrDownload, URL: "https://example.test/cli.tar.gz", Err: context.DeadlineExceeded},
			[]string{"ran out of time", "re-check"},
		},
		{
			"checksum mismatch",
			&installError{Kind: installErrChecksum, Err: errors.New("codexbar checksum mismatch")},
			[]string{"SHA-256", "discarded"},
		},
		{
			"unpack failed",
			&installError{Kind: installErrUnpack, Path: "/cache/codexbar/0.45.2", Err: errors.New("permission denied")},
			[]string{"/cache/codexbar/0.45.2", "permissions"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hint := installHint(cmd, c.err)
			for _, want := range c.want {
				require.Contains(t, hint, want)
			}
		})
	}
}

// TestInstallHint_BySource covers a command that exists but didn't answer
// `--version`: the fix differs by where the command came from.
func TestInstallHint_BySource(t *testing.T) {
	err := errors.New("exit status 1")

	settings := installHint(resolvedCommand{Argv: []string{"/opt/cb"}, Source: sourceSettings}, err)
	require.Contains(t, settings, "clear the field")

	path := installHint(resolvedCommand{Argv: []string{"/usr/bin/codexbar"}, Source: sourcePath}, err)
	require.Contains(t, path, "PATH")

	download := installHint(
		resolvedCommand{Argv: []string{"/cache/CodexBarCLI"}, Source: sourceDownload, CacheDir: "/cache"}, err)
	require.Contains(t, download, "/cache")

	require.Empty(t, installHint(resolvedCommand{Argv: []string{"cb"}}, err), "unknown source has nothing to add")
}

// TestDownloadHint_NamesThePlatformsOwnVersion guards a bug the Windows entry
// introduced: the hints used to interpolate the upstream constant, so a Windows
// operator was told to expect a version the plugin never tried to fetch.
func TestDownloadHint_NamesThePlatformsOwnVersion(t *testing.T) {
	winAsset := codexbarAssets["windows-amd64"]
	hint := downloadHint(&installError{
		Kind:    installErrDownload,
		URL:     winAsset.url,
		Version: winAsset.version,
		Err:     errors.New("unexpected status 404"),
	})
	require.Contains(t, hint, winPinnedVersion)
	require.NotContains(t, hint, pinnedVersion, "the upstream version is not what Windows fetches")
}
