package domain

import (
	"testing"
	"time"
)

// TestFlexibleDate_EndOfDay_DateOnlyExtendsToEndOfDay proves a date-only value
// ("2026-09-30", which UnmarshalJSON parses to midnight) is extended to the last
// representable instant of that SAME calendar day — covering the whole day, not
// truncating it at midnight (Issue #1630 review round 2).
func TestFlexibleDate_EndOfDay_DateOnlyExtendsToEndOfDay(t *testing.T) {
	d := FlexibleDate(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC))

	got := d.EndOfDay()

	want := time.Date(2026, 9, 30, 23, 59, 59, 999999999, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("EndOfDay() = %v, want %v", got, want)
	}
	if got.Day() != 30 || got.Month() != time.September {
		t.Fatalf("EndOfDay() must stay within Sep 30 (got %v)", got)
	}
}

// TestFlexibleDate_EndOfDay_FullTimestampRespectedExactly proves a full RFC3339 timestamp
// with a real, non-midnight time-of-day is returned UNCHANGED — not pushed forward toward
// the next calendar day. This is the RED/GREEN case for the review finding: the pre-fix
// behavior (unconditional +23h59m59s) would have moved "2026-09-30T15:00:00Z" to
// "2026-10-01T14:59:59Z", almost a full extra day later.
func TestFlexibleDate_EndOfDay_FullTimestampRespectedExactly(t *testing.T) {
	d := FlexibleDate(time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC))

	got := d.EndOfDay()

	want := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("EndOfDay() = %v, want %v (must be respected exactly, not extended)", got, want)
	}
}

// TestFlexibleDate_EndOfDay_MidnightRFC3339TreatedAsDateOnly documents the one ambiguous
// case: an RFC3339 timestamp that is EXACTLY midnight is indistinguishable from a date-only
// value (FlexibleDate carries no separate flag recording which form was sent) and is
// treated the same way — extended to end of that day. This is a deliberate, documented
// choice (see FlexibleDate.EndOfDay's doc comment), not an oversight.
func TestFlexibleDate_EndOfDay_MidnightRFC3339TreatedAsDateOnly(t *testing.T) {
	d := FlexibleDate(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC))

	got := d.EndOfDay()

	want := time.Date(2026, 9, 30, 23, 59, 59, 999999999, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("EndOfDay() = %v, want %v", got, want)
	}
}
