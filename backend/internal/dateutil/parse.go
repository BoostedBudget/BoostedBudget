// Package dateutil provides shared date parsing and validation utilities
// for use in Huma operations and services.
package dateutil

import (
	"fmt"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

const dateLayout = "2006-01-02"

// ParseDate parses a required YYYY-MM-DD date string, returning a huma 400 error
// with a consistent message if the input is empty or malformed.
func ParseDate(s, field string) (time.Time, error) {
	if s == "" {
		return time.Time{}, huma.Error400BadRequest(fmt.Sprintf("%s is required", field))
	}
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return time.Time{}, huma.Error400BadRequest(fmt.Sprintf("Invalid %s format, expected YYYY-MM-DD", field))
	}
	return t, nil
}

// ParseOptionalDate parses an optional YYYY-MM-DD date string.
// Returns nil if the input is empty, a pointer to the parsed time if valid,
// or a huma 400 error if the input is non-empty but malformed.
func ParseOptionalDate(s, field string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return nil, huma.Error400BadRequest(fmt.Sprintf("Invalid %s format, expected YYYY-MM-DD", field))
	}
	return &t, nil
}

// ParseDateRange parses a required start/end date pair, validates that end is not
// before start, and returns both times. Returns a huma 400 error on any failure.
func ParseDateRange(startStr, endStr string) (start, end time.Time, err error) {
	start, err = ParseDate(startStr, "start_date")
	if err != nil {
		return
	}
	end, err = ParseDate(endStr, "end_date")
	if err != nil {
		return
	}
	err = ValidateDateRange(start, end)
	return
}

// ParseOptionalDateRange parses an optional start/end date pair.
// Either or both may be empty, in which case nil pointers are returned.
// Returns a huma 400 error if any non-empty value is malformed.
func ParseOptionalDateRange(startStr, endStr string) (start, end *time.Time, err error) {
	start, err = ParseOptionalDate(startStr, "start_date")
	if err != nil {
		return
	}
	end, err = ParseOptionalDate(endStr, "end_date")
	return
}
