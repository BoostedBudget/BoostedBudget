package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// #1631 Batch F1: IncomeSummary.StartDate/EndDate are Postgres DATE-shaped values (the
// investment_transactions.date range the summary was computed over) that were serialized
// directly as time.Time, producing "2026-01-02T00:00:00Z" on the wire instead of the plain
// calendar day. See domain/date_display.go and #1631 for the defect class;
// assertExactDateString (defined in analytics_date_display_test.go, same package) asserts exact
// equality, never Contains, to keep the timestamp form from passing.
func TestIncomeSummary_DateFieldsSerializeAsCalendarDays(t *testing.T) {
	summary := &IncomeSummary{
		StartDate:        time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		StartDateDisplay: "2026-01-02",
		EndDate:          time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC),
		EndDateDisplay:   "2026-01-09",
	}

	payload, err := json.Marshal(summary)
	require.NoError(t, err)

	assertExactDateString(t, payload, "start_date", "2026-01-02")
	assertExactDateString(t, payload, "end_date", "2026-01-09")
}
