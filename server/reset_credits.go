package main

import (
	"encoding/json"
	"time"
)

const codexResetCreditsWindowID = "reset-credits"

// ResetCredits carries only display data. An absent count is different from
// zero: Win-CodexBar supplies a human summary and the next expiry instead of an
// inventory. Credit IDs and redemption metadata are deliberately not exposed.
type ResetCredits struct {
	AvailableCount *int          `json:"available_count,omitempty"`
	Credits        []ResetCredit `json:"credits,omitempty"`
	Summary        string        `json:"summary,omitempty"`
	NextExpiresAt  time.Time     `json:"next_expires_at,omitzero"`
}

type ResetCredit struct {
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at,omitzero"`
}

func (u *cbUsage) resetCredits() *ResetCredits {
	if credits := parseResetCredits(u.ResetCredits); credits != nil {
		return credits
	}
	// The Windows port exposes reset credits as an informational extra window.
	// Keep its summary intact; extracting a number from prose would be brittle.
	for _, extra := range u.Extra {
		if extra.ID == codexResetCreditsWindowID && extra.Window != nil {
			return &ResetCredits{
				Summary:       extra.Window.ResetDescription,
				NextExpiresAt: parseTimeOr(extra.Window.ResetsAt, time.Time{}),
			}
		}
	}
	return nil
}

// Decode optional reset data separately so a malformed inventory never makes
// otherwise valid quota windows (or other providers) disappear.
func parseResetCredits(raw json.RawMessage) *ResetCredits {
	var snapshot struct {
		AvailableCount *int `json:"availableCount"`
		SnakeCount     *int `json:"available_count"`
		Credits        []struct {
			Status    string `json:"status"`
			ExpiresAt string `json:"expires_at"`
		} `json:"credits"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil
	}
	count := snapshot.AvailableCount
	if count == nil {
		count = snapshot.SnakeCount
	}
	if (count == nil && len(snapshot.Credits) == 0) || (count != nil && *count < 0) {
		return nil
	}
	out := &ResetCredits{AvailableCount: count}
	for _, credit := range snapshot.Credits {
		out.Credits = append(out.Credits, ResetCredit{
			Status:    credit.Status,
			ExpiresAt: parseTimeOr(credit.ExpiresAt, time.Time{}),
		})
	}
	return out
}
