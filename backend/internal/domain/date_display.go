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
//
// ⚠ THE INVARIANT THIS FUNCTION REQUIRES OF ITS CALLERS (#1631 gap D1). Every date display field
// in this package documents its output as the "UTC calendar day (YYYY-MM-DD)" — around 45 of them.
// That promise and the paragraph above are only both true when the value handed in ALREADY names
// the intended day in its own location. So the contract is:
//
//	formatDateOnly's caller must pass a value whose LOCATION already names the intended
//	calendar day.
//
// Two provenances satisfy it, and they are the only two:
//
//   - A pgx-SCANNED DATE. pgx decodes a DATE as midnight in time.UTC, so the value's own location
//     IS UTC and the promise holds with nothing to do. This is what every Populate*Display method
//     below is handed, and it is why they may call this function directly.
//   - A COMPUTED instant that has been reduced through CalendarDayUTC first — a range echo, a
//     clamp, a caller-supplied RFC3339 offset. Nothing upstream fixes the location of such a
//     value, so the producing site must fix it, and FormatCalendarDayUTC is the one-call form.
//
// ⚠ WHY THIS FUNCTION CANNOT NORMALISE ON ITS CALLERS' BEHALF. Calling .UTC() here would be the
// exact bug the paragraph above warns about: pgx may hand back a DATE as midnight in a POSITIVE
// offset, and converting that instant to UTC lands on the previous day. A conversion cannot tell
// "midnight in +14:00, meaning 2026-03-15" from "10:00Z on 2026-03-14" — they are the same instant
// — so the day has to be decided where the provenance is known, which is the call site and never
// here. TestNoComputedDateDisplayFormatsInItsOwnLocation (internal/repository) enforces that split
// mechanically: a bare `x.Format(time.DateOnly)` under internal/ must either route through
// FormatCalendarDayUTC or be allowlisted with the provenance that makes its own location correct.
//
// ⚠ That guard can only see the FORMATTER, so it is blind to the other way the invariant breaks: a
// caller-supplied instant written onto the time.Time field and then rendered by one of the
// Populate*Display methods below, which are blessed. FlexibleDate is the only source of that shape
// (it accepts RFC3339, so the caller picks the offset), and
// TestNoFlexibleDateReachesADateFieldUnnormalised is the guard for it. Both halves are needed;
// neither subsumes the other.
func formatDateOnly(t time.Time) string {
	return t.Format(time.DateOnly)
}

// CalendarDayUTC reduces an instant to the calendar day it names IN UTC, as midnight UTC.
//
// This is the normalisation a COMPUTED date-shaped value needs before it can honour the "UTC
// calendar day" promise its display field makes — see formatDateOnly's invariant above.
//
// It returns midnight in time.UTC, not merely a same-day instant, because its second job is to be
// the value PERSISTED into a DATE column: pgx encodes a DATE from the value's own Year/Month/Day,
// so a normalised display string over a differently-located stored value would keep the promise on
// the way out and break it on the way back in. Using one function on both sides of that round trip
// is what makes the two agree. services/budget_service.go's budgetCalendarDayUTC was the first
// instance of this and now delegates here; its doc comment carries the fuller argument for why UTC
// (rather than the server's local zone or a per-user zone) is the zone that decides "which day".
//
// Idempotent, so a site that normalises the stored value and then formats it cannot drift.
func CalendarDayUTC(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// FormatCalendarDayUTC renders a COMPUTED instant as its UTC calendar day (YYYY-MM-DD).
//
// The one-call form of CalendarDayUTC + formatDateOnly, and the form every computed producing site
// outside this package uses. A pgx-scanned DATE needs neither — it is already midnight UTC — and
// routing one through here is a harmless no-op but obscures its provenance, so scanned sites stay
// on the Populate*Display methods.
func FormatCalendarDayUTC(t time.Time) string {
	return formatDateOnly(CalendarDayUTC(t))
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
