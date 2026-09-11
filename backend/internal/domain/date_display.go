package domain

import "time"

// Date-only wire rendering for the banking activity feed (#1631).
//
// transactions.date and transactions.authorized_date are DATE columns (migration 000010), and the
// unified feed's sort_date is COALESCE(authorized_date, date)::date. A date-only value rendered
// through a timestamp shifts back a day for every user west of UTC — the defect
// HoldingSnapshotView.AsOfDate documents, and the one InvestmentTransaction.DateDisplay had
// already fixed for the investment leg of the same endpoint.
//
// ⚠ WHY A SIBLING STRING RATHER THAN A TYPE SWAP. Date is load-bearing as a time.Time on paths
// that have nothing to do with serialization: it is a SQL bind param on insert, a scan target, and
// the source of the V:2 keyset cursor sort value (buildKeysetCursor folds it via time.DateOnly).
// Retyping it would drag all of that along; keeping it and adding a display field leaves every one
// of those paths untouched. The display fields carry db:"-" so they are never mistaken for
// columns.
//
// ⚠ These are populated at the DB EDGE, not lazily on marshal. A struct that never reached a
// populate site serializes an EMPTY date rather than a wrong one — deliberately the louder
// failure, and why the populate call lives in the one shared scan finalizer rather than at each
// caller.

// formatDateOnly renders a DATE-backed value as its calendar day.
//
// Formatted in the value's own location, NOT converted to UTC first: pgx hands a DATE back as
// midnight in some location, and converting midnight-in-a-positive-offset to UTC would roll the
// calendar day backwards — the very shift this whole file exists to prevent.
func formatDateOnly(t time.Time) string {
	return t.Format(time.DateOnly)
}

// formatNullableDateOnly renders an optional DATE-backed value, preserving NULL as nil so the JSON
// key is omitted rather than present-and-empty.
func formatNullableDateOnly(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := formatDateOnly(*t)
	return &s
}

// PopulateDateDisplay fills DateDisplay / AuthorizedDateDisplay from the time.Time fields.
// Idempotent, so a second call at a later enrichment step is harmless.
func (t *Transaction) PopulateDateDisplay() {
	t.DateDisplay = formatDateOnly(t.Date)
	t.AuthorizedDateDisplay = formatNullableDateOnly(t.AuthorizedDate)
}

// PopulateDateDisplay fills SortDateDisplay / DateDisplay / AuthorizedDateDisplay from the
// time.Time fields. Idempotent.
//
// All three are populated for investment rows too: SortDate and Date are set on both legs of the
// UNION, and AuthorizedDate is NULL on the investment leg, which correctly yields nil.
func (item *ActivityItem) PopulateDateDisplay() {
	item.SortDateDisplay = formatDateOnly(item.SortDate)
	item.DateDisplay = formatDateOnly(item.Date)
	item.AuthorizedDateDisplay = formatNullableDateOnly(item.AuthorizedDate)
}
