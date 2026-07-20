package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Augment can't be read through codexbar on Linux (codexbar's augment support is
// macOS-only), so this plugin talks to Augment's Analytics API directly with a
// service-account token. It reports monthly consumption (credits or USD) for a
// user, as a percentage of their budget when one is known.
//
// Endpoints (POST, Bearer <token>, JSON):
//   - /analytics/v0/cost-analytics          → per-user monthly cost/credits
//   - /analytics/v0/get-user-budget-overrides → per-user monthly budget limit

const augmentAPIBase = "https://api.augmentcode.com"

const (
	augmentResourceCredits = "credits"
	augmentResourceUSD     = "usd"

	apiResourceCredits = "BUDGET_RESOURCE_CREDITS_CONSUMED"
	apiResourceUSD     = "BUDGET_RESOURCE_USD_COST"

	// defaultAugmentCreditsBudget is the assumed monthly credit cap when nothing
	// else is configured, so a credits plan always renders a used-of-budget bar.
	defaultAugmentCreditsBudget = 2_500_000.0
)

// jsonPoster POSTs a JSON body with a bearer token and returns status + body.
// exec-free seam: real HTTP in production (realJSONPost), scripted in tests.
type jsonPoster func(ctx context.Context, url, token string, body []byte) (int, []byte, error)

// augmentClient fetches one user's Augment usage.
type augmentClient struct {
	base          string
	token         string
	email         string
	resource      string  // "credits" | "usd"
	budget        float64 // explicit monthly budget from config; <= 0 means resolve one
	defaultBudget float64 // fallback budget when none is configured/overridden (0 = none)
	post          jsonPoster
	now           func() time.Time
}

func (c *augmentClient) isCredits() bool { return c.resource != augmentResourceUSD }

// --- Analytics API wire types -------------------------------------------------

type augError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type augCostResp struct {
	DataPoints []struct {
		CostMetrics struct {
			BilledAmountUSD string `json:"billed_amount_usd"`
			CreditsConsumed string `json:"credits_consumed"`
		} `json:"cost_metrics"`
	} `json:"data_points"`
	Error *augError `json:"error"`
}

type augBudgetResp struct {
	Overrides []struct {
		UserEmail string `json:"user_email"`
		Config    struct {
			Limit string `json:"limit"`
		} `json:"config"`
	} `json:"overrides"`
	Error *augError `json:"error"`
}

// fetchUsage returns this month's Augment usage as a ProviderUsage.
func (c *augmentClient) fetchUsage(ctx context.Context) (*ProviderUsage, error) {
	start, end, hasRange := augmentMonthRange(c.now())

	var usage float64
	if hasRange { // no completed day yet on the 1st of the month
		var err error
		usage, err = c.fetchConsumption(ctx, start, end)
		if err != nil {
			return nil, err
		}
	}

	budget, err := c.resolveBudget(ctx)
	if err != nil {
		return nil, err
	}
	return c.toProviderUsage(usage, budget), nil
}

// resolveBudget picks the monthly budget: an explicit config value wins, then a
// per-user budget override from the API, then the configured default (e.g. the
// 2.5M-credit fallback) so a bar/percentage still renders.
func (c *augmentClient) resolveBudget(ctx context.Context) (float64, error) {
	if c.budget > 0 {
		return c.budget, nil
	}
	override, err := c.fetchBudget(ctx)
	if err != nil {
		return 0, err
	}
	if override > 0 {
		return override, nil
	}
	return c.defaultBudget, nil
}

func (c *augmentClient) fetchConsumption(ctx context.Context, start, end string) (float64, error) {
	body, _ := json.Marshal(map[string]any{
		"start_date": start,
		"end_date":   end,
		"filters":    map[string]any{"user_emails": []string{c.email}},
	})
	var r augCostResp
	if err := c.call(ctx, "/analytics/v0/cost-analytics", body, &r); err != nil {
		return 0, err
	}
	if r.Error != nil {
		return 0, fmt.Errorf("augment: %s", r.Error.Message)
	}
	var total float64
	for _, dp := range r.DataPoints {
		raw := dp.CostMetrics.CreditsConsumed
		if !c.isCredits() {
			raw = dp.CostMetrics.BilledAmountUSD
		}
		total += parseFloatOr(raw, 0)
	}
	return total, nil
}

// fetchBudget returns the user's monthly budget limit, or 0 when none is set.
func (c *augmentClient) fetchBudget(ctx context.Context) (float64, error) {
	resource := apiResourceCredits
	if !c.isCredits() {
		resource = apiResourceUSD
	}
	body, _ := json.Marshal(map[string]any{
		"user_emails": []string{c.email},
		"resource":    resource,
		"period":      "BUDGET_PERIOD_MONTHLY",
	})
	var r augBudgetResp
	if err := c.call(ctx, "/analytics/v0/get-user-budget-overrides", body, &r); err != nil {
		return 0, err
	}
	if r.Error != nil {
		return 0, fmt.Errorf("augment: %s", r.Error.Message)
	}
	for _, o := range r.Overrides {
		if equalFoldEmail(o.UserEmail, c.email) {
			return parseFloatOr(o.Config.Limit, 0), nil
		}
	}
	return 0, nil
}

// call POSTs body to path and decodes the JSON response into out.
func (c *augmentClient) call(ctx context.Context, path string, body []byte, out any) error {
	status, raw, err := c.post(ctx, c.base+path, c.token, body)
	if err != nil {
		return fmt.Errorf("augment %s: %w", path, err)
	}
	if jsonErr := json.Unmarshal(raw, out); jsonErr != nil {
		// A non-2xx with a non-JSON body (gateway error, auth HTML): surface the
		// status rather than a decode error.
		if status < 200 || status >= 300 {
			return fmt.Errorf("augment %s: HTTP %d", path, status)
		}
		return fmt.Errorf("augment %s: %w", path, jsonErr)
	}
	return nil
}

// toProviderUsage renders the consumption + budget as a ProviderUsage: a monthly
// window with a used-of-budget percentage when a budget is known, plus a Detail
// headline that always carries the raw consumption.
func (c *augmentClient) toProviderUsage(usage, budget float64) *ProviderUsage {
	pu := &ProviderUsage{
		Provider:  "augment",
		Source:    "analytics",
		Detail:    augmentAmount(usage, c.isCredits()) + " this month",
		FetchedAt: c.now().UTC(),
	}
	label := "Monthly credits"
	if !c.isCredits() {
		label = "Monthly spend"
	}
	if budget > 0 {
		pu.Plan = augmentAmount(budget, c.isCredits()) + " budget"
		pu.Windows = []UtilizationWindow{{
			Label:            label,
			UtilizationPct:   usage / budget * 100,
			ResetAt:          augmentMonthReset(c.now()),
			ResetDescription: "renews " + augmentMonthReset(c.now()).Format("Jan 2"),
		}}
	}
	return pu
}

// --- helpers ------------------------------------------------------------------

// augmentMonthRange returns the UTC month-to-date range the Analytics API wants:
// the 1st of the month through yesterday. hasRange is false on the 1st (no
// completed day yet), when the caller should report zero usage.
func augmentMonthRange(now time.Time) (start, end string, hasRange bool) {
	u := now.UTC()
	first := time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
	yesterday := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	return first.Format("2006-01-02"), yesterday.Format("2006-01-02"), !first.After(yesterday)
}

// augmentMonthReset is the first of next month (UTC) — when monthly usage resets.
func augmentMonthReset(now time.Time) time.Time {
	u := now.UTC()
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
}

// augmentAmount formats a usage/budget number for display.
func augmentAmount(v float64, credits bool) string {
	if credits {
		return withThousands(int64(v)) + " credits"
	}
	return "$" + strconv.FormatFloat(v, 'f', 2, 64)
}

// withThousands renders an integer with comma group separators.
func withThousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := ""
	if n < 0 {
		neg, s = "-", s[1:]
	}
	var out []byte
	for i, d := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, d)
	}
	return neg + string(out)
}

func parseFloatOr(s string, fallback float64) float64 {
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return fallback
}

func equalFoldEmail(a, b string) bool {
	return len(a) == len(b) && toLowerASCII(a) == toLowerASCII(b)
}

func toLowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// realJSONPost is the production jsonPoster.
func realJSONPost(ctx context.Context, url, token string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, raw, nil
}
