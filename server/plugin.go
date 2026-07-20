package main

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/kandev/kandev/pkg/pluginsdk"
)

const (
	webhookKeyStatus    = "status"
	webhookKeyProviders = "providers"
	webhookKeySession   = "session"

	// probeTimeout bounds a `--version` check; reportTimeout bounds a usage run.
	// `--provider all` fans out across ~60 providers, hence the generous bound.
	probeTimeout  = 60 * time.Second
	reportTimeout = 180 * time.Second

	// cacheTTL bounds how often a webhook hit re-runs codexbar. Matches the
	// native feature's 5-minute utilization cache. The cache is process-local,
	// and kandev restarts the plugin whenever the operator saves settings.
	cacheTTL = 5 * time.Minute

	configKeyCommand       = "command"
	configKeyProviders     = "providers"
	configKeyWarnThreshold = "warn_threshold"
	configKeyHighThreshold = "high_threshold"

	defaultWarnThreshold = 75.0 // % — a window turns amber at or above this
	defaultHighThreshold = 90.0 // % — a window turns red at or above this
)

type cacheEntry struct {
	body []byte
	at   time.Time
}

// plugin implements pluginsdk.Plugin (via UnimplementedPlugin). Its three
// webhooks are relayed by kandev from
// GET /api/plugins/kandev-provider-usage/webhooks/{status,providers,session}
// over gRPC; the plugin's UI bundle is the only intended caller.
type plugin struct {
	pluginsdk.UnimplementedPlugin

	// Seams injected for tests; production values set in newPlugin.
	run      runner
	lookPath func(string) (string, error)
	now      func() time.Time
	dl       *downloader

	mu             sync.Mutex
	statusCache    cacheEntry
	providersCache cacheEntry
	sessionCache   map[string]cacheEntry // keyed by resolved provider
}

var _ pluginsdk.Plugin = (*plugin)(nil)

func newPlugin() *plugin {
	return &plugin{
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		},
		lookPath:     exec.LookPath,
		now:          time.Now,
		dl:           newDownloader(),
		sessionCache: make(map[string]cacheEntry),
	}
}

func (p *plugin) HandleWebhook(ctx context.Context, req *pluginsdk.WebhookRequest) (*pluginsdk.WebhookResponse, error) {
	query, err := url.ParseQuery(req.Query)
	if err != nil {
		query = url.Values{}
	}
	refresh := query.Get("refresh") == "1"

	switch req.WebhookKey {
	case webhookKeyStatus:
		return jsonResponse(200, p.statusJSON(ctx, refresh)), nil
	case webhookKeyProviders:
		return jsonResponse(200, p.providersJSON(ctx, refresh)), nil
	case webhookKeySession:
		return jsonResponse(200, p.sessionJSON(ctx, query.Get("task_id"), query.Get("active"), refresh)), nil
	default:
		return jsonResponse(404, []byte(`{"error":"unknown webhook"}`)), nil
	}
}

func jsonResponse(status int32, body []byte) *pluginsdk.WebhookResponse {
	return &pluginsdk.WebhookResponse{
		Status:  status,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    body,
	}
}

// --- command resolution -------------------------------------------------------

// resolveCommand picks the codexbar invocation: the operator-configured command
// wins, then a `codexbar` binary on PATH, then the pinned per-platform binary
// (downloaded + cached on first use). A resolution failure is carried on the
// returned command's Err so callers can degrade to setup guidance.
func (p *plugin) resolveCommand(ctx context.Context) resolvedCommand {
	if argv := strings.Fields(p.configuredCommand(ctx)); len(argv) > 0 {
		return resolvedCommand{Argv: argv, Source: sourceSettings}
	}
	if path, err := p.lookPath("codexbar"); err == nil {
		return resolvedCommand{Argv: []string{path}, Source: sourcePath}
	}
	bin, err := p.dl.ensure(ctx)
	if err != nil {
		return resolvedCommand{Source: sourceDownload, Err: err}
	}
	return resolvedCommand{Argv: []string{bin}, Source: sourceDownload}
}

// --- status webhook -----------------------------------------------------------

// statusJSON probes the resolved codexbar command, caching the result for
// cacheTTL. Probe failures are the payload here, not errors — status is 200.
func (p *plugin) statusJSON(ctx context.Context, refresh bool) []byte {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !refresh && p.statusCache.body != nil && p.now().Sub(p.statusCache.at) < cacheTTL {
		return p.statusCache.body
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	status := probeInstall(probeCtx, p.resolveCommand(probeCtx), p.run)
	body, err := json.Marshal(status)
	if err != nil {
		body = []byte(`{"installed":false,"error":"encoding status"}`)
	}
	p.statusCache = cacheEntry{body: body, at: p.now()}
	return body
}

// --- providers webhook (Settings page) ----------------------------------------

// ProviderError is one provider codexbar couldn't read (not signed in, no web
// support on this OS, ...), surfaced so the Settings page can list it as
// unavailable rather than silently dropping it.
type ProviderError struct {
	Provider string `json:"provider"`
	Message  string `json:"message"`
}

// AllProvidersReport is the providers-webhook payload rendered by the Settings
// page: utilization for every provider that has usage, plus the ones codexbar
// couldn't read, plus the codexbar install status for setup guidance.
type AllProvidersReport struct {
	GeneratedAt   string          `json:"generated_at"`
	Codexbar      InstallStatus   `json:"codexbar"`
	WarnThreshold float64         `json:"warn_threshold"`
	HighThreshold float64         `json:"high_threshold"`
	Providers     []ProviderUsage `json:"providers"`
	Unavailable   []ProviderError `json:"unavailable"`
}

func (p *plugin) providersJSON(ctx context.Context, refresh bool) []byte {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !refresh && p.providersCache.body != nil && p.now().Sub(p.providersCache.at) < cacheTTL {
		return p.providersCache.body
	}

	runCtx, cancel := context.WithTimeout(ctx, reportTimeout)
	defer cancel()
	report := p.collectProviders(runCtx)
	body := marshalOr(report, `{"error":"encoding providers report"}`)
	p.providersCache = cacheEntry{body: body, at: p.now()}
	return body
}

// collectProviders runs codexbar for the configured providers (or `all` when
// unconfigured) and partitions the result into usable utilization vs
// unavailable providers. A codexbar-level failure degrades to a status-only
// report so the page can render setup guidance.
func (p *plugin) collectProviders(ctx context.Context) *AllProvidersReport {
	warn, high := p.configuredThresholds(ctx)
	cmd := p.resolveCommand(ctx)
	report := &AllProvidersReport{
		GeneratedAt:   p.now().UTC().Format(time.RFC3339),
		WarnThreshold: warn,
		HighThreshold: high,
		Providers:     []ProviderUsage{},
		Unavailable:   []ProviderError{},
	}

	configured := p.configuredProviders(ctx)
	entries, err := p.usageEntries(ctx, cmd, configured, report)
	if err != nil {
		log.Printf("codexbar run failed (degrading to status-only report): %v", err)
		report.Codexbar = probeInstall(ctx, cmd, p.run)
		return report
	}
	report.Codexbar = InstallStatus{Command: commandDisplay(cmd), Source: cmd.Source, Installed: true}

	for _, e := range entries {
		if e.Error != nil {
			report.Unavailable = append(report.Unavailable, ProviderError{Provider: e.Provider, Message: e.Error.Message})
			continue
		}
		if u := e.toProviderUsage(p.now()); u != nil {
			report.Providers = append(report.Providers, *u)
		}
	}
	return report
}

// usageEntries fetches provider entries: one `--provider all` run when
// unconfigured, otherwise one run per configured provider (a provider whose run
// fails outright is recorded as unavailable and skipped). Returns an error only
// when codexbar could not run at all.
func (p *plugin) usageEntries(ctx context.Context, cmd resolvedCommand, configured []string, report *AllProvidersReport) ([]cbEntry, error) {
	if len(configured) == 0 {
		return runUsage(ctx, cmd, p.run, "all")
	}
	var entries []cbEntry
	var ranAny bool
	for _, prov := range configured {
		es, err := runUsage(ctx, cmd, p.run, prov)
		if err != nil {
			if !ranAny {
				return nil, err // first provider couldn't run codexbar at all
			}
			report.Unavailable = append(report.Unavailable, ProviderError{Provider: prov, Message: err.Error()})
			continue
		}
		ranAny = true
		entries = append(entries, es...)
	}
	return entries, nil
}

// --- session webhook (chat-bar icon) ------------------------------------------

// SessionUsageReport is the session-webhook payload rendered by the chat-bar
// icon: utilization for the provider that backs the active session.
type SessionUsageReport struct {
	GeneratedAt     string         `json:"generated_at"`
	Codexbar        InstallStatus  `json:"codexbar"`
	KandevSessionID string         `json:"kandev_session_id"`
	Provider        string         `json:"provider"` // resolved codexbar provider, "" if unknown
	WarnThreshold   float64        `json:"warn_threshold"`
	HighThreshold   float64        `json:"high_threshold"`
	Usage           *ProviderUsage `json:"usage"`
	Error           string         `json:"error,omitempty"`
}

func (p *plugin) sessionJSON(ctx context.Context, taskID, activeSessionID string, refresh bool) []byte {
	warn, high := p.configuredThresholds(ctx)
	report := SessionUsageReport{
		GeneratedAt:     p.now().UTC().Format(time.RFC3339),
		KandevSessionID: activeSessionID,
		WarnThreshold:   warn,
		HighThreshold:   high,
	}
	report.Provider = p.resolveProvider(ctx, taskID, activeSessionID)
	if report.Provider == "" {
		// Unknown provider — still report codexbar status so the popover can
		// distinguish "no agent yet" from "codexbar missing".
		report.Codexbar = p.probeStatus(ctx)
		return marshalOr(report, `{"error":"encoding session report"}`)
	}

	p.mu.Lock()
	if !refresh {
		if entry, ok := p.sessionCache[report.Provider]; ok && p.now().Sub(entry.at) < cacheTTL {
			p.mu.Unlock()
			return withSessionEnvelope(entry.body, report)
		}
	}
	p.mu.Unlock()

	runCtx, cancel := context.WithTimeout(ctx, reportTimeout)
	defer cancel()
	cmd := p.resolveCommand(runCtx)
	entries, err := runUsage(runCtx, cmd, p.run, report.Provider)
	if err != nil {
		log.Printf("codexbar session run failed (degrading): %v", err)
		report.Codexbar = probeInstall(runCtx, cmd, p.run)
		return marshalOr(report, `{"error":"encoding session report"}`)
	}
	report.Codexbar = InstallStatus{Command: commandDisplay(cmd), Source: cmd.Source, Installed: true}
	report.Usage, report.Error = pickProviderUsage(entries, report.Provider, p.now())

	body := marshalOr(report, `{"error":"encoding session report"}`)
	p.cacheSession(report.Provider, body)
	return body
}

// pickProviderUsage selects the entry for provider from a codexbar result,
// returning its usage or the provider's error message.
func pickProviderUsage(entries []cbEntry, provider string, now time.Time) (*ProviderUsage, string) {
	for _, e := range entries {
		if e.Provider != "" && e.Provider != provider {
			continue
		}
		if e.Error != nil {
			return nil, e.Error.Message
		}
		return e.toProviderUsage(now), ""
	}
	return nil, ""
}

// resolveProvider maps the active kandev session to a codexbar provider id via
// the Host data API (capability api_read: ["sessions"]). Best-effort: "" when
// the Host is unavailable or the session/agent doesn't match a known provider.
func (p *plugin) resolveProvider(ctx context.Context, taskID, activeSessionID string) string {
	host := p.Host()
	if host == nil || activeSessionID == "" {
		return ""
	}
	filter := pluginsdk.SessionFilter{}
	if taskID != "" {
		filter.TaskIDs = []string{taskID}
	}
	sessions, _, err := host.Sessions().List(ctx, filter, pluginsdk.Page{Limit: 200})
	if err != nil {
		log.Printf("resolving session provider: %v", err)
		return ""
	}
	for _, s := range sessions {
		if s.ID == activeSessionID {
			return providerForSession(s)
		}
	}
	return ""
}

// probeStatus returns the cached-or-fresh codexbar install status. Used on the
// session path when there's no provider to query but the UI still needs to know
// whether codexbar is available.
func (p *plugin) probeStatus(ctx context.Context) InstallStatus {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	return probeInstall(probeCtx, p.resolveCommand(probeCtx), p.run)
}

func (p *plugin) cacheSession(provider string, body []byte) {
	p.mu.Lock()
	p.sessionCache[provider] = cacheEntry{body: body, at: p.now()}
	p.mu.Unlock()
}

// --- config -------------------------------------------------------------------

func (p *plugin) config(ctx context.Context) map[string]any {
	host := p.Host()
	if host == nil {
		return map[string]any{}
	}
	cfg, err := host.GetConfig(ctx)
	if err != nil {
		log.Printf("reading plugin config: %v", err)
		return map[string]any{}
	}
	return cfg
}

func (p *plugin) configuredCommand(ctx context.Context) string {
	command, _ := p.config(ctx)[configKeyCommand].(string)
	return strings.TrimSpace(command)
}

// configuredProviders parses the comma-separated provider allowlist. Empty means
// "query all providers".
func (p *plugin) configuredProviders(ctx context.Context) []string {
	raw, _ := p.config(ctx)[configKeyProviders].(string)
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if s := strings.ToLower(strings.TrimSpace(part)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// configuredThresholds reads the amber/red utilization cutoffs from operator
// config, falling back to sane defaults. A configured value only wins when
// positive, and high is clamped to at least warn.
func (p *plugin) configuredThresholds(ctx context.Context) (warn, high float64) {
	cfg := p.config(ctx)
	warn = positiveFloatOr(cfg[configKeyWarnThreshold], defaultWarnThreshold)
	high = positiveFloatOr(cfg[configKeyHighThreshold], defaultHighThreshold)
	if high < warn {
		high = warn
	}
	return warn, high
}

// positiveFloatOr coerces a JSON config value (numbers arrive as float64) to a
// positive float, or returns the fallback.
func positiveFloatOr(v any, fallback float64) float64 {
	if f, ok := v.(float64); ok && f > 0 {
		return f
	}
	return fallback
}

// --- helpers ------------------------------------------------------------------

func marshalOr(v any, fallback string) []byte {
	body, err := json.Marshal(v)
	if err != nil {
		return []byte(fallback)
	}
	return body
}

// withSessionEnvelope re-stamps a cached session body with the current request's
// envelope fields (generated_at, kandev_session_id, thresholds) while keeping
// the cached codexbar usage payload. Falls back to the cached body on any error.
func withSessionEnvelope(cached []byte, fresh SessionUsageReport) []byte {
	var prev SessionUsageReport
	if err := json.Unmarshal(cached, &prev); err != nil {
		return cached
	}
	prev.GeneratedAt = fresh.GeneratedAt
	prev.KandevSessionID = fresh.KandevSessionID
	prev.WarnThreshold = fresh.WarnThreshold
	prev.HighThreshold = fresh.HighThreshold
	return marshalOr(prev, string(cached))
}
