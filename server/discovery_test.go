package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryDoesNotWaitForUsageOrRunProviderCommands(t *testing.T) {
	p := newTestPlugin(t, map[string]any{configKeyAugmentToken: "private-token"}, nil,
		func(context.Context, string, ...string) ([]byte, error) {
			t.Error("discovery must not execute a CLI")
			return nil, errors.New("unexpected command")
		})
	p.scanProviders = testScanner(map[string]bool{"codex": true}, nil, nil).scan
	// A slow usage poll already owns this lock. Discovery must remain independent.
	p.pollMu.Lock()
	defer p.pollMu.Unlock()
	response := make(chan *pluginsdk.WebhookResponse, 1)
	go func() {
		r, _ := p.HandleWebhook(context.Background(), webhookGet("discovery", ""))
		response <- r
	}()
	select {
	case r := <-response:
		require.EqualValues(t, http.StatusOK, r.Status)
		var body struct {
			DetectedProviders []detectedProvider `json:"detected_providers"`
		}
		require.NoError(t, json.Unmarshal(r.Body, &body))
		require.Equal(t, []detectedProvider{{"augment", "configuration"}, {"codex", "cli"}}, body.DetectedProviders)
		require.NotContains(t, string(r.Body), "private-token")
	case <-time.After(time.Second):
		t.Fatal("local discovery waited for the usage poll")
	}
	r, err := p.HandleWebhook(context.Background(), &pluginsdk.WebhookRequest{WebhookKey: "discovery", Method: "POST"})
	require.NoError(t, err)
	require.EqualValues(t, http.StatusMethodNotAllowed, r.Status)
}

func testScanner(commands, files map[string]bool, env map[string]string) providerScanner {
	return providerScanner{
		home: "/fixture/home", getenv: func(key string) string { return env[key] },
		lookPath: func(command string) (string, error) {
			if commands[command] {
				return "/fixture/bin/" + command, nil
			}
			return "", errors.New("not installed")
		},
		exists: func(path string) bool { return files[path] },
	}
}

func TestProviderDiscoveryRequiresLocalEvidence(t *testing.T) {
	scanner := testScanner(map[string]bool{"claude": true, "copilot": true}, map[string]bool{
		"/Applications/Cursor.app":               true,
		"/fixture/home/.gemini/oauth_creds.json": true,
	}, nil)
	require.Equal(t, []detectedProvider{
		{"claude", "cli"}, {"copilot", "cli"}, {"cursor", "app"}, {"gemini", "configuration"},
	}, scanner.scan(nil))
	require.Empty(t, testScanner(nil, nil, nil).scan(nil), "absent providers must not have settings sections")
	require.Equal(t, []detectedProvider{{"cursor", "configuration"}}, testScanner(nil, map[string]bool{
		filepath.Join("/fixture/home", ".cursor", "cli-config.json"): true,
	}, nil).scan(nil), "an Agent CLI login marker keeps its Cursor controls available")
	require.Equal(t, []detectedProvider{{"augment", "configuration"}, {"cursor", "configuration"}}, testScanner(nil, nil, nil).scan(map[string]any{
		configKeyAugmentToken: "secret", cursorCookieSetting: "secret",
	}))
}

func TestProviderDiscoveryHonorsCustomCredentialLocations(t *testing.T) {
	scanner := testScanner(nil, map[string]bool{
		filepath.Join("/custom/codex", "auth.json"):                    true,
		filepath.Join("/custom/config", "github-copilot", "apps.json"): true,
		filepath.Join("/custom/config", "cursor", "auth.json"):         true,
	}, map[string]string{"CODEX_HOME": "/custom/codex", "XDG_CONFIG_HOME": "/custom/config", "APPDATA": "/custom/roaming"})
	require.Equal(t, []detectedProvider{{"codex", "configuration"}, {"copilot", "configuration"}, {"cursor", "configuration"}}, scanner.scan(nil))
}

func TestProviderDiscoveryKeepsInstalledFailuresAndOmitsSpeculativeFailures(t *testing.T) {
	p := newTestPlugin(t, nil, nil, nil)
	p.scanProviders = testScanner(map[string]bool{"cursor": true}, nil, nil).scan
	report := &AllProvidersReport{
		Providers:   []ProviderUsage{{Provider: "claude"}},
		Unavailable: []ProviderError{{Provider: "cursor", Message: "Session expired"}, {Provider: "copilot", Message: "Not installed"}},
	}
	require.Equal(t, []detectedProvider{{"claude", "usage"}, {"cursor", "cli"}}, p.discoverProviders(nil, report))
	// A paused connection remains manageable even if its credentials were
	// discovered only by the CLI, rather than one of our local path checks.
	require.Equal(t, []detectedProvider{{"claude", "usage"}, {"cursor", "cli"}, {"grok", "configuration"}}, p.discoverProviders(map[string]any{configKeyDisabledProviders: "grok"}, report))
}

func TestDisabledProvidersAreNotPolledOrReturnedBySessions(t *testing.T) {
	var calls int32
	cfg := codexbarConfig(map[string]any{configKeyProviders: "cursor,codex", configKeyDisabledProviders: "cursor"})
	p := newTestPlugin(t, cfg, nil, providerRunner(&calls, map[string][]byte{"codex": []byte(`[{"provider":"codex","usage":{"primary":{"usedPercent":12}}}]`)}))
	p.scanProviders = testScanner(map[string]bool{"cursor": true, "codex": true}, nil, nil).scan
	report := p.pollOnce(context.Background(), 0)
	require.EqualValues(t, 1, atomic.LoadInt32(&calls))
	require.Len(t, report.Providers, 1)
	require.Equal(t, "codex", report.Providers[0].Provider)
	require.Equal(t, []detectedProvider{{"codex", "cli"}, {"cursor", "cli"}}, report.DetectedProviders)
	host := p.Host().(*fakeHost)
	host.sessions = append(host.sessions, session("cursor-session", "Cursor Agent"))
	var sessionReport SessionUsageReport
	require.NoError(t, json.Unmarshal(p.sessionJSON(context.Background(), "task-1", "cursor-session", false), &sessionReport))
	require.Nil(t, sessionReport.Usage)
	require.True(t, sessionReport.Codexbar.Installed)
	require.Contains(t, sessionReport.Error, "disabled")
	require.EqualValues(t, 1, atomic.LoadInt32(&calls), "disabled session cannot fetch usage on demand")
	require.False(t, providerDisabled(cfg, ""), "unknown sessions still need the general install status")
	// No phantom empty token in the default config can disable unknown IDs.
	require.False(t, providerDisabled(nil, ""))
	require.True(t, providerDisabled(map[string]any{configKeyDisabledProviders: " Opencode "}, "opencodego"))
}

func TestDisabledProvidersAreFilteredFromFullCLISweep(t *testing.T) {
	p := newTestPlugin(t, codexbarConfig(map[string]any{configKeyProviders: "all", configKeyDisabledProviders: "cursor,codex"}), nil,
		usageRunner(nil, []byte("["+sampleClaudeInner()+","+sampleCodexEntry+","+sampleCursorError+"]"), nil))
	report := p.pollOnce(context.Background(), 0)
	require.Len(t, report.Providers, 1)
	require.Equal(t, "claude", report.Providers[0].Provider)
	require.Empty(t, report.Unavailable)
	require.Equal(t, []detectedProvider{{"claude", "usage"}, {"codex", "configuration"}, {"cursor", "configuration"}}, report.DetectedProviders)
}

func TestProviderDiscoveryWorksWhenCodexbarCannotRun(t *testing.T) {
	p := newTestPlugin(t, nil, nil, nil)
	p.scanProviders = testScanner(map[string]bool{"copilot": true}, nil, nil).scan
	report := p.pollOnce(context.Background(), 0)
	require.False(t, report.Codexbar.Installed)
	require.Equal(t, []detectedProvider{{"copilot", "cli"}}, report.DetectedProviders)
}
