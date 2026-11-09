package main

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kdlbs/kandev/pluginsdk"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestGetProviderUsageUsesSnapshotWithoutIO(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	p := newPlugin()
	p.now = func() time.Time { return now }
	p.snapshot = &AllProvidersReport{GeneratedAt: now.Add(-time.Minute).Format(time.RFC3339), PollMinutes: 5, Providers: []ProviderUsage{{Provider: "future-provider", FetchedAt: now.Add(-time.Minute), Windows: []UtilizationWindow{{Label: "5-hour", UtilizationPct: 20}}}}}
	result, err := p.InvokeAgentTool(context.Background(), &pluginsdk.AgentToolRequest{Name: providerUsageTool, Arguments: map[string]any{}, Context: pluginsdk.AgentToolContext{WorkspaceID: "ws-1"}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, "1", result.StructuredContent["schema_version"])
	require.Equal(t, "ws-1", result.StructuredContent["scope"].(map[string]any)["invocation_workspace_id"])
	providers := result.StructuredContent["providers"].([]any)
	require.Equal(t, "future-provider", providers[0].(map[string]any)["provider_id"])
}

func TestGetProviderUsageConcurrentCallsNeverRunProviders(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	p := newPlugin()
	p.now = func() time.Time { return now }
	p.snapshot = &AllProvidersReport{GeneratedAt: now.Format(time.RFC3339), PollMinutes: 5, Providers: []ProviderUsage{{Provider: "codex", FetchedAt: now}}}
	var calls atomic.Int32
	p.run = func(context.Context, string, ...string) ([]byte, error) {
		calls.Add(1)
		return nil, nil
	}

	const readers = 32
	var wg sync.WaitGroup
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := p.InvokeAgentTool(context.Background(), &pluginsdk.AgentToolRequest{Name: providerUsageTool, Arguments: map[string]any{}})
			require.NoError(t, err)
			require.Equal(t, "available", result.StructuredContent["providers"].([]any)[0].(map[string]any)["availability_state"])
		}()
	}
	wg.Wait()
	require.Zero(t, calls.Load(), "MCP snapshot reads must not trigger provider I/O")
}

func TestGetProviderUsageIsWellFormedBeforeFirstPoll(t *testing.T) {
	p := newPlugin()
	p.now = func() time.Time { return time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC) }
	result, err := p.InvokeAgentTool(context.Background(), &pluginsdk.AgentToolRequest{Name: providerUsageTool, Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, result.StructuredContent["partial"].(bool))
	require.Empty(t, result.StructuredContent["providers"].([]any))
}

func TestProviderUsageProjectionIsPartialWhenCodexbarIsDegradedButCursorSucceeds(t *testing.T) {
	now := time.Date(2026, 9, 20, 2, 0, 0, 0, time.UTC)
	content := providerUsageProjection(&AllProvidersReport{
		GeneratedAt: now.Format(time.RFC3339),
		PollMinutes: 5,
		Providers:   []ProviderUsage{{Provider: "cursor", FetchedAt: now}},
	}, now, now, "workspace")
	require.True(t, content["partial"].(bool), "a direct Cursor success does not make a failed CodexBar poll complete")
}

func TestProviderUsageRecordIsStaleAtExactBoundary(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	r := providerUsageRecord(ProviderUsage{Provider: "codex", FetchedAt: now.Add(-10 * time.Minute)}, time.Time{}, now, 600)
	require.True(t, r["stale"].(bool))
	require.Equal(t, "telemetry_stale", r["availability_state"])
}

func TestProviderUsageRecordStaleTelemetryDoesNotRetainQuotaExhaustionReason(t *testing.T) {
	now := time.Date(2026, 9, 20, 2, 0, 0, 0, time.UTC)
	r := providerUsageRecord(ProviderUsage{
		Provider:  "codex",
		FetchedAt: now.Add(-10 * time.Minute),
		Windows:   []UtilizationWindow{{Label: "5-hour", UtilizationPct: 100, ResetAt: now.Add(time.Hour)}},
	}, time.Time{}, now, 600)
	require.Equal(t, "telemetry_stale", r["availability_state"])
	require.NotContains(t, r, "reason", "stale telemetry cannot claim current quota exhaustion")
	require.Equal(t, "telemetry_stale", r["windows"].([]any)[0].(map[string]any)["availability_state"])
}

func TestProviderUsageRecordExhaustionIgnoresScopedWindow(t *testing.T) {
	now := time.Now().UTC()
	r := providerUsageRecord(ProviderUsage{Provider: "codex", Windows: []UtilizationWindow{{Label: "scoped", Scoped: true, UtilizationPct: 100}}}, now, now, 600)
	require.Equal(t, "available", r["availability_state"])
	require.NotEmpty(t, r["warnings"])
}

func TestProviderUsageRecordBoundsWindowPercentagesForAgentSchema(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, provider string
		used, want     float64
		state          string
	}{
		{"over-budget Augment", "augment", 120, 100, "available"},
		{"over-budget Cursor", "cursor", 175, 100, "quota_exhausted"},
		{"negative source value", "cursor", -5, 0, "available"},
		{"nonfinite source value", "cursor", math.NaN(), 0, "available"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := providerUsageRecord(ProviderUsage{Provider: tc.provider, Windows: []UtilizationWindow{{Label: "monthly", UtilizationPct: tc.used, ResetAt: now.Add(time.Hour)}}}, now, now, 600)
			window := r["windows"].([]any)[0].(map[string]any)
			require.Equal(t, tc.want, window["utilization_percentage"])
			require.Equal(t, 100-tc.want, window["remaining_percentage"])
			require.Equal(t, tc.state, r["availability_state"])
			if tc.provider == "augment" {
				require.Equal(t, "available", window["availability_state"])
				require.NotContains(t, r, "reason")
				require.NotEmpty(t, r["warnings"])
			}
		})
	}
}

func TestWindowIDPreservesSourceSlotWhenPrimaryIsMissing(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	secondary := &cbWindow{UsedPercent: 30, WindowMinutes: 10080}
	withPrimary := (&cbUsage{Primary: &cbWindow{UsedPercent: 10, WindowMinutes: 300}, Secondary: secondary}).windows("codex")
	withoutPrimary := (&cbUsage{Secondary: secondary}).windows("codex")
	first := providerUsageRecord(ProviderUsage{Provider: "codex", Windows: withPrimary}, now, now, 600)
	second := providerUsageRecord(ProviderUsage{Provider: "codex", Windows: withoutPrimary}, now, now, 600)
	require.Equal(t, "window-2", first["windows"].([]any)[1].(map[string]any)["window_id"])
	require.Equal(t, "window-2", second["windows"].([]any)[0].(map[string]any)["window_id"])
}

func TestExtraWindowIDUsesSourceIdentityNotDisplayTitle(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	usage := &cbUsage{Extra: []cbExtra{
		{ID: "alpha", Title: "Requests", Window: &cbWindow{UsedPercent: 25}},
		{ID: "beta", Title: "Requests", Window: &cbWindow{UsedPercent: 30}},
	}}
	first := providerUsageRecord(ProviderUsage{Provider: "codex", Windows: usage.windows("codex")}, now, now, 600)
	firstWindows := first["windows"].([]any)
	firstID := firstWindows[0].(map[string]any)["window_id"]
	require.NotEqual(t, firstID, firstWindows[1].(map[string]any)["window_id"])
	usage.Extra[0].Title = "Renamed"
	second := providerUsageRecord(ProviderUsage{Provider: "codex", Windows: usage.windows("codex")}, now, now, 600)
	require.Equal(t, firstID, second["windows"].([]any)[0].(map[string]any)["window_id"])
}

func TestProviderUsageProjectionRetainsOneRecordAfterRefreshFailure(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	snap := &AllProvidersReport{
		GeneratedAt: now.Format(time.RFC3339),
		PollMinutes: 5,
		Providers:   []ProviderUsage{{Provider: "claude", FetchedAt: now}},
		Unavailable: []ProviderError{{Provider: "claude", Kind: "provider_unavailable", Message: "Bearer secret"}},
	}
	content := providerUsageProjection(snap, now, now, "workspace")
	providers := content["providers"].([]any)
	require.Len(t, providers, 1)
	require.NotEmpty(t, providers[0].(map[string]any)["warnings"])
	require.True(t, content["partial"].(bool))
}

func TestSanitizedFailureStateAllowlistAndRedaction(t *testing.T) {
	for _, tc := range []struct{ kind, want string }{{"not_configured", "not_configured"}, {"unsupported", "unsupported"}, {"bogus", "unknown"}} {
		t.Run(tc.kind, func(t *testing.T) {
			state, reason := sanitizedFailureState(ProviderError{Kind: tc.kind, Message: "Bearer secret@example.com /home/me/.codex"})
			require.Equal(t, tc.want, state)
			require.NotContains(t, reason, "secret")
		})
	}
}

func TestClassifyProviderErrorRecognizesUpstreamAuthAndAvailabilityFailures(t *testing.T) {
	for _, tc := range []struct{ kind, message, want string }{
		{"provider", "No Cursor session found.", "not_configured"},
		{"provider", "Provider not installed: Codex auth.json not found.", "not_configured"},
		{"provider_unavailable", "upstream unavailable", "provider_unavailable"},
		{"unsupported", "provider is unsupported", "unsupported"},
		{"provider", "unrecognized future provider failure", ""},
	} {
		t.Run(tc.message, func(t *testing.T) {
			require.Equal(t, tc.want, classifyProviderError(tc.kind, tc.message))
		})
	}
}

func TestProviderUsageProjectionPreservesHardFailureClassification(t *testing.T) {
	now := time.Date(2026, 9, 21, 2, 0, 0, 0, time.UTC)
	content := providerUsageProjection(&AllProvidersReport{
		GeneratedAt: now.Format(time.RFC3339),
		Unavailable: []ProviderError{{Provider: "codex", Kind: "not_configured", Message: "Provider not installed: Codex auth.json not found"}},
	}, now, now, "workspace")
	providers := content["providers"].([]any)
	require.Len(t, providers, 1)
	record := providers[0].(map[string]any)
	require.Equal(t, "not_configured", record["availability_state"])
	require.Equal(t, "not_configured", record["reason"].(map[string]any)["code"])
}

func TestCursorFailurePreservesRecoveryAndAgentClassification(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	report := &AllProvidersReport{
		GeneratedAt: now.Format(time.RFC3339),
		Providers:   []ProviderUsage{{Provider: "cursor", FetchedAt: now}},
		Unavailable: []ProviderError{{Provider: "cursor", Kind: "not_configured"}},
	}
	cursorUnavailable(report, &cursorTeamSelectionError{errors.New("network unavailable")})
	require.Empty(t, report.Providers, "unverified selected-team usage must not be retained")
	require.True(t, report.cursorVerificationFailed)
	require.Len(t, report.Unavailable, 1)
	failure := report.Unavailable[0]
	require.Equal(t, "cursor_team_lookup", failure.Code)
	require.True(t, failure.AccountUsageAvailable, "Settings must keep the explicit account-recovery option")
	record := providerUsageProjection(report, now, now, "workspace")["providers"].([]any)[0].(map[string]any)
	require.Equal(t, "provider_unavailable", record["availability_state"])
	require.NotContains(t, record, "setup", "operator recovery details must not leak into agent telemetry")
}

func TestManifestConstrainsNestedWindowsAndReasons(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "manifest.yaml"))
	require.NoError(t, err)
	var manifest map[string]any
	require.NoError(t, yaml.Unmarshal(data, &manifest))
	tools := manifest["agent_tools"].([]any)
	schema := tools[0].(map[string]any)["output_schema"].(map[string]any)
	provider := schema["properties"].(map[string]any)["providers"].(map[string]any)["items"].(map[string]any)
	props := provider["properties"].(map[string]any)
	window := props["windows"].(map[string]any)["items"].(map[string]any)
	require.Contains(t, window["required"], "availability_state")
	require.Equal(t, false, window["additionalProperties"])
	reason := props["reason"].(map[string]any)
	require.Contains(t, reason["required"], "retryable")
	require.Equal(t, false, reason["additionalProperties"])
}
