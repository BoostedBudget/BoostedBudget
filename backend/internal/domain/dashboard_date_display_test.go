package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// #1631 Batch F1: NetWorthResponse.AsOfDate, NetWorthHistoryResponse.StartDate/EndDate,
// NetWorthHistorySnapshot.Date, BalanceSummaryResponse.AsOfDate,
// BalanceHistoryResponse.StartDate/EndDate, and BalanceHistorySnapshot.Date are all
// Postgres DATE-shaped values that were serialized directly as time.Time, producing
// "2026-01-02T00:00:00Z" on the wire instead of the plain calendar day. See
// domain/date_display.go and #1631 for the defect class; assertExactDateString (defined in
// analytics_date_display_test.go) asserts exact equality, never Contains, to keep the timestamp
// form from passing.

func TestNetWorthResponse_AsOfDateSerializesAsCalendarDay(t *testing.T) {
	resp := &NetWorthResponse{
		AsOfDate:        time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		AsOfDateDisplay: "2026-01-02",
	}

	payload, err := json.Marshal(resp)
	require.NoError(t, err)

	assertExactDateString(t, payload, "as_of_date", "2026-01-02")
}

func TestNetWorthHistoryResponse_DateFieldsSerializeAsCalendarDays(t *testing.T) {
	resp := &NetWorthHistoryResponse{
		StartDate:        time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		StartDateDisplay: "2026-01-02",
		EndDate:          time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC),
		EndDateDisplay:   "2026-01-09",
	}

	payload, err := json.Marshal(resp)
	require.NoError(t, err)

	assertExactDateString(t, payload, "start_date", "2026-01-02")
	assertExactDateString(t, payload, "end_date", "2026-01-09")
}

func TestNetWorthHistorySnapshot_DateSerializesAsCalendarDay(t *testing.T) {
	snap := NetWorthHistorySnapshot{
		Date:        time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
		DateDisplay: "2026-01-03",
	}

	payload, err := json.Marshal(snap)
	require.NoError(t, err)

	assertExactDateString(t, payload, "date", "2026-01-03")
}

func TestBalanceSummaryResponse_AsOfDateSerializesAsCalendarDay(t *testing.T) {
	resp := &BalanceSummaryResponse{
		AsOfDate:        time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		AsOfDateDisplay: "2026-01-02",
	}

	payload, err := json.Marshal(resp)
	require.NoError(t, err)

	assertExactDateString(t, payload, "as_of_date", "2026-01-02")
}

func TestBalanceHistoryResponse_DateFieldsSerializeAsCalendarDays(t *testing.T) {
	resp := &BalanceHistoryResponse{
		StartDate:        time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		StartDateDisplay: "2026-01-02",
		EndDate:          time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC),
		EndDateDisplay:   "2026-01-09",
	}

	payload, err := json.Marshal(resp)
	require.NoError(t, err)

	assertExactDateString(t, payload, "start_date", "2026-01-02")
	assertExactDateString(t, payload, "end_date", "2026-01-09")
}

func TestBalanceHistorySnapshot_DateSerializesAsCalendarDay(t *testing.T) {
	snap := BalanceHistorySnapshot{
		Date:        time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
		DateDisplay: "2026-01-03",
	}

	payload, err := json.Marshal(snap)
	require.NoError(t, err)

	assertExactDateString(t, payload, "date", "2026-01-03")
}
