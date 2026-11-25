package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kdlbs/kandev/pluginsdk"
	"github.com/stretchr/testify/require"
)

const devinDailyWeeklyJSON = `[{"provider":"devin","source":"api","usage":{
	"primary":{"usedPercent":24,"windowMinutes":1440,"resetsAt":"2026-11-26T00:00:00Z"},
	"secondary":{"usedPercent":63,"windowMinutes":10080,"resetsAt":"2026-11-30T00:00:00Z"},
	"updatedAt":"2026-11-25T09:00:00Z"}}]`

const devinWeeklyOnlyJSON = `[{"provider":"devin","source":"api","usage":{
	"secondary":{"usedPercent":63,"windowMinutes":10080,"resetsAt":"2026-11-30T00:00:00Z"}}}]`

const devinWindowsJSON = `[{"provider":"devin","source":"api","usage":{
	"primary":{"usedPercent":0},"secondary":{"usedPercent":37}}}]`

func TestProviderForSessionDevinPrecedesModelAndCustomProfile(t *testing.T) {
	for _, tc := range []struct {
		name    string
		session pluginsdk.Session
	}{
		{"Claude model", pluginsdk.Session{AgentDisplayName: "Devin", AgentProfileName: "Claude Code Custom", Model: "claude-sonnet-4"}},
		{"GPT model", pluginsdk.Session{AgentDisplayName: "Devin Agent", AgentProfileName: "OpenAI Codex Custom", Model: "gpt-5"}},
		{"Fusion model", pluginsdk.Session{AgentDisplayName: "Devin", AgentProfileName: "Codex helper", Model: "Fusion 3"}},
		{"profile identifies Devin", pluginsdk.Session{AgentProfileName: "Devin Custom Agent", Model: "claude-opus-4"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, "devin", providerForSession(tc.session))
		})
	}
}

func TestRunUsageFastDevinUsesAutomaticSourceOnce(t *testing.T) {
	calls := 0
	entries, err := runUsageFast(context.Background(), resolvedCommand{Argv: []string{"codexbar"}}, func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls++
		require.Contains(t, args, "devin")
		require.NotContains(t, args, "--source")
		return []byte(devinDailyWeeklyJSON), nil
	}, "devin")
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.True(t, hasUsage(entries))
}

func TestDevinSessionWebhookUsesHostSessionDisplayName(t *testing.T) {
	sess := pluginsdk.Session{
		ID: "devin-session", TaskID: "task-1", AgentDisplayName: "Devin",
		AgentProfileName: "Claude Code Custom", Model: "claude-sonnet-4",
	}
	var mu sync.Mutex
	var devinArgs []string
	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[len(args)-1] == "--version" {
			return []byte("CodexBar 0.83.0\n"), nil
		}
		provider := argValue(args, "--provider")
		if provider == "devin" {
			mu.Lock()
			devinArgs = append([]string(nil), args...)
			mu.Unlock()
			return []byte(devinWeeklyOnlyJSON), nil
		}
		if provider == "claude" {
			return []byte("[" + sampleClaudeInner() + "]"), nil
		}
		return []byte("[]"), nil
	}
	p := newTestPlugin(t, codexbarConfig(map[string]any{configKeyProviders: "claude"}), []pluginsdk.Session{sess}, run)
	response, err := p.HandleWebhook(context.Background(), &pluginsdk.WebhookRequest{
		WebhookKey: webhookKeySession, Method: "GET", Query: "task_id=task-1&active=devin-session",
	})
	require.NoError(t, err)
	var report SessionUsageReport
	require.NoError(t, json.Unmarshal(response.Body, &report))
	require.Equal(t, "devin", report.Provider)
	require.NotNil(t, report.Usage)
	require.Len(t, report.Usage.Windows, 1, "a weekly-only response keeps its secondary slot")
	require.Equal(t, "weekly", report.Usage.Windows[0].Label)
	require.InDelta(t, 63.0, report.Usage.Windows[0].UtilizationPct, 1e-9)
	mu.Lock()
	require.NotEmpty(t, devinArgs)
	require.NotContains(t, devinArgs, "--source")
	mu.Unlock()
}

func TestDevinIsAddedToDefaultPollingOnlyWhenDiscovered(t *testing.T) {
	for _, discovered := range []bool{false, true} {
		t.Run(map[bool]string{false: "without connection", true: "with connection"}[discovered], func(t *testing.T) {
			var mu sync.Mutex
			var providers []string
			run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
				if len(args) > 0 && args[len(args)-1] == "--version" {
					return []byte("CodexBar 0.83.0\n"), nil
				}
				provider := argValue(args, "--provider")
				mu.Lock()
				providers = append(providers, provider)
				mu.Unlock()
				if provider == "devin" {
					return []byte(devinDailyWeeklyJSON), nil
				}
				return []byte("[]"), nil
			}
			p := newTestPlugin(t, codexbarConfig(nil), nil, run)
			if discovered {
				p.scanProviders = func(map[string]any) []detectedProvider { return []detectedProvider{{"devin", "cli"}} }
			}
			report := p.pollOnce(context.Background(), 0)
			mu.Lock()
			count := 0
			for _, provider := range providers {
				if provider == "devin" {
					count++
				}
			}
			mu.Unlock()
			if discovered {
				require.Equal(t, 1, count)
				require.Contains(t, report.DetectedProviders, detectedProvider{"devin", "cli"})
			} else {
				require.Zero(t, count)
				require.NotContains(t, report.DetectedProviders, detectedProvider{"devin", "cli"})
			}
		})
	}
}

func TestDevinManualEnvironmentConnectionEnablesDefaultPollingWithoutCLI(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "codexbar.json")
	const manualConfig = `{"providers":[{"id":"devin","cookieSource":"manual","cookieHeader":null,"workspaceID":"org_fixture"}]}`
	require.NoError(t, os.WriteFile(configPath, []byte(manualConfig), 0600))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEXBAR_CONFIG", configPath)
	t.Setenv("DEVIN_BEARER_TOKEN", "environment-secret")
	t.Setenv("DEVIN_ORG", "org_fixture")
	t.Setenv("PATH", t.TempDir()) // Empty PATH fixture: Devin has no CLI.

	var mu sync.Mutex
	var devinArgs []string
	devinCalls := 0
	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[len(args)-1] == "--version" {
			return []byte("CodexBar 0.83.0\n"), nil
		}
		if argValue(args, "--provider") == "devin" {
			mu.Lock()
			devinCalls++
			devinArgs = append([]string(nil), args...)
			mu.Unlock()
			return []byte(devinDailyWeeklyJSON), nil
		}
		return []byte("[]"), nil
	}
	p := newTestPlugin(t, codexbarConfig(nil), nil, run)
	p.scanProviders = func(cfg map[string]any) []detectedProvider {
		found := scanLocalProviders(cfg)
		return append(found, configuredCodexbarProviders()...)
	}
	discovery := p.discoveryWebhook(context.Background(), "GET")
	require.NotContains(t, string(discovery.Body), "environment-secret")
	require.NotContains(t, string(discovery.Body), "org_fixture")
	report := p.pollOnce(context.Background(), 0)
	mu.Lock()
	require.NotEmpty(t, devinArgs, "the configured manual environment connection is polled by default")
	require.Equal(t, 1, devinCalls)
	require.NotContains(t, devinArgs, "--source")
	mu.Unlock()
	require.Contains(t, report.DetectedProviders, detectedProvider{"devin", "configuration"})
	require.Len(t, report.Providers, 1)
	require.Equal(t, "devin", report.Providers[0].Provider)
}

func TestDevinAllowlistAndPauseControlBackgroundPolling(t *testing.T) {
	for _, tc := range []struct {
		name       string
		allowlist  string
		disabled   string
		discovered bool
		wantDevin  bool
	}{
		{name: "explicit inclusion", allowlist: "devin", wantDevin: true},
		{name: "explicit exclusion", allowlist: "claude", discovered: true},
		{name: "paused", allowlist: "devin", disabled: "devin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var devinCalls int
			run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
				if len(args) > 0 && args[len(args)-1] == "--version" {
					return []byte("CodexBar 0.83.0\n"), nil
				}
				if argValue(args, "--provider") == "devin" {
					mu.Lock()
					devinCalls++
					mu.Unlock()
					return []byte(devinDailyWeeklyJSON), nil
				}
				return []byte("[]"), nil
			}
			cfg := codexbarConfig(map[string]any{configKeyProviders: tc.allowlist})
			if tc.disabled != "" {
				cfg[configKeyDisabledProviders] = tc.disabled
			}
			p := newTestPlugin(t, cfg, []pluginsdk.Session{{ID: "devin-session", TaskID: "task-1", AgentDisplayName: "Devin"}}, run)
			if tc.discovered {
				p.scanProviders = func(map[string]any) []detectedProvider { return []detectedProvider{{"devin", "configuration"}} }
			}
			p.pollOnce(context.Background(), 0)
			var sessionReport SessionUsageReport
			if tc.disabled != "" {
				response, err := p.HandleWebhook(context.Background(), &pluginsdk.WebhookRequest{WebhookKey: webhookKeySession, Method: "GET", Query: "task_id=task-1&active=devin-session"})
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(response.Body, &sessionReport))
				require.Equal(t, "devin", sessionReport.Provider)
				require.Contains(t, sessionReport.Error, "disabled")
			}
			mu.Lock()
			gotCalls := devinCalls
			mu.Unlock()
			require.Equal(t, tc.wantDevin, gotCalls == 1)
			if tc.disabled != "" {
				// The paused provider is resolved but does not launch an on-demand query.
				require.Zero(t, gotCalls)
			}
		})
	}
}

func TestDevinFullSweepRemainsSingleCallAndFiltersPausedRows(t *testing.T) {
	calls := 0
	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[len(args)-1] == "--version" {
			return []byte("CodexBar 0.83.0\n"), nil
		}
		calls++
		require.Equal(t, providersAll, argValue(args, "--provider"))
		return []byte("[{\"provider\":\"devin\",\"usage\":{\"primary\":{\"usedPercent\":12}}}," + sampleClaudeInner() + "]"), nil
	}
	p := newTestPlugin(t, codexbarConfig(map[string]any{configKeyProviders: providersAll, configKeyDisabledProviders: "devin"}), nil, run)
	report := p.pollOnce(context.Background(), 0)
	require.Equal(t, 1, calls)
	require.Len(t, report.Providers, 1)
	require.Equal(t, "claude", report.Providers[0].Provider)
	require.Empty(t, report.Unavailable)
}

func TestDevinAuthErrorHasGuidanceAlongsideHealthyProviderUsage(t *testing.T) {
	const unavailable = `[{"provider":"devin","error":{"kind":"provider","code":1,"message":"Error: selected source requires web support and is only supported on macOS."}}]`
	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[len(args)-1] == "--version" {
			return []byte("CodexBar 0.83.0\n"), nil
		}
		if argValue(args, "--provider") == "devin" {
			return []byte(unavailable), errors.New("exit status 1")
		}
		return []byte("[" + sampleClaudeInner() + "]"), nil
	}
	p := newTestPlugin(t, codexbarConfig(map[string]any{configKeyProviders: "claude,devin"}), nil, run)
	report := p.pollOnce(context.Background(), 0)
	require.Len(t, report.Providers, 1)
	require.Equal(t, "claude", report.Providers[0].Provider)
	require.Len(t, report.Unavailable, 1)
	failure := report.Unavailable[0]
	require.Equal(t, "devin", failure.Provider)
	require.Equal(t, "not_configured", failure.Kind)
	require.Equal(t, "devin_authorization", failure.Code)
	require.NotNil(t, failure.Setup)
	require.Contains(t, failure.Setup.Hint, devinSetupDocs)
	require.Contains(t, failure.Message, "only supported on macOS", "the original CLI diagnostic is retained")
}

func TestDevinSetupClassifiesAuthButNotServiceErrors(t *testing.T) {
	for _, message := range []string{
		"No Devin session found.",
		"Devin rejected the session token (invalid or expired).",
		"No Devin organization was found.",
		"No available fetch strategy for devin.",
		"Provider not installed: API key not found. Set DEVIN_BEARER_TOKEN.",
		"Authentication required.",
		"Error: selected source requires web support and is only supported on macOS.",
	} {
		require.True(t, devinNeedsAuthorization("provider", message), message)
	}
	require.False(t, devinNeedsAuthorization("provider", "Devin API error: service unavailable"))
	require.False(t, devinNeedsAuthorization("provider", "Devin API error: HTTP 503"))
	require.Contains(t, devinSetupHint("linux"), "macOS-only")
	require.Contains(t, devinSetupHint("linux"), "cookieSource=manual")
	require.Contains(t, devinSetupHint("linux"), "environment token does not require cookieHeader")
	require.Contains(t, devinSetupHint("darwin"), "Chromium")
	require.Contains(t, devinSetupHint("windows"), "DEVIN_BEARER_TOKEN")
	require.Contains(t, devinSetupHint("windows"), "DEVIN_ORG")
	require.NotContains(t, devinSetupHint("windows"), "workspace_id")
}

func TestDevinUnavailableFetchStrategyGetsSetupAndRetainsDiagnostic(t *testing.T) {
	const message = "No available fetch strategy for devin."
	report := &AllProvidersReport{Unavailable: []ProviderError{{Provider: "devin", Kind: "provider", Message: message}}}
	newPlugin().addDevinSetup(report)
	require.Len(t, report.Unavailable, 1)
	require.Equal(t, message, report.Unavailable[0].Message)
	require.Equal(t, "not_configured", report.Unavailable[0].Kind)
	require.Equal(t, "devin_authorization", report.Unavailable[0].Code)
	require.NotNil(t, report.Unavailable[0].Setup)
}

func TestDevinQuotaWindowsAndResets(t *testing.T) {
	for _, tc := range []struct {
		name       string
		raw        string
		labels     []string
		percent    []float64
		resets     []string
		resetUnset []bool
	}{
		{name: "daily and weekly", raw: devinDailyWeeklyJSON, labels: []string{"daily", "weekly"}, percent: []float64{24, 63}, resets: []string{"2026-11-26T00:00:00Z", "2026-11-30T00:00:00Z"}},
		{name: "weekly only", raw: devinWeeklyOnlyJSON, labels: []string{"weekly"}, percent: []float64{63}, resets: []string{"2026-11-30T00:00:00Z"}},
		{name: "Windows adapter with zero daily", raw: devinWindowsJSON, labels: []string{"daily", "weekly"}, percent: []float64{0, 37}, resetUnset: []bool{true, true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries, err := parseCodexbarUsage([]byte(tc.raw))
			require.NoError(t, err)
			u := entries[0].toProviderUsage(time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC))
			require.NotNil(t, u)
			require.Len(t, u.Windows, len(tc.labels))
			for i, window := range u.Windows {
				require.Equal(t, tc.labels[i], window.Label)
				require.InDelta(t, tc.percent[i], window.UtilizationPct, 1e-9)
				if len(tc.resets) > 0 {
					require.Equal(t, tc.resets[i], window.ResetAt.UTC().Format(time.RFC3339))
				} else if tc.resetUnset[i] {
					require.True(t, window.ResetAt.IsZero())
				}
			}
		})
	}
}
