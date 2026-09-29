package main

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"time"
)

// DailyUsage is a currency total for an explicit calendar day, not a quota.
type DailyUsage struct {
	Used     float64   `json:"used"`
	Currency string    `json:"currency"`
	StartAt  time.Time `json:"start_at"`
	EndAt    time.Time `json:"end_at"`
}

// Dashboard events carry cents, including plan-covered costs when reported.
// Optional history must never turn an incomplete total into an apparent zero.
func (c *cursorClient) fetchDailyUsage(ctx context.Context, auth cursorAuth, teamID, userID int64, now time.Time) *DailyUsage {
	if teamID != 0 && userID == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	now = now.UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	daily := &DailyUsage{Currency: "USD", StartAt: start, EndAt: start.AddDate(0, 0, 1)}
	const pageSize, maxPages = 100, 20
	seen, expected := 0, -1
	for page := 1; page <= maxPages; page++ {
		body := map[string]any{
			"startDate": strconv.FormatInt(start.UnixMilli(), 10),
			"endDate":   strconv.FormatInt(now.UnixMilli(), 10),
			"page":      page, "pageSize": pageSize,
		}
		if teamID != 0 {
			body["teamId"], body["userId"] = teamID, userID
		}
		payload, _ := json.Marshal(body)
		raw, err := c.request(ctx, auth, "/api/dashboard/get-filtered-usage-events", false, payload)
		if err != nil {
			return nil
		}
		response := cursorDecode(raw)
		count := response.number("totalUsageEventsCount")
		var events []cursorObject
		if count == nil || *count > pageSize*maxPages || math.Trunc(*count) != *count ||
			json.Unmarshal(response["usageEventsDisplay"], &events) != nil || events == nil {
			return nil
		}
		if expected < 0 {
			expected = int(*count)
		}
		// A changing page set cannot safely be presented as a complete daily sum.
		if expected != int(*count) || len(events) > pageSize {
			return nil
		}
		seen += len(events)
		if seen > expected || len(events) == 0 && seen < expected {
			return nil
		}
		for _, event := range events {
			if teamID != 0 {
				for key, want := range map[string]int64{"owningTeam": teamID, "owningUser": userID} {
					if value, present := event[key]; present && cursorID(value) != want {
						return nil
					}
				}
			}
			at := event.date("timestamp")
			if at.IsZero() {
				return nil
			}
			if at.Before(start) || at.After(now) || !at.Before(daily.EndAt) {
				continue
			}
			cents := cursorEventCents(event)
			if cents == nil {
				return nil
			}
			daily.Used += *cents / 100
			if math.IsInf(daily.Used, 0) {
				return nil
			}
		}
		if seen == expected {
			return daily
		}
	}
	return nil
}

func cursorEventCents(event cursorObject) *float64 {
	// chargedCents already includes Cursor's fee; do not count the fee twice.
	if cents := event.number("chargedCents"); cents != nil {
		return cents
	}
	cents := event.object("tokenUsage").number("totalCents")
	if cents == nil {
		return nil
	}
	if _, present := event["cursorTokenFee"]; present {
		fee := event.number("cursorTokenFee")
		if fee == nil {
			return nil
		}
		*cents += *fee
	}
	return cents
}
