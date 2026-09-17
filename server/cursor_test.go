package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var cursorTestNow = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

// Same shape as the reported single-window Enterprise CLI response, with
// synthetic account identifiers. The secondary/tertiary fields really are nil.
const cursorEnterpriseCLI = `[{"provider":"cursor","source":"web","usage":{
  "accountEmail":"cursor@example.test","identity":{"accountID":"user_test","accountEmail":"cursor@example.test"},
  "loginMethod":"Cursor Enterprise","primary":{"resetsAt":"2026-10-02T00:00:00Z","usedPercent":2.6216666666666666,"windowMinutes":43200},
  "secondary":null,"tertiary":null,"updatedAt":"2026-09-10T12:00:00Z"}}]`

const cursorEnterpriseSummary = `{
  "membershipType":"enterprise","billingCycleStart":"2026-09-02T00:00:00Z","billingCycleEnd":"2026-10-02T00:00:00Z",
  "individualUsage":{"plan":{"enabled":true,"autoPercentUsed":0,"apiPercentUsed":6.25,"totalPercentUsed":6.25},
    "onDemand":{"enabled":true,"used":0,"limit":25000,"remaining":25000}},
  "teamUsage":{"onDemand":{"enabled":true,"used":75000,"limit":600000,"remaining":525000}}}`

const cursorRequestsFixture = `{"gpt-4":{"numRequests":37,"numRequestsTotal":50,"maxRequestUsage":750}}`

func cursorBase(t *testing.T) *ProviderUsage {
	t.Helper()
	entries, err := parseCodexbarUsage([]byte(cursorEnterpriseCLI))
	require.NoError(t, err)
	return entries[0].toProviderUsage(cursorTestNow)
}

func TestCursorEnterpriseCombinesRequestsBreakdownAndPersonalSpend(t *testing.T) {
	base := cursorBase(t)
	out := mapCursorUsage(base, cursorDecode([]byte(cursorEnterpriseSummary)), nil, cursorDecode([]byte(cursorRequestsFixture)), cursorTestNow)
	require.Equal(t, "Cursor Enterprise", out.Plan)
	require.Len(t, out.Windows, 3)
	require.Equal(t, "Total Usage", out.Windows[0].Label)
	require.InDelta(t, 37.0/750*100, out.Windows[0].UtilizationPct, 1e-9)
	require.Equal(t, "37 of 750 included requests", out.Windows[0].Detail)
	require.Equal(t, "Auto Usage", out.Windows[1].Label)
	require.Zero(t, out.Windows[1].UtilizationPct)
	require.True(t, out.Windows[1].Scoped)
	require.Equal(t, "API Usage", out.Windows[2].Label)
	require.Equal(t, 6.25, out.Windows[2].UtilizationPct)
	require.True(t, out.Windows[2].Scoped)
	for _, window := range out.Windows {
		require.Equal(t, "2026-10-02T00:00:00Z", window.ResetAt.Format(time.RFC3339))
	}
	require.Equal(t, &UsageSpend{Used: 0, Limit: floatPtr(250), Currency: "USD"}, out.ExtraUsage)
	require.Len(t, base.Windows, 1, "enrichment must not mutate a shared cached payload")
	require.InDelta(t, 2.6216666666666666, base.Windows[0].UtilizationPct, 1e-12)
}

func floatPtr(n float64) *float64 { return &n }

func TestCursorDashboardUsageAndSpendUnits(t *testing.T) {
	rpc := cursorDecode([]byte(`{"enabled":true,"billingCycleEnd":"1790899200000",
    "planUsage":{"totalPercentUsed":27,"autoPercentUsed":2,"apiPercentUsed":100},
    "spendLimitUsage":{"individualUsed":"36404"}}`))
	out := mapCursorUsage(nil, nil, rpc, nil, cursorTestNow)
	require.Len(t, out.Windows, 3)
	require.Equal(t, []float64{27, 2, 100}, []float64{out.Windows[0].UtilizationPct, out.Windows[1].UtilizationPct, out.Windows[2].UtilizationPct})
	require.Equal(t, "2026-10-02T00:00:00Z", out.Windows[0].ResetAt.Format(time.RFC3339))
	require.Equal(t, &UsageSpend{Used: 364.04, Currency: "USD"}, out.ExtraUsage)
	// Percentages below one are already percent units, not fractions to scale.
	out = mapCursorUsage(nil, cursorDecode([]byte(`{"individualUsage":{"plan":{"totalPercentUsed":0.36,"autoPercentUsed":0}}}`)), nil, nil, cursorTestNow)
	require.Equal(t, 0.36, out.Windows[0].UtilizationPct)
	require.Len(t, out.Windows, 2, "absent API usage must not become 0%")
}

func TestCursorMissingInvalidAndUnlimitedBuckets(t *testing.T) {
	for _, raw := range []string{
		`{}`, `{"individualUsage":{"plan":{"limit":0,"used":10}}}`,
		`{"individualUsage":{"plan":{"totalPercentUsed":null,"autoPercentUsed":"NaN","apiPercentUsed":false}}}`,
		`{"individualUsage":{"plan":{"enabled":false,"totalPercentUsed":10},"onDemand":{"enabled":false,"used":100}}}`,
	} {
		out := mapCursorUsage(nil, cursorDecode([]byte(raw)), nil, nil, cursorTestNow)
		require.Empty(t, out.Windows, raw)
		require.Nil(t, out.ExtraUsage, raw)
	}
	// Remaining alone is insufficient to invent a budget or spend.
	require.Nil(t, cursorSpend(cursorDecode([]byte(`{"remaining":0}`)), "used", "limit", "remaining", ""))
	spend := cursorSpend(cursorDecode([]byte(`{"used":1000,"limit":0}`)), "used", "limit", "remaining", "")
	require.Equal(t, &UsageSpend{Used: 10, Currency: "USD"}, spend)
}

func TestCursorEnterprisePlaceholderCycleKeepsKnownReset(t *testing.T) {
	base := cursorBase(t)
	rpc := cursorDecode([]byte(`{"billingCycleStart":"1789041600000","billingCycleEnd":"1789041600000","displayThreshold":100}`))
	out := mapCursorUsage(base, nil, rpc, nil, cursorTestNow)
	require.Equal(t, base.Windows, out.Windows)
}

func TestCursorSharedBucketsAndPersonalZero(t *testing.T) {
	for _, personal := range []string{`null`, `{"enabled":false,"used":0,"limit":0}`} {
		summary := cursorDecode([]byte(`{"individualUsage":{"onDemand":` + personal + `},"teamUsage":{
      "pooled":{"used":125000,"limit":4000000},"onDemand":{"used":50000,"limit":500000}}}`))
		out := mapCursorUsage(nil, summary, nil, nil, cursorTestNow)
		require.InDelta(t, 3.125, out.Windows[0].UtilizationPct, 1e-9)
		require.Contains(t, out.Windows[0].Detail, "shared team")
		require.Equal(t, &UsageSpend{Used: 500, Limit: floatPtr(5000), Currency: "USD", Scope: "team"}, out.ExtraUsage)
	}
	rpc := cursorDecode([]byte(`{"spendLimitUsage":{"individualUsed":0,"individualLimit":1000,"pooledUsed":90000,"totalSpend":90000}}`))
	out := mapCursorUsage(cursorBase(t), nil, rpc, nil, cursorTestNow)
	require.Zero(t, out.ExtraUsage.Used)
	require.Empty(t, out.ExtraUsage.Scope)
}

func TestCursorCodexbarBreakdownAndProviderCost(t *testing.T) {
	raw := `[{
    "provider":"cursor","usage":{"primary":{"usedPercent":27},"secondary":{"usedPercent":2},"tertiary":{"usedPercent":100},
    "providerCost":{"used":364.04,"limit":500,"currencyCode":"USD"}}}]`
	entries, err := parseCodexbarUsage([]byte(raw))
	require.NoError(t, err)
	out := entries[0].toProviderUsage(cursorTestNow)
	require.Equal(t, "Total Usage", out.Windows[0].Label)
	require.True(t, out.Windows[1].Scoped)
	require.True(t, out.Windows[2].Scoped)
	require.Equal(t, 364.04, out.ExtraUsage.Used, "CodexBar cost is already dollars")
	require.Equal(t, 500.0, *out.ExtraUsage.Limit)
	require.Nil(t, cursorProviderCost([]byte(`{"used":"bad","currencyCode":"USD"}`)))
	personal := cursorProviderCost([]byte(`{"used":900,"limit":1000,"personalUsed":0,"currencyCode":"USD"}`))
	require.Zero(t, personal.Used)
	require.Nil(t, personal.Limit, "a team limit cannot become a personal limit")
}

func cursorTestClient(t *testing.T, handler http.HandlerFunc) *cursorClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := newCursorClient()
	c.webBase, c.rpcBase = srv.URL, srv.URL
	c.auth = func(context.Context, map[string]any) (cursorAuth, error) {
		return cursorBearerAuth("eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"auth0|user_test"}`)) + ".fixture")
	}
	return c
}

func cursorFixtureHandler(counter *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if counter != nil {
			counter.Add(1)
		}
		body := "{}"
		switch r.URL.Path {
		case "/api/auth/me":
			body = `{"sub":"user_test","email":"cursor@example.test"}`
		case "/api/usage-summary":
			body = cursorEnterpriseSummary
		case "/api/usage":
			body = cursorRequestsFixture
		}
		_, _ = w.Write([]byte(body))
	}
}

func TestCursorFetchEnrichesOnlySameAccount(t *testing.T) {
	var count atomic.Int32
	c := cursorTestClient(t, cursorFixtureHandler(&count))
	out, err := c.fetch(context.Background(), nil, cursorBase(t), cursorTestNow)
	require.NoError(t, err)
	require.Len(t, out.Windows, 3)
	require.Empty(t, out.DetailWarning)
	require.Equal(t, int32(4), count.Load())
	base := cursorBase(t)
	base.accountID = "another-account"
	_, err = c.fetch(context.Background(), nil, base, cursorTestNow)
	require.ErrorContains(t, err, "different or unverified account")
	require.Equal(t, int32(5), count.Load(), "mismatched accounts stop before all detail calls")
	base.accountID, base.accountEmail = "", ""
	_, err = c.fetch(context.Background(), nil, base, cursorTestNow)
	require.ErrorContains(t, err, "unverified account")
}

func TestCursorFetchFallsBackToAgentCLIAuth(t *testing.T) {
	var identityCalls atomic.Int32
	c := cursorTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/me" {
			identityCalls.Add(1)
			if strings.Contains(r.Header.Get("Cookie"), "stale_user") {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		cursorFixtureHandler(nil)(w, r)
	})
	stale, err := cursorBearerAuth(cursorAuthFixtureFor("stale_user"))
	require.NoError(t, err)
	agent, err := cursorBearerAuth(cursorAuthFixtureFor("user_test"))
	require.NoError(t, err)
	stale.alternates = []cursorAuth{agent}
	c.auth = func(context.Context, map[string]any) (cursorAuth, error) { return stale, nil }

	out, err := c.fetch(context.Background(), nil, cursorBase(t), cursorTestNow)
	require.NoError(t, err)
	require.Len(t, out.Windows, 3)
	require.Equal(t, int32(2), identityCalls.Load())
}

func TestCursorFetchDoesNotSwitchAccountsAfterPrimaryUsageFailure(t *testing.T) {
	var agentIdentityCalls atomic.Int32
	c := cursorTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		desktop := strings.Contains(r.Header.Get("Cookie"), "desktop_user")
		switch r.URL.Path {
		case "/api/auth/me":
			if desktop {
				_, _ = w.Write([]byte(`{"sub":"desktop_user","email":"desktop@example.test"}`))
				return
			}
			agentIdentityCalls.Add(1)
			cursorFixtureHandler(nil)(w, r)
		case "/api/usage-summary", "/api/usage", "/aiserver.v1.DashboardService/GetCurrentPeriodUsage":
			if desktop || strings.HasPrefix(r.URL.Path, "/aiserver") {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			cursorFixtureHandler(nil)(w, r)
		default:
			cursorFixtureHandler(nil)(w, r)
		}
	})
	desktop, err := cursorBearerAuth(cursorAuthFixtureFor("desktop_user"))
	require.NoError(t, err)
	agent, err := cursorBearerAuth(cursorAuthFixtureFor("user_test"))
	require.NoError(t, err)
	desktop.alternates = []cursorAuth{agent}
	c.auth = func(context.Context, map[string]any) (cursorAuth, error) { return desktop, nil }

	_, err = c.fetch(context.Background(), nil, nil, cursorTestNow)
	require.ErrorContains(t, err, "did not return usable account usage")
	require.Zero(t, agentIdentityCalls.Load(), "a quota failure must not silently switch to another account")
}

func TestCursorFetchRetriesDownstreamRejectionOnlyForSameAccount(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		agentUser string
		wantErr   string
	}{
		{name: "same account", agentUser: "desktop_user"},
		{name: "different account", agentUser: "agent_user", wantErr: "signed in to different accounts"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var identityCalls atomic.Int32
			c := cursorTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/auth/me" {
					call := identityCalls.Add(1)
					user := "desktop_user"
					if call > 1 {
						user = testCase.agentUser
					}
					_, _ = w.Write([]byte(`{"sub":"` + user + `","email":"` + user + `@example.test"}`))
					return
				}
				if identityCalls.Load() == 1 && r.URL.Path != "/api/usage-summary" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				cursorFixtureHandler(nil)(w, r)
			})
			desktop, err := cursorBearerAuth(cursorAuthFixtureFor("desktop_user"))
			require.NoError(t, err)
			agent, err := cursorBearerAuth(cursorAuthFixtureFor(testCase.agentUser))
			require.NoError(t, err)
			desktop.alternates = []cursorAuth{agent}
			c.auth = func(context.Context, map[string]any) (cursorAuth, error) { return desktop, nil }

			out, err := c.fetch(context.Background(), nil, nil, cursorTestNow)
			if testCase.wantErr != "" {
				require.ErrorContains(t, err, testCase.wantErr)
				require.Nil(t, out)
			} else {
				require.NoError(t, err)
				require.Len(t, out.Windows, 3)
			}
			require.Equal(t, int32(2), identityCalls.Load())
		})
	}
}

func TestCursorFetchReportsAgentLoadErrorOnlyWhenFallbackIsNeeded(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		statusCode int
		want       string
	}{
		{name: "desktop rejected", statusCode: http.StatusUnauthorized, want: "Agent CLI saved session is unreadable"},
		{name: "desktop service failure", statusCode: http.StatusServiceUnavailable, want: "Cursor returned HTTP 503"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			c := cursorTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(testCase.statusCode)
			})
			desktop, err := cursorBearerAuth(cursorAuthFixtureFor("desktop_user"))
			require.NoError(t, err)
			desktop.alternateErr = errors.New("Cursor Agent CLI saved session is unreadable")
			c.auth = func(context.Context, map[string]any) (cursorAuth, error) { return desktop, nil }

			_, err = c.fetch(context.Background(), nil, nil, cursorTestNow)
			require.ErrorContains(t, err, testCase.want)
		})
	}
}

func TestCursorOptionalFailuresKeepQuotaAndDoNotLeakResponseBodies(t *testing.T) {
	c := cursorTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/me" {
			cursorFixtureHandler(nil)(w, r)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("credential=DO-NOT-EXPOSE"))
	})
	base := cursorBase(t)
	out, err := c.fetch(context.Background(), nil, base, cursorTestNow)
	require.NoError(t, err)
	require.Equal(t, base.Windows, out.Windows)
	require.Equal(t, "Detailed quotas unavailable; showing the available quota.", out.DetailWarning)
	require.NotContains(t, out.DetailWarning, "DO-NOT-EXPOSE")
}

func TestCursorCookieBearerMustMatchVerifiedWebUser(t *testing.T) {
	var rpcCalls atomic.Int32
	c := cursorTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/aiserver") {
			rpcCalls.Add(1)
		}
		if r.URL.Path == "/api/auth/me" {
			_, _ = w.Write([]byte(`{"sub":"different-user","email":"other@example.test"}`))
			return
		}
		cursorFixtureHandler(nil)(w, r)
	})
	_, err := c.fetch(context.Background(), nil, nil, cursorTestNow)
	require.NoError(t, err)
	require.Zero(t, rpcCalls.Load(), "do not mix RPC data from a second cookie's account")
}

func TestCursorRequestsHaveCorrectAuthScopeAndUser(t *testing.T) {
	c := cursorTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/aiserver") {
			if r.Method != http.MethodPost || r.Header.Get("Connect-Protocol-Version") != "1" || r.Header.Get("Authorization") == "" || r.Header.Get("Cookie") != "" {
				t.Error("incorrect RPC request auth or method")
			}
		} else if r.Method != http.MethodGet || r.Header.Get("Cookie") == "" || r.Header.Get("Authorization") != "" {
			t.Error("incorrect REST request auth or method")
		}
		switch r.URL.Path {
		case "/api/auth/me", "/api/usage-summary", "/aiserver.v1.DashboardService/GetCurrentPeriodUsage":
		case "/api/usage":
			if r.URL.Query().Get("user") != "user_test" {
				t.Error("usage request must identify the verified user")
			}
		default:
			t.Errorf("unexpected Cursor request: %s", r.URL.Path)
		}
		cursorFixtureHandler(nil)(w, r)
	})
	_, err := c.fetch(context.Background(), nil, cursorBase(t), cursorTestNow)
	require.NoError(t, err)
}

func TestCursorRedirectDoesNotForwardCredentials(t *testing.T) {
	var reached atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Add(1) }))
	defer destination.Close()
	c := cursorTestClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) })
	_, err := c.fetch(context.Background(), nil, cursorBase(t), cursorTestNow)
	require.ErrorContains(t, err, "HTTP 302")
	require.Zero(t, reached.Load())
}

func TestCursorEnrichmentUsesSharedSnapshotAndHonorsAllowlist(t *testing.T) {
	var count atomic.Int32
	p := newTestPlugin(t, codexbarConfig(map[string]any{"codexbar_providers": "cursor"}), nil, providerRunner(nil, map[string][]byte{"cursor": []byte(cursorEnterpriseCLI)}))
	p.now = func() time.Time { return cursorTestNow }
	p.cursor = cursorTestClient(t, cursorFixtureHandler(&count))
	report := p.pollOnce(context.Background(), 0)
	require.Len(t, report.Providers, 1)
	require.Len(t, report.Providers[0].Windows, 3)
	_, err := p.HandleWebhook(context.Background(), webhookGet(webhookKeyProviders, ""))
	require.NoError(t, err)
	require.Equal(t, int32(4), count.Load(), "webhooks must reuse the warm snapshot")
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "cursor@example.test")
	require.NotContains(t, string(encoded), "fixture")

	p = newTestPlugin(t, codexbarConfig(map[string]any{"codexbar_providers": "claude"}), nil, providerRunner(nil, map[string][]byte{"claude": []byte(sampleClaudeJSON)}))
	p.cursor = cursorTestClient(t, cursorFixtureHandler(&count))
	p.pollOnce(context.Background(), 0)
	require.Equal(t, int32(4), count.Load())
}

func TestCursorDirectUsageSurvivesCodexbarInstallFailure(t *testing.T) {
	var codexbarCalls, cursorCalls atomic.Int32
	p := newTestPlugin(t, codexbarConfig(map[string]any{"codexbar_providers": "cursor"}), nil,
		func(context.Context, string, ...string) ([]byte, error) {
			codexbarCalls.Add(1)
			return nil, errors.New("exec: codexbar: not found")
		})
	p.now = func() time.Time { return cursorTestNow }
	p.cursor = cursorTestClient(t, cursorFixtureHandler(&cursorCalls))

	report := p.pollOnce(context.Background(), 0)

	require.False(t, report.Codexbar.Installed)
	require.Equal(t, int32(1), codexbarCalls.Load(), "only the failed version probe reaches codexbar")
	require.Equal(t, int32(4), cursorCalls.Load(), "identity and detail APIs are fetched directly")
	require.Empty(t, report.Unavailable)
	require.Len(t, report.Providers, 1)
	require.Equal(t, "cursor", report.Providers[0].Provider)
	require.Len(t, report.Providers[0].Windows, 3)
}

func TestCursorDirectFailureSurvivesCodexbarInstallFailure(t *testing.T) {
	p := newTestPlugin(t, codexbarConfig(map[string]any{"codexbar_providers": "cursor"}), nil,
		func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("exec: codexbar: not found")
		})
	p.cursor = &cursorClient{auth: func(context.Context, map[string]any) (cursorAuth, error) {
		return cursorAuth{}, errors.New(cursorDetailHint)
	}}

	report := p.pollOnce(context.Background(), 0)

	require.False(t, report.Codexbar.Installed)
	require.Empty(t, report.Providers)
	require.Equal(t, []ProviderError{{Provider: "cursor", Message: cursorDetailHint}}, report.Unavailable)
}

func TestCursorAuthFailurePreservesBaselineAndOtherProviders(t *testing.T) {
	p := newTestPlugin(t, codexbarConfig(map[string]any{"codexbar_providers": "cursor,claude"}), nil,
		providerRunner(nil, map[string][]byte{"cursor": []byte(cursorEnterpriseCLI), "claude": []byte(sampleClaudeJSON)}))
	p.cursor = &cursorClient{auth: func(context.Context, map[string]any) (cursorAuth, error) {
		return cursorAuth{}, errors.New(cursorDetailHint)
	}}
	report := p.pollOnce(context.Background(), 0)
	require.Len(t, report.Providers, 2)
	require.Empty(t, report.Unavailable)
	require.Len(t, report.Providers[0].Windows, 1)
	require.Equal(t, cursorDetailHint, report.Providers[0].DetailWarning)
}
