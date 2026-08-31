package main

import (
	"encoding/json"
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

// sampleOpenCodeGoEntry mirrors codexbar's opencodego output: local-SQLite
// source with a 5-hour primary window and an optional weekly window.
const sampleOpenCodeGoEntry = `{"provider":"opencodego","source":"local","version":"",
  "pace":{"primary":{"summary":"52% in reserve | Lasts until reset","stage":"farBehind"}},
  "usage":{"primary":{"resetsAt":"2026-08-07T22:00:00Z","usedPercent":34,"windowMinutes":300,"resetDescription":"Resets 10pm"},
    "secondary":{"resetsAt":"2026-08-13T00:00:00Z","usedPercent":58,"windowMinutes":10080,"resetDescription":"Resets Aug 13"},
    "tertiary":null,
    "identity":{"providerID":"opencodego","planName":"Zen"},"updatedAt":"2026-08-07T18:00:00Z"}}`

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

func TestToProviderUsage_OpenCodeGoWindows(t *testing.T) {
	entries, err := parseCodexbarUsage([]byte("[" + sampleOpenCodeGoEntry + "]"))
	require.NoError(t, err)
	u := entries[0].toProviderUsage(time.Unix(0, 0))
	require.NotNil(t, u)
	require.Equal(t, "opencodego", u.Provider)
	require.Equal(t, "local", u.Source)
	require.Equal(t, "Zen", u.Plan, "plan from identity")
	require.Len(t, u.Windows, 2)
	require.Equal(t, "5-hour", u.Windows[0].Label)
	require.InDelta(t, 34.0, u.Windows[0].UtilizationPct, 1e-9)
	require.Equal(t, "weekly", u.Windows[1].Label)
	require.InDelta(t, 58.0, u.Windows[1].UtilizationPct, 1e-9)
	require.NotNil(t, u.PacePrime)
	require.Contains(t, u.PacePrime.Summary, "in reserve")
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

// --- Win-CodexBar wire format -------------------------------------------------
//
// Verbatim `codexbar-cli usage --provider <p> --format json --no-color` payloads
// from Win-CodexBar 0.55.0, the codexbar-compatible CLI on Windows. It spells the
// usage payload with underscores, carries no identity block, and reports a
// failing provider's error as a bare string. Only account identifiers are
// substituted; the sub-second timestamps and the fields the plugin ignores
// (cost, model_specific, is_informational) are kept as the CLI emits them.

const sampleWinClaudeJSON = `[{"cost":null,"pace":{"primary":{"deltaPercent":-17.522222222222222,"expectedUsedPercent":28.522222222222222,"stage":"farbehind","willLastToReset":true},"secondary":{"deltaPercent":-8.706018518518519,"expectedUsedPercent":18.70601851851852,"stage":"behind","willLastToReset":true}},"provider":"claude","source":"oauth","usage":{"extra_rate_windows":[{"id":"claude-weekly-scoped-fable","title":"Fable only","window":{"is_informational":false,"resets_at":"2026-09-04T07:59:59.921951Z","used_percent":15.0,"window_minutes":10080}}],"login_method":"Claude Max 5x","primary":{"is_informational":false,"resets_at":"2026-08-29T18:59:59.921703Z","used_percent":11.0,"window_minutes":300},"secondary":{"is_informational":false,"resets_at":"2026-09-04T07:59:59.921722Z","used_percent":10.0,"window_minutes":10080},"updated_at":"2026-08-29T15:25:33.605249100Z"}}]`

// sampleWinCopilotJSON pins a window the port reports as used_percent 0 with no
// window_minutes, and an informational extra window carrying only a
// reset_description.
const sampleWinCopilotJSON = `[{"cost":null,"provider":"copilot","source":"oauth",
  "usage":{"extra_rate_windows":[{"id":"ai-credits","title":"AI credits",
      "window":{"is_informational":true,"reset_description":"0 AI credits used","used_percent":0.0}}],
    "login_method":"Copilot Business",
    "primary":{"is_informational":false,"resets_at":"2026-09-01T00:00:00Z","used_percent":0.0},
    "updated_at":"2026-08-27T13:18:13Z"}}]`

// sampleWinMixedJSON is the case that used to discard everything: a provider
// that reported a string error, alongside one that returned usage.
const sampleWinMixedJSON = `[{"error":"Provider not installed: Codex auth.json not found.","provider":"codex"},
  {"cost":null,"provider":"claude","source":"oauth",
   "usage":{"login_method":"Claude Max 5x",
     "primary":{"resets_at":"2026-08-27T14:10:00Z","used_percent":7.0,"window_minutes":300},
     "updated_at":"2026-08-27T13:18:36Z"}}]`

// sampleWinCursorJSON also carries the port's cost block and its model_specific
// window — both of which the plugin has no slot for and drops.
const sampleWinCursorJSON = `[{"cost":{"currency_code":"USD","period":"Cursor and Third Party (since 2026-08-16T17:32:10.000Z)","resets_at":"2026-09-16T17:32:10Z","updated_at":"2026-08-29T15:25:32.755433400Z","used":0.0},"provider":"cursor","source":"web","usage":{"account_email":"user@example.com","login_method":"Cursor Free","model_specific":{"is_informational":false,"resets_at":"2026-09-16T17:32:10Z","used_percent":0.0},"primary":{"is_informational":false,"resets_at":"2026-09-16T17:32:10Z","used_percent":0.0},"secondary":{"is_informational":false,"resets_at":"2026-09-16T17:32:10Z","used_percent":0.0},"updated_at":"2026-08-29T15:25:32.919273700Z"}}]`

func TestParseWinCodexbar_SnakeCaseUsage(t *testing.T) {
	entries, err := parseCodexbarUsage([]byte(sampleWinClaudeJSON))
	require.NoError(t, err)
	require.Len(t, entries, 1)

	u := entries[0].toProviderUsage(time.Unix(1000, 0))
	require.NotNil(t, u)
	require.Equal(t, "claude", u.Provider)
	require.Equal(t, "Claude Max 5x", u.Plan, "plan falls back to login_method when identity is absent")
	require.Equal(t, "2026-08-29T15:25:33Z", u.FetchedAt.UTC().Format(time.RFC3339),
		"sub-second precision, as the CLI emits it")

	require.Len(t, u.Windows, 3, "primary, secondary and the scoped extra all survive")
	require.Equal(t, "5-hour", u.Windows[0].Label)
	require.Equal(t, 11.0, u.Windows[0].UtilizationPct)
	require.Equal(t, "2026-08-29T18:59:59Z", u.Windows[0].ResetAt.UTC().Format(time.RFC3339))
	require.Equal(t, "weekly", u.Windows[1].Label)
	require.Equal(t, 10.0, u.Windows[1].UtilizationPct)
	require.Equal(t, "Fable only", u.Windows[2].Label)
	require.Equal(t, 15.0, u.Windows[2].UtilizationPct)
	require.True(t, u.Windows[2].Scoped)

	// The port reports pace as stage plus numeric deltas, with no summary.
	require.NotNil(t, u.PacePrime)
	require.Equal(t, "farbehind", u.PacePrime.Stage)
	require.Empty(t, u.PacePrime.Summary)
}

func TestParseWinCodexbar_ZeroPercentAndInformationalWindow(t *testing.T) {
	entries, err := parseCodexbarUsage([]byte(sampleWinCopilotJSON))
	require.NoError(t, err)

	u := entries[0].toProviderUsage(time.Unix(1000, 0))
	require.NotNil(t, u)
	require.Equal(t, "Copilot Business", u.Plan)
	require.Len(t, u.Windows, 2)

	// used_percent 0 is a real reading, not an absent field.
	require.Equal(t, 0.0, u.Windows[0].UtilizationPct)
	require.Equal(t, "Primary", u.Windows[0].Label, "no window_minutes leaves the slot name")
	require.Equal(t, "AI credits", u.Windows[1].Label)
	require.Equal(t, "0 AI credits used", u.Windows[1].ResetDescription)
	require.True(t, u.Windows[1].ResetAt.IsZero(), "no resets_at stays zero and is omitted on the wire")
}

func TestParseWinCodexbar_StringErrorKeepsHealthyEntries(t *testing.T) {
	entries, err := parseCodexbarUsage([]byte(sampleWinMixedJSON))
	require.NoError(t, err, "a string error must not fail the whole array")
	require.Len(t, entries, 2)

	require.NotNil(t, entries[0].Error)
	require.Equal(t, "Provider not installed: Codex auth.json not found.", entries[0].Error.Message)
	require.Nil(t, entries[0].Usage)

	require.Nil(t, entries[1].Error)
	require.NotNil(t, entries[1].toProviderUsage(time.Unix(1000, 0)))
}

func TestParseWinCodexbar_CursorAccountFieldsIgnored(t *testing.T) {
	entries, err := parseCodexbarUsage([]byte(sampleWinCursorJSON))
	require.NoError(t, err)

	u := entries[0].toProviderUsage(time.Unix(1000, 0))
	require.NotNil(t, u)
	require.Equal(t, "Cursor Free", u.Plan)
	require.Len(t, u.Windows, 2, "primary and secondary; model_specific and cost have no slot")
	require.Equal(t, 0.0, u.Windows[0].UtilizationPct)
}

// TestParseCodexbar_ObjectErrorStillDecodes guards the upstream spelling: the
// tolerant decode must not regress the object form.
func TestParseCodexbar_ObjectErrorStillDecodes(t *testing.T) {
	entries, err := parseCodexbarUsage([]byte("[" + sampleCursorError + "]"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.NotNil(t, entries[0].Error)
	require.Equal(t, "No Cursor session found.", entries[0].Error.Message)
	require.Equal(t, "provider", entries[0].Error.Kind)
	require.Equal(t, 1, entries[0].Error.Code)
}

// TestUtilizationWindow_OmitsUnknownReset covers both CLIs: a window with no
// reset timestamp must leave reset_at off the wire, so the UI falls back to
// reset_description instead of reading the zero time as "resets now".
func TestUtilizationWindow_OmitsUnknownReset(t *testing.T) {
	const upstreamNoResetsAt = `[{"provider":"amp","source":"cli",
      "usage":{"primary":{"usedPercent":42,"windowMinutes":300},"updatedAt":"2026-08-27T13:00:00Z"}}]`

	entries, err := parseCodexbarUsage([]byte(upstreamNoResetsAt))
	require.NoError(t, err)
	u := entries[0].toProviderUsage(time.Unix(1000, 0))
	require.NotNil(t, u)
	require.True(t, u.Windows[0].ResetAt.IsZero())

	blob, err := json.Marshal(u.Windows[0])
	require.NoError(t, err)
	require.NotContains(t, string(blob), "reset_at")
	require.NotContains(t, string(blob), "0001-01-01")
}

// TestParseCodexbar_SpellingPrecedence pins the merge rule the UnmarshalJSON
// methods rely on. No CLI writes both spellings, so this is a contract test, not
// a scenario: it exists so a reordering of the merge fails loudly.
func TestParseCodexbar_SpellingPrecedence(t *testing.T) {
	const both = `[{"provider":"p","usage":{
      "loginMethod":"camel","login_method":"snake",
      "updatedAt":"2026-01-01T00:00:00Z","updated_at":"2026-02-02T00:00:00Z",
      "primary":{"usedPercent":11,"used_percent":99,"windowMinutes":300,"window_minutes":10080,
        "resetsAt":"2026-03-03T00:00:00Z","resets_at":"2026-04-04T00:00:00Z",
        "resetDescription":"camel","reset_description":"snake"}}}]`

	entries, err := parseCodexbarUsage([]byte(both))
	require.NoError(t, err)
	u := entries[0].Usage
	require.Equal(t, "camel", u.LoginMethod)
	require.Equal(t, "2026-01-01T00:00:00Z", u.UpdatedAt)
	require.Equal(t, 11.0, u.Primary.UsedPercent)
	require.Equal(t, 300, u.Primary.WindowMinutes)
	require.Equal(t, "2026-03-03T00:00:00Z", u.Primary.ResetsAt)
	require.Equal(t, "camel", u.Primary.ResetDescription)

	// Known limitation of a zero-value merge: an explicit camelCase zero cannot be
	// told apart from an absent field, so a present snake value wins there.
	const camelZero = `[{"provider":"p","usage":{"primary":{"usedPercent":0,"used_percent":42}}}]`
	entries, err = parseCodexbarUsage([]byte(camelZero))
	require.NoError(t, err)
	require.Equal(t, 42.0, entries[0].Usage.Primary.UsedPercent)
}

// TestParseCodexbar_BadSnakeValueIsNotFatal guards the blast radius: the
// snake_case pass is a fallback, so a wrongly-typed value there must be ignored
// rather than discard every healthy provider in the array.
func TestParseCodexbar_BadSnakeValueIsNotFatal(t *testing.T) {
	const bad = `[{"provider":"broken","usage":{"login_method":7,"primary":{"used_percent":"nope"}}},
      {"provider":"claude","usage":{"login_method":"Claude Max 5x","primary":{"used_percent":7.0,"window_minutes":300}}}]`

	entries, err := parseCodexbarUsage([]byte(bad))
	require.NoError(t, err, "a bad value in the fallback spelling must not fail the document")
	require.Len(t, entries, 2)
	require.Equal(t, 0.0, entries[0].Usage.Primary.UsedPercent, "the unusable value is simply skipped")
	require.Equal(t, 7.0, entries[1].Usage.Primary.UsedPercent, "the healthy provider is unaffected")

	// The canonical camelCase spelling stays strict: that is the contract.
	const badCamel = `[{"provider":"p","usage":{"primary":{"usedPercent":"nope"}}}]`
	_, err = parseCodexbarUsage([]byte(badCamel))
	require.Error(t, err)
}
