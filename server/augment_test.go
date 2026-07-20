package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// realish cost-analytics response (trimmed) for one user, month-to-date.
const augCostSample = `{"data_points":[{"cost_metrics":{"base_cost_usd":"0","billed_amount_usd":"12.34","credits_consumed":"959232"}}],"metadata":{"returned_data_point_count":1}}`

type postResp struct {
	status int
	body   string
}

// fakePoster answers by URL suffix; unmatched paths 404.
func fakePoster(calls *int, byPath map[string]postResp) jsonPoster {
	return func(_ context.Context, url, _ string, _ []byte) (int, []byte, error) {
		if calls != nil {
			*calls++
		}
		for path, r := range byPath {
			if strings.HasSuffix(url, path) {
				return r.status, []byte(r.body), nil
			}
		}
		return 404, []byte(`{}`), nil
	}
}

func midMonth() time.Time { return time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC) }

func newAugmentClient(resource string, budget float64, poster jsonPoster) *augmentClient {
	return &augmentClient{
		base: augmentAPIBase, token: "tok", email: "a@b.com",
		resource: resource, budget: budget, post: poster, now: midMonth,
	}
}

func TestAugment_CreditsNoBudget(t *testing.T) {
	c := newAugmentClient(augmentResourceCredits, 0, fakePoster(nil, map[string]postResp{
		"/cost-analytics":            {200, augCostSample},
		"/get-user-budget-overrides": {200, `{"overrides":[]}`},
	}))
	u, err := c.fetchUsage(context.Background())
	require.NoError(t, err)
	require.Equal(t, "augment", u.Provider)
	require.Equal(t, "analytics", u.Source)
	require.Equal(t, "959,232 credits this month", u.Detail)
	require.Empty(t, u.Windows, "no budget -> no percentage window")
}

func TestAugment_CreditsManualBudget(t *testing.T) {
	var calls int
	c := newAugmentClient(augmentResourceCredits, 2_000_000, fakePoster(&calls, map[string]postResp{
		"/cost-analytics": {200, augCostSample},
	}))
	u, err := c.fetchUsage(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, calls, "manual budget skips the budget-override call")
	require.Len(t, u.Windows, 1)
	require.InDelta(t, 959232.0/2_000_000*100, u.Windows[0].UtilizationPct, 1e-9)
	require.Equal(t, "Monthly credits", u.Windows[0].Label)
	require.Equal(t, "2,000,000 credits budget", u.Plan)
	require.Equal(t, "2026-08-01", u.Windows[0].ResetAt.Format("2006-01-02"))
}

func TestAugment_BudgetFromOverride(t *testing.T) {
	c := newAugmentClient(augmentResourceCredits, 0, fakePoster(nil, map[string]postResp{
		"/cost-analytics":            {200, augCostSample},
		"/get-user-budget-overrides": {200, `{"overrides":[{"user_email":"A@B.com","config":{"limit":"2500000"}}]}`},
	}))
	u, err := c.fetchUsage(context.Background())
	require.NoError(t, err)
	require.Len(t, u.Windows, 1)
	require.InDelta(t, 959232.0/2_500_000*100, u.Windows[0].UtilizationPct, 1e-9)
}

func TestAugment_USD(t *testing.T) {
	c := newAugmentClient(augmentResourceUSD, 0, fakePoster(nil, map[string]postResp{
		"/cost-analytics":            {200, augCostSample},
		"/get-user-budget-overrides": {200, `{"overrides":[]}`},
	}))
	u, err := c.fetchUsage(context.Background())
	require.NoError(t, err)
	require.Equal(t, "$12.34 this month", u.Detail)
}

func TestAugment_FirstOfMonthSkipsCost(t *testing.T) {
	var calls int
	c := newAugmentClient(augmentResourceCredits, 0, fakePoster(&calls, map[string]postResp{
		"/get-user-budget-overrides": {200, `{"overrides":[]}`},
	}))
	c.now = func() time.Time { return time.Date(2026, 7, 1, 6, 0, 0, 0, time.UTC) }
	u, err := c.fetchUsage(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, calls, "only the budget call runs on the 1st")
	require.Equal(t, "0 credits this month", u.Detail)
}

func TestAugment_APIError(t *testing.T) {
	c := newAugmentClient(augmentResourceCredits, 0, fakePoster(nil, map[string]postResp{
		"/cost-analytics": {400, `{"error":{"code":"InvalidArgument","message":"user_email(s) not found on this organization: x"}}`},
	}))
	_, err := c.fetchUsage(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestAugment_HTTPErrorNonJSON(t *testing.T) {
	c := newAugmentClient(augmentResourceCredits, 0, fakePoster(nil, map[string]postResp{
		"/cost-analytics": {502, `<html>bad gateway</html>`},
	}))
	_, err := c.fetchUsage(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "502")
}

func TestWithThousands(t *testing.T) {
	require.Equal(t, "959,232", withThousands(959232))
	require.Equal(t, "1,000,000", withThousands(1000000))
	require.Equal(t, "42", withThousands(42))
	require.Equal(t, "0", withThousands(0))
}

func TestAugmentMonthRange(t *testing.T) {
	start, end, ok := augmentMonthRange(midMonth())
	require.True(t, ok)
	require.Equal(t, "2026-07-01", start)
	require.Equal(t, "2026-07-19", end)

	_, _, ok = augmentMonthRange(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	require.False(t, ok, "no completed day on the 1st")
}

func TestAugmentResource(t *testing.T) {
	require.Equal(t, augmentResourceCredits, augmentResource(""))
	require.Equal(t, augmentResourceCredits, augmentResource("credits"))
	require.Equal(t, augmentResourceUSD, augmentResource("USD"))
	require.Equal(t, augmentResourceUSD, augmentResource("BUDGET_RESOURCE_USD_COST"))
	require.Equal(t, augmentResourceCredits, augmentResource(123))
}
