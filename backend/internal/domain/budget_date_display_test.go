package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// #1631 Batch F2: Budget.StartDate/EndDate are Postgres DATE columns held as time.Time with a
// live json tag, serializing as "2026-09-01T00:00:00Z" — a day early for every client west of
// UTC. See domain/date_display.go for the defect class and assertExactDateString (defined in
// analytics_date_display_test.go, same package) for why this must be exact-equality, not
// strings.Contains.

func TestBudget_DateFieldsSerializeAsCalendarDaysAfterPopulate(t *testing.T) {
	end := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	b := &Budget{
		StartDate: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   &end,
	}
	b.PopulateDateDisplay()

	payload, err := json.Marshal(b)
	require.NoError(t, err)

	assertExactDateString(t, payload, "start_date", "2026-09-01")
	assertExactDateString(t, payload, "end_date", "2026-09-30")
}

func TestBudget_NilEndDateOmitsTheKey(t *testing.T) {
	b := &Budget{
		StartDate: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   nil,
	}
	b.PopulateDateDisplay()

	payload, err := json.Marshal(b)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))
	_, present := decoded["end_date"]
	if present {
		t.Fatalf("end_date must be OMITTED (not empty-string) when EndDate is nil — an empty string would render as a literal \" - Invalid Date\" range suffix at BudgetCard.tsx:102")
	}
}
