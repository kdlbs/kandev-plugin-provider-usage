package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Upstream CodexBar nests this inventory under usage.codexResetCredits. Credit
// properties retain the provider's snake_case spelling inside that object.
const sampleResetCredits = `{"availableCount":3,"credits":[
  {"id":"private-credit-id","status":"available","expires_at":"2026-09-12T12:00:00.123Z","title":"Full reset"},
  {"status":"available","expires_at":"2026-09-10T12:00:00Z"},
  {"status":"available","expires_at":null},
  {"status":"available","expires_at":"2026-09-08T12:00:00Z"},
  {"status":"redeemed","expires_at":"2026-09-11T12:00:00Z"}
],"updatedAt":"2026-09-09T12:00:00Z"}`

func TestCodexResetCredits_SurviveProviderResponse(t *testing.T) {
	raw := `[{"provider":"codex","source":"oauth","usage":{
    "primary":{"usedPercent":42,"windowMinutes":300},
    "codexResetCredits":` + sampleResetCredits + `}}]`
	entries, err := parseCodexbarUsage([]byte(raw))
	require.NoError(t, err)
	u := entries[0].toProviderUsage(time.Now())
	require.Len(t, u.Windows, 1, "reset credits are not a quota window")
	blob, err := json.Marshal(u)
	require.NoError(t, err)
	var response map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(blob, &response))
	require.Contains(t, response, "reset_credits")
	require.JSONEq(t, `{"available_count":3,"credits":[
    {"status":"available","expires_at":"2026-09-12T12:00:00.123Z"},
    {"status":"available","expires_at":"2026-09-10T12:00:00Z"},
    {"status":"available"},
    {"status":"available","expires_at":"2026-09-08T12:00:00Z"},
    {"status":"redeemed","expires_at":"2026-09-11T12:00:00Z"}
  ]}`, string(response["reset_credits"]))
	require.NotContains(t, string(blob), "private-credit-id", "only display data leaves the backend")
}

func TestCodexResetCredits_OptionalInventory(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"count only", `{"availableCount":2}`, `{"available_count":2}`},
		{"zero", `{"availableCount":0,"credits":[]}`, `{"available_count":0}`},
		{"snake case", `{"available_count":2}`, `{"available_count":2}`},
		{"unknown expiry", `{"availableCount":1,"credits":[{"status":"available","expires_at":"bad date"}]}`, `{"available_count":1,"credits":[{"status":"available"}]}`},
		{"absent", `null`, ""},
		{"malformed count", `{"availableCount":"many"}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := `[{"provider":"codex","usage":{"primary":{"usedPercent":42},"codexResetCredits":` + tc.data + `}},` + sampleClaudeInner() + `]`
			entries, err := parseCodexbarUsage([]byte(raw))
			require.NoError(t, err, "optional reset data must not discard healthy quota data")
			require.Len(t, entries, 2)
			blob, err := json.Marshal(entries[0].toProviderUsage(time.Now()))
			require.NoError(t, err)
			var response map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(blob, &response))
			if tc.want == "" {
				require.NotContains(t, response, "reset_credits")
			} else {
				require.Contains(t, response, "reset_credits")
				require.JSONEq(t, tc.want, string(response["reset_credits"]))
			}
		})
	}
}

func TestCodexResetCredits_WindowsInformationalWindow(t *testing.T) {
	// Win-CodexBar v0.56.8 emits a summary and the next expiry, not an inventory.
	const raw = `[{"provider":"codex","source":"oauth","usage":{
    "primary":{"used_percent":42,"window_minutes":300},
    "extra_rate_windows":[
      {"id":"reset-credits","title":"Reset credits","window":{
        "is_informational":true,"used_percent":0,"reset_description":"2 reset credits available",
        "resets_at":"2026-09-10T12:00:00Z"}},
      {"id":"spark","title":"Spark","window":{"used_percent":99}}
    ]}}]`
	entries, err := parseCodexbarUsage([]byte(raw))
	require.NoError(t, err)
	u := entries[0].toProviderUsage(time.Now())
	require.Len(t, u.Windows, 2, "the summary is not rendered as 0% used")
	require.Equal(t, "Spark", u.Windows[1].Label)
	blob, err := json.Marshal(u)
	require.NoError(t, err)
	var response map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(blob, &response))
	require.JSONEq(t, `{"summary":"2 reset credits available","next_expires_at":"2026-09-10T12:00:00Z"}`, string(response["reset_credits"]))
}
