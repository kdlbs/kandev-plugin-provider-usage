package main

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCopilotCreditsFromCodexbarDetailContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/codexbar-v0.83.0-copilot-credits.json")
	require.NoError(t, err)
	entries, err := parseCodexbarUsage(raw)
	require.NoError(t, err)
	usage := entries[0].toProviderUsage(time.Now())
	require.Equal(t, 3313.0, usage.CopilotCredits.Used)
	require.Equal(t, 25000.0, *usage.CopilotCredits.Allowance)
	require.Equal(t, "codexbar", usage.CopilotCredits.AllowanceSource)
	require.Equal(t, "2026-12-01T00:00:00Z", usage.CopilotCredits.ResetAt.Format(time.RFC3339))
	require.Equal(t, 14.0, usage.Windows[0].UtilizationPct, "credit detail must not rewrite the reported quota")
}

func TestCopilotCreditsRequireNumericSourceData(t *testing.T) {
	for _, tc := range []struct {
		name, details string
		wantUsed      *float64
		wantAllowance *float64
	}{
		{"used without allowance", `[{"rows":[{"id":"copilot-seat-credits","usageValue":3313}]}]`, floatPtr(3313), nil},
		{"reported zero", `[{"rows":[{"id":"copilot-seat-credits","usageValue":0}]}]`, floatPtr(0), nil},
		{"progress only", `[{"rows":[{"id":"copilot-seat-credits","progress":{"used":3313,"total":25000}}]}]`, floatPtr(3313), floatPtr(25000)},
		{"rounded text only", `[{"rows":[{"id":"copilot-seat-credits","value":"3,313 / 25,000"}]}]`, nil, nil},
		{"unrelated credit pool", `[{"rows":[{"id":"other-credits","usageValue":99}]}]`, nil, nil},
		{"invalid optional details", `{"rows":"unavailable"}`, nil, nil},
		{"invalid counter", `[{"rows":[{"id":"copilot-seat-credits","usageValue":-1}]}]`, nil, nil},
		{"invalid total", `[{"rows":[{"id":"copilot-seat-credits","progress":{"used":3313,"total":0}}]}]`, floatPtr(3313), nil},
		{"inconsistent progress", `[{"rows":[{"id":"copilot-seat-credits","usageValue":3313,"progress":{"used":10,"total":100}}]}]`, floatPtr(3313), nil},
		{"missing data", `[]`, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries, err := parseCodexbarUsage([]byte(`[{"provider":"copilot","usage":{"primary":{"usedPercent":14},"details":` + tc.details + `}}]`))
			require.NoError(t, err, "optional details must not break existing quota parsing")
			usage := entries[0].toProviderUsage(time.Now())
			require.Equal(t, 14.0, usage.Windows[0].UtilizationPct)
			if tc.wantUsed == nil {
				require.Nil(t, usage.CopilotCredits)
				return
			}
			require.Equal(t, *tc.wantUsed, usage.CopilotCredits.Used)
			require.Equal(t, tc.wantAllowance, usage.CopilotCredits.Allowance)
		})
	}
}

func TestCopilotAllowanceAppliedByPollWithoutInventingQuota(t *testing.T) {
	for _, tc := range []struct {
		name string
		used float64
	}{
		{"credit counter", 3313}, {"over allowance", 26000}, {"observed zero", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPlugin(t, codexbarConfig(map[string]any{
				configKeyProviders: "copilot", configKeyCopilotAllowance: 25000.0,
			}), nil, func(_ context.Context, _ string, args ...string) ([]byte, error) {
				if args[len(args)-1] == "--version" {
					return []byte("CodexBar 0.83.0"), nil
				}
				payload := []any{map[string]any{"provider": "copilot", "usage": map[string]any{
					"copilotMeteredZeroCredits": tc.used == 0,
					"details": []any{map[string]any{"rows": []any{map[string]any{
						"id": "copilot-seat-credits", "usageValue": tc.used,
					}}}},
				}}}
				return json.Marshal(payload)
			})
			report := p.pollOnce(context.Background(), 0)
			require.Len(t, report.Providers, 1)
			usage := report.Providers[0]
			require.Equal(t, tc.used, usage.CopilotCredits.Used)
			require.Equal(t, 25000.0, *usage.CopilotCredits.Allowance)
			require.Equal(t, "settings", usage.CopilotCredits.AllowanceSource)
			require.Empty(t, usage.Windows, "a configured allowance is not a confirmed provider quota")
		})
	}
}

func TestCopilotAllowanceValidationAndObservedZero(t *testing.T) {
	for _, allowance := range []any{nil, "25000", -1.0, 0.0, math.NaN(), math.Inf(1)} {
		usage := &ProviderUsage{Provider: "copilot", CopilotCredits: &CopilotCredits{Used: 3313}}
		applyCopilotAllowance(usage, map[string]any{configKeyCopilotAllowance: allowance})
		require.Nil(t, usage.CopilotCredits.Allowance)
	}
	entries, err := parseCodexbarUsage([]byte(`[{"provider":"copilot","usage":{"copilotMeteredZeroCredits":true}}]`))
	require.NoError(t, err)
	require.Equal(t, 0.0, entries[0].toProviderUsage(time.Now()).CopilotCredits.Used)
	usage := &ProviderUsage{Provider: "copilot"}
	applyCopilotAllowance(usage, map[string]any{configKeyCopilotAllowance: 25000.0})
	require.Nil(t, usage.CopilotCredits, "a configured total alone is not evidence of zero consumption")
}
