package domain

import (
	"fmt"
	"time"
)

// FlexibleDate is a time.Time-shaped wire type for REQUEST fields that write a Postgres DATE
// column (#1631 Batch F2: budgets.start_date/end_date, savings_goals.deadline). It accepts EITHER
// a full RFC3339 timestamp or a bare "YYYY-MM-DD" date-only string on unmarshal.
//
// ⚠ WHY BOTH FORMS, DELIBERATELY, RATHER THAN NARROWING TO JUST THE DATE-ONLY FORM. Today's
// frontend sends `new Date(dateOnly).toISOString()` (CreateBudgetDialog.tsx,
// CreateSavingsGoalDialog.tsx, UpdateSavingsGoalDialog.tsx) — an RFC3339 timestamp — and that
// round-trips stably today because a date-only string parses as UTC midnight. Rejecting that form
// would break the in-flight frontend and any third-party caller already sending RFC3339. So this
// widens acceptance rather than narrowing it: both forms are valid forever, not just during a
// migration window.
//
// ⚠ WHY A NAMED TYPE RATHER THAN LEAVING THE FIELD AS time.Time. encoding/json dispatches to a
// field's own UnmarshalJSON, or (via the encoding.TextUnmarshaler fallback for a bare JSON string)
// its own UnmarshalText — and time.Time's own unmarshal logic requires RFC3339, hard rejecting a
// bare date. There is no way to widen time.Time's parsing per-field without wrapping it in a type
// that carries its own unmarshal method.
//
// ⚠ WHY encoding.TextUnmarshaler, NOT json.Unmarshaler. Huma generates the OpenAPI/JSON-schema
// for a field from its Go type, and separately VALIDATES incoming request bodies against that
// generated schema BEFORE ever unmarshaling into the Go struct (huma's validateBody: parse body
// into a generic `any`, validate against the schema, THEN json.Unmarshal into the typed struct —
// see huma.go's validateBody/parseBodyInto). If this type surfaced a `format:"date-time"` schema
// (what a raw time.Time gets), huma's own format validator (validateFormat in the huma package)
// would reject a bare "2026-09-01" as not-RFC3339 at the schema-validation pass, before
// UnmarshalJSON/UnmarshalText below ever ran — the widened parsing here would be dead code and
// every bare-date request would still 422. huma's schemaFromType has a specific carve-out: a type
// implementing encoding.TextUnmarshaler (and NOT huma.SchemaProvider) gets a bare
// `{type: "string"}` schema with NO format, which the format validator's switch does not
// recognize and therefore does not check. That is the whole reason this type implements
// TextUnmarshaler/TextMarshaler and not json.Marshaler/json.Unmarshaler — the interface choice is
// what turns off huma's own too-strict format check.
type FlexibleDate time.Time

// UnmarshalText implements encoding.TextUnmarshaler. It tries, in order: RFC3339Nano, RFC3339,
// then a bare "YYYY-MM-DD" date-only string (time.DateOnly). The first one that parses wins.
func (d *FlexibleDate) UnmarshalText(text []byte) error {
	s := string(text)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, time.DateOnly} {
		if t, err := time.Parse(layout, s); err == nil {
			*d = FlexibleDate(t)
			return nil
		}
	}
	return fmt.Errorf("invalid date %q: expected an RFC3339 timestamp or a YYYY-MM-DD date", s)
}

// MarshalText implements encoding.TextMarshaler, matching time.Time's own RFC3339Nano rendering.
func (d FlexibleDate) MarshalText() ([]byte, error) {
	return time.Time(d).MarshalText()
}
