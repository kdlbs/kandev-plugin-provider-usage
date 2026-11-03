package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCopilotSetupUsesEffectiveCLIAndProbesOnlyHelp(t *testing.T) {
	cmd := resolvedCommand{Argv: []string{"/managed cache/O'Brien/CodexBarCLI"}, Source: sourceDownload}
	for _, supported := range []bool{true, false} {
		p := &plugin{run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			require.Equal(t, cmd.Argv[0], name)
			require.Equal(t, []string{"config", "login", "--help"}, args, "background polling must never start login")
			if supported {
				return []byte("config login --provider copilot"), nil
			}
			return nil, errors.New("unsupported command")
		}}
		report := &AllProvidersReport{Unavailable: []ProviderError{{Provider: "copilot", Message: "No available fetch strategy for copilot."}}}
		p.addCopilotSetup(context.Background(), cmd, report)
		failure := report.Unavailable[0]
		require.Equal(t, "copilot_authorization", failure.Code)
		require.NotNil(t, failure.Setup)
		require.Contains(t, failure.Setup.Hint, "same operating-system user")
		if supported {
			require.Contains(t, failure.Setup.Command, "CodexBarCLI")
			require.NotEmpty(t, failure.Setup.Shell)
		} else {
			require.Empty(t, failure.Setup.Command)
			require.Contains(t, failure.Setup.Hint, "Upgrade CodexBar")
			require.Contains(t, failure.Setup.Hint, "Update CodexBar")
		}
	}
}

func TestCopilotServiceAndUnsupportedSourceErrorsAreNotLoginFailures(t *testing.T) {
	p := &plugin{run: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("no auth probe for service errors")
		return nil, nil
	}}
	for _, message := range []string{"HTTP 503 service unavailable", "network timeout", "Unsupported source oauth for copilot", "HTTP 403 subscription unavailable"} {
		report := &AllProvidersReport{Unavailable: []ProviderError{{Provider: "copilot", Message: message}}}
		p.addCopilotSetup(context.Background(), resolvedCommand{}, report)
		require.Empty(t, report.Unavailable[0].Code)
		require.Nil(t, report.Unavailable[0].Setup)
	}
}

func TestCopilotCommandShellQuoting(t *testing.T) {
	cmd := resolvedCommand{Argv: []string{"/cache/O'Brien and $private/CodexBarCLI", "fixed;argument"}}
	command, shell := copilotLoginCommand(cmd, "linux")
	require.Equal(t, "POSIX shell", shell)
	if runtime.GOOS != "windows" {
		// Ask the shell to parse the rendered command without executing it.
		out, err := exec.Command("sh", "-c", "set -- "+command+"; printf '%s\\n' \"$@\"").Output()
		require.NoError(t, err)
		require.Equal(t, "/cache/O'Brien and $private/CodexBarCLI\nfixed;argument\nconfig\nlogin\n--provider\ncopilot\n", string(out))
	}
	command, shell = copilotLoginCommand(resolvedCommand{Argv: []string{`C:\managed cache\O'Brien\codexbar-cli.exe`}}, "windows")
	require.Equal(t, "PowerShell", shell)
	require.Equal(t, `& 'C:\managed cache\O''Brien\codexbar-cli.exe' 'config' 'login' '--provider' 'copilot'`, command)
}

func TestCopilotRefreshRecoversFromReturnedUsage(t *testing.T) {
	authorized := false
	run := providerRunner(nil, nil)
	p := newTestPlugin(t, codexbarConfig(map[string]any{"codexbar_providers": "copilot"}), nil, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if args[len(args)-1] == "--version" {
			return run(ctx, name, args...)
		}
		if args[len(args)-1] == "--help" {
			return []byte("config login --provider copilot"), nil
		}
		if !authorized {
			return []byte(`[{"provider":"copilot","error":{"message":"No available fetch strategy for copilot."}}]`), errors.New("exit 1")
		}
		return []byte(`[{"provider":"copilot","source":"api","usage":{"primary":{"usedPercent":12,"windowMinutes":43200},"identity":{"planName":"Business"}}}]`), nil
	})
	p.cursor = nil
	before := p.pollOnce(context.Background(), 0)
	require.Empty(t, before.Providers)
	require.Equal(t, "copilot_authorization", before.Unavailable[0].Code)
	authorized = true
	after := p.pollOnce(context.Background(), 0)
	require.Empty(t, after.Unavailable)
	require.Len(t, after.Providers, 1)
	require.Equal(t, "copilot", after.Providers[0].Provider)
	require.Equal(t, "api", after.Providers[0].Source)
	require.Equal(t, "Premium interactions", after.Providers[0].Windows[0].Label)
}

func TestCodexbar083SourceFallbackAndNonzeroJSON(t *testing.T) {
	missing, err := os.ReadFile("testdata/codexbar-v0.83.0-copilot-missing.json")
	require.NoError(t, err)
	oauth, err := os.ReadFile("testdata/codexbar-v0.83.0-copilot-oauth.json")
	require.NoError(t, err)
	help, err := os.ReadFile("testdata/codexbar-v0.83.0-login-help.txt")
	require.NoError(t, err)
	calls := 0
	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls++
		if calls == 1 {
			require.Equal(t, "oauth", args[len(args)-1])
			return oauth, errors.New("exit 1")
		}
		return missing, errors.New("exit 1")
	}
	cmd := resolvedCommand{Argv: []string{"/managed cache/CodexBarCLI"}, Source: sourceDownload}
	entries, err := runUsageFast(context.Background(), cmd, run, "copilot")
	require.NoError(t, err, "valid per-provider errors survive nonzero exit")
	require.Equal(t, 2, calls)
	require.Len(t, entries, 1)
	require.Equal(t, "copilot", entries[0].Provider)
	require.True(t, copilotNeedsAuthorization(entries[0].Error.Message))
	p := &plugin{run: func(context.Context, string, ...string) ([]byte, error) { return help, nil }}
	report := &AllProvidersReport{Unavailable: []ProviderError{{Provider: "copilot", Message: entries[0].Error.Message}}}
	p.addCopilotSetup(context.Background(), cmd, report)
	require.NotEmpty(t, report.Unavailable[0].Setup.Command)
}

func TestCodexbar083SyntheticUsageCompatibility(t *testing.T) {
	raw, err := os.ReadFile("testdata/codexbar-v0.83.0-usage-synthetic.json")
	require.NoError(t, err)
	entries, err := runUsage(context.Background(), resolvedCommand{Argv: []string{"CodexBarCLI"}}, func(context.Context, string, ...string) ([]byte, error) { return raw, errors.New("exit 1") }, providersAll)
	require.NoError(t, err)
	require.Len(t, entries, 3)
	cursor := entries[0].toProviderUsage(time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC))
	require.Equal(t, "Business", cursor.Plan)
	require.Len(t, cursor.Windows, 3)
	require.Equal(t, "Total Usage", cursor.Windows[0].Label)
	copilot := entries[1].toProviderUsage(time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC))
	require.Equal(t, "api", copilot.Source)
	require.Equal(t, "Premium interactions", copilot.Windows[0].Label)
	require.Equal(t, float64(12), copilot.Windows[0].UtilizationPct)
	require.NotNil(t, entries[2].Error)
}
