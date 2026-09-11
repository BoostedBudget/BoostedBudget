package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// FlexibleDate accepts either a plain calendar day ("2026-09-01") or a full RFC3339
// timestamp ("2026-09-01T00:00:00Z") on unmarshal, and always marshals back out as
// RFC3339 (matching the wire format time.Time already produced).
//
// # Why this type exists (Issue #1630 Task 9)
//
// Every READ endpoint in this API documents and accepts a plain "YYYY-MM-DD" string via
// a `query:"..."` string field with a `(YYYY-MM-DD)` doc hint. But
// CreateBudgetRequest.StartDate/EndDate were plain time.Time, which Huma publishes as
// `format: date-time` and validates strictly against RFC3339 — so "2026-09-01" alone
// 422'd on writes while every read endpoint accepted it. Undiscoverable and
// inconsistent, and the existing frontend already sends the RFC3339 form, so neither
// form can be dropped.
//
// Three fixes were considered and rejected:
//  1. A Huma resolver: cannot work. StartDate's Go type decides how the JSON body is
//     unmarshalled; "2026-09-01" fails that unmarshal before any resolver ever runs.
//  2. A bare `format:"date"` tag on a string field: cannot work either. Huma validates
//     format strictly, so it would reject the RFC3339 form the frontend already sends.
//  3. Omitting `format` entirely: leaves the field undiscoverable (the actual
//     complaint) and starves a later MCP schema-flattening task that needs `format` on
//     every leaf field.
//
// Instead, this type implements huma.SchemaProvider to publish
// `anyOf: [{type: string, format: date}, {type: string, format: date-time}]` — Huma's
// own request validation (huma/v2 validate.go validateAnyOf) accepts the request as
// long as at least one alternative matches, so either form passes — and implements its
// own UnmarshalJSON to accept both forms once the struct is decoded a second time.
//
// ⚠ New shared surface: no shared date type existed in this repo before this. Keep this
// type's surface small and do not retrofit other endpoints onto it in the same change
// that introduces it — every other read endpoint's plain `string` + doc hint pattern is
// untouched by design.
type FlexibleDate time.Time

// Time returns the underlying time.Time value.
func (d FlexibleDate) Time() time.Time {
	return time.Time(d)
}

// String implements fmt.Stringer, formatting as RFC3339.
func (d FlexibleDate) String() string {
	return time.Time(d).Format(time.RFC3339)
}

// MarshalJSON always emits RFC3339, matching the pre-existing time.Time wire format so
// no response shape changes.
func (d FlexibleDate) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(d))
}

// UnmarshalJSON accepts either "YYYY-MM-DD" (time.DateOnly) or a full RFC3339
// timestamp. Any other shape is rejected with a message naming the field's legal
// formats.
func (d *FlexibleDate) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("invalid date: must be a JSON string in YYYY-MM-DD or RFC3339 format")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		*d = FlexibleDate(t)
		return nil
	}
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		*d = FlexibleDate(t)
		return nil
	}
	return fmt.Errorf("invalid date %q: must be YYYY-MM-DD (e.g. 2026-09-01) or RFC3339 (e.g. 2026-09-01T00:00:00Z)", s)
}

// EndOfDay returns the instant to use as an INCLUSIVE end-of-range boundary for this value.
//
// Issue #1630 review round 2: a date-only value ("2026-09-30") unmarshals to midnight
// (00:00:00, via UnmarshalJSON's time.DateOnly branch) — used bare as a range's end
// boundary, it would exclude the entire calendar day it names. A full RFC3339 timestamp,
// in contrast, already names an exact instant the caller chose deliberately (e.g.
// "2026-09-30T15:00:00Z") and must be respected exactly, not pushed forward to nearly a
// full day later.
//
// FlexibleDate carries no separate "was this date-only" flag — it is a plain time.Time
// alias — so this distinguishes the two cases by wall-clock time: a value that lands
// exactly on midnight (00:00:00.000000000, in whatever location it carries) is treated as
// a date-only marker and extended to the last representable instant of that calendar day
// (23:59:59.999999999, same location); anything else is returned completely unchanged.
// Every caller that uses a FlexibleDate as a range's END boundary (never its start) should
// call this instead of Time().
func (d FlexibleDate) EndOfDay() time.Time {
	t := time.Time(d)
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 {
		return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, t.Location())
	}
	return t
}

// Schema implements huma.SchemaProvider, publishing anyOf[date, date-time] so the field
// stays discoverable (documents YYYY-MM-DD like every read endpoint) while continuing
// to accept the RFC3339 timestamps existing callers already send. Huma validates the
// raw request body against this schema (huma/v2 huma.go validateBody) before decoding
// into the Go struct, so this is what actually lets both forms through — UnmarshalJSON
// above only runs on the (already-validated) second decode.
func (FlexibleDate) Schema(r huma.Registry) *huma.Schema {
	return &huma.Schema{
		AnyOf: []*huma.Schema{
			{Type: "string", Format: "date"},
			{Type: "string", Format: "date-time"},
		},
	}
}
