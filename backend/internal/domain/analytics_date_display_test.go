package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #1631 Batch F1: CashFlowSummaryResponse.StartDate/EndDate, CashFlowHistoryResponse.StartDate/
// EndDate, and CashFlowPeriodData.PeriodStart/PeriodEnd are all rendered from Postgres DATE-shaped
// values but were serialized directly as time.Time, producing "2026-01-02T00:00:00Z" on the wire
// instead of the plain calendar day. See domain/date_display.go and #1631 for the defect class.
//
// ⚠ Assertions are exact-equality or "no T in the string", never strings.Contains — a Contains
// check on the timestamp form ("2026-01-02T00:00:00Z") still contains "2026-01-02" and would pass
// on the unfixed code.

func assertExactDateString(t *testing.T, payload []byte, key, want string) {
	t.Helper()
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))
	got, ok := decoded[key]
	require.True(t, ok, "key %q must be present on the wire", key)
	s, isStr := got.(string)
	require.True(t, isStr, "key %q must serialize as a string, got %T (%v)", key, got, got)
	assert.Equal(t, want, s, "key %q must be the plain calendar day, not a timestamp", key)
	assert.NotContains(t, s, "T", "key %q must carry no time component", key)
}

func TestCashFlowSummaryResponse_DateFieldsSerializeAsCalendarDays(t *testing.T) {
	resp := &CashFlowSummaryResponse{
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

func TestCashFlowHistoryResponse_DateFieldsSerializeAsCalendarDays(t *testing.T) {
	resp := &CashFlowHistoryResponse{
		StartDate:        time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		StartDateDisplay: "2026-01-02",
		EndDate:          time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC),
		EndDateDisplay:   "2026-01-09",
		// ⚠ requested_end_date is the third date on this struct and was missed by the mechanical
		// inventory of this defect class: it echoes the caller's request rather than reading a
		// DATE column, so a scan keyed on migration column names could not see it.
		RequestedEndDate:        time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC),
		RequestedEndDateDisplay: "2026-01-11",
	}

	payload, err := json.Marshal(resp)
	require.NoError(t, err)

	assertExactDateString(t, payload, "start_date", "2026-01-02")
	assertExactDateString(t, payload, "end_date", "2026-01-09")
	assertExactDateString(t, payload, "requested_end_date", "2026-01-11")
}

func TestCashFlowPeriodData_DateFieldsSerializeAsCalendarDays(t *testing.T) {
	data := CashFlowPeriodData{
		PeriodStart:        time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		PeriodStartDisplay: "2026-01-02",
		PeriodEnd:          time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC),
		PeriodEndDisplay:   "2026-01-08",
	}

	payload, err := json.Marshal(data)
	require.NoError(t, err)

	assertExactDateString(t, payload, "period_start", "2026-01-02")
	assertExactDateString(t, payload, "period_end", "2026-01-08")
}
