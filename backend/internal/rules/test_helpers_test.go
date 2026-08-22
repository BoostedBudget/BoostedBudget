package rules

import (
	"time"
)

// strPtr returns a pointer to the string.
func strPtr(s string) *string {
	return &s
}

// mustParseDate parses a YYYY-MM-DD date string or panics.
func mustParseDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}
