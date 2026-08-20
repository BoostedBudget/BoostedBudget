package repository

// Issue #1566 — DB-free unit coverage for the repository-level tenant-scoping assertions.
//
// The DB-backed rejection tests live in tenant_scoped_writes_integration_test.go (build tag
// `integration`). The cases here exercise assertSingleTenantOverrides / assertSingleTenantAccounts
// as pure functions, so they must NOT be integration-tagged.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestTenantAssertions_EmptyActor_Rejected closes a vacuous-match hole in the #1566 assertions,
// raised by adversarial review.
//
// Both assertions compared element.UserID against the passed userID and nothing else, so
// assertSingleTenantOverrides("", []{UserID: ""}) and assertSingleTenantAccounts("", []{UserID: ""})
// PASSED — no mismatch exists when both sides are empty. The tenancy check would then wave through a
// batch with no owner at all, leaving the DB's own NOT NULL/FK constraints as the only thing standing
// between that and rows nobody owns. An empty actor is never a valid tenant, so the assertion should
// say so rather than relying on a downstream constraint to notice.
//
// This test needs no database — it calls the assertion helpers directly with in-memory slices — so
// it lives in this untagged file and runs in the normal `go test ./...` unit suite, rather than
// beside the DB-backed tenancy tests in tenant_scoped_writes_integration_test.go.
func TestTenantAssertions_EmptyActor_Rejected(t *testing.T) {
	t.Run("overrides", func(t *testing.T) {
		err := assertSingleTenantOverrides("", []domain.MerchantNameOverride{{UserID: ""}})
		require.Error(t, err, "an empty actor must be rejected outright, not matched vacuously "+
			"against equally-empty element user_ids")
		assert.ErrorIs(t, err, ErrOverrideTenantMismatch)
	})

	t.Run("accounts", func(t *testing.T) {
		err := assertSingleTenantAccounts("", []domain.Account{{UserID: ""}})
		require.Error(t, err, "an empty actor must be rejected outright, not matched vacuously "+
			"against equally-empty element user_ids")
		assert.ErrorIs(t, err, ErrAccountBatchTenantMismatch)
	})

	t.Run("a real actor with a real matching element still passes", func(t *testing.T) {
		// Guards against over-correcting into rejecting everything.
		require.NoError(t, assertSingleTenantOverrides("u1", []domain.MerchantNameOverride{{UserID: "u1"}}))
		require.NoError(t, assertSingleTenantAccounts("u1", []domain.Account{{UserID: "u1"}}))
	})
}
