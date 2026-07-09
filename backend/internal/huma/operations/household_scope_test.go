package operations

import (
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRejectHouseholdScope_HardensAgainstMalformedValues is defense-in-depth, per Codex
// adversarial review (P2): the HTTP layer already constrains these fields via
// `enum:"personal,household"`, but rejectHouseholdScope itself must not rely solely on
// that — it must reject ANY non-personal value (not just the exact string "household"),
// so a direct/internal caller that bypasses Huma's request binding still can't slip a
// malformed scope value (e.g. "HOUSEHOLD", " household") through as if it were personal.
// Issue #1369 — Phase 5.
func TestRejectHouseholdScope_HardensAgainstMalformedValues(t *testing.T) {
	for _, scope := range []string{"household", "HOUSEHOLD", " household", "Household", "household "} {
		t.Run(scope, func(t *testing.T) {
			err := rejectHouseholdScope(scope)
			require.Error(t, err)
			var statusErr huma.StatusError
			require.ErrorAs(t, err, &statusErr)
			assert.Equal(t, 400, statusErr.GetStatus())
		})
	}
}

func TestRejectHouseholdScope_AllowsPersonalOrEmpty(t *testing.T) {
	for _, scope := range []string{"", "personal"} {
		t.Run(scope, func(t *testing.T) {
			assert.NoError(t, rejectHouseholdScope(scope))
		})
	}
}
