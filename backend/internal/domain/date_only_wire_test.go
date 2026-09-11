package domain

import (
	"encoding/json"
	"reflect"
	"regexp"
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
// ─── Batch J (#1631 gap 6): format:"date" on the remaining date-only response fields ──────────
//
// The tag itself is declarative and cannot be asserted from Go — the meaningful check is that
// the value each of these fields actually carries on the wire is a bare calendar day, never a
// timestamp, for every field whose date-only-ness required reading a population site rather than
// being obvious from the type. See batch-j-report.md for the full tag-by-tag list, the declines,
// and the population site read for each.
var dateOnlyRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func assertWireValueIsCalendarDayOnly(t *testing.T, payload []byte, key string) {
	t.Helper()
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))
	raw, ok := decoded[key]
	require.True(t, ok, "key %q must be present on the wire", key)
	s, isStr := raw.(string)
	require.True(t, isStr, "key %q must serialize as a string, got %T", key, raw)
	assert.Regexp(t, dateOnlyRegex, s, "key %q must be a bare YYYY-MM-DD calendar day, got %q", key, s)
}

func TestAccountBalanceHistory_DateFieldsAreCalendarDaysOnly(t *testing.T) {
	first := "2026-01-02"
	hist := AccountBalanceHistory{
		AccountID: "acct-1",
		FirstDate: &first,
		Points: []AccountBalanceHistoryPoint{
			{Date: "2026-01-02", CurrencyCode: "USD"},
		},
	}
	payload, err := json.Marshal(hist)
	require.NoError(t, err)

	assertWireValueIsCalendarDayOnly(t, payload, "first_date")

	pointPayload, err := json.Marshal(hist.Points[0])
	require.NoError(t, err)
	assertWireValueIsCalendarDayOnly(t, pointPayload, "date")
}

func TestSavingsGoalContribution_OccurredOnIsCalendarDayOnly(t *testing.T) {
	// #1631 batch J: occurred_on is deliberately projected as `occurred_on::text` over a DATE
	// column (savings_contribution_repository.go), pinned by an existing integration test —
	// this asserts the wire shape that projection produces.
	c := SavingsGoalContribution{OccurredOn: "2026-01-02"}
	payload, err := json.Marshal(c)
	require.NoError(t, err)
	assertWireValueIsCalendarDayOnly(t, payload, "occurred_on")
}

func TestNetWorthBucket_DateIsCalendarDayOnly(t *testing.T) {
	payload, err := json.Marshal(NetWorthBucket{Date: "2026-01-02", NetWorth: 100})
	require.NoError(t, err)
	assertWireValueIsCalendarDayOnly(t, payload, "date")
}

func TestRecurringSubscriptionItem_And_Response_DatesAreCalendarDayOnly(t *testing.T) {
	item := RecurringSubscriptionItem{
		MerchantName:     "Netflix",
		LastChargeDate:   "2026-01-02",
		NextExpectedDate: "2026-02-02",
	}
	payload, err := json.Marshal(item)
	require.NoError(t, err)
	assertWireValueIsCalendarDayOnly(t, payload, "last_charge_date")
	assertWireValueIsCalendarDayOnly(t, payload, "next_expected_date")

	resp := RecurringSubscriptionsResponse{Subscriptions: []RecurringSubscriptionItem{item}, AsOfDate: "2026-01-02"}
	respPayload, err := json.Marshal(resp)
	require.NoError(t, err)
	assertWireValueIsCalendarDayOnly(t, respPayload, "as_of_date")
}

func TestLargestTransactionItem_TopExpenseResponse_UnusualSpendItem_DatesAreCalendarDayOnly(t *testing.T) {
	ltPayload, err := json.Marshal(LargestTransactionItem{Date: "2026-01-02"})
	require.NoError(t, err)
	assertWireValueIsCalendarDayOnly(t, ltPayload, "date")

	tePayload, err := json.Marshal(TopExpenseResponse{Date: "2026-01-02", StartDate: "2026-01-01"})
	require.NoError(t, err)
	assertWireValueIsCalendarDayOnly(t, tePayload, "date")
	assertWireValueIsCalendarDayOnly(t, tePayload, "start_date")

	usPayload, err := json.Marshal(UnusualSpendItem{Date: "2026-01-02"})
	require.NoError(t, err)
	assertWireValueIsCalendarDayOnly(t, usPayload, "date")
}

func TestHoldingView_DayChangeAsOfIsCalendarDayOnly(t *testing.T) {
	asOf := "2026-01-02"
	v := HoldingView{DayChangeAsOf: &asOf}
	payload, err := json.Marshal(v)
	require.NoError(t, err)
	assertWireValueIsCalendarDayOnly(t, payload, "day_change_as_of")
}

// TestAnalyticsRangeResponses_StartEndDatesAreCalendarDayOnly covers the *Response structs whose
// start_date/end_date pair is populated identically via t.Format("2006-01-02") in
// analytics_service.go / analytics_category_hierarchy.go — verified at those population sites,
// not merely by type, since a plain `string` field says nothing about its actual contents.
func TestAnalyticsRangeResponses_StartEndDatesAreCalendarDayOnly(t *testing.T) {
	const start, end = "2026-01-01", "2026-01-31"

	cases := []struct {
		name    string
		payload []byte
		keys    []string
	}{
		{"AverageMonthlySpendResponse", marshalOrPanic(t, AverageMonthlySpendResponse{StartDate: start, EndDate: end}), []string{"start_date", "end_date"}},
		{"CategoryBreakdownForMerchantResponse", marshalOrPanic(t, CategoryBreakdownForMerchantResponse{StartDate: start, EndDate: end}), []string{"start_date", "end_date"}},
		{"CategoryHierarchyResponse", marshalOrPanic(t, CategoryHierarchyResponse{StartDate: start, EndDate: end}), []string{"start_date", "end_date"}},
		{"CategoryTotalResponse", marshalOrPanic(t, CategoryTotalResponse{StartDate: start, EndDate: end}), []string{"start_date", "end_date"}},
		{"IncomeSourceBreakdownResponse", marshalOrPanic(t, IncomeSourceBreakdownResponse{StartDate: start, EndDate: end}), []string{"start_date", "end_date"}},
		{"LargestTransactionsResponse", marshalOrPanic(t, LargestTransactionsResponse{StartDate: start, EndDate: end}), []string{"start_date", "end_date"}},
		{"NetWorthTrendResponse", marshalOrPanic(t, NetWorthTrendResponse{StartDate: start, EndDate: end}), []string{"start_date", "end_date"}},
		{"SavingsRateResponse", marshalOrPanic(t, SavingsRateResponse{StartDate: start, EndDate: end}), []string{"start_date", "end_date"}},
		{"SpendingByCategoryResponse.end_date", marshalOrPanic(t, SpendingByCategoryResponse{StartDate: start, EndDate: end}), []string{"end_date"}},
		{"SpendingByMerchantResponse", marshalOrPanic(t, SpendingByMerchantResponse{StartDate: start, EndDate: end}), []string{"start_date", "end_date"}},
		{"SpendingTrendPeriod.period_end", marshalOrPanic(t, SpendingTrendPeriod{PeriodStart: start, PeriodEnd: end}), []string{"period_end"}},
		{"SpendingTrendsResponse", marshalOrPanic(t, SpendingTrendsResponse{StartDate: start, EndDate: end}), []string{"start_date", "end_date"}},
		{"UnusualSpendResponse", marshalOrPanic(t, UnusualSpendResponse{StartDate: start, EndDate: end}), []string{"start_date", "end_date"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range tc.keys {
				assertWireValueIsCalendarDayOnly(t, tc.payload, key)
			}
		})
	}
}

func marshalOrPanic(t *testing.T, v any) []byte {
	t.Helper()
	payload, err := json.Marshal(v)
	require.NoError(t, err)
	return payload
}

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
