package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Cursor's dashboard exposes fields omitted from the CLI's normalized quota
// response, particularly for Enterprise accounts. Optional enrichment uses the
// same account and never replaces a working quota with a failed lookup.
type cursorClient struct {
	webBase string
	rpcBase string
	do      func(*http.Request) (*http.Response, error)
	auth    func(context.Context, map[string]any) (cursorAuth, error)
}

var (
	errCursorSessionRejected   = errors.New("Cursor session was rejected. Sign in again or update Cursor · Session cookie.")
	errCursorAccountMismatch   = errors.New("Cursor details use a different or unverified account. Set Cursor · Session cookie for the account shown by CodexBar.")
	errCursorAlternateMismatch = errors.New("Cursor Desktop and Agent CLI are signed in to different accounts. Sign in to the same account or set Cursor · Session cookie explicitly.")
)

type cursorAccount struct {
	id    string
	email string
}

// cursorAuthFallbackError marks an authentication rejection for which another
// automatic credential may be tried. Once the primary identity is known, the
// alternate must prove it belongs to that same account.
type cursorAuthFallbackError struct {
	err     error
	account cursorAccount
}

func (e *cursorAuthFallbackError) Error() string { return e.err.Error() }
func (e *cursorAuthFallbackError) Unwrap() error { return e.err }

func newCursorClient() *cursorClient {
	client := &http.Client{
		Timeout: 15 * time.Second,
		// Cookies and bearer tokens must never follow a redirect to another host.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &cursorClient{webBase: "https://cursor.com", rpcBase: "https://api2.cursor.sh", do: client.Do, auth: loadCursorAuth}
}

func (c *cursorClient) request(ctx context.Context, auth cursorAuth, endpoint string, rpc bool, payload []byte) ([]byte, error) {
	method, base := http.MethodGet, c.webBase
	var body io.Reader
	if rpc {
		base = c.rpcBase
		if payload == nil {
			payload = []byte("{}")
		}
	}
	if payload != nil {
		method, body = http.MethodPost, bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+endpoint, body)
	if err != nil {
		return nil, errors.New("Cannot prepare Cursor request.")
	}
	if rpc {
		req.Header.Set("Authorization", "Bearer "+auth.bearer)
		req.Header.Set("Connect-Protocol-Version", "1")
	} else {
		req.Header.Set("Cookie", auth.cookie)
		if method == http.MethodPost {
			req.Header.Set("Origin", c.webBase)
		}
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	const maxBytes = int64(2 << 20)
	resp, err := c.do(req)
	if err != nil {
		return nil, errors.New("Cursor request failed or timed out.")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, errCursorSessionRejected
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Cursor returned HTTP %d.", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil || int64(len(data)) > maxBytes {
		return nil, errors.New("Cursor response is incomplete or too large.")
	}
	return data, nil
}

func (c *cursorClient) fetch(ctx context.Context, cfg map[string]any, base *ProviderUsage, now time.Time) (*ProviderUsage, error) {
	teamID, err := cursorConfiguredTeamID(cfg)
	if err != nil {
		return nil, err
	}
	auth, err := c.auth(ctx, cfg)
	if err != nil {
		return nil, err
	}
	alternates := auth.alternates
	alternateErr := auth.alternateErr
	auth.alternates = nil
	auth.alternateErr = nil
	usage, err := c.fetchWithAuth(ctx, auth, teamID, base, nil, now)
	if err == nil {
		return usage, nil
	}
	var fallbackErr *cursorAuthFallbackError
	canFallback := errors.As(err, &fallbackErr) || base != nil && errors.Is(err, errCursorAccountMismatch)
	if !canFallback {
		return nil, err
	}
	var expected *cursorAccount
	if fallbackErr != nil && (fallbackErr.account.id != "" || fallbackErr.account.email != "") {
		account := fallbackErr.account
		expected = &account
	}
	lastErr := err
	for _, candidate := range alternates {
		candidate.alternates = nil
		candidate.alternateErr = nil
		usage, candidateErr := c.fetchWithAuth(ctx, candidate, teamID, base, expected, now)
		if candidateErr == nil {
			return usage, nil
		}
		lastErr = candidateErr
	}
	if alternateErr != nil {
		return nil, alternateErr
	}
	return nil, lastErr
}

func (c *cursorClient) fetchWithAuth(ctx context.Context, auth cursorAuth, teamID int64, base *ProviderUsage, expected *cursorAccount, now time.Time) (*ProviderUsage, error) {
	raw, err := c.request(ctx, auth, "/api/auth/me", false, nil)
	if err != nil {
		if errors.Is(err, errCursorSessionRejected) {
			return nil, &cursorAuthFallbackError{err: err}
		}
		return nil, err
	}
	identity := cursorDecode(raw)
	id := identity.text("id")
	if id == "" {
		id = identity.text("sub")
	}
	if id == "" {
		id = identity.text("userId")
	}
	if i := strings.LastIndex(id, "|"); i >= 0 {
		id = id[i+1:]
	}
	email := strings.TrimSpace(identity.text("email"))
	if id == "" && email == "" {
		return nil, errors.New("Cursor did not report an account identity for its usage details.")
	}
	if expected != nil && !cursorAccountMatches(expected.id, expected.email, id, email) {
		return nil, errCursorAlternateMismatch
	}
	if base != nil {
		if !cursorAccountMatches(base.accountID, base.accountEmail, id, email) {
			return nil, errCursorAccountMismatch
		}
	}
	// A pasted header can contain more than one kind of session cookie. Only
	// use its extracted bearer token when it identifies the verified web user.
	if id == "" || auth.userID != id {
		auth.bearer = ""
	}
	if id == "" {
		id = auth.userID
	}
	if teamID != 0 {
		out, err := c.fetchTeamUsage(ctx, auth, teamID, email, now)
		if err != nil {
			wrapped := fmt.Errorf("Cursor team %d: %w", teamID, err)
			if errors.Is(err, errCursorSessionRejected) {
				return nil, &cursorAuthFallbackError{err: wrapped, account: cursorAccount{id: id, email: email}}
			}
			return nil, wrapped
		}
		out.accountID, out.accountEmail = id, email
		return out, nil
	}
	endpoints := []struct {
		path string
		rpc  bool
	}{
		{path: "/api/usage-summary"},
		{path: "/aiserver.v1.DashboardService/GetCurrentPeriodUsage", rpc: true},
		{path: "/api/usage?" + url.Values{"user": {id}}.Encode()},
	}
	results := make([][]byte, len(endpoints))
	resultErrs := make([]error, len(endpoints))
	var wg sync.WaitGroup
	for i, endpoint := range endpoints {
		if endpoint.rpc && auth.bearer == "" || i == 2 && id == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], resultErrs[i] = c.request(ctx, auth, endpoint.path, endpoint.rpc, nil)
		}()
	}
	wg.Wait()
	for _, resultErr := range resultErrs {
		if errors.Is(resultErr, errCursorSessionRejected) {
			return nil, &cursorAuthFallbackError{
				err: errCursorSessionRejected, account: cursorAccount{id: id, email: email},
			}
		}
	}
	summary, rpc, requests := cursorDecode(results[0]), cursorDecode(results[1]), cursorDecode(results[2])
	out := mapCursorUsage(base, summary, rpc, requests, now)
	if len(out.Windows) == 0 && out.ExtraUsage == nil {
		return nil, errors.New("Cursor did not return usable account usage.")
	}
	out.accountID, out.accountEmail = id, email
	if len(summary) == 0 && len(rpc.object("planUsage")) == 0 {
		out.DetailWarning = "Detailed quotas unavailable; showing the available quota."
	}
	return out, nil
}

func cursorAccountMatches(wantID, wantEmail, gotID, gotEmail string) bool {
	matched := wantEmail != "" && gotEmail != "" && strings.EqualFold(wantEmail, gotEmail)
	if wantID != "" && gotID != "" {
		matched = wantID == gotID
	}
	return matched
}

// cursorObject decodes optional dashboard fields independently: omitted/null
// values are unknown, while a reported zero is real usage. RPC numbers may be
// encoded as strings. No response bodies or auth values appear in errors.
type cursorObject map[string]json.RawMessage

func cursorDecode(raw []byte) cursorObject {
	var out cursorObject
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}
func (o cursorObject) object(key string) cursorObject { return cursorDecode(o[key]) }
func (o cursorObject) text(key string) string         { var s string; _ = json.Unmarshal(o[key], &s); return s }
func (o cursorObject) enabled() bool {
	return !bytes.Equal(bytes.TrimSpace(o["enabled"]), []byte("false"))
}
func (o cursorObject) number(key string) *float64 {
	raw := strings.TrimSpace(string(o[key]))
	if strings.HasPrefix(raw, "\"") {
		raw = o.text(key)
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return nil
	}
	return &n
}
func (o cursorObject) date(key string) time.Time {
	if date, err := time.Parse(time.RFC3339Nano, o.text(key)); err == nil {
		return date
	}
	if millis := o.number(key); millis != nil && *millis > 0 && *millis < 1e14 {
		return time.UnixMilli(int64(*millis)).UTC()
	}
	return time.Time{}
}
func firstCursorNumber(values ...*float64) *float64 {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
func cursorRatio(used, limit *float64) *float64 {
	if used == nil || limit == nil || *limit <= 0 || math.IsInf(*limit, 0) {
		return nil
	}
	ratio := *used / *limit * 100
	if math.IsInf(ratio, 0) || math.IsNaN(ratio) {
		return nil
	}
	return &ratio
}
func cursorBucketUsed(bucket cursorObject, usedKey, limitKey, remainingKey string) *float64 {
	if used := bucket.number(usedKey); used != nil {
		return used
	}
	limit, remaining := bucket.number(limitKey), bucket.number(remainingKey)
	if limit == nil || remaining == nil {
		return nil
	}
	used := math.Max(0, *limit-*remaining)
	return &used
}

func mapCursorUsage(base *ProviderUsage, summary, rpc, requests cursorObject, now time.Time) *ProviderUsage {
	out := &ProviderUsage{Provider: "cursor", Source: "cursor-api", FetchedAt: now, Windows: []UtilizationWindow{}}
	if base != nil {
		*out = *base
		out.Windows = append([]UtilizationWindow(nil), base.Windows...)
	}
	reset := cursorBillingReset(summary)
	if reset.IsZero() {
		reset = cursorBillingReset(rpc)
	}
	if !reset.IsZero() {
		for i := range out.Windows {
			out.Windows[i].ResetAt = reset
		}
	}
	if out.Plan == "" {
		if plan := summary.text("membershipType"); plan != "" {
			out.Plan = "Cursor " + strings.ToUpper(plan[:1]) + plan[1:]
		}
	}
	setWindow := func(label string, pct *float64, scoped bool, detail string) {
		if pct == nil {
			return
		}
		w := UtilizationWindow{Label: label, UtilizationPct: *pct, ResetAt: reset, Scoped: scoped, Detail: detail}
		for i := range out.Windows {
			if out.Windows[i].Label == label {
				if w.ResetAt.IsZero() {
					w.ResetAt, w.ResetDescription = out.Windows[i].ResetAt, out.Windows[i].ResetDescription
				}
				out.Windows[i] = w
				return
			}
		}
		out.Windows = append(out.Windows, w)
	}
	individual, team := summary.object("individualUsage"), summary.object("teamUsage")
	plan, rpcPlan := individual.object("plan"), rpc.object("planUsage")
	if !plan.enabled() {
		plan = nil
	}
	if !rpc.enabled() || !rpcPlan.enabled() {
		rpcPlan = nil
	}
	requestQuota := requests.object("gpt-4")
	requestUsed := firstCursorNumber(requestQuota.number("numRequests"), requestQuota.number("numRequestsTotal"))
	requestLimit := requestQuota.number("maxRequestUsage")
	if pct := cursorRatio(requestUsed, requestLimit); pct != nil {
		setWindow("Total Usage", pct, false, fmt.Sprintf("%.0f of %.0f included requests", *requestUsed, *requestLimit))
	} else {
		total := firstCursorNumber(rpcPlan.number("totalPercentUsed"), plan.number("totalPercentUsed"),
			cursorRatio(cursorBucketUsed(rpcPlan, "totalSpend", "limit", "remaining"), rpcPlan.number("limit")),
			cursorRatio(cursorBucketUsed(plan, "used", "limit", "remaining"), plan.number("limit")))
		detail := ""
		for _, candidate := range []struct {
			bucket cursorObject
			team   bool
		}{{individual.object("overall"), false}, {team.object("pooled"), true}} {
			if total != nil || !candidate.bucket.enabled() {
				continue
			}
			used, limit := cursorBucketUsed(candidate.bucket, "used", "limit", "remaining"), candidate.bucket.number("limit")
			total = cursorRatio(used, limit)
			if total != nil {
				detail = fmt.Sprintf("$%.2f of $%.2f", *used/100, *limit/100)
				if candidate.team {
					detail += " · shared team allowance"
				}
			}
		}
		setWindow("Total Usage", total, false, detail)
	}
	setWindow("Auto Usage", firstCursorNumber(rpcPlan.number("autoPercentUsed"), plan.number("autoPercentUsed")), true, "")
	setWindow("API Usage", firstCursorNumber(rpcPlan.number("apiPercentUsed"), plan.number("apiPercentUsed")), true, "")
	// A zero personal spend must not fall through to the whole team's spend.
	spend := cursorSpend(individual.object("onDemand"), "used", "limit", "remaining", "")
	if spend == nil {
		spend = cursorSpend(team.object("onDemand"), "used", "limit", "remaining", "team")
	}
	if spend == nil {
		bucket := rpc.object("spendLimitUsage")
		spend = cursorSpend(bucket, "individualUsed", "individualLimit", "individualRemaining", "")
		if spend == nil {
			spend = cursorSpend(bucket, "pooledUsed", "pooledLimit", "pooledRemaining", "team")
		}
		if spend == nil {
			scope := ""
			if bucket.text("limitType") == "team" {
				scope = "team"
			}
			spend = cursorSpend(bucket, "totalSpend", "limit", "remaining", scope)
		}
	}
	if spend != nil {
		out.ExtraUsage = spend
	}
	return out
}

func cursorBillingReset(payload cursorObject) time.Time {
	end, start := payload.date("billingCycleEnd"), payload.date("billingCycleStart")
	// Enterprise's RPC can return identical start/end placeholders with no
	// plan data. They must not replace the CLI's real scheduled reset.
	if !start.IsZero() && !end.After(start) {
		return time.Time{}
	}
	return end
}

func cursorSpend(bucket cursorObject, usedKey, limitKey, remainingKey, scope string) *UsageSpend {
	if !bucket.enabled() {
		return nil
	}
	used := cursorBucketUsed(bucket, usedKey, limitKey, remainingKey)
	if used == nil {
		return nil
	}
	spend := &UsageSpend{Used: *used / 100, Currency: "USD", Scope: scope}
	if limit := bucket.number(limitKey); limit != nil && *limit > 0 {
		dollars := *limit / 100
		spend.Limit = &dollars
	}
	return spend
}

// Upstream usage.providerCost represents on-demand usage, already in currency
// units. The Windows top-level cost has different semantics and is not used.
func cursorProviderCost(raw []byte) *UsageSpend {
	cost := cursorDecode(raw)
	used := firstCursorNumber(cost.number("personalUsed"), cost.number("used"))
	currency := cost.text("currencyCode")
	if used == nil || currency == "" {
		return nil
	}
	spend := &UsageSpend{Used: *used, Currency: currency}
	if cost.number("personalUsed") == nil {
		if limit := cost.number("limit"); limit != nil && *limit > 0 {
			spend.Limit = limit
		}
	}
	return spend
}
