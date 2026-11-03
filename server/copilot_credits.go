package main

import (
	"encoding/json"
	"math"
	"time"
)

const configKeyCopilotAllowance = "copilot_monthly_credits"

// CopilotCredits carries exact counters, separate from provider quota windows.
// An allowance configured here or in CodexBar is a display preference, not an
// enforced GitHub billing limit or evidence that the provider is exhausted.
type CopilotCredits struct {
	Used             float64   `json:"used"`
	Allowance        *float64  `json:"allowance,omitempty"`
	AllowanceSource  string    `json:"allowance_source,omitempty"`
	ResetAt          time.Time `json:"reset_at,omitzero"`
	ResetDescription string    `json:"reset_description,omitempty"`
}

func validCreditCount(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}

// CodexBar v0.83.0 emits a stable seat-credit detail row in usage.details.
// Numeric usageValue/progress fields preserve precision; rounded percentages
// and presentation strings cannot reconstruct a trustworthy absolute count.
func copilotCredits(raw json.RawMessage, windows []UtilizationWindow) *CopilotCredits {
	var sections []struct {
		Rows []struct {
			ID             string   `json:"id"`
			UsageValue     *float64 `json:"usageValue"`
			SecondaryValue string   `json:"secondaryValue"`
			Progress       *struct {
				Used  *float64 `json:"used"`
				Total *float64 `json:"total"`
			} `json:"progress"`
		} `json:"rows"`
	}
	if json.Unmarshal(raw, &sections) != nil {
		return nil
	}
	for _, section := range sections {
		for _, row := range section.Rows {
			if row.ID != "copilot-seat-credits" {
				continue
			}
			used := row.UsageValue
			if used == nil && row.Progress != nil {
				used = row.Progress.Used
			}
			if used == nil || !validCreditCount(*used) {
				continue
			}
			credits := &CopilotCredits{Used: *used, ResetDescription: row.SecondaryValue}
			if progress := row.Progress; progress != nil && progress.Used != nil && *progress.Used == *used &&
				progress.Total != nil && validCreditCount(*progress.Total) && *progress.Total > 0 {
				credits.Allowance = progress.Total
				credits.AllowanceSource = "codexbar"
			}
			for _, window := range windows {
				if !window.Scoped && !window.ResetAt.IsZero() {
					credits.ResetAt = window.ResetAt
					break
				}
			}
			return credits
		}
	}
	return nil
}

func applyCopilotAllowance(usage *ProviderUsage, cfg map[string]any) {
	if usage.Provider != "copilot" || usage.CopilotCredits == nil {
		return
	}
	allowance := positiveFloatOr(cfg[configKeyCopilotAllowance], 0)
	if validCreditCount(allowance) && allowance > 0 {
		usage.CopilotCredits.Allowance = &allowance
		usage.CopilotCredits.AllowanceSource = "settings"
	}
}
