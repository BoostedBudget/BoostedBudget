//go:build integration

package repository

// Issue #1684 Task 6 — UpdateFromSync, the sibling that replaces Update on the `modified` path.
//
// WHY THIS IS AN INTEGRATION TEST AND NOT A pgxmock ONE. Every clause this method must replicate is
// a SQL-semantics claim a mock cannot evaluate: `COALESCE($n, pending_transaction_id_blind_idx)`
// (monotonic, so a re-delivery carrying no pointer must not erase a stored one), the
// `valid_category_source` CHECK that rejects '', and the custom-category name subquery that the
// stale-CASE rewrite must not silently drop.
//
// ⚠ NO EXISTING TEST CALLS THIS METHOD. TestSettledPending_UpdatePathAlsoLearnsThePointer_Integration
// exercises `Update`, and TestProcessModifiedTransactions_StrandedSettlementRepairFailureReturns
// asserts an error return — neither would notice the COALESCE going missing here. This file is the
// only thing standing between #1578's stranded-settlement repair and a silent regression on the
// exact path #1684 modifies.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestUpdateFromSync_KeepsThePointerAndWritesTheFullTuple_Integration is the whole contract of the
// sibling in one place: it is Update's provider-field write PLUS the coherent category tuple, tags
// and review state, in ONE statement.
func TestUpdateFromSync_KeepsThePointerAndWritesTheFullTuple_Integration(t *testing.T) {
	t.Run("pointer_is_learned_and_never_clobbered", func(t *testing.T) {
		ctx := context.Background()
		f := seedSettlementFixture(t)

		f.addPostedTwin(t, ctx, domain.Transaction{})
		require.Nil(t, f.storedPointer(t, ctx, f.postedID), "the twin arrived with no pointer")

		stored, err := f.repo.GetByID(ctx, f.postedID)
		require.NoError(t, err)
		stored.PendingTransactionID = &f.pendingExtID
		require.NoError(t, f.repo.UpdateFromSync(ctx, stored))

		require.NotNil(t, f.storedPointer(t, ctx, f.postedID),
			"UpdateFromSync must be able to teach a stored row its pending pointer")
		assert.Equal(t, f.enc.BlindIndex(f.pendingExtID), *f.storedPointer(t, ctx, f.postedID),
			"the pointer is derived with the SAME blind index external_transaction_id uses")

		// ⚠ THE COALESCE. A re-delivery of an ordinary (non-settlement) modification carries no
		// pointer; without COALESCE this statement would erase the one learned above, and
		// pullForwardStrandedSettlements would find nothing to repair.
		stored.PendingTransactionID = nil
		require.NoError(t, f.repo.UpdateFromSync(ctx, stored))
		require.NotNil(t, f.storedPointer(t, ctx, f.postedID),
			"a later UpdateFromSync carrying no pointer must not clobber the stored one")
	})

	t.Run("custom_override_resolves_its_display_name", func(t *testing.T) {
		ctx := context.Background()
		f := seedSettlementFixture(t)

		customID := uuid.New().String()
		_, err := f.tx.Exec(ctx, `
			INSERT INTO custom_categories (id, user_id, name) VALUES ($1, $2, 'Pets')
		`, customID, f.userID)
		require.NoError(t, err, "seed custom category")

		f.addPostedTwin(t, ctx, domain.Transaction{})
		stored, err := f.repo.GetByID(ctx, f.postedID)
		require.NoError(t, err)

		kind := "custom"
		reviewedAt := time.Now().UTC().Truncate(time.Microsecond)
		stored.CategoryOverrideKind = &kind
		stored.CustomCategoryOverrideID = &customID
		stored.PFCPrimaryOverride = nil
		stored.PFCDetailedOverride = nil
		stored.CategorySourceField = domain.CategorySourceRule
		stored.Reviewed = true
		stored.ReviewedAt = &reviewedAt
		stored.Tags = []string{"pet"}
		stored.TagsSource = []domain.TagSource{{Tag: "pet", Source: "rule"}}
		require.NoError(t, f.repo.UpdateFromSync(ctx, stored))

		back, err := f.repo.GetByID(ctx, f.postedID)
		require.NoError(t, err)
		require.NotNil(t, back.CategoryOverrideKind)
		assert.Equal(t, "custom", *back.CategoryOverrideKind)
		require.NotNil(t, back.CustomCategoryOverrideID)
		assert.Equal(t, customID, *back.CustomCategoryOverrideID)
		assert.Nil(t, back.PFCPrimaryOverride, "kind='custom' requires both pfc overrides NULL")
		assert.Nil(t, back.PFCDetailedOverride)

		// The display name for kind='custom' is NOT on the transaction — it lives in
		// custom_categories. Dropping that resolution would store NULL and render a blank category.
		require.NotNil(t, back.EffectiveCategoryName,
			"kind='custom' must resolve its display name from custom_categories")
		assert.Equal(t, "Pets", *back.EffectiveCategoryName)

		assert.Equal(t, []string{"pet"}, back.Tags)
		require.Len(t, back.TagsSource, 1, "tags_source must travel with tags or the tag loses provenance")
		assert.Equal(t, "rule", back.TagsSource[0].Source)
		assert.True(t, back.Reviewed)
		require.NotNil(t, back.ReviewedAt)
		assert.WithinDuration(t, reviewedAt, *back.ReviewedAt, time.Millisecond)
	})

	t.Run("null_category_source_is_normalised_to_plaid", func(t *testing.T) {
		ctx := context.Background()
		f := seedSettlementFixture(t)

		f.addPostedTwin(t, ctx, domain.Transaction{})
		_, err := f.tx.Exec(ctx,
			`UPDATE transactions SET category_source = NULL WHERE id = $1`, f.postedID)
		require.NoError(t, err, "legacy rows can carry a NULL category_source")

		stored, err := f.repo.GetByID(ctx, f.postedID)
		require.NoError(t, err)
		assert.Equal(t, domain.CategorySource(""), stored.CategorySourceField,
			"the scan hands back '' for a NULL column — it cannot tell NULL from ''")

		require.NoError(t, f.repo.UpdateFromSync(ctx, stored))

		var source *string
		require.NoError(t, f.tx.QueryRow(ctx,
			`SELECT category_source FROM transactions WHERE id = $1`, f.postedID).Scan(&source))
		require.NotNil(t, source, "writing NULL back is as wrong as writing ''")
		assert.Equal(t, string(domain.CategorySourcePlaid), *source,
			"valid_category_source rejects ''; the legacy NULL must land as 'plaid'")
	})

	t.Run("unknown_row_is_not_found", func(t *testing.T) {
		ctx := context.Background()
		f := seedSettlementFixture(t)

		err := f.repo.UpdateFromSync(ctx, &domain.Transaction{
			ID: uuid.New().String(), UserID: f.userID,
		})
		assert.ErrorIs(t, err, ErrTransactionNotFound,
			"the sibling keeps Update's not-found contract so the sync can hold its cursor")
	})
}
