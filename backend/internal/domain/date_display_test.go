package domain

import (
	"testing"
	"time"
)

// #1631 gap D1. formatDateOnly deliberately formats in the value's OWN location, and every date
// display field documents its output as the UTC calendar day. These two only agree when the value
// already names the intended day in its own location — true for a pgx-scanned DATE, false for a
// computed instant. CalendarDayUTC / FormatCalendarDayUTC are what a computed producing site uses
// to make the documented promise true at the point the value is produced.
func TestFormatCalendarDayUTC_ReducesToTheUTCCalendarDay(t *testing.T) {
	t.Parallel()

	plus14 := time.FixedZone("+14", 14*3600)
	minus11 := time.FixedZone("-11", -11*3600)

	cases := []struct {
		name string
		in   time.Time
		want string
	}{
		{
			// The case the whole gap is about: midnight in a positive offset is the PREVIOUS day
			// in UTC, so formatDateOnly alone would report a day UTC does not agree with.
			name: "midnight in a positive offset rolls back to the previous UTC day",
			in:   time.Date(2026, 3, 15, 0, 0, 0, 0, plus14),
			want: "2026-03-14",
		},
		{
			name: "late evening in a negative offset rolls forward to the next UTC day",
			in:   time.Date(2026, 3, 15, 20, 0, 0, 0, minus11),
			want: "2026-03-16",
		},
		{
			// The pgx-scanned shape: already midnight UTC, so this is a no-op and a site that
			// routes a scanned DATE through it is not harmed, only redundant.
			name: "midnight UTC is unchanged",
			in:   time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
			want: "2026-03-15",
		},
		{
			name: "a mid-day UTC instant reports the day it falls in",
			in:   time.Date(2026, 3, 15, 23, 59, 59, 999999999, time.UTC),
			want: "2026-03-15",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := FormatCalendarDayUTC(tc.in); got != tc.want {
				t.Fatalf("FormatCalendarDayUTC(%s) = %q, want %q", tc.in.Format(time.RFC3339), got, tc.want)
			}
		})
	}
}

// CalendarDayUTC must return midnight UTC, not merely a same-day instant: its other job is to be
// the value PERSISTED into a DATE column (budget_service.go, savings_goal_service.go), and pgx
// encodes a DATE from the value's own Year/Month/Day — so a non-UTC location would store a
// different day than the display string reports.
func TestCalendarDayUTC_IsMidnightInUTC(t *testing.T) {
	t.Parallel()

	got := CalendarDayUTC(time.Date(2026, 3, 15, 0, 0, 0, 0, time.FixedZone("+05:30", 5*3600+1800)))
	want := time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("CalendarDayUTC = %s (loc %v), want %s in UTC", got.Format(time.RFC3339Nano), got.Location(), want.Format(time.RFC3339Nano))
	}
	// Idempotent, so a site that normalises twice (once for the stored value, once for the
	// display) cannot drift.
	if again := CalendarDayUTC(got); !again.Equal(got) {
		t.Fatalf("CalendarDayUTC is not idempotent: %s then %s", got, again)
	}
}
