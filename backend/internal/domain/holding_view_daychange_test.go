package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue #1631 M7: HoldingView now carries the day change's ANCHOR and the server's staleness
// verdict, so a client can label a figure computed from an old close instead of presenting it as
// today's move.
func TestNewHoldingView_ExposesTheDayChangeAnchor(t *testing.T) {
	qty, price, value, close := 10.0, 105.0, 1050.0, 100.0

	t.Run("a fresh close yields a date and a not-stale verdict", func(t *testing.T) {
		asOf := time.Now().UTC().AddDate(0, 0, -1)
		v := NewHoldingView(&Holding{
			Quantity: qty, InstitutionPrice: &price, InstitutionValue: &value,
			Security: &Security{Name: "Fresh ETF", ClosePrice: &close, ClosePriceAsOf: &asOf},
		})
		require.NotNil(t, v.DayChangeAsOf)
		assert.Equal(t, asOf.Format("2006-01-02"), *v.DayChangeAsOf,
			"the anchor must be a date-only string; a timestamp shifts back a day west of UTC")
		require.NotNil(t, v.DayChangeIsStale)
		assert.False(t, *v.DayChangeIsStale)
	})

	t.Run("a month-old close is reported stale rather than suppressed", func(t *testing.T) {
		asOf := time.Now().UTC().AddDate(0, -1, 0)
		v := NewHoldingView(&Holding{
			Quantity: qty, InstitutionPrice: &price, InstitutionValue: &value,
			Security: &Security{Name: "Stale ETF", ClosePrice: &close, ClosePriceAsOf: &asOf},
		})
		require.NotNil(t, v.DayChangeIsStale)
		assert.True(t, *v.DayChangeIsStale)
		assert.NotNil(t, v.DayChange, "the figure is still served — labelled, not hidden")
	})

	t.Run("no day change means no staleness verdict", func(t *testing.T) {
		// Otherwise a client could read "not stale" as "trustworthy" when there is no figure.
		v := NewHoldingView(&Holding{
			Quantity: qty, InstitutionValue: &value,
			Security: &Security{Name: "No Price"},
		})
		assert.Nil(t, v.DayChange)
		assert.Nil(t, v.DayChangeIsStale)
	})

	t.Run("an undated close yields no anchor", func(t *testing.T) {
		v := NewHoldingView(&Holding{
			Quantity: qty, InstitutionPrice: &price, InstitutionValue: &value,
			Security: &Security{Name: "Undated", ClosePrice: &close},
		})
		assert.Nil(t, v.DayChangeAsOf)
		require.NotNil(t, v.DayChangeIsStale, "an undated price is not a stale one")
		assert.False(t, *v.DayChangeIsStale)
	})
}
