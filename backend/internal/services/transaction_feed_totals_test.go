package services

// Issue #1626 Phase 2 — applyFeedTotals must carry ABSENCE, not a zero.
//
// The repository already distinguishes "not computed" (nil *domain.FeedTotals) from a computed
// zero. applyFeedTotals is the hop where that distinction used to die: the response's fields were
// non-pointer scalars, so writing a computed (0, 0) produced a response indistinguishable from one
// where nothing was computed. The cases below pin all three states — absent, computed zero, and
// computed non-zero — so the discipline the doc comment describes is a property of the code.

import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyFeedTotals_DistinguishesAbsenceFromZero(t *testing.T) {
	t.Run("nil leaves both fields absent", func(t *testing.T) {
		resp := &TransactionListResponse{}
		applyFeedTotals(resp, nil)
		assert.Nil(t, resp.TotalCount, "nil means NOT COMPUTED — writing 0 would claim a number")
		assert.Nil(t, resp.TotalAmount)
	})

	t.Run("a computed zero is present and zero", func(t *testing.T) {
		resp := &TransactionListResponse{}
		applyFeedTotals(resp, &domain.FeedTotals{Count: 0, Amount: 0})
		require.NotNil(t, resp.TotalCount,
			"an empty filtered set on the FIRST page did compute its totals; they are (0, 0) and must "+
				"reach the client as such")
		require.NotNil(t, resp.TotalAmount)
		assert.Equal(t, 0, *resp.TotalCount)
		assert.Equal(t, 0.0, *resp.TotalAmount)
	})

	t.Run("a computed non-zero is copied through", func(t *testing.T) {
		resp := &TransactionListResponse{}
		applyFeedTotals(resp, &domain.FeedTotals{Count: 7, Amount: -12.25})
		require.NotNil(t, resp.TotalCount)
		require.NotNil(t, resp.TotalAmount)
		assert.Equal(t, 7, *resp.TotalCount)
		assert.InDelta(t, -12.25, *resp.TotalAmount, 0.0001)
	})
}
