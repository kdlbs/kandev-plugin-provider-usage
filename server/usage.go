package main

import (
	"encoding/json"
	"fmt"
	"time"
)

// UtilizationWindow is one rate-limit window's utilization. Mirrors the shape
// the native kandev "Subscription Utilization" feature exposed, so the UI shape
// is unchanged by moving the capability into this plugin.
type UtilizationWindow struct {
	Label          string  `json:"label"`           // e.g. "5-hour", "weekly"
	UtilizationPct float64 `json:"utilization_pct"` // 0–100
	// ResetAt is omitted when unknown: the zero time would serialize as
	// "0001-01-01T00:00:00Z", which the UI reads as a real (long past) date and
	// renders as "resets now" instead of falling back to ResetDescription.
	ResetAt time.Time `json:"reset_at,omitzero"`
	// ResetDescription is codexbar's human-friendly reset string, kept verbatim
	// because it already carries the provider's own timezone/wording.
	ResetDescription string `json:"reset_description,omitempty"`
	// Scoped marks a narrow/extra window (e.g. codexbar's "Fable only") so the UI
	// can exclude it from the at-a-glance peak while still listing it.
	Scoped bool `json:"scoped,omitempty"`
}

// Pace carries codexbar's optional burn-rate summary for a window ("52% in
// reserve | Expected 56% used | Lasts until reset"). The native feature never
// had this; it's surfaced as an optional extra.
type Pace struct {
	Summary string `json:"summary,omitempty"`
	Stage   string `json:"stage,omitempty"`
}

// ProviderUsage is the full utilization response for one provider. Superset of
// the native ProviderUsage: adds Source (where codexbar read the data) and the
// optional Pace summaries.
type ProviderUsage struct {
	Provider string              `json:"provider"`       // "claude", "codex", ...
	Plan     string              `json:"plan,omitempty"` // e.g. "max", "pro", "free"
	Windows  []UtilizationWindow `json:"windows"`
	// Detail is a human headline for providers whose usage isn't a rate-limit
	// window percentage — e.g. Augment's raw monthly consumption ("959,232
	// credits this month"). Empty for codexbar providers.
	Detail string `json:"detail,omitempty"`
	// DetailExtra is a sober sub-line under Detail — e.g. Augment's per-day
	// average and projected month-end total.
	DetailExtra  string        `json:"detail_extra,omitempty"`
	FetchedAt    time.Time     `json:"fetched_at"`
	Source       string        `json:"source,omitempty"` // codexbar source: oauth/web/cli/...
	PacePrime    *Pace         `json:"pace_primary,omitempty"`
	PaceSec      *Pace         `json:"pace_secondary,omitempty"`
	ResetCredits *ResetCredits `json:"reset_credits,omitempty"`
}

// --- codexbar JSON wire types (subset of `codexbar usage --format json`) ------
//
// Two CLIs feed these types and they spell the usage payload differently:
// upstream codexbar emits camelCase, while the Win-CodexBar port (the only
// codexbar-compatible CLI on Windows) emits snake_case and reports a failing
// provider's `error` as a bare string rather than an object. Both spellings are
// accepted as co-equal — the port may re-converge on upstream's, or upstream may
// change — via UnmarshalJSON methods that decode the camelCase form through the
// existing tags and let the snake_case form fill in whatever it left zero. That
// merge rule is safe because a single CLI never writes both spellings, so the
// absent one always decodes to the zero value.

// cbEntry is one element of codexbar's top-level JSON array (one per provider).
type cbEntry struct {
	Provider string   `json:"provider"`
	Source   string   `json:"source"`
	Version  string   `json:"version"`
	Usage    *cbUsage `json:"usage"`
	Pace     *cbPace  `json:"pace"`
	Error    *cbError `json:"error"`
}

type cbError struct {
	Code    int    `json:"code"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// UnmarshalJSON accepts both an object (upstream) and a bare string (the port),
// which is the difference that matters most: a string here used to fail the
// whole array's decode, discarding every healthy provider alongside the one that
// reported an error.
func (e *cbError) UnmarshalJSON(data []byte) error {
	var msg string
	if err := json.Unmarshal(data, &msg); err == nil {
		e.Message = msg
		return nil
	}
	type alias cbError
	return json.Unmarshal(data, (*alias)(e))
}

type cbUsage struct {
	Primary      *cbWindow       `json:"primary"`
	Secondary    *cbWindow       `json:"secondary"`
	Tertiary     *cbWindow       `json:"tertiary"`
	Extra        []cbExtra       `json:"extraRateWindows"`
	LoginMethod  string          `json:"loginMethod"`
	Identity     *cbIdentity     `json:"identity"`
	UpdatedAt    string          `json:"updatedAt"`
	ResetCredits json.RawMessage `json:"codexResetCredits"`
}

// UnmarshalJSON fills the fields the port spells with underscores. It carries no
// `identity` block at all, so planName falls through to LoginMethod, which it
// populates with the plan ("Claude Max 5x").
func (u *cbUsage) UnmarshalJSON(data []byte) error {
	type alias cbUsage
	if err := json.Unmarshal(data, (*alias)(u)); err != nil {
		return err
	}
	var snake struct {
		Extra        []cbExtra       `json:"extra_rate_windows"`
		LoginMethod  string          `json:"login_method"`
		UpdatedAt    string          `json:"updated_at"`
		ResetCredits json.RawMessage `json:"codex_reset_credits"`
	}
	// Best-effort: a wrongly-typed key here was simply skipped before these
	// spellings were known, and must stay non-fatal — failing the document would
	// discard every healthy provider in the array alongside it.
	_ = json.Unmarshal(data, &snake)
	if len(u.Extra) == 0 {
		u.Extra = snake.Extra
	}
	if u.LoginMethod == "" {
		u.LoginMethod = snake.LoginMethod
	}
	if u.UpdatedAt == "" {
		u.UpdatedAt = snake.UpdatedAt
	}
	if len(u.ResetCredits) == 0 {
		u.ResetCredits = snake.ResetCredits
	}
	return nil
}

type cbIdentity struct {
	ProviderID string `json:"providerID"`
	PlanName   string `json:"planName"`
}

type cbWindow struct {
	ResetsAt         string  `json:"resetsAt"`
	ResetDescription string  `json:"resetDescription"`
	UsedPercent      float64 `json:"usedPercent"`
	WindowMinutes    int     `json:"windowMinutes"`
}

// UnmarshalJSON fills the fields the port spells with underscores. A window the
// port reports as used_percent 0 stays 0 either way, so the zero-value merge
// rule can't lose a legitimately empty window.
func (w *cbWindow) UnmarshalJSON(data []byte) error {
	type alias cbWindow
	if err := json.Unmarshal(data, (*alias)(w)); err != nil {
		return err
	}
	var snake struct {
		ResetsAt         string  `json:"resets_at"`
		ResetDescription string  `json:"reset_description"`
		UsedPercent      float64 `json:"used_percent"`
		WindowMinutes    int     `json:"window_minutes"`
	}
	_ = json.Unmarshal(data, &snake) // best-effort, see cbUsage.UnmarshalJSON
	if w.ResetsAt == "" {
		w.ResetsAt = snake.ResetsAt
	}
	if w.ResetDescription == "" {
		w.ResetDescription = snake.ResetDescription
	}
	if w.UsedPercent == 0 {
		w.UsedPercent = snake.UsedPercent
	}
	if w.WindowMinutes == 0 {
		w.WindowMinutes = snake.WindowMinutes
	}
	return nil
}

type cbExtra struct {
	ID     string    `json:"id"`
	Title  string    `json:"title"`
	Window *cbWindow `json:"window"`
}

type cbPace struct {
	Primary   *cbPaceSide `json:"primary"`
	Secondary *cbPaceSide `json:"secondary"`
}

type cbPaceSide struct {
	Summary string `json:"summary"`
	Stage   string `json:"stage"`
}

// parseCodexbarUsage decodes `codexbar usage --format json` output (a JSON array
// of provider entries).
func parseCodexbarUsage(raw []byte) ([]cbEntry, error) {
	var entries []cbEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parsing codexbar output: %w", err)
	}
	return entries, nil
}

// toProviderUsage converts a codexbar entry that carries usage into the
// canonical ProviderUsage. Returns nil when the entry has no usage payload.
func (e cbEntry) toProviderUsage(now time.Time) *ProviderUsage {
	if e.Usage == nil {
		return nil
	}
	pu := &ProviderUsage{
		Provider:  e.Provider,
		Source:    e.Source,
		Plan:      e.Usage.planName(),
		Windows:   e.Usage.windows(e.Provider),
		FetchedAt: parseTimeOr(e.Usage.UpdatedAt, now),
	}
	if e.Provider == "codex" {
		pu.ResetCredits = e.Usage.resetCredits()
	}
	if e.Pace != nil {
		pu.PacePrime = e.Pace.Primary.toPace()
		pu.PaceSec = e.Pace.Secondary.toPace()
	}
	return pu
}

func (u *cbUsage) planName() string {
	if u.Identity != nil && u.Identity.PlanName != "" {
		return u.Identity.PlanName
	}
	return u.LoginMethod
}

// windows flattens primary/secondary/tertiary + extra windows into the canonical
// ordered list, skipping any that codexbar left nil.
func (u *cbUsage) windows(provider string) []UtilizationWindow {
	out := make([]UtilizationWindow, 0, 3+len(u.Extra))
	for i, w := range []*cbWindow{u.Primary, u.Secondary, u.Tertiary} {
		if w == nil {
			continue
		}
		out = append(out, w.toWindow(defaultWindowLabel(i, w.WindowMinutes)))
	}
	for _, ex := range u.Extra {
		if ex.Window == nil {
			continue
		}
		if provider == "codex" && ex.ID == codexResetCreditsWindowID {
			continue // shown as manual resets, not a utilization percentage
		}
		label := ex.Title
		if label == "" {
			label = windowLabelFromMinutes(ex.Window.WindowMinutes)
		}
		w := ex.Window.toWindow(label)
		w.Scoped = true
		out = append(out, w)
	}
	return out
}

func (w *cbWindow) toWindow(label string) UtilizationWindow {
	return UtilizationWindow{
		Label:            label,
		UtilizationPct:   w.UsedPercent,
		ResetAt:          parseTimeOr(w.ResetsAt, time.Time{}),
		ResetDescription: w.ResetDescription,
	}
}

func (p *cbPaceSide) toPace() *Pace {
	if p == nil {
		return nil
	}
	return &Pace{Summary: p.Summary, Stage: p.Stage}
}

// defaultWindowLabel names the primary/secondary/tertiary windows, preferring a
// duration-derived label ("5-hour", "weekly") and falling back to slot names.
func defaultWindowLabel(slot, minutes int) string {
	if l := windowLabelFromMinutes(minutes); l != "" {
		return l
	}
	switch slot {
	case 0:
		return "Primary"
	case 1:
		return "Secondary"
	default:
		return "Tertiary"
	}
}

// windowLabelFromMinutes maps a codexbar windowMinutes to a friendly label.
// Returns "" for durations that don't match a well-known window.
func windowLabelFromMinutes(minutes int) string {
	switch minutes {
	case 0:
		return ""
	case 300:
		return "5-hour"
	case 1440:
		return "daily"
	case 10080:
		return "weekly"
	case 43200:
		return "monthly"
	}
	switch {
	case minutes%10080 == 0:
		return fmt.Sprintf("%d-week", minutes/10080)
	case minutes%1440 == 0:
		return fmt.Sprintf("%d-day", minutes/1440)
	case minutes%60 == 0:
		return fmt.Sprintf("%d-hour", minutes/60)
	default:
		return fmt.Sprintf("%d-min", minutes)
	}
}

// parseTimeOr parses an RFC3339 timestamp, returning the fallback on failure.
func parseTimeOr(s string, fallback time.Time) time.Time {
	if s == "" {
		return fallback
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return fallback
}
