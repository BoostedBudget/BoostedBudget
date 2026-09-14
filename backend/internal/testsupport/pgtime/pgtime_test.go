package pgtime

import (
	"testing"
	"time"
)

func TestStored_TruncatesOnlySubMicroseconds(t *testing.T) {
	for _, nanos := range []int{0, 1, 499, 500, 999, 1000, 1001, 538427527, 999999999} {
		input := time.Date(2026, 9, 13, 23, 52, 41, nanos, time.UTC)
		want := time.Date(2026, 9, 13, 23, 52, 41, nanos/1000*1000, time.UTC)
		got := Stored(input)
		if got != want {
			t.Errorf("Stored(%s) = %s, want %s", input.Format(time.RFC3339Nano), got.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
		}
		if got.Equal(Stored(input.Add(time.Microsecond))) {
			t.Errorf("Stored must preserve a one-microsecond difference for %s", input.Format(time.RFC3339Nano))
		}
	}
}

func TestStored_PreservesInstantAcrossLocations(t *testing.T) {
	zone := time.FixedZone("test-offset", -4*60*60)
	input := time.Date(2026, 9, 13, 19, 52, 41, 538427527, zone)
	want := time.Date(2026, 9, 13, 23, 52, 41, 538427000, time.UTC)
	if got := Stored(input); got != want {
		t.Fatalf("Stored = %#v, want %#v", got, want)
	}
}

func TestStored_PreUnixEpoch(t *testing.T) {
	input := time.Date(1969, 12, 31, 23, 59, 59, 999999999, time.UTC)
	want := time.Date(1969, 12, 31, 23, 59, 59, 999999000, time.UTC)
	if got := Stored(input); got != want {
		t.Fatalf("Stored = %s, want %s", got, want)
	}
}

func TestStored_RemovesMonotonicClockData(t *testing.T) {
	got := Stored(time.Now())
	if got != got.Round(0) || got.Location() != time.UTC {
		t.Fatalf("Stored must return a UTC wall-clock value: %#v", got)
	}
}
