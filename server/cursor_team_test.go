package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/stretchr/testify/require"
)

const cursorSelectedTeam = "30677937"

const cursorTeamsFixture = `{"teams":[
  {"id":30677936,"name":"Other team","requestQuotaPerSeat":8},
  {"id":30677937,"name":"Selected team","requestQuotaPerSeat":2}
]}`
const cursorTeamDetailsFixture = `{"teamId":30677937,"userId":42}`
const cursorTeamSpendFixture = `{"teamId":30677937,"teamMemberSpend":[
  {"userId":9,"email":"someone-else@example.test","fastPremiumRequests":900,"spendCents":99999},
  {"userId":42,"email":"cursor@example.test","fastPremiumRequests":37,"spendCents":0,"hardLimitOverrideDollars":250}
]}`

func cursorTeamFixtureHandler(t *testing.T, overrides map[string]string, calls *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			calls.Add(1)
		}
		var body string
		switch r.URL.Path {
		case "/api/auth/me":
			body = `{"sub":"user_test","email":"cursor@example.test"}`
		case "/api/dashboard/teams":
			body = cursorTeamsFixture
		case "/api/dashboard/team":
			body = cursorTeamDetailsFixture
		case "/api/dashboard/get-team-spend":
			body = cursorTeamSpendFixture
		case "/api/usage-summary":
			body = `{"teamId":30677936,` + strings.TrimSpace(cursorEnterpriseSummary)[1:]
		default:
			t.Errorf("unscoped request must not be used for selected-team usage: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") == "" {
			t.Error("team requests must use the authenticated session cookie, not an RPC bearer")
		}
		if strings.HasPrefix(r.URL.Path, "/api/dashboard/") {
			if r.Method != http.MethodPost || r.Header.Get("Origin") == "" || r.Header.Get("Content-Type") != "application/json" {
				t.Error("dashboard reads need JSON POST and Origin")
			}
			var payload map[string]json.RawMessage
			if json.NewDecoder(r.Body).Decode(&payload) != nil {
				t.Error("invalid team request body")
			}
			if r.URL.Path != "/api/dashboard/teams" && string(payload["teamId"]) != cursorSelectedTeam {
				t.Error("team requests must contain the explicitly selected numeric teamId")
			}
			if r.URL.Path == "/api/dashboard/teams" && string(payload["activeOnly"]) != "false" {
				t.Error("team selection must check all available memberships")
			}
		} else if r.Method != http.MethodGet {
			t.Error("identity and summary must use GET")
		}
		if override, ok := overrides[r.URL.Path]; ok {
			body = override
			if body == "unavailable" {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("credential=DO-NOT-EXPOSE"))
				return
			}
		}
		_, _ = w.Write([]byte(body))
	}
}

func TestCursorTeamSelectionUsesRequestedTeamAndCurrentMember(t *testing.T) {
	var calls atomic.Int32
	c := cursorTestClient(t, cursorTeamFixtureHandler(t, nil, &calls))
	base := cursorBase(t)
	base.ExtraUsage = &UsageSpend{Used: 999, Currency: "USD"}
	out, err := c.fetch(context.Background(), map[string]any{cursorTeamSetting: cursorSelectedTeam}, base, cursorTestNow)
	require.NoError(t, err)
	require.Equal(t, int32(5), calls.Load())
	require.Equal(t, cursorSelectedTeam, out.TeamID)
	require.Equal(t, "Selected team", out.TeamName)
	require.Len(t, out.Windows, 1, "unverified account-summary Auto/API usage must not be retained")
	require.Equal(t, "Total Usage", out.Windows[0].Label)
	require.InDelta(t, 3.7, out.Windows[0].UtilizationPct, 1e-9)
	require.Equal(t, "37 of 1000 included requests", out.Windows[0].Detail)
	require.True(t, out.Windows[0].ResetAt.IsZero(), "the default team's reset is not a selected-team reset")
	require.Equal(t, &UsageSpend{Used: 0, Limit: floatPtr(250), Currency: "USD"}, out.ExtraUsage)
	require.Contains(t, out.DetailWarning, "selected-team")
	require.Equal(t, 999.0, base.ExtraUsage.Used, "cached CLI response is immutable")
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "30677936")
	require.NotContains(t, string(raw), "example.test")
}

func TestCursorTeamMixedAuthRejectionFallsBackToSameAgentAccount(t *testing.T) {
	var identityCalls atomic.Int32
	fixture := cursorTeamFixtureHandler(t, nil, nil)
	c := cursorTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/me" {
			identityCalls.Add(1)
			fixture(w, r)
			return
		}
		if identityCalls.Load() == 1 && r.URL.Path == "/api/usage-summary" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fixture(w, r)
	})
	desktop, err := cursorBearerAuth(cursorAuthFixtureFor("user_test"))
	require.NoError(t, err)
	agent, err := cursorBearerAuth(cursorAuthFixtureFor("user_test"))
	require.NoError(t, err)
	desktop.alternates = []cursorAuth{agent}
	c.auth = func(context.Context, map[string]any) (cursorAuth, error) { return desktop, nil }

	out, err := c.fetch(context.Background(), map[string]any{cursorTeamSetting: cursorSelectedTeam}, nil, cursorTestNow)
	require.NoError(t, err)
	require.Equal(t, cursorSelectedTeam, out.TeamID)
	require.Equal(t, int32(2), identityCalls.Load())
}

func TestCursorSelectedTeamOnlyKeepsSummaryWithMatchingScope(t *testing.T) {
	for _, scope := range []string{`"teamId":30677937,`, `"teamId":"30677937",`, `"teamUsage":{"teamId":30677937},`, ``, `"teamId":30677936,`, `"teamId":null,`} {
		t.Run(scope, func(t *testing.T) {
			summary := `{` + scope + `"billingCycleEnd":"2026-10-02T00:00:00Z","individualUsage":{"plan":{"autoPercentUsed":2,"apiPercentUsed":100}}}`
			c := cursorTestClient(t, cursorTeamFixtureHandler(t, map[string]string{"/api/usage-summary": summary}, nil))
			out, err := c.fetch(context.Background(), map[string]any{cursorTeamSetting: cursorSelectedTeam}, cursorBase(t), cursorTestNow)
			require.NoError(t, err)
			if strings.Contains(scope, cursorSelectedTeam) {
				require.Len(t, out.Windows, 3)
				require.Equal(t, 2.0, out.Windows[1].UtilizationPct)
				require.Equal(t, 100.0, out.Windows[2].UtilizationPct)
				require.False(t, out.Windows[0].ResetAt.IsZero())
				require.Empty(t, out.DetailWarning)
			} else {
				require.Len(t, out.Windows, 1)
				require.True(t, out.Windows[0].ResetAt.IsZero())
			}
		})
	}
}

func TestCursorSelectedTeamRejectsMissingMembershipWrongTeamAndUnavailableUsage(t *testing.T) {
	for name, overrides := range map[string]map[string]string{
		"missing membership": {"/api/dashboard/teams": `{"teams":[{"id":30677936}]}`},
		"wrong team details": {"/api/dashboard/team": `{"teamId":30677936,"userId":42}`},
		"wrong team spend":   {"/api/dashboard/get-team-spend": `{"teamId":30677936,"teamMemberSpend":[]}`},
		"spend unavailable":  {"/api/dashboard/get-team-spend": "unavailable"},
		"no member match":    {"/api/dashboard/get-team-spend": `{"teamMemberSpend":[{"userId":9,"email":"cursor@example.test","fastPremiumRequests":99,"spendCents":999}]}`},
	} {
		t.Run(name, func(t *testing.T) {
			c := cursorTestClient(t, cursorTeamFixtureHandler(t, overrides, nil))
			out, err := c.fetch(context.Background(), map[string]any{cursorTeamSetting: cursorSelectedTeam}, cursorBase(t), cursorTestNow)
			require.Error(t, err)
			require.Nil(t, out)
			require.Contains(t, err.Error(), cursorSelectedTeam)
			require.NotContains(t, err.Error(), "DO-NOT-EXPOSE")
		})
	}
}

func TestCursorSelectedTeamDoesNotInferMissingQuotaOrAnotherMembersSpend(t *testing.T) {
	c := cursorTestClient(t, cursorTeamFixtureHandler(t, map[string]string{
		"/api/dashboard/teams":          `{"teams":[{"id":30677937,"name":"Selected team"}]}`,
		"/api/dashboard/team":           "unavailable",
		"/api/dashboard/get-team-spend": `{"teamMemberSpend":[{"email":"cursor@example.test","fastPremiumRequests":37,"spendCents":12345,"hardLimitOverrideDollars":250}]}`,
	}, nil))
	out, err := c.fetch(context.Background(), map[string]any{cursorTeamSetting: cursorSelectedTeam}, cursorBase(t), cursorTestNow)
	require.NoError(t, err)
	require.Empty(t, out.Windows, "do not infer 500 requests or reuse the other team's quota")
	require.Equal(t, &UsageSpend{Used: 123.45, Limit: floatPtr(250), Currency: "USD"}, out.ExtraUsage)
	_, err = cursorTeamMember(cursorDecode([]byte(`{"teamMemberSpend":[{"userId":42},{"userId":42}]}`)), 42, "")
	require.ErrorContains(t, err, "multiple usage entries")
}

func TestCursorSelectedTeamAcceptsCurrentMemberSpendShapes(t *testing.T) {
	t.Run("overall spend without legacy request fields", func(t *testing.T) {
		c := cursorTestClient(t, cursorTeamFixtureHandler(t, map[string]string{
			"/api/dashboard/teams": `{"teams":[{
				"id":30677937,"name":"Selected team","pricingStrategy":"tokens",
				"billingCycleStart":"2026-09-01T00:00:00Z",
				"billingCycleEnd":"2026-10-01T00:00:00Z"
			}]}`,
			"/api/dashboard/get-team-spend": `{"teamId":30677937,"teamMemberSpend":[{
				"userId":42,"email":"cursor@example.test","overallSpendCents":860,
				"monthlyLimitDollars":30,"hardLimitOverrideDollars":200,
				"effectivePerUserLimitDollars":30
			}]}`,
		}, nil))
		out, err := c.fetch(context.Background(), map[string]any{cursorTeamSetting: cursorSelectedTeam}, cursorBase(t), cursorTestNow)
		require.NoError(t, err)
		require.Len(t, out.Windows, 1)
		require.Equal(t, "Total Usage", out.Windows[0].Label)
		require.InDelta(t, 28.6666667, out.Windows[0].UtilizationPct, 1e-7)
		require.Equal(t, "$8.60 of $30.00 personal limit", out.Windows[0].Detail)
		require.Equal(t, "2026-10-01T00:00:00Z", out.Windows[0].ResetAt.Format(time.RFC3339))
		require.Equal(t, &UsageSpend{Used: 8.60, Limit: floatPtr(30), Currency: "USD", Label: "Total spend"}, out.ExtraUsage)
		require.Equal(t, &Pace{
			Stage: "behind", Summary: "3% in reserve | Expected 32% used",
		}, out.PacePrime)
		require.Equal(t, "Only selected-team usage is shown. Auto and API quotas were not reported for this team.", out.DetailWarning)
	})

	t.Run("tiered member percentages", func(t *testing.T) {
		c := cursorTestClient(t, cursorTeamFixtureHandler(t, map[string]string{
			"/api/dashboard/get-team-spend": `{"teamId":30677937,"teamMemberSpend":[{
				"userId":42,"email":"cursor@example.test","spendCents":0,
				"billingTier":"TIER_1000","totalPercentUsed":0.75,
				"autoPercentUsed":1.2,"apiPercentUsed":0
			}]}`,
		}, nil))
		out, err := c.fetch(context.Background(), map[string]any{cursorTeamSetting: cursorSelectedTeam}, cursorBase(t), cursorTestNow)
		require.NoError(t, err)
		require.Len(t, out.Windows, 3)
		require.InDelta(t, 0.75, out.Windows[0].UtilizationPct, 1e-9)
		require.InDelta(t, 1.2, out.Windows[1].UtilizationPct, 1e-9)
		require.Zero(t, out.Windows[2].UtilizationPct)
		require.Equal(t, &UsageSpend{Used: 0, Currency: "USD"}, out.ExtraUsage)
		require.Equal(t, "Only selected-team usage is shown. Reset times were not reported for this team.", out.DetailWarning)
		require.NotContains(t, out.DetailWarning, "Auto")
		require.NotContains(t, out.DetailWarning, "API")
	})
}

func TestCursorTeamMissingDetailsChecksQuotaRows(t *testing.T) {
	reset := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	require.Empty(t, cursorTeamMissingDetails(&ProviderUsage{Windows: []UtilizationWindow{
		{Label: "Total Usage", ResetAt: reset},
		{Label: "Auto Usage"},
		{Label: "API Usage"},
	}}))
	require.Equal(t,
		"Only selected-team usage is shown. Auto quota was not reported for this team.",
		cursorTeamMissingDetails(&ProviderUsage{Windows: []UtilizationWindow{
			{Label: "Total Usage", ResetAt: reset},
			{Label: "API Usage"},
		}}),
	)
}

func TestCursorConfiguredTeamID(t *testing.T) {
	for _, blank := range []any{nil, "", "  "} {
		id, err := cursorConfiguredTeamID(map[string]any{cursorTeamSetting: blank})
		require.NoError(t, err)
		require.Zero(t, id)
		require.False(t, cursorTeamConfigured(map[string]any{cursorTeamSetting: blank}))
	}
	id, err := cursorConfiguredTeamID(map[string]any{cursorTeamSetting: " 30677937 "})
	require.NoError(t, err)
	require.Equal(t, int64(30677937), id)
	for _, invalid := range []any{"0", "-1", "1.5", "3e7", "team=30677937", "9223372036854775808", true, 30677937.0, []any{}} {
		_, err := cursorConfiguredTeamID(map[string]any{cursorTeamSetting: invalid})
		require.Error(t, err)
	}
}

func TestCursorSelectedTeamFailureNeverFallsBackToCLIQuota(t *testing.T) {
	for _, polled := range []string{"cursor,claude", "claude"} {
		t.Run(polled, func(t *testing.T) {
			cfg := codexbarConfig(map[string]any{"codexbar_providers": polled, cursorTeamSetting: cursorSelectedTeam})
			p := newTestPlugin(t, cfg, []pluginsdk.Session{session("cursor-session", "Cursor Agent")},
				providerRunner(nil, map[string][]byte{"cursor": []byte(cursorEnterpriseCLI), "claude": []byte(sampleClaudeJSON)}))
			p.cursor = &cursorClient{auth: func(context.Context, map[string]any) (cursorAuth, error) {
				return cursorAuth{}, errors.New("Selected team's session unavailable")
			}}
			report := p.pollOnce(context.Background(), 0)
			require.Len(t, report.Providers, 1)
			require.Equal(t, "claude", report.Providers[0].Provider)
			if strings.Contains(polled, "cursor") {
				require.Len(t, report.Unavailable, 1)
				require.Equal(t, "cursor", report.Unavailable[0].Provider)
			}
			response, err := p.HandleWebhook(context.Background(), webhookGet(webhookKeySession, "task_id=task-1&active=cursor-session"))
			require.NoError(t, err)
			var sessionReport SessionUsageReport
			require.NoError(t, json.Unmarshal(response.Body, &sessionReport))
			require.Nil(t, sessionReport.Usage, "the session must not show the CLI's other-team usage")
			require.Contains(t, sessionReport.Error, "Selected team's session unavailable")
		})
	}
}

func TestCursorSelectedTeamWorksWhenCLICursorUsageIsUnavailable(t *testing.T) {
	cfg := codexbarConfig(map[string]any{"codexbar_providers": "cursor", cursorTeamSetting: cursorSelectedTeam})
	p := newTestPlugin(t, cfg, nil, providerRunner(nil, map[string][]byte{"cursor": []byte("[" + sampleCursorError + "]")}))
	p.cursor = cursorTestClient(t, cursorTeamFixtureHandler(t, nil, nil))
	report := p.pollOnce(context.Background(), 0)
	require.Empty(t, report.Unavailable)
	require.Len(t, report.Providers, 1)
	require.Equal(t, cursorSelectedTeam, report.Providers[0].TeamID)
	require.InDelta(t, 3.7, report.Providers[0].Windows[0].UtilizationPct, 1e-9)
}
