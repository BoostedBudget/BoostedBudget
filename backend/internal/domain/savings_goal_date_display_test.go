package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// #1631 Batch F2: SavingsGoal.Deadline and SavingsGoalProgressItem.Deadline are Postgres DATE-
// shaped values held as *time.Time with a live json tag. See domain/date_display.go for the
// defect class and assertExactDateString (analytics_date_display_test.go, same package) for the
// exact-equality requirement.

func TestSavingsGoal_DeadlineSerializesAsCalendarDayAfterPopulate(t *testing.T) {
	deadline := time.Date(2026, 12, 25, 0, 0, 0, 0, time.UTC)
	g := &SavingsGoal{Deadline: &deadline}
	g.PopulateDateDisplay()

	payload, err := json.Marshal(g)
	require.NoError(t, err)

	assertExactDateString(t, payload, "deadline", "2026-12-25")
}

func TestSavingsGoal_NilDeadlineOmitsTheKey(t *testing.T) {
	g := &SavingsGoal{Deadline: nil}
	g.PopulateDateDisplay()

	payload, err := json.Marshal(g)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))
	if _, present := decoded["deadline"]; present {
		t.Fatal("deadline must be OMITTED (not empty-string) when Deadline is nil")
	}
}

func TestSavingsGoalProgressItem_DeadlineSerializesAsCalendarDay(t *testing.T) {
	want := "2026-12-25"
	item := &SavingsGoalProgressItem{DeadlineDisplay: &want}

	payload, err := json.Marshal(item)
	require.NoError(t, err)

	assertExactDateString(t, payload, "deadline", "2026-12-25")
}
