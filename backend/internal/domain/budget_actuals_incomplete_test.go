package domain

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActualsIncomplete pins the observable contract of ErrBudgetActualsIncomplete and its
// reasons-carrying wrapper (Issue #1685): the sentinel is reachable through errors.Is from both a
// bare wrap and a constructed instance, errors.As recovers the reasons, and those reasons are
// de-duplicated and sorted on construction.
func TestActualsIncomplete(t *testing.T) {
	// A bare fmt.Errorf wrap of the sentinel must still satisfy errors.Is — this is the shape the
	// sibling read barriers use.
	bare := fmt.Errorf("publish aborted: %w", ErrBudgetActualsIncomplete)
	assert.True(t, errors.Is(bare, ErrBudgetActualsIncomplete),
		"a bare fmt.Errorf wrap of the sentinel must satisfy errors.Is")

	// Three reasons, one duplicated and supplied out of order, must round-trip as two, sorted.
	err := NewBudgetActualsIncompleteError("zeta_reason", "alpha_reason", "zeta_reason")

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBudgetActualsIncomplete),
		"a constructed error must satisfy errors.Is against the sentinel")

	var incomplete *BudgetActualsIncompleteError
	require.True(t, errors.As(err, &incomplete),
		"errors.As must recover the reasons-carrying type")
	assert.Equal(t, []string{"alpha_reason", "zeta_reason"}, incomplete.Reasons,
		"reasons must be de-duplicated and sorted on construction")
}

// TestActualsIncompleteEmptyReasons pins the degenerate case: zero reasons still satisfies
// errors.Is, recovers the type, and never panics when formatted.
func TestActualsIncompleteEmptyReasons(t *testing.T) {
	err := NewBudgetActualsIncompleteError()

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBudgetActualsIncomplete),
		"zero reasons must still satisfy errors.Is")
	assert.NotPanics(t, func() { _ = err.Error() },
		"formatting an error with zero reasons must not panic")

	var incomplete *BudgetActualsIncompleteError
	require.True(t, errors.As(err, &incomplete),
		"errors.As must recover the type even with zero reasons")
	assert.Empty(t, incomplete.Reasons)
}
