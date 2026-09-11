package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #1631 Batch F2: budgets.start_date/end_date and savings_goals.deadline are Postgres DATE
// columns, but the REQUEST structs that write them (CreateBudgetRequest, UpdateBudgetRequest,
// CreateSavingsGoalRequest, UpdateSavingsGoalRequest) accept an RFC3339 timestamp today, and the
// current frontend relies on that: CreateBudgetDialog.tsx / CreateSavingsGoalDialog.tsx /
// UpdateSavingsGoalDialog.tsx all send `new Date(dateOnly).toISOString()`. Breaking that would be
// a regression, not a fix — so FlexibleDate must accept BOTH an RFC3339 timestamp and a bare
// "YYYY-MM-DD" date-only string, never rejecting the form the current frontend already sends.
//
// See domain/flexible_date.go for why this needs to be a distinct type rather than a `time.Time`
// field: encoding/json calls a field's own UnmarshalJSON/UnmarshalText, and time.Time's rejects a
// bare date outright.

type flexibleDateHolder struct {
	D FlexibleDate `json:"d"`
}

func TestFlexibleDate_AcceptsRFC3339Timestamp(t *testing.T) {
	var h flexibleDateHolder
	require.NoError(t, json.Unmarshal([]byte(`{"d":"2026-09-01T00:00:00Z"}`), &h))
	assert.True(t, time.Time(h.D).Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)))
}

func TestFlexibleDate_AcceptsBareDateOnlyString(t *testing.T) {
	var h flexibleDateHolder
	require.NoError(t, json.Unmarshal([]byte(`{"d":"2026-09-01"}`), &h))
	assert.True(t, time.Time(h.D).Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)))
}

func TestFlexibleDate_RejectsGarbage(t *testing.T) {
	var h flexibleDateHolder
	err := json.Unmarshal([]byte(`{"d":"not-a-date"}`), &h)
	assert.Error(t, err)
}

func TestFlexibleDate_PointerFieldAcceptsNull(t *testing.T) {
	type holder struct {
		D *FlexibleDate `json:"d"`
	}
	var h holder
	require.NoError(t, json.Unmarshal([]byte(`{"d":null}`), &h))
	assert.Nil(t, h.D)
}
