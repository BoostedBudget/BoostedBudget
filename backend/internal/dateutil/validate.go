package dateutil

import (
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// ValidateDateRange returns a huma 400 error if end is strictly before start.
func ValidateDateRange(start, end time.Time) error {
	if end.Before(start) {
		return huma.Error400BadRequest("end_date cannot be before start_date")
	}
	return nil
}
