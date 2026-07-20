// Package main tests. Exercises plugin.HandleWebhook end to end against a fake
// Host and an injected runner — no go-plugin spawn and no real codexbar needed,
// mirroring the other kandev plugins' test approach.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

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
	p.lookPath = noLookPath
	p.run = run
	p.dl = &downloader{
		cacheDir: t.TempDir(),
		platform: "linux-amd64",
		fetch: func(context.Context, string) (io.ReadCloser, error) {
			return nil, errors.New("no network in tests")
		},
	}
	p.SetHost(&fakeHost{config: config, sessions: sessions})
	return p
}

func webhookGet(key, query string) *pluginsdk.WebhookRequest {
	return &pluginsdk.WebhookRequest{WebhookKey: key, Method: "GET", Query: query}
}

// usageRunner answers `--version` probes and `usage` runs distinctly, counting
// usage invocations.
func usageRunner(usageCalls *int, out []byte, err error) runner {
	return func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[len(args)-1] == "--version" {
			return []byte("CodexBar 0.45.2\n"), nil
		}
		if usageCalls != nil {
			*usageCalls++
		}
		return out, err
	}
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

func TestHandleWebhook_ProvidersPartitions(t *testing.T) {
	calls := 0
	p := newTestPlugin(t, codexbarConfig(nil), nil, usageRunner(&calls, sampleAll(), nil))

	resp, err := p.HandleWebhook(context.Background(), webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)
	require.Equal(t, int32(200), resp.Status)

	var report AllProvidersReport
	require.NoError(t, json.Unmarshal(resp.Body, &report))
	require.True(t, report.Codexbar.Installed)
	require.Len(t, report.Providers, 2, "claude + codex have usage")
	require.Len(t, report.Unavailable, 1, "cursor is unavailable")
	require.Equal(t, "cursor", report.Unavailable[0].Provider)
	require.Equal(t, defaultWarnThreshold, report.WarnThreshold)
	require.Equal(t, defaultHighThreshold, report.HighThreshold)
	require.Equal(t, 1, calls, "one `--provider all` run")
}

func TestHandleWebhook_ProvidersConfiguredList(t *testing.T) {
	calls := 0
	cfg := codexbarConfig(map[string]any{"providers": "claude, codex"})
	// Each per-provider run returns just its own entry; the runner echoes the
	// same sample regardless, so both configured providers resolve to claude —
	// what matters is one run per configured provider.
	p := newTestPlugin(t, cfg, nil, usageRunner(&calls, []byte("["+sampleClaudeInner()+"]"), nil))

	resp, err := p.HandleWebhook(context.Background(), webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)

	var report AllProvidersReport
	require.NoError(t, json.Unmarshal(resp.Body, &report))
	require.Equal(t, 2, calls, "one run per configured provider")
	require.NotEmpty(t, report.Providers)
}

func TestHandleWebhook_ProvidersDegradesWhenMissing(t *testing.T) {
	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		return nil, errors.New("exec: codexbar: not found")
	}
	p := newTestPlugin(t, codexbarConfig(nil), nil, run)

	resp, err := p.HandleWebhook(context.Background(), webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)
	require.Equal(t, int32(200), resp.Status, "a missing CLI is a degraded report, not a 500")

	var report AllProvidersReport
	require.NoError(t, json.Unmarshal(resp.Body, &report))
	require.False(t, report.Codexbar.Installed)
	require.Empty(t, report.Providers)
}

func TestHandleWebhook_ProvidersCached(t *testing.T) {
	calls := 0
	p := newTestPlugin(t, codexbarConfig(nil), nil, usageRunner(&calls, sampleAll(), nil))
	ctx := context.Background()

	_, err := p.HandleWebhook(ctx, webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)
	_, err = p.HandleWebhook(ctx, webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)
	require.Equal(t, 1, calls, "second hit inside TTL serves the cache")

	_, err = p.HandleWebhook(ctx, webhookGet(webhookKeyProviders, "refresh=1"))
	require.NoError(t, err)
	require.Equal(t, 2, calls, "refresh=1 bypasses the cache")
}

// --- session webhook ----------------------------------------------------------

func TestHandleWebhook_SessionResolvesProvider(t *testing.T) {
	sessions := []pluginsdk.Session{session("kandev-sess", "Claude Code")}
	p := newTestPlugin(t, codexbarConfig(nil), sessions,
		usageRunner(nil, []byte("["+sampleClaudeInner()+"]"), nil))

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
	p := newTestPlugin(t, codexbarConfig(nil), sessions, usageRunner(nil, sampleAll(), nil))

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
	sessions := []pluginsdk.Session{session("kandev-sess", "Cursor Agent")}
	// codexbar returns the cursor error entry with a non-zero exit.
	p := newTestPlugin(t, codexbarConfig(nil), sessions,
		usageRunner(nil, []byte("["+sampleCursorError+"]"), errors.New("exit status 1")))

	resp, err := p.HandleWebhook(context.Background(),
		webhookGet(webhookKeySession, "task_id=task-1&active=kandev-sess"))
	require.NoError(t, err)

	var report SessionUsageReport
	require.NoError(t, json.Unmarshal(resp.Body, &report))
	require.Equal(t, "cursor", report.Provider)
	require.Nil(t, report.Usage)
	require.Contains(t, report.Error, "Cursor")
}

func TestHandleWebhook_SessionThresholdsFromConfig(t *testing.T) {
	cfg := codexbarConfig(map[string]any{"warn_threshold": 60.0, "high_threshold": 85.0})
	sessions := []pluginsdk.Session{session("kandev-sess", "Claude Code")}
	p := newTestPlugin(t, cfg, sessions, usageRunner(nil, []byte("["+sampleClaudeInner()+"]"), nil))

	resp, err := p.HandleWebhook(context.Background(),
		webhookGet(webhookKeySession, "task_id=task-1&active=kandev-sess"))
	require.NoError(t, err)

	var report SessionUsageReport
	require.NoError(t, json.Unmarshal(resp.Body, &report))
	require.Equal(t, 60.0, report.WarnThreshold)
	require.Equal(t, 85.0, report.HighThreshold)
}

func TestHandleWebhook_SessionCachedPerProvider(t *testing.T) {
	calls := 0
	sessions := []pluginsdk.Session{session("kandev-sess", "Claude Code")}
	p := newTestPlugin(t, codexbarConfig(nil), sessions,
		usageRunner(&calls, []byte("["+sampleClaudeInner()+"]"), nil))
	ctx := context.Background()
	q := "task_id=task-1&active=kandev-sess"

	_, err := p.HandleWebhook(ctx, webhookGet(webhookKeySession, q))
	require.NoError(t, err)
	_, err = p.HandleWebhook(ctx, webhookGet(webhookKeySession, q))
	require.NoError(t, err)
	require.Equal(t, 1, calls, "second hit inside TTL serves the cached provider usage")

	_, err = p.HandleWebhook(ctx, webhookGet(webhookKeySession, q+"&refresh=1"))
	require.NoError(t, err)
	require.Equal(t, 2, calls, "refresh=1 bypasses the cache")
}

func TestProvidersCacheExpires(t *testing.T) {
	calls := 0
	p := newTestPlugin(t, codexbarConfig(nil), nil, usageRunner(&calls, sampleAll(), nil))
	current := time.Unix(1000, 0)
	p.now = func() time.Time { return current }
	ctx := context.Background()

	_, err := p.HandleWebhook(ctx, webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)
	_, err = p.HandleWebhook(ctx, webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)
	require.Equal(t, 1, calls)

	current = current.Add(cacheTTL + time.Second)
	_, err = p.HandleWebhook(ctx, webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)
	require.Equal(t, 2, calls, "expired cache re-runs codexbar")
}
