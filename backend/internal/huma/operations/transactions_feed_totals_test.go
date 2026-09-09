package operations

// Issue #1626 Phase 2 — the wire hop for the whole-set totals.
//
// WHY THIS FILE EXISTS. Three layers document the invariant "nil means NOT COMPUTED, never zero":
// the repository returns *domain.FeedTotals, applyFeedTotals refuses to write zeroes for nil, and
// the output body's own doc says "omitted, not zeroed". Nothing tested the last hop — the one that
// actually decides what a client sees — and while the body's fields were `int`/`float64` with
// `,omitempty` the invariant was not merely untested but UNIMPLEMENTABLE: the zero value IS the
// omitted value, so a computed (0, 0) was byte-identical to "not computed", and the DEFAULT
// unfiltered feed (mixed income and expense, netting to exactly zero) reported a present
// total_count beside an absent total_amount, which a client following the documented convention
// must read as "not computed" — a lie about a number that was computed.
//
// The cases below are written against the JSON, because presence-vs-absence is a property of the
// serialised body and of nothing else.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ptrTo takes the address of a literal. The whole-set totals are pointer-typed precisely so a
// computed zero is not the absent value, which means every fixture has to spell out which it means.
func ptrTo[T any](v T) *T { return &v }

func TestGetTransactions_WholeSetTotals_OmittedNeverZeroed(t *testing.T) {
	ptrInt := func(i int) *int { return ptrTo(i) }
	ptrFloat := func(f float64) *float64 { return ptrTo(f) }

	bodyJSON := func(t *testing.T, resp *services.TransactionListResponse) map[string]any {
		t.Helper()
		handler := NewTransactionHandler(&mockTransactionService{listResponse: resp})
		output, err := handler.GetTransactions(context.Background(), &GetTransactionsInput{
			AuthenticatedInput:    inputs.AuthenticatedInput{UserID: "user-123"},
			CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		})
		require.NoError(t, err)
		require.NotNil(t, output)
		raw, err := json.Marshal(output.Body)
		require.NoError(t, err)
		var decoded map[string]any
		require.NoError(t, json.Unmarshal(raw, &decoded))
		return decoded
	}

	base := func(count *int, amount *float64) *services.TransactionListResponse {
		return &services.TransactionListResponse{
			Transactions: []*domain.ActivityItem{},
			Limit:        50,
			TotalCount:   count,
			TotalAmount:  amount,
		}
	}

	t.Run("computed totals reach the body", func(t *testing.T) {
		got := bodyJSON(t, base(ptrInt(3), ptrFloat(42.5)))
		assert.Equal(t, float64(3), got["total_count"],
			"the handler must copy the service's count onto the body — nothing tested this hop before")
		assert.InDelta(t, 42.5, got["total_amount"], 0.0001)
	})

	t.Run("a computed EMPTY set stays present as zeroes", func(t *testing.T) {
		got := bodyJSON(t, base(ptrInt(0), ptrFloat(0)))
		require.Contains(t, got, "total_count",
			"first page, filter matches nothing: the totals WERE computed and are (0, 0). Dropping "+
				"them makes the response byte-identical to a cursored page that computed nothing, so "+
				"the client cannot render \"0 results, $0.00\" — the case where a set total is most useful")
		require.Contains(t, got, "total_amount")
		assert.Equal(t, float64(0), got["total_count"])
		assert.Equal(t, float64(0), got["total_amount"])
	})

	t.Run("a non-empty set netting to zero keeps total_amount present", func(t *testing.T) {
		got := bodyJSON(t, base(ptrInt(5), ptrFloat(0)))
		require.Contains(t, got, "total_amount",
			"the DEFAULT unfiltered feed mixes income and expense under PFCv2 signs, so netting to "+
				"exactly 0 is ordinary. Omitting it beside a present total_count of 5 tells a client "+
				"following the documented convention that the amount was not computed, which is false")
		assert.Equal(t, float64(0), got["total_amount"])
		assert.Equal(t, float64(5), got["total_count"])
	})

	t.Run("not computed omits both", func(t *testing.T) {
		got := bodyJSON(t, base(nil, nil))
		assert.NotContains(t, got, "total_count",
			"a cursored page computes no aggregate; the fields must be ABSENT so the client can tell "+
				"\"not computed\" from a real zero")
		assert.NotContains(t, got, "total_amount")
	})
}
