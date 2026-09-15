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
	var wg sync.WaitGroup
	for i, endpoint := range endpoints {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if raw, err := c.request(ctx, auth, endpoint.path, false, endpoint.body); err == nil {
				results[i] = cursorDecode(raw)
			}
		}()
	}
	wg.Wait()
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
		if cents := member.number("spendCents"); cents != nil {
			personal := &UsageSpend{Used: *cents / 100, Currency: "USD"}
			if limit := member.number("hardLimitOverrideDollars"); limit != nil {
				if *limit > 0 {
					personal.Limit = limit
				}
			} else if out.ExtraUsage != nil && out.ExtraUsage.Scope == "" {
				personal.Limit = out.ExtraUsage.Limit
			}
			out.ExtraUsage = personal
		}
	}
	if len(out.Windows) == 0 && out.ExtraUsage == nil {
		return nil, errors.New("No usage was reported for you in this team. Check the selected team and this account's access.")
	}
	if summary == nil {
		out.DetailWarning = "Only selected-team usage is shown. Additional quotas and reset times were not reported for this team."
	}
	return out, nil
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
