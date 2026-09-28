package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCursorDailySpend(t *testing.T) {
	for _, team := range []bool{false, true} {
		t.Run(fmt.Sprint("team=", team), func(t *testing.T) {
			for _, tc := range []struct {
				name  string
				pages []string
				want  *float64
			}{
				{"paginated costs", []string{
					`{"totalUsageEventsCount":3,"usageEventsDisplay":[{"timestamp":"1789041600000","chargedCents":123,"tokenUsage":{"totalCents":999},"owningTeam":"30677937","owningUser":"42"}]}`,
					`{"totalUsageEventsCount":3,"usageEventsDisplay":[{"timestamp":"1788998400000","tokenUsage":{"totalCents":"25.5"},"cursorTokenFee":1.5,"owningTeam":"30677937","owningUser":"42"},{"timestamp":"1788998399999","chargedCents":500,"owningTeam":"30677937","owningUser":"42"}]}`,
				}, floatPtr(1.50)},
				{"empty day", []string{`{"totalUsageEventsCount":0,"usageEventsDisplay":[]}`}, floatPtr(0)},
				{"reported zero", []string{`{"totalUsageEventsCount":1,"usageEventsDisplay":[{"timestamp":"1789041600000","chargedCents":0}]}`}, floatPtr(0)},
				{"missing costs", []string{`{"totalUsageEventsCount":1,"usageEventsDisplay":[{"timestamp":"1789041600000"}]}`}, nil},
				{"malformed", []string{`{}`}, nil},
				{"null events", []string{`{"totalUsageEventsCount":0,"usageEventsDisplay":null}`}, nil},
				{"incomplete pages", []string{`{"totalUsageEventsCount":2,"usageEventsDisplay":[{"timestamp":"1789041600000","chargedCents":100}]}`, `{"totalUsageEventsCount":2,"usageEventsDisplay":[]}`}, nil},
				{"endpoint unavailable", []string{"unavailable"}, nil},
				{"invalid timestamp", []string{`{"totalUsageEventsCount":1,"usageEventsDisplay":[{"chargedCents":100}]}`}, nil},
				{"invalid cost", []string{`{"totalUsageEventsCount":1,"usageEventsDisplay":[{"timestamp":"1789041600000","chargedCents":-1}]}`}, nil},
			} {
				t.Run(tc.name, func(t *testing.T) {
					calls := 0
					fixture := cursorFixtureHandler(nil)
					if team {
						fixture = cursorTeamFixtureHandler(t, nil, nil)
					}
					c := cursorTestClient(t, func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path != "/api/dashboard/get-filtered-usage-events" {
							fixture(w, r)
							return
						}
						calls++
						require.Equal(t, http.MethodPost, r.Method)
						require.NotEmpty(t, r.Header.Get("Cookie"))
						require.NotEmpty(t, r.Header.Get("Origin"))
						var body map[string]json.RawMessage
						require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
						require.Equal(t, strconv.Itoa(calls), string(body["page"]))
						require.JSONEq(t, `"1788998400000"`, string(body["startDate"]))
						require.JSONEq(t, `"1789041600000"`, string(body["endDate"]))
						if team {
							require.Equal(t, cursorSelectedTeam, string(body["teamId"]))
							require.Equal(t, "42", string(body["userId"]))
						} else {
							require.NotContains(t, body, "teamId")
							require.NotContains(t, body, "userId")
						}
						if calls > len(tc.pages) || tc.pages[calls-1] == "unavailable" {
							w.WriteHeader(503)
							return
						}
						_, _ = w.Write([]byte(tc.pages[calls-1]))
					})
					cfg := map[string]any{}
					if team {
						cfg[cursorTeamSetting] = cursorSelectedTeam
					}
					// The day is explicitly UTC, independent of the server's local timezone.
					out, err := c.fetch(context.Background(), cfg, cursorBase(t), cursorTestNow.In(time.FixedZone("test", -7*3600)))
					require.NoError(t, err, "optional daily data must not break quotas")
					require.NotEmpty(t, out.Windows)
					raw, err := json.Marshal(out)
					require.NoError(t, err)
					var payload struct {
						Daily *struct {
							Used     float64   `json:"used"`
							Currency string    `json:"currency"`
							Start    time.Time `json:"start_at"`
							End      time.Time `json:"end_at"`
						} `json:"daily_usage"`
					}
					require.NoError(t, json.Unmarshal(raw, &payload))
					require.Positive(t, calls, "daily usage endpoint must be requested")
					if tc.want == nil {
						require.Nil(t, payload.Daily)
						return
					}
					require.NotNil(t, payload.Daily)
					require.InDelta(t, *tc.want, payload.Daily.Used, 1e-9)
					require.Equal(t, "USD", payload.Daily.Currency)
					require.Equal(t, "2026-09-10T00:00:00Z", payload.Daily.Start.Format(time.RFC3339))
					require.Equal(t, "2026-09-11T00:00:00Z", payload.Daily.End.Format(time.RFC3339))
				})
			}
		})
	}
}

func TestCursorDailyUsageRejectsWrongTeamOrMember(t *testing.T) {
	for _, field := range []string{"owningTeam", "owningUser"} {
		t.Run(field, func(t *testing.T) {
			c := cursorTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"totalUsageEventsCount":1,"usageEventsDisplay":[{"timestamp":"1789041600000","chargedCents":100,"%s":"999"}]}`, field)
			})
			require.Nil(t, c.fetchDailyUsage(context.Background(), cursorAuth{}, 30677937, 42, cursorTestNow))
		})
	}
}

func TestCursorDailyUsageRequiresTeamMemberIdentity(t *testing.T) {
	c := cursorTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("must not request whole-team daily spend") })
	require.Nil(t, c.fetchDailyUsage(context.Background(), cursorAuth{}, 30677937, 0, cursorTestNow))
}

func TestCursorDailyUsageRejectsChangingOrExcessiveHistory(t *testing.T) {
	for _, count := range []int{2001, 1, 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			calls := 0
			c := cursorTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				n := count
				if calls > 1 {
					n++
				}
				_, _ = fmt.Fprintf(w, `{"totalUsageEventsCount":%d,"usageEventsDisplay":[{"timestamp":"1789041600000","chargedCents":100},{"timestamp":"1789041600000","chargedCents":200}]}`, n)
			})
			require.Nil(t, c.fetchDailyUsage(context.Background(), cursorAuth{}, 0, 0, cursorTestNow))
		})
	}
}
