package main

// The agent tool deliberately projects the poller's in-memory report. An MCP
// call must never start codexbar, read configuration, or use the Host API.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kandev/kandev/pkg/pluginsdk"
)

const providerUsageTool = "get_provider_usage"

var _ pluginsdk.AgentToolPlugin = (*plugin)(nil)

func (p *plugin) InvokeAgentTool(_ context.Context, req *pluginsdk.AgentToolRequest) (*pluginsdk.AgentToolResult, error) {
	if req == nil || req.Name != providerUsageTool {
		return nil, fmt.Errorf("unknown agent tool")
	}
	if len(req.Arguments) != 0 {
		return nil, fmt.Errorf("get_provider_usage accepts no arguments")
	}
	now := p.now().UTC()
	snap, at := p.currentSnapshot()
	content := providerUsageProjection(snap, at, now, req.Context.WorkspaceID)
	return &pluginsdk.AgentToolResult{Text: providerUsageText(content), StructuredContent: content}, nil
}

func providerUsageProjection(snap *AllProvidersReport, snapshotAt, now time.Time, workspaceID string) map[string]any {
	poll := int64(defaultPollMinutes * 60)
	if snap != nil && snap.PollMinutes >= minPollMinutes {
		poll = int64(snap.PollMinutes * 60)
	}
	staleAfter := poll * 2
	result := map[string]any{
		"schema_version": "1", "evaluated_at": now.Format(time.RFC3339),
		"snapshot_generated_at": nil, "poll_interval_seconds": poll,
		"stale_after_seconds": staleAfter, "partial": snap == nil || !snap.Codexbar.Installed,
		"scope":     map[string]any{"usage_scope": "instance", "user_scoped": false, "invocation_workspace_id": workspaceID},
		"providers": []any{},
	}
	if snap == nil {
		return result
	}
	generated := parseTimeOr(snap.GeneratedAt, snapshotAt).UTC()
	if !generated.IsZero() {
		result["snapshot_generated_at"] = generated.Format(time.RFC3339)
	}
	providers := make([]any, 0, len(snap.Providers)+len(snap.Unavailable))
	failed := map[string]bool{}
	emitted := map[string]bool{}
	for _, unavailable := range snap.Unavailable {
		failed[unavailable.Provider] = true
	}
	for _, usage := range snap.Providers {
		record := providerUsageRecord(usage, generated, now, staleAfter)
		if failed[usage.Provider] {
			warnings, _ := record["warnings"].([]any)
			record["warnings"] = append(warnings, "Latest provider refresh failed; cached telemetry is retained.")
			result["partial"] = true
		}
		providers = append(providers, record)
		emitted[usage.Provider] = true
	}
	for _, unavailable := range snap.Unavailable {
		if emitted[unavailable.Provider] {
			result["partial"] = true
			continue
		}
		state, reason := sanitizedFailureState(unavailable)
		providers = append(providers, map[string]any{"provider_id": unavailable.Provider, "provider_name": providerDisplayName(unavailable.Provider), "account": nil, "support_state": supportFor(state), "availability_state": state, "fetched_at": nil, "age_seconds": int64(0), "stale": false, "windows": []any{}, "reason": reason})
		result["partial"] = true
	}
	result["providers"] = providers
	return result
}

func providerUsageRecord(u ProviderUsage, fetched, now time.Time, staleAfter int64) map[string]any {
	if !u.FetchedAt.IsZero() {
		fetched = u.FetchedAt.UTC()
	}
	age := int64(0)
	if !fetched.IsZero() && now.After(fetched) {
		age = int64(now.Sub(fetched).Seconds())
	}
	stale := age >= staleAfter
	state := "available"
	warnings := []any{}
	windows := make([]any, 0, len(u.Windows))
	var earliest time.Time
	for i, w := range u.Windows {
		windowState := "available"
		if w.UtilizationPct >= 100 {
			windowState = "quota_exhausted"
		}
		if w.UtilizationPct >= 100 && !w.Scoped {
			state = "quota_exhausted"
			if w.ResetAt.After(now) && (earliest.IsZero() || w.ResetAt.Before(earliest)) {
				earliest = w.ResetAt
			}
		}
		if w.UtilizationPct >= 100 && w.Scoped {
			warnings = append(warnings, "A scoped usage window is exhausted.")
		}
		window := map[string]any{"window_id": stableWindowID(i, w), "window_name": w.Label, "window_seconds": nil, "scoped": w.Scoped, "utilization_percentage": w.UtilizationPct, "remaining_percentage": maxPercent(100 - w.UtilizationPct), "reset_at": nil, "availability_state": windowState}
		if w.WindowSeconds != nil {
			window["window_seconds"] = *w.WindowSeconds
		}
		if !w.ResetAt.IsZero() {
			window["reset_at"] = w.ResetAt.UTC().Format(time.RFC3339)
		}
		windows = append(windows, window)
	}
	if stale {
		state = "telemetry_stale"
		// The retained windows are contextual only once they cross the freshness
		// boundary. Do not report a current quota-exhaustion reason for them.
		earliest = time.Time{}
	}
	r := map[string]any{"provider_id": u.Provider, "provider_name": providerDisplayName(u.Provider), "account": nil, "support_state": "supported", "availability_state": state, "fetched_at": nil, "age_seconds": age, "stale": stale, "windows": windows}
	if !fetched.IsZero() {
		r["fetched_at"] = fetched.Format(time.RFC3339)
	}
	if !earliest.IsZero() {
		r["reason"] = map[string]any{"code": "quota_exhausted", "retryable": false, "user_action_required": false, "reset_at": earliest.UTC().Format(time.RFC3339)}
	}
	if len(warnings) > 0 {
		r["warnings"] = warnings
	}
	return r
}

func stableWindowID(i int, w UtilizationWindow) string {
	if w.Scoped {
		return "extra-" + strings.ToLower(strings.ReplaceAll(w.Label, " ", "-"))
	}
	return fmt.Sprintf("window-%d", i+1)
}
func maxPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}
func providerDisplayName(id string) string {
	if id == "" {
		return "Provider"
	}
	return strings.ToUpper(id[:1]) + id[1:]
}
func supportFor(state string) string {
	if state == "unsupported" {
		return "unsupported"
	}
	return "unknown"
}
func sanitizedFailureState(e ProviderError) (string, map[string]any) {
	switch e.Kind {
	case "not_configured":
		return "not_configured", map[string]any{"code": "not_configured", "retryable": false, "user_action_required": true}
	case "unsupported":
		return "unsupported", map[string]any{"code": "unsupported", "retryable": false, "user_action_required": false}
	case "provider_unavailable":
		return "provider_unavailable", map[string]any{"code": "provider_unavailable", "retryable": true, "user_action_required": false}
	default:
		return "unknown", map[string]any{"code": "unknown", "retryable": false, "user_action_required": false}
	}
}
func providerUsageText(content map[string]any) string {
	return "Provider usage is instance-wide cached telemetry. See structured content for freshness and availability."
}
