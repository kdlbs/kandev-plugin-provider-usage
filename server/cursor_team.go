package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const cursorTeamSetting = "cursor_team_id"

// A selected team must never fall back to CodexBar's unscoped account data.
type cursorTeamSelectionError struct{ error }

func cursorTeamConfigured(cfg map[string]any) bool {
	value := cfg[cursorTeamSetting]
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return value != nil
}

func cursorConfiguredTeamID(cfg map[string]any) (int64, error) {
	if !cursorTeamConfigured(cfg) {
		return 0, nil
	}
	text, ok := cfg[cursorTeamSetting].(string)
	if ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return 0, nil
		}
		if id := cursorID(json.RawMessage(text)); id > 0 {
			return id, nil
		}
	}
	return 0, errors.New("Invalid Cursor team selection. Choose a team in plugin settings.")
}

// IDs may be JSON strings or integers; do not round them through float64.
func cursorID(raw json.RawMessage) int64 {
	text := strings.TrimSpace(string(raw))
	if strings.HasPrefix(text, "\"") {
		if json.Unmarshal(raw, &text) != nil {
			return 0
		}
	}
	if text == "" || strings.IndexFunc(text, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0
	}
	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

func cursorResponseTeamID(payload cursorObject) (int64, bool) {
	for _, key := range []string{"teamId", "team_id"} {
		if raw, ok := payload[key]; ok {
			return cursorID(raw), true
		}
	}
	if team := payload.object("team"); team != nil {
		if raw, ok := team["id"]; ok {
			return cursorID(raw), true
		}
		if id, found := cursorResponseTeamID(team); found {
			return id, true
		}
	}
	if team := payload.object("teamUsage"); team != nil {
		return cursorResponseTeamID(team)
	}
	return 0, false
}

func (c *cursorClient) teams(ctx context.Context, auth cursorAuth) ([]cursorObject, error) {
	raw, err := c.request(ctx, auth, "/api/dashboard/teams", false, []byte(`{"activeOnly":false}`))
	if err != nil {
		return nil, err
	}
	var teams []cursorObject
	if json.Unmarshal(cursorDecode(raw)["teams"], &teams) != nil {
		return nil, errors.New("Cannot read the teams available to this Cursor session.")
	}
	return teams, nil
}

func (c *cursorClient) fetchTeamUsage(ctx context.Context, auth cursorAuth, teamID int64, email string, now time.Time) (*ProviderUsage, error) {
	teams, err := c.teams(ctx, auth)
	if err != nil {
		return nil, err
	}
	var team cursorObject
	for _, candidate := range teams {
		if cursorID(candidate["id"]) == teamID {
			team = candidate
			break
		}
	}
	if team == nil {
		return nil, errors.New("This team is not available to the signed-in account. Choose a team in plugin settings or update the session cookie.")
	}
	// These dashboard POSTs explicitly select a team. The personal summary
	// and RPC do not document team selection, so never add speculative query
	// parameters or merge their unverified account-wide data into this report.
	teamBody := []byte(fmt.Sprintf(`{"teamId":%d}`, teamID))
	endpoints := []struct {
		path string
		body []byte
	}{
		{"/api/dashboard/team", teamBody},
		{"/api/dashboard/get-team-spend", teamBody},
		{"/api/usage-summary", nil},
	}
	results := make([]cursorObject, len(endpoints))
	resultErrs := make([]error, len(endpoints))
	var wg sync.WaitGroup
	for i, endpoint := range endpoints {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if raw, err := c.request(ctx, auth, endpoint.path, false, endpoint.body); err == nil {
				results[i] = cursorDecode(raw)
			} else {
				resultErrs[i] = err
			}
		}()
	}
	wg.Wait()
	for _, resultErr := range resultErrs {
		if errors.Is(resultErr, errCursorSessionRejected) {
			return nil, errCursorSessionRejected
		}
	}
	details, spend, summary := results[0], results[1], results[2]
	for _, payload := range []cursorObject{details, spend} {
		if reported, present := cursorResponseTeamID(payload); present && reported != teamID {
			return nil, errors.New("Cursor returned data for a different team; its usage was discarded.")
		}
	}
	// A summary is usable only when it identifies the requested team. An
	// absent team ID is not proof of scope, even if the account email matches.
	if reported, present := cursorResponseTeamID(summary); !present || reported != teamID {
		summary = nil
	}
	out := mapCursorUsage(nil, summary, nil, nil, now)
	out.TeamID = strconv.FormatInt(teamID, 10)
	out.TeamName = strings.TrimSpace(team.text("name"))
	out.Source = "cursor-team"
	if out.Plan == "" {
		out.Plan = "Cursor Team"
	}
	member, err := cursorTeamMember(spend, cursorID(details["userId"]), email)
	if err != nil {
		return nil, err
	}
	if member != nil {
		used := member.number("fastPremiumRequests")
		quota := firstCursorNumber(team.number("requestQuotaPerSeat"), team.number("request_quota_per_seat"))
		// Cursor's requestQuotaPerSeat is a multiplier of the 500-request
		// allowance. No reported multiplier means no inferred request limit.
		if used != nil && quota != nil && *quota > 0 {
			limit := *quota * 500
			if pct := cursorRatio(used, &limit); pct != nil {
				window := UtilizationWindow{
					Label: "Total Usage", UtilizationPct: *pct,
					ResetAt: cursorBillingReset(summary),
					Detail:  fmt.Sprintf("%.0f of %.0f included requests", *used, limit),
				}
				windows := []UtilizationWindow{window}
				for _, previous := range out.Windows {
					if previous.Label != window.Label {
						windows = append(windows, previous)
					}
				}
				out.Windows = windows
			}
		}
		// Current tiered team responses report the authoritative percentages on
		// the member row. These fields also cover plans that no longer expose
		// fastPremiumRequests or requestQuotaPerSeat.
		for _, metric := range []struct {
			label  string
			field  string
			scoped bool
		}{
			{"Total Usage", "totalPercentUsed", false},
			{"Auto Usage", "autoPercentUsed", true},
			{"API Usage", "apiPercentUsed", true},
		} {
			pct := member.number(metric.field)
			if pct == nil {
				continue
			}
			window := UtilizationWindow{
				Label: metric.label, UtilizationPct: *pct,
				ResetAt: cursorBillingReset(summary), Scoped: metric.scoped,
			}
			replaced := false
			for i := range out.Windows {
				if out.Windows[i].Label == metric.label {
					if window.ResetAt.IsZero() {
						window.ResetAt = out.Windows[i].ResetAt
						window.ResetDescription = out.Windows[i].ResetDescription
					}
					out.Windows[i] = window
					replaced = true
					break
				}
			}
			if !replaced {
				out.Windows = append(out.Windows, window)
			}
		}
		if cents := member.number("spendCents"); cents != nil {
			personal := &UsageSpend{Used: *cents / 100, Currency: "USD"}
			if limit := firstCursorNumber(
				member.number("effectivePerUserLimitDollars"),
				member.number("monthlyLimitDollars"),
				member.number("hardLimitOverrideDollars"),
			); limit != nil {
				if *limit > 0 {
					personal.Limit = limit
				}
			} else if out.ExtraUsage != nil && out.ExtraUsage.Scope == "" {
				personal.Limit = out.ExtraUsage.Limit
			}
			out.ExtraUsage = personal
		} else if cents := member.number("overallSpendCents"); cents != nil {
			// Cursor's token-based Enterprise plans apply the effective member
			// limit to total usage, including plan-covered spend. Other plans use
			// the limit for on-demand spend only, so only token plans get a
			// percentage derived from overallSpendCents.
			usedDollars := *cents / 100
			total := &UsageSpend{
				Used: usedDollars, Currency: "USD", Label: "Total spend",
			}
			limit := firstCursorNumber(
				member.number("effectivePerUserLimitDollars"),
				member.number("monthlyLimitDollars"),
				member.number("hardLimitOverrideDollars"),
			)
			tokenPlan := strings.EqualFold(team.text("pricingStrategy"), "tokens")
			if tokenPlan && limit != nil && *limit > 0 {
				total.Limit = limit
			}
			out.ExtraUsage = total
			if tokenPlan && total.Limit != nil {
				hasTotal := false
				for _, window := range out.Windows {
					hasTotal = hasTotal || window.Label == "Total Usage"
				}
				if !hasTotal {
					pct := usedDollars / *total.Limit * 100
					out.Windows = append([]UtilizationWindow{{
						Label: "Total Usage", UtilizationPct: pct,
						ResetAt: cursorBillingReset(team),
						Detail:  fmt.Sprintf("$%.2f of $%.2f personal limit", usedDollars, *total.Limit),
					}}, out.Windows...)
				}
			}
		}
	}
	if out.PacePrime == nil {
		start := team.date("billingCycleStart")
		teamReset := cursorBillingReset(team)
		for i := range out.Windows {
			if out.Windows[i].Label != "Total Usage" {
				continue
			}
			if out.Windows[i].ResetAt.IsZero() {
				out.Windows[i].ResetAt = teamReset
			}
			out.PacePrime = linearUsagePace(
				out.Windows[i].UtilizationPct, start, out.Windows[i].ResetAt, now,
			)
			break
		}
	}
	if len(out.Windows) == 0 && out.ExtraUsage == nil {
		return nil, errors.New("No usage was reported for you in this team. Check the selected team and this account's access.")
	}
	if summary == nil {
		out.DetailWarning = cursorTeamMissingDetails(out)
	}
	return out, nil
}

func cursorTeamMissingDetails(usage *ProviderUsage) string {
	reported := map[string]bool{}
	hasReset := false
	for _, window := range usage.Windows {
		reported[window.Label] = true
		hasReset = hasReset || !window.ResetAt.IsZero() || window.ResetDescription != ""
	}
	missing := []string{}
	for _, metric := range []struct {
		label string
		name  string
	}{
		{"Total Usage", "Total"},
		{"Auto Usage", "Auto"},
		{"API Usage", "API"},
	} {
		if !reported[metric.label] {
			missing = append(missing, metric.name)
		}
	}
	if len(missing) == 0 && hasReset {
		return ""
	}
	detail := "Only selected-team usage is shown."
	if len(missing) > 0 {
		names := missing[0]
		if len(missing) == 2 {
			names = missing[0] + " and " + missing[1]
		} else if len(missing) == 3 {
			names = missing[0] + ", " + missing[1] + " and " + missing[2]
		}
		verb := "quota was"
		if len(missing) > 1 {
			verb = "quotas were"
		}
		detail += " " + names + " " + verb + " not reported for this team."
	}
	if !hasReset {
		detail += " Reset times were not reported for this team."
	}
	return detail
}

func cursorTeamMember(spend cursorObject, userID int64, email string) (cursorObject, error) {
	var members []cursorObject
	if json.Unmarshal(spend["teamMemberSpend"], &members) != nil {
		return nil, nil
	}
	var selected cursorObject
	for _, member := range members {
		matched := email != "" && strings.EqualFold(strings.TrimSpace(member.text("email")), email)
		if memberID := cursorID(member["userId"]); userID != 0 && memberID != 0 {
			matched = memberID == userID
		}
		if matched {
			if selected != nil {
				return nil, errors.New("Cursor returned multiple usage entries for your team identity; their usage was discarded.")
			}
			selected = member
		}
	}
	return selected, nil
}
