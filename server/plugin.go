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

	// probeTimeout bounds a `--version` check; perProviderTimeout bounds a single
	// provider's usage run; reportTimeout bounds the whole providers report (all
	// providers run concurrently, so it's the slowest single provider, not the
	// sum). Some agent CLIs (notably `claude`) take several seconds to answer.
	probeTimeout       = 20 * time.Second
	perProviderTimeout = 20 * time.Second
	reportTimeout      = 45 * time.Second

	// cacheTTL bounds how often a webhook hit re-runs codexbar. Matches the
	// native feature's 5-minute utilization cache. The cache is process-local,
	// and kandev restarts the plugin whenever the operator saves settings.
	cacheTTL = 5 * time.Minute

	configKeyCommand       = "command"
	configKeyProviders     = "providers"
	configKeyWarnThreshold = "warn_threshold"
	configKeyHighThreshold = "high_threshold"
	configKeyPollMinutes   = "poll_interval_minutes"

	defaultWarnThreshold = 75.0 // % — a window turns amber at or above this
	defaultHighThreshold = 90.0 // % — a window turns red at or above this

	defaultPollMinutes = 5.0 // background snapshot refresh interval
	minPollMinutes     = 1.0 // floor, so a misconfig can't hammer codexbar

	// providersAll is the config sentinel that opts into codexbar's full
	// ~60-provider sweep (slow; most are web-only and error on this host).
	providersAll = "all"
)

// defaultProviders is the curated set the Settings page queries when the
// operator hasn't set `providers`. These are the agent providers that read
// LOCAL credentials/CLIs, so they resolve quickly and without web cookies —
// unlike codexbar's ~50 web-only providers, which the `all` sweep wastes time
// probing. Each is still queried; unconfigured ones surface as unavailable.
var defaultProviders = []string{
	"claude", "codex", "gemini", "grok", "copilot", "cursor", "opencode", "amp",
}

// plugin implements pluginsdk.Plugin (via UnimplementedPlugin). Its three
// webhooks are relayed by kandev from
// GET /api/plugins/kandev-provider-usage/webhooks/{status,providers,session}
// over gRPC; the plugin's UI bundle is the only intended caller.
//
// A background poller (started once the Host is injected) refreshes a single
// snapshot of every provider's utilization every poll_interval minutes. All
// three webhooks serve that snapshot instantly — codexbar only runs on the
// timer or on an explicit ?refresh=1. This keeps the Settings page and chat-bar
// icon snappy even though the codexbar CLI can take several seconds per run.
type plugin struct {
	pluginsdk.UnimplementedPlugin

	// Seams injected for tests; production values set in newPlugin.
	run      runner
	lookPath func(string) (string, error)
	now      func() time.Time
	dl       *downloader

	// disablePoller keeps the background goroutine from starting in tests, so
	// the snapshot is built synchronously by the webhook path instead.
	disablePoller bool
	pollerOnce    sync.Once

	// pollMu serializes snapshot rebuilds so the ticker and a manual refresh
	// never run codexbar concurrently.
	pollMu sync.Mutex

	// mu guards the snapshot pointer and its timestamp.
	mu         sync.Mutex
	snapshot   *AllProvidersReport
	snapshotAt time.Time
}

var _ pluginsdk.Plugin = (*plugin)(nil)

func newPlugin() *plugin {
	return &plugin{
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		},
		lookPath: exec.LookPath,
		now:      time.Now,
		dl:       newDownloader(),
	}
}

// SetHost stores the Host and starts the background poller on first injection.
// SetHost is called once, from a Serve goroutine after the host broker dials.
func (p *plugin) SetHost(h pluginsdk.Host) {
	p.UnimplementedPlugin.SetHost(h)
	if p.disablePoller {
		return
	}
	p.pollerOnce.Do(func() { go p.pollLoop() })
}

// pollLoop refreshes the snapshot immediately, then every poll_interval minutes,
// for the life of the plugin subprocess (reaped on process exit).
func (p *plugin) pollLoop() {
	ctx := context.Background()
	p.pollOnce(ctx, 0)
	for {
		timer := time.NewTimer(p.pollInterval(ctx))
		<-timer.C
		p.pollOnce(ctx, 0)
	}
}

// pollOnce rebuilds the snapshot, serialized by pollMu. When maxAge > 0 it skips
// the rebuild if another poll already produced a snapshot younger than maxAge
// (collapsing concurrent first-load requests behind one codexbar run).
func (p *plugin) pollOnce(ctx context.Context, maxAge time.Duration) *AllProvidersReport {
	p.pollMu.Lock()
	defer p.pollMu.Unlock()
	if maxAge > 0 {
		if snap, at := p.currentSnapshot(); snap != nil && p.now().Sub(at) < maxAge {
			return snap
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, reportTimeout)
	defer cancel()
	report := p.collectProviders(runCtx)
	p.mu.Lock()
	p.snapshot, p.snapshotAt = report, p.now()
	p.mu.Unlock()
	return report
}

func (p *plugin) currentSnapshot() (*AllProvidersReport, time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snapshot, p.snapshotAt
}

// snapshotForRead returns the snapshot a webhook should serve: a forced rebuild
// on refresh, the current snapshot when one exists, otherwise a synchronous
// first build (bounded to a recent one so concurrent first-loads share it).
func (p *plugin) snapshotForRead(ctx context.Context, refresh bool) *AllProvidersReport {
	if refresh {
		return p.pollOnce(ctx, 0)
	}
	if snap, _ := p.currentSnapshot(); snap != nil {
		return snap
	}
	return p.pollOnce(ctx, cacheTTL)
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

// statusJSON serves the codexbar install status from the current snapshot (the
// poll probes codexbar each cycle). Status is always 200; a failed probe is the
// payload, not an error.
func (p *plugin) statusJSON(ctx context.Context, refresh bool) []byte {
	snap := p.snapshotForRead(ctx, refresh)
	return marshalOr(snap.Codexbar, `{"installed":false,"error":"encoding status"}`)
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
	GeneratedAt   string        `json:"generated_at"`
	Codexbar      InstallStatus `json:"codexbar"`
	WarnThreshold float64       `json:"warn_threshold"`
	HighThreshold float64       `json:"high_threshold"`
	// PollMinutes is the background refresh interval, echoed so the UI can show
	// "auto-refreshes every N min".
	PollMinutes float64         `json:"poll_minutes"`
	Providers   []ProviderUsage `json:"providers"`
	Unavailable []ProviderError `json:"unavailable"`
}

func (p *plugin) providersJSON(ctx context.Context, refresh bool) []byte {
	return marshalOr(p.snapshotForRead(ctx, refresh), `{"error":"encoding providers report"}`)
}

// collectProviders probes codexbar, then queries each provider and partitions
// the result into usable utilization vs unavailable providers. When codexbar
// itself can't run, it degrades to a status-only report so the page can render
// setup guidance.
func (p *plugin) collectProviders(ctx context.Context) *AllProvidersReport {
	warn, high := p.configuredThresholds(ctx)
	cmd := p.resolveCommand(ctx)
	report := &AllProvidersReport{
		GeneratedAt:   p.now().UTC().Format(time.RFC3339),
		WarnThreshold: warn,
		HighThreshold: high,
		PollMinutes:   p.pollInterval(ctx).Minutes(),
		Providers:     []ProviderUsage{},
		Unavailable:   []ProviderError{},
	}

	// Probe once up front (a fast `--version`): this cleanly separates "codexbar
	// is broken" (degraded report) from "a provider is unavailable" (listed).
	probeCtx, cancelProbe := context.WithTimeout(ctx, probeTimeout)
	status := probeInstall(probeCtx, cmd, p.run)
	cancelProbe()
	report.Codexbar = status
	if !status.Installed {
		log.Printf("codexbar unavailable (status-only report): %s", status.Error)
		return report
	}

	entries := p.queryProviders(ctx, cmd, p.providerList(ctx), report)
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

// providerList resolves which providers the Settings page queries: the operator
// allowlist, else the curated default set. The special value "all" opts into
// codexbar's full (slow) sweep, signalled by a nil return.
func (p *plugin) providerList(ctx context.Context) []string {
	configured := p.configuredProviders(ctx)
	if len(configured) == 1 && configured[0] == providersAll {
		return nil
	}
	if len(configured) == 0 {
		return defaultProviders
	}
	return configured
}

// queryProviders fetches usage for each provider CONCURRENTLY (each in its own
// bounded context), so the report's wall-clock is the slowest single provider
// rather than their sum. A nil list means the codexbar `all` sweep (one call).
// Providers whose run fails outright are recorded as unavailable.
func (p *plugin) queryProviders(ctx context.Context, cmd resolvedCommand, providers []string, report *AllProvidersReport) []cbEntry {
	if providers == nil {
		entries, err := runUsage(ctx, cmd, p.run, providersAll)
		if err != nil {
			report.Unavailable = append(report.Unavailable, ProviderError{Provider: providersAll, Message: err.Error()})
		}
		return entries
	}

	type result struct {
		entries []cbEntry
		perr    *ProviderError
	}
	results := make([]result, len(providers))
	var wg sync.WaitGroup
	for i, prov := range providers {
		wg.Add(1)
		go func(i int, prov string) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, perProviderTimeout)
			defer cancel()
			es, err := runUsageFast(cctx, cmd, p.run, prov)
			if err != nil {
				results[i] = result{perr: &ProviderError{Provider: prov, Message: err.Error()}}
				return
			}
			results[i] = result{entries: es}
		}(i, prov)
	}
	wg.Wait()

	var entries []cbEntry
	for _, r := range results {
		if r.perr != nil {
			report.Unavailable = append(report.Unavailable, *r.perr)
			continue
		}
		entries = append(entries, r.entries...)
	}
	return entries
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

const sessionEncodeErr = `{"error":"encoding session report"}`

func (p *plugin) sessionJSON(ctx context.Context, taskID, activeSessionID string, refresh bool) []byte {
	warn, high := p.configuredThresholds(ctx)
	report := SessionUsageReport{
		GeneratedAt:     p.now().UTC().Format(time.RFC3339),
		KandevSessionID: activeSessionID,
		WarnThreshold:   warn,
		HighThreshold:   high,
	}
	report.Provider = p.resolveProvider(ctx, taskID, activeSessionID)

	// The chat-bar icon reads from the same polled snapshot as the Settings
	// page — no per-hover codexbar run.
	snap := p.snapshotForRead(ctx, refresh)
	report.Codexbar = snap.Codexbar
	if report.Provider == "" {
		// Unknown provider — the popover distinguishes "no known agent" from
		// "codexbar missing" via the codexbar status.
		return marshalOr(report, sessionEncodeErr)
	}
	if usage, errMsg, found := sessionFromSnapshot(snap, report.Provider); found {
		report.Usage, report.Error = usage, errMsg
		return marshalOr(report, sessionEncodeErr)
	}

	// Provider outside the polled set (e.g. a narrowed `providers` allowlist):
	// fetch it on demand.
	if !snap.Codexbar.Installed {
		return marshalOr(report, sessionEncodeErr)
	}
	runCtx, cancel := context.WithTimeout(ctx, perProviderTimeout)
	defer cancel()
	entries, err := runUsageFast(runCtx, p.resolveCommand(runCtx), p.run, report.Provider)
	if err != nil {
		log.Printf("codexbar session run failed (degrading): %v", err)
		return marshalOr(report, sessionEncodeErr)
	}
	report.Usage, report.Error = pickProviderUsage(entries, report.Provider, p.now())
	return marshalOr(report, sessionEncodeErr)
}

// sessionFromSnapshot returns the snapshot's usage or error for a provider, and
// whether the snapshot covered it at all.
func sessionFromSnapshot(snap *AllProvidersReport, provider string) (*ProviderUsage, string, bool) {
	for i := range snap.Providers {
		if snap.Providers[i].Provider == provider {
			u := snap.Providers[i]
			return &u, "", true
		}
	}
	for _, e := range snap.Unavailable {
		if e.Provider == provider {
			return nil, e.Message, true
		}
	}
	return nil, "", false
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

// pollInterval reads the background refresh interval from config (minutes),
// clamped to a sane floor.
func (p *plugin) pollInterval(ctx context.Context) time.Duration {
	m := positiveFloatOr(p.config(ctx)[configKeyPollMinutes], defaultPollMinutes)
	if m < minPollMinutes {
		m = minPollMinutes
	}
	return time.Duration(m * float64(time.Minute))
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
