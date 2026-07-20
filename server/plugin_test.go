// Package main tests. Exercises plugin.HandleWebhook end to end against a fake
// Host and an injected runner — no go-plugin spawn and no real codexbar needed,
// mirroring the other kandev plugins' test approach.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/stretchr/testify/require"
)

// fakeHost serves GetConfig and Sessions() — the only Host surfaces this plugin
// uses. Everything else comes from UnimplementedHostData.
type fakeHost struct {
	pluginsdk.UnimplementedHostData
	config   map[string]any
	sessions []pluginsdk.Session
}

func (h *fakeHost) GetState(context.Context, string, string, string) (map[string]any, bool, error) {
	return nil, false, nil
}
func (h *fakeHost) SetState(context.Context, string, string, string, map[string]any) error {
	return nil
}
func (h *fakeHost) DeleteState(context.Context, string, string, string) error { return nil }
func (h *fakeHost) ListState(context.Context, string, string) ([]pluginsdk.StateEntry, error) {
	return nil, nil
}
func (h *fakeHost) GetConfig(context.Context) (map[string]any, error) {
	if h.config == nil {
		return map[string]any{}, nil
	}
	return h.config, nil
}
func (h *fakeHost) RevealSecret(context.Context, string) (string, error) { return "", nil }
func (h *fakeHost) GetSecret(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (h *fakeHost) SetSecret(context.Context, string, string) error         { return nil }
func (h *fakeHost) DeleteSecret(context.Context, string) error              { return nil }
func (h *fakeHost) EmitEvent(context.Context, string, map[string]any) error { return nil }

func (h *fakeHost) Sessions() pluginsdk.SessionReader {
	return fakeSessionReader{sessions: h.sessions}
}

type fakeSessionReader struct {
	sessions []pluginsdk.Session
}

func (r fakeSessionReader) List(context.Context, pluginsdk.SessionFilter, pluginsdk.Page) ([]pluginsdk.Session, *pluginsdk.PageInfo, error) {
	return r.sessions, nil, nil
}
func (r fakeSessionReader) CodeStats(context.Context, pluginsdk.SessionFilter, pluginsdk.Page) ([]pluginsdk.SessionCodeStats, *pluginsdk.PageInfo, error) {
	return nil, nil, nil
}

func noLookPath(string) (string, error) { return "", errors.New("not found") }

// newTestPlugin wires a plugin with a scripted runner, no PATH lookup, and a
// downloader that never hits the network (so an unconfigured resolve fails
// cleanly instead of downloading). Pass config {"command":"codexbar"} to select
// the settings source.
func newTestPlugin(t *testing.T, config map[string]any, sessions []pluginsdk.Session, run runner) *plugin {
	t.Helper()
	p := newPlugin()
	p.disablePoller = true // build the snapshot synchronously in the webhook path
	p.lookPath = noLookPath
	p.run = run
	p.dl = &downloader{
		cacheDir: t.TempDir(),
		platform: "linux-amd64",
		fetch: func(context.Context, string) (io.ReadCloser, error) {
			return nil, errors.New("no network in tests")
		},
	}
	// Default: Augment API is never reached in tests unless a test opts in.
	p.httpPost = func(context.Context, string, string, []byte) (int, []byte, error) {
		return 0, nil, errors.New("no augment network in tests")
	}
	p.SetHost(&fakeHost{config: config, sessions: sessions})
	return p
}

func webhookGet(key, query string) *pluginsdk.WebhookRequest {
	return &pluginsdk.WebhookRequest{WebhookKey: key, Method: "GET", Query: query}
}

// usageRunner answers `--version` probes and returns `out`/`err` for any usage
// run, counting usage invocations atomically (the providers path runs providers
// concurrently).
func usageRunner(usageCalls *int32, out []byte, err error) runner {
	return func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[len(args)-1] == "--version" {
			return []byte("CodexBar 0.45.2\n"), nil
		}
		if usageCalls != nil {
			atomic.AddInt32(usageCalls, 1)
		}
		return out, err
	}
}

// providerRunner returns per-provider output keyed by the `--provider` value; a
// provider absent from the map returns an exec error (unavailable).
func providerRunner(usageCalls *int32, byProvider map[string][]byte) runner {
	return func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[len(args)-1] == "--version" {
			return []byte("CodexBar 0.45.2\n"), nil
		}
		if usageCalls != nil {
			atomic.AddInt32(usageCalls, 1)
		}
		if out, ok := byProvider[argValue(args, "--provider")]; ok {
			return out, nil
		}
		return []byte("[]"), errors.New("exit status 1")
	}
}

func argValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func codexbarConfig(extra map[string]any) map[string]any {
	cfg := map[string]any{"command": "codexbar"}
	for k, v := range extra {
		cfg[k] = v
	}
	return cfg
}

func sampleAll() []byte {
	return []byte("[" + sampleClaudeInner() + "," + sampleCodexEntry + "," + sampleCursorError + "]")
}

// sampleClaudeInner returns the single claude entry (without array brackets).
func sampleClaudeInner() string {
	entries, _ := parseCodexbarUsage([]byte(sampleClaudeJSON))
	b, _ := json.Marshal(entries[0])
	return string(b)
}

func session(id, provider string) pluginsdk.Session {
	return pluginsdk.Session{ID: id, TaskID: "task-1", AgentDisplayName: provider}
}

// --- status webhook -----------------------------------------------------------

func TestHandleWebhook_UnknownKey(t *testing.T) {
	p := newTestPlugin(t, nil, nil, usageRunner(nil, nil, nil))
	resp, err := p.HandleWebhook(context.Background(), webhookGet("nope", ""))
	require.NoError(t, err)
	require.Equal(t, int32(404), resp.Status)
}

func TestHandleWebhook_Status(t *testing.T) {
	p := newTestPlugin(t, codexbarConfig(nil), nil, usageRunner(nil, nil, nil))
	resp, err := p.HandleWebhook(context.Background(), webhookGet(webhookKeyStatus, ""))
	require.NoError(t, err)
	require.Equal(t, int32(200), resp.Status)

	var status InstallStatus
	require.NoError(t, json.Unmarshal(resp.Body, &status))
	require.True(t, status.Installed)
	require.Equal(t, "0.45.2", status.Version)
	require.Equal(t, sourceSettings, status.Source)
}

func TestHandleWebhook_StatusDegradesWhenMissing(t *testing.T) {
	// No config command + not on PATH + no-network downloader -> unresolved.
	run := func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("should not run")
	}
	p := newTestPlugin(t, nil, nil, run)
	resp, err := p.HandleWebhook(context.Background(), webhookGet(webhookKeyStatus, ""))
	require.NoError(t, err)

	var status InstallStatus
	require.NoError(t, json.Unmarshal(resp.Body, &status))
	require.False(t, status.Installed)
	require.Equal(t, sourceDownload, status.Source)
	require.Contains(t, status.Error, "no network", "download failure surfaced as degraded status")
}

// --- providers webhook --------------------------------------------------------

// perProviderRunner maps the three sample providers by their --provider value;
// everything else (e.g. the rest of the default set) errors → unavailable.
func perProviderRunner(calls *int32) runner {
	return providerRunner(calls, map[string][]byte{
		"claude": []byte("[" + sampleClaudeInner() + "]"),
		"codex":  []byte("[" + sampleCodexEntry + "]"),
		"cursor": []byte("[" + sampleCursorError + "]"),
	})
}

func TestHandleWebhook_ProvidersPartitions(t *testing.T) {
	var calls int32
	cfg := codexbarConfig(map[string]any{"providers": "claude, codex, cursor"})
	p := newTestPlugin(t, cfg, nil, perProviderRunner(&calls))

	resp, err := p.HandleWebhook(context.Background(), webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)
	require.Equal(t, int32(200), resp.Status)

	var report AllProvidersReport
	require.NoError(t, json.Unmarshal(resp.Body, &report))
	require.True(t, report.Codexbar.Installed)
	require.Len(t, report.Providers, 2, "claude + codex have usage")
	require.Len(t, report.Unavailable, 1, "cursor reports an error entry")
	require.Equal(t, "cursor", report.Unavailable[0].Provider)
	require.Equal(t, defaultWarnThreshold, report.WarnThreshold)
	require.Equal(t, defaultHighThreshold, report.HighThreshold)
	// >= 3: each provider is queried once; providers with no usage on the fast
	// oauth source are retried once on the default source.
	require.GreaterOrEqual(t, calls, int32(3))
}

func TestHandleWebhook_ProvidersDefaultSet(t *testing.T) {
	var calls int32
	// No `providers` config -> the curated default set is queried; providers
	// absent from the runner map surface as unavailable.
	p := newTestPlugin(t, codexbarConfig(nil), nil, perProviderRunner(&calls))

	resp, err := p.HandleWebhook(context.Background(), webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)

	var report AllProvidersReport
	require.NoError(t, json.Unmarshal(resp.Body, &report))
	require.GreaterOrEqual(t, calls, int32(len(defaultProviders)), "at least one run per default provider")
	require.Len(t, report.Providers, 2, "only claude + codex have usage in the default set")
	// cursor (error entry) + the default providers with no mapping are unavailable.
	require.NotEmpty(t, report.Unavailable)
}

func TestHandleWebhook_ProvidersAllSweep(t *testing.T) {
	var calls int32
	cfg := codexbarConfig(map[string]any{"providers": "all"})
	p := newTestPlugin(t, cfg, nil, usageRunner(&calls, sampleAll(), nil))

	resp, err := p.HandleWebhook(context.Background(), webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)

	var report AllProvidersReport
	require.NoError(t, json.Unmarshal(resp.Body, &report))
	require.Equal(t, int32(1), calls, `"all" is a single codexbar sweep`)
	require.Len(t, report.Providers, 2)
	require.Len(t, report.Unavailable, 1)
}

func TestHandleWebhook_ProvidersDegradesWhenMissing(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("exec: codexbar: not found")
	}
	p := newTestPlugin(t, codexbarConfig(nil), nil, run)

	resp, err := p.HandleWebhook(context.Background(), webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)
	require.Equal(t, int32(200), resp.Status, "a missing CLI is a degraded report, not a 500")

	var report AllProvidersReport
	require.NoError(t, json.Unmarshal(resp.Body, &report))
	require.False(t, report.Codexbar.Installed, "probe fails -> degraded")
	require.Empty(t, report.Providers)
	require.Empty(t, report.Unavailable, "no providers are queried once the probe fails")
}

func TestHandleWebhook_ProvidersCached(t *testing.T) {
	var calls int32
	cfg := codexbarConfig(map[string]any{"providers": "claude"})
	p := newTestPlugin(t, cfg, nil, perProviderRunner(&calls))
	ctx := context.Background()

	_, err := p.HandleWebhook(ctx, webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)
	_, err = p.HandleWebhook(ctx, webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)
	require.Equal(t, int32(1), calls, "second hit inside TTL serves the cache")

	_, err = p.HandleWebhook(ctx, webhookGet(webhookKeyProviders, "refresh=1"))
	require.NoError(t, err)
	require.Equal(t, int32(2), calls, "refresh=1 bypasses the cache")
}

// --- session webhook ----------------------------------------------------------

func TestHandleWebhook_SessionResolvesProvider(t *testing.T) {
	// The chat icon serves from the same polled snapshot as the Settings page.
	sessions := []pluginsdk.Session{session("kandev-sess", "Claude Code")}
	p := newTestPlugin(t, codexbarConfig(nil), sessions, perProviderRunner(nil))

	resp, err := p.HandleWebhook(context.Background(),
		webhookGet(webhookKeySession, "task_id=task-1&active=kandev-sess"))
	require.NoError(t, err)
	require.Equal(t, int32(200), resp.Status)

	var report SessionUsageReport
	require.NoError(t, json.Unmarshal(resp.Body, &report))
	require.Equal(t, "claude", report.Provider)
	require.True(t, report.Codexbar.Installed)
	require.NotNil(t, report.Usage)
	require.Equal(t, "claude", report.Usage.Provider)
	require.NotEmpty(t, report.Usage.Windows)
}

func TestHandleWebhook_SessionUnknownProvider(t *testing.T) {
	sessions := []pluginsdk.Session{session("kandev-sess", "Mystery Agent")}
	p := newTestPlugin(t, codexbarConfig(nil), sessions, perProviderRunner(nil))

	resp, err := p.HandleWebhook(context.Background(),
		webhookGet(webhookKeySession, "task_id=task-1&active=kandev-sess"))
	require.NoError(t, err)

	var report SessionUsageReport
	require.NoError(t, json.Unmarshal(resp.Body, &report))
	require.Equal(t, "", report.Provider)
	require.Nil(t, report.Usage)
	require.True(t, report.Codexbar.Installed, "still reports codexbar status")
}

func TestHandleWebhook_SessionProviderError(t *testing.T) {
	// cursor is in the default polled set and reports an error entry in the
	// snapshot; the session picks that up as its error.
	sessions := []pluginsdk.Session{session("kandev-sess", "Cursor Agent")}
	p := newTestPlugin(t, codexbarConfig(nil), sessions, perProviderRunner(nil))

	resp, err := p.HandleWebhook(context.Background(),
		webhookGet(webhookKeySession, "task_id=task-1&active=kandev-sess"))
	require.NoError(t, err)

	var report SessionUsageReport
	require.NoError(t, json.Unmarshal(resp.Body, &report))
	require.Equal(t, "cursor", report.Provider)
	require.Nil(t, report.Usage)
	require.Contains(t, report.Error, "Cursor")
}

func TestHandleWebhook_SessionOnDemandForUnpolledProvider(t *testing.T) {
	// Operator narrowed the polled set to codex, but the session runs claude —
	// the snapshot doesn't cover it, so the session fetches claude on demand.
	cfg := codexbarConfig(map[string]any{"providers": "codex"})
	sessions := []pluginsdk.Session{session("kandev-sess", "Claude Code")}
	p := newTestPlugin(t, cfg, sessions, perProviderRunner(nil))

	resp, err := p.HandleWebhook(context.Background(),
		webhookGet(webhookKeySession, "task_id=task-1&active=kandev-sess"))
	require.NoError(t, err)

	var report SessionUsageReport
	require.NoError(t, json.Unmarshal(resp.Body, &report))
	require.Equal(t, "claude", report.Provider)
	require.NotNil(t, report.Usage, "claude fetched on demand though outside the polled set")
	require.Equal(t, "claude", report.Usage.Provider)
}

func TestHandleWebhook_SessionThresholdsFromConfig(t *testing.T) {
	cfg := codexbarConfig(map[string]any{"warn_threshold": 60.0, "high_threshold": 85.0})
	sessions := []pluginsdk.Session{session("kandev-sess", "Claude Code")}
	p := newTestPlugin(t, cfg, sessions, perProviderRunner(nil))

	resp, err := p.HandleWebhook(context.Background(),
		webhookGet(webhookKeySession, "task_id=task-1&active=kandev-sess"))
	require.NoError(t, err)

	var report SessionUsageReport
	require.NoError(t, json.Unmarshal(resp.Body, &report))
	require.Equal(t, 60.0, report.WarnThreshold)
	require.Equal(t, 85.0, report.HighThreshold)
}

func TestSessionAndProvidersShareSnapshot(t *testing.T) {
	// A session read serves the snapshot built by the first read; a second read
	// (session or providers) doesn't re-run codexbar until a refresh.
	var calls int32
	sessions := []pluginsdk.Session{session("kandev-sess", "Claude Code")}
	p := newTestPlugin(t, codexbarConfig(nil), sessions, perProviderRunner(&calls))
	ctx := context.Background()
	q := "task_id=task-1&active=kandev-sess"

	_, err := p.HandleWebhook(ctx, webhookGet(webhookKeySession, q))
	require.NoError(t, err)
	first := atomic.LoadInt32(&calls)
	require.Positive(t, first, "first read builds the snapshot")

	_, err = p.HandleWebhook(ctx, webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)
	require.Equal(t, first, atomic.LoadInt32(&calls), "providers read serves the same snapshot")

	_, err = p.HandleWebhook(ctx, webhookGet(webhookKeySession, q+"&refresh=1"))
	require.NoError(t, err)
	require.Greater(t, atomic.LoadInt32(&calls), first, "refresh=1 rebuilds the snapshot")
}

func TestHandleWebhook_ProvidersIncludesAugment(t *testing.T) {
	cfg := codexbarConfig(map[string]any{
		"providers":         "codex", // keep the codexbar set tiny
		"augment_api_token": "tok",
		"augment_email":     "a@b.com",
	})
	p := newTestPlugin(t, cfg, nil, perProviderRunner(nil))
	p.now = midMonth
	p.httpPost = fakePoster(nil, map[string]postResp{
		"/cost-analytics":            {200, augCostSample},
		"/get-user-budget-overrides": {200, `{"overrides":[]}`},
	})

	resp, err := p.HandleWebhook(context.Background(), webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)

	var report AllProvidersReport
	require.NoError(t, json.Unmarshal(resp.Body, &report))
	var aug *ProviderUsage
	for i := range report.Providers {
		if report.Providers[i].Provider == "augment" {
			aug = &report.Providers[i]
		}
	}
	require.NotNil(t, aug, "augment appears alongside codexbar providers")
	require.Equal(t, "959,232 credits this month", aug.Detail)
	require.Len(t, aug.Windows, 1, "credits plan gets the 2.5M default budget bar")
	require.InDelta(t, 959232.0/2_500_000*100, aug.Windows[0].UtilizationPct, 1e-9)
}

func TestAugmentDefaultBudget(t *testing.T) {
	require.Equal(t, defaultAugmentCreditsBudget, augmentDefaultBudget(augmentResourceCredits))
	require.Zero(t, augmentDefaultBudget(augmentResourceUSD))
}

func TestHandleWebhook_AugmentErrorIsUnavailable(t *testing.T) {
	cfg := codexbarConfig(map[string]any{
		"providers":         "codex",
		"augment_api_token": "tok",
		"augment_email":     "a@b.com",
	})
	p := newTestPlugin(t, cfg, nil, perProviderRunner(nil))
	p.now = midMonth
	p.httpPost = fakePoster(nil, map[string]postResp{
		"/cost-analytics": {400, `{"error":{"message":"user_email(s) not found"}}`},
	})

	resp, err := p.HandleWebhook(context.Background(), webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)

	var report AllProvidersReport
	require.NoError(t, json.Unmarshal(resp.Body, &report))
	var found bool
	for _, e := range report.Unavailable {
		if e.Provider == "augment" {
			found = true
			require.Contains(t, e.Message, "not found")
		}
	}
	require.True(t, found, "an augment fetch error lists it as unavailable")
}

func TestHandleWebhook_NoAugmentWithoutConfig(t *testing.T) {
	// Without a token/email, the Augment API is never called (poster would error).
	p := newTestPlugin(t, codexbarConfig(map[string]any{"providers": "codex"}), nil, perProviderRunner(nil))
	resp, err := p.HandleWebhook(context.Background(), webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)

	var report AllProvidersReport
	require.NoError(t, json.Unmarshal(resp.Body, &report))
	for _, pr := range report.Providers {
		require.NotEqual(t, "augment", pr.Provider)
	}
	for _, e := range report.Unavailable {
		require.NotEqual(t, "augment", e.Provider)
	}
}

func TestPollOnceDedupsWithinMaxAge(t *testing.T) {
	var calls int32
	cfg := codexbarConfig(map[string]any{"providers": "claude"})
	p := newTestPlugin(t, cfg, nil, perProviderRunner(&calls))
	ctx := context.Background()

	p.pollOnce(ctx, cacheTTL)
	p.pollOnce(ctx, cacheTTL)
	require.Equal(t, int32(1), atomic.LoadInt32(&calls), "second poll within maxAge reuses the snapshot")

	p.pollOnce(ctx, 0)
	require.Equal(t, int32(2), atomic.LoadInt32(&calls), "maxAge 0 forces a rebuild")
}
