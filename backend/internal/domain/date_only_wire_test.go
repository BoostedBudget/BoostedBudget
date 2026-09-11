package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #1631 gap 1: the BANKING feed used to serialize its DATE columns as full RFC 3339 timestamps.
//
// transactions.date and transactions.authorized_date are DATE columns (migration 000010, lines
// 24-25), and so is the sort_date the unified feed computes from them. Rendering a date-only value
// through a timestamp shifts it back a day for every client west of UTC — the defect
// HoldingSnapshotView.AsOfDate documents and InvestmentTransaction.DateDisplay already fixed for
// the investment leg of the very same feed. This file is the banking half.
//
// ⚠ WHY EVERY ASSERTION IS EQUALITY OR ABSENCE, NEVER Contains. A test written as
// assert.Contains(payload, "2026-01-02") passes for "2026-01-02T00:00:00Z" too and therefore
// proves nothing at all. Each case below asserts the exact wire string, and
// assertNoTimestampShapedDates asserts the absence of the timestamp form outright.

// dateOnlyWireFields lists every JSON key on the banking feed that is backed by a DATE column and
// must therefore reach clients as a plain YYYY-MM-DD string.
//
// Timestamp-backed keys are deliberately NOT here: reviewed_at, created_at, updated_at,
// category_override_date and reversed_at are all TIMESTAMP WITH TIME ZONE columns, for which RFC
// 3339 is the correct and intended rendering.
var dateOnlyWireFields = []string{"date", "sort_date", "authorized_date"}

// assertDateOnlyWireValue asserts that the JSON key is present and holds exactly want.
func assertDateOnlyWireValue(t *testing.T, payload []byte, key, want string) {
	t.Helper()
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))
	got, ok := decoded[key]
	require.True(t, ok, "key %q must be present on the wire", key)
	assert.Equal(t, want, got, "key %q must be the plain calendar day, not a timestamp", key)
}

// assertNoTimestampShapedDates asserts that none of the date-only keys carries a value with a time
// component. This is the assertion that actually fails on the pre-fix code.
func assertNoTimestampShapedDates(t *testing.T, payload []byte) {
	t.Helper()
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))
	for _, key := range dateOnlyWireFields {
		raw, ok := decoded[key]
		if !ok {
			continue
		}
		s, isStr := raw.(string)
		require.True(t, isStr, "key %q must serialize as a string, got %T", key, raw)
		assert.NotContains(t, s, "T", "key %q must carry no time component, got %q", key, s)
		assert.Len(t, s, len("2006-01-02"), "key %q must be exactly YYYY-MM-DD, got %q", key, s)
	}
}

func TestActivityItem_DateFieldsSerializeAsCalendarDays(t *testing.T) {
	authorized := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	item := &ActivityItem{
		Source:         FeedSourceBanking,
		SortDate:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ID:             "txn-1",
		UserID:         "user-1",
		AccountID:      "acct-1",
		Date:           time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		AuthorizedDate: &authorized,
		Amount:         -12.34,
		Name:           "Coffee",
	}
	item.PopulateDateDisplay()

	payload, err := json.Marshal(item)
	require.NoError(t, err)

	assertDateOnlyWireValue(t, payload, "date", "2026-01-02")
	assertDateOnlyWireValue(t, payload, "sort_date", "2026-01-01")
	assertDateOnlyWireValue(t, payload, "authorized_date", "2026-01-01")
	assertNoTimestampShapedDates(t, payload)
}

// A nil authorized_date must stay ABSENT rather than become an empty string. The two most
// prominent UI sites render formatDate(authorized_date || date), so an empty string there would
// short-circuit to "Invalid Date" instead of falling through to date.
func TestActivityItem_NilAuthorizedDateStaysAbsent(t *testing.T) {
	item := &ActivityItem{
		Source:   FeedSourceBanking,
		SortDate: time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC),
		Date:     time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC),
	}
	item.PopulateDateDisplay()

	payload, err := json.Marshal(item)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))
	_, present := decoded["authorized_date"]
	assert.False(t, present, "authorized_date must be omitted, not an empty string")
	assertNoTimestampShapedDates(t, payload)
}

func TestNewBankingActivityItem_PopulatesDateDisplaysFromTheTransaction(t *testing.T) {
	authorized := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	txn := &Transaction{
		ID:             "txn-1",
		UserID:         "user-1",
		AccountID:      "acct-1",
		Source:         SourcePlaid,
		Amount:         -12.34,
		Date:           time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		AuthorizedDate: &authorized,
		Name:           "Coffee",
	}

	payload, err := json.Marshal(NewBankingActivityItem(txn))
	require.NoError(t, err)

	// The wrapper is a construction site in its own right: the service layer builds every
	// banking-only feed response through it, so forgetting to populate here would ship empty
	// strings rather than timestamps.
	assertDateOnlyWireValue(t, payload, "date", "2026-01-02")
	assertDateOnlyWireValue(t, payload, "sort_date", "2026-01-01")
	assertDateOnlyWireValue(t, payload, "authorized_date", "2026-01-01")
	assertNoTimestampShapedDates(t, payload)
}

func TestTransaction_DateFieldsSerializeAsCalendarDays(t *testing.T) {
	authorized := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	txn := &Transaction{
		ID:             "txn-1",
		UserID:         "user-1",
		AccountID:      "acct-1",
		Source:         SourcePlaid,
		Amount:         -12.34,
		Date:           time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		AuthorizedDate: &authorized,
		Name:           "Coffee",
	}
	txn.PopulateDateDisplay()

	payload, err := json.Marshal(txn)
	require.NoError(t, err)

	assertDateOnlyWireValue(t, payload, "date", "2026-01-02")
	assertDateOnlyWireValue(t, payload, "authorized_date", "2026-01-01")
	assertNoTimestampShapedDates(t, payload)
}

func TestTransaction_NilAuthorizedDateStaysAbsent(t *testing.T) {
	txn := &Transaction{Date: time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)}
	txn.PopulateDateDisplay()

	payload, err := json.Marshal(txn)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))
	_, present := decoded["authorized_date"]
	assert.False(t, present, "authorized_date must be omitted, not an empty string")
	assertNoTimestampShapedDates(t, payload)
}

func TestRulePreviewMatch_DateSerializesAsCalendarDay(t *testing.T) {
	payload, err := json.Marshal(RulePreviewMatch{
		TransactionID: "txn-1",
		Date:          "2026-01-02",
		Name:          "Coffee",
		Amount:        -12.34,
	})
	require.NoError(t, err)

	assertDateOnlyWireValue(t, payload, "date", "2026-01-02")
	assertNoTimestampShapedDates(t, payload)
}

// TestNoDateOnlyWireFieldIsBackedByATimeValue is the mechanical half: a reflection guard over the
// three types that carry the banking feed, refusing any time.Time (or *time.Time) field whose json
// tag is one of the date-only keys.
//
// It exists because the population calls are easy to add and the struct tag is easy to move back.
// A future field of type time.Time carrying a json name this list does not know about is NOT
// caught — the guard can only police the names it knows are DATE-backed — so extend
// dateOnlyWireFields when a DATE column joins these types.
func TestNoDateOnlyWireFieldIsBackedByATimeValue(t *testing.T) {
	dateOnly := make(map[string]bool, len(dateOnlyWireFields))
	for _, f := range dateOnlyWireFields {
		dateOnly[f] = true
	}

	timeType := reflect.TypeOf(time.Time{})
	for _, subject := range []any{ActivityItem{}, Transaction{}, RulePreviewMatch{}} {
		typ := reflect.TypeOf(subject)
		t.Run(typ.Name(), func(t *testing.T) {
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				name := strings.Split(field.Tag.Get("json"), ",")[0]
				if !dateOnly[name] {
					continue
				}
				ft := field.Type
				if ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				assert.NotEqual(t, timeType, ft,
					"%s.%s serializes as %q, a DATE-backed key, so it must be a YYYY-MM-DD string rather than a time value",
					typ.Name(), field.Name, name)
			}
		})
	}
}
