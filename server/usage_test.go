package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// sampleClaudeJSON is a trimmed real `codexbar usage --provider claude
// --format json` payload: two windows + a scoped extra window + pace.
const sampleClaudeJSON = `[{"provider":"claude","source":"claude","version":"2.1.215",
  "pace":{"primary":{"summary":"52% in reserve | Lasts until reset","stage":"farBehind"},
          "secondary":{"summary":"12% in deficit","stage":"ahead"}},
  "usage":{
    "primary":{"resetsAt":"2026-07-20T13:10:00Z","usedPercent":4,"windowMinutes":300,"resetDescription":"Resets 2:10pm"},
    "secondary":{"resetsAt":"2026-07-22T01:00:00Z","usedPercent":89,"windowMinutes":10080,"resetDescription":"Resets Jul 22"},
    "tertiary":null,
    "extraRateWindows":[{"id":"claude-weekly-scoped-fable","title":"Fable only",
      "window":{"resetsAt":"2026-07-22T00:59:00Z","usedPercent":100,"windowMinutes":10080,"resetDescription":"Resets Jul 22"}}],
    "identity":{"providerID":"claude"},"updatedAt":"2026-07-20T10:56:46Z"}}]`

const sampleCodexEntry = `{"provider":"codex","source":"oauth","version":"0.136.0",
  "usage":{"primary":{"resetsAt":"2026-08-13T22:28:08Z","usedPercent":4,"windowMinutes":43200,"resetDescription":"Aug 13"},
    "secondary":null,"tertiary":null,"loginMethod":"free",
    "identity":{"providerID":"codex","loginMethod":"free"},"updatedAt":"2026-07-20T10:56:54Z"}}`

const sampleCursorError = `{"source":"auto","provider":"cursor",
  "error":{"kind":"provider","message":"No Cursor session found.","code":1}}`

func TestToProviderUsage_ClaudeWindows(t *testing.T) {
	entries, err := parseCodexbarUsage([]byte(sampleClaudeJSON))
	require.NoError(t, err)
	require.Len(t, entries, 1)

	now := time.Unix(1000, 0)
	u := entries[0].toProviderUsage(now)
	require.NotNil(t, u)
	require.Equal(t, "claude", u.Provider)
	require.Equal(t, "claude", u.Source)

	// primary (5-hour), secondary (weekly), + one extra window titled "Fable only".
	require.Len(t, u.Windows, 3)
	require.Equal(t, "5-hour", u.Windows[0].Label)
	require.InDelta(t, 4.0, u.Windows[0].UtilizationPct, 1e-9)
	require.Equal(t, "weekly", u.Windows[1].Label)
	require.InDelta(t, 89.0, u.Windows[1].UtilizationPct, 1e-9)
	require.Equal(t, "Fable only", u.Windows[2].Label)
	require.InDelta(t, 100.0, u.Windows[2].UtilizationPct, 1e-9)

	require.Equal(t, "2026-07-20T13:10:00Z", u.Windows[0].ResetAt.UTC().Format(time.RFC3339))
	require.Equal(t, "2026-07-20T10:56:46Z", u.FetchedAt.UTC().Format(time.RFC3339))

	require.NotNil(t, u.PacePrime)
	require.Contains(t, u.PacePrime.Summary, "in reserve")
}

func TestToProviderUsage_CodexPlanFromLoginMethod(t *testing.T) {
	entries, err := parseCodexbarUsage([]byte("[" + sampleCodexEntry + "]"))
	require.NoError(t, err)
	u := entries[0].toProviderUsage(time.Unix(0, 0))
	require.NotNil(t, u)
	require.Equal(t, "codex", u.Provider)
	require.Equal(t, "free", u.Plan, "plan falls back to loginMethod")
	require.Len(t, u.Windows, 1)
	require.Equal(t, "monthly", u.Windows[0].Label, "43200 minutes -> monthly")
}

func TestToProviderUsage_NilForErrorEntry(t *testing.T) {
	entries, err := parseCodexbarUsage([]byte("[" + sampleCursorError + "]"))
	require.NoError(t, err)
	require.Nil(t, entries[0].toProviderUsage(time.Now()))
	require.NotNil(t, entries[0].Error)
	require.Contains(t, entries[0].Error.Message, "Cursor")
}

func TestWindowLabelFromMinutes(t *testing.T) {
	require.Equal(t, "5-hour", windowLabelFromMinutes(300))
	require.Equal(t, "weekly", windowLabelFromMinutes(10080))
	require.Equal(t, "monthly", windowLabelFromMinutes(43200))
	require.Equal(t, "daily", windowLabelFromMinutes(1440))
	require.Equal(t, "3-hour", windowLabelFromMinutes(180))
	require.Equal(t, "2-week", windowLabelFromMinutes(20160))
	require.Equal(t, "", windowLabelFromMinutes(0))
}

func TestParseCodexbarUsage_Invalid(t *testing.T) {
	_, err := parseCodexbarUsage([]byte("not json"))
	require.Error(t, err)
}
