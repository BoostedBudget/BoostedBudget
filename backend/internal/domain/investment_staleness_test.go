package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Issue #1631 M7: close_price_as_of was write-only, so "day change" could be computed from a
// week-old close with nothing able to notice. These cases pin the bound and, importantly, the
// weekend case — the reason the bound is not one day.
func TestHolding_ClosePriceIsStale(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC) // a Monday
	mk := func(asOf *time.Time) *Holding {
		return &Holding{Security: &Security{ClosePriceAsOf: asOf}}
	}
	at := func(y int, m time.Month, d int) *time.Time {
		v := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		return &v
	}

	t.Run("Friday's close is fresh on Monday", func(t *testing.T) {
		// The ordinary case, and the reason a one-day bound would be wrong: Plaid's close price is
		// an overnight update, so the newest close available on a Monday is Friday's.
		assert.False(t, mk(at(2026, 9, 11)).ClosePriceIsStale(now))
	})

	t.Run("Thursday's close is fresh on Monday, covering a holiday weekend", func(t *testing.T) {
		assert.False(t, mk(at(2026, 9, 10)).ClosePriceIsStale(now))
	})

	t.Run("a week-old close is stale", func(t *testing.T) {
		assert.True(t, mk(at(2026, 9, 7)).ClosePriceIsStale(now))
	})

	t.Run("a month-old close is stale", func(t *testing.T) {
		assert.True(t, mk(at(2026, 8, 14)).ClosePriceIsStale(now))
	})

	t.Run("an absent timestamp is not reported as stale", func(t *testing.T) {
		// An undated price is a different problem from an old one. Reporting it as stale would
		// suppress the figure for every security whose provider dates it loosely.
		assert.False(t, mk(nil).ClosePriceIsStale(now))
	})

	t.Run("no security at all is not reported as stale", func(t *testing.T) {
		assert.False(t, (&Holding{}).ClosePriceIsStale(now))
	})
}
