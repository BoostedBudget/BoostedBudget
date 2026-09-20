//go:build integration

package repository

// Issue #1569 round 1 (I-2). scanTransactionDestSliceWithState is now the SINGLE positional mapping
// from transactionSelectColumns to Transaction fields — four hand-maintained copies were
// consolidated into it. Nothing guarded the survivor.
//
// The gap this closes was demonstrated, not imagined: swapping `&tx.PFCPrimary` with
// `&tx.PFCDetailed` in that slice — two same-typed *string, i.e. silent category corruption on
// every read in the application — left `go build`, the full unit suite AND the full integration
// suite green. A length check cannot catch it; a mock cannot catch it (pgxmock hands back whatever
// the test author put in the row, in the order they put it). Only a real row written through real
// SQL and read back through the real scan can.
//
// So: write a DISTINCT sentinel into every column of each same-typed group and assert each one
// comes back in its own field.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestScanDestinations_MapColumnsToTheRightFields_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	linkedItemID := uuid.New().String()
	accountID := uuid.New().String()
	txnID := uuid.New().String()

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "scanmap-1569-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "test-item-"+linkedItemID)
	require.NoError(t, err, "seed linked_item")

	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Scan Map Checking', 'depository', 'active')
	`, accountID, linkedItemID, userID, "plaid-acc-scanmap-"+accountID)
	require.NoError(t, err, "seed account")

	// MANDATORY: transactions.user_account_id carries a LIVE FK to user_accounts(id)
	// (migrations/000034...up.sql:21), so the target must exist in the SAME tx BEFORE the
	// transactions INSERT. On user_accounts only user_id and institution_id are NOT NULL without a
	// default (migrations/000031...up.sql:21,25). Its id is the sentinel the round-trip asserts.
	userAccountID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id)
		VALUES ($1, $2, $3)
	`, userAccountID, userID, "ins-scanmap-"+userAccountID)
	require.NoError(t, err, "seed user_account")

	// Every value below is unique ACROSS ITS TYPE GROUP. Reusing a value anywhere in a group is
	// what would let a swap pass, so the sentinels spell out which column they came from. The five
	// PFC columns take real enum members instead of "*_SENTINEL" strings because CHECK constraints
	// (valid_pfc_primary, valid_pfc_detailed_suffix, valid_pfc_confidence_level,
	// valid_pfc_primary_override) reject anything else — they are still pairwise distinct, which is
	// all the test needs.
	//
	// ONE HONEST LIMITATION. effective_pfc_primary / effective_pfc_detailed are GENERATED ALWAYS
	// columns (`CASE WHEN category_override_kind = 'pfc' THEN pfc_*_override ELSE pfc_* END`), so
	// they cannot be given sentinels of their own; with category_override_kind = 'pfc' they take
	// the two override sentinels verbatim. A swap between effective_pfc_primary and
	// pfc_primary_override (identical values) is therefore NOT detected here. A swap between
	// effective_pfc_primary and effective_pfc_detailed IS — those hold distinct values — and so is
	// every swap in the rest of the *string group, which is the dangerous class: adjacent
	// same-typed destinations that no compiler, mock or length check can tell apart.
	authorizedDate := time.Date(2024, 3, 4, 0, 0, 0, 0, time.UTC)
	date := time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)
	reviewedAt := time.Date(2024, 3, 6, 12, 0, 0, 0, time.UTC)
	deletedAt := time.Date(2024, 3, 7, 12, 0, 0, 0, time.UTC)
	categoryOverrideDate := time.Date(2024, 3, 8, 12, 0, 0, 0, time.UTC)

	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (
			id, user_id, account_id, linked_item_id,
			source, external_transaction_id, external_transaction_id_blind_idx, external_metadata,
			amount, date, authorized_date, name, merchant_name,
			pfc_primary, pfc_detailed, pfc_confidence_level, pfc_category_icon_url,
			payment_channel, pending, user_notes, created_at, updated_at,
			tags, tags_source,
			category_override_kind, pfc_primary_override, pfc_detailed_override,
			category_source, category_override_date,
			effective_category_name,
			reviewed, reviewed_at,
			deleted_at, deleted_reason, deleted_by,
			user_account_id
		) VALUES (
			$1, $2, $3, $4,
			'plaid', NULL, $5, NULL,
			123.45, $6, $7, 'NAME_SENTINEL', 'MERCHANT_NAME_SENTINEL',
			'FOOD_AND_DRINK', 'FOOD_AND_DRINK_COFFEE', 'VERY_HIGH', 'ICON_URL_SENTINEL',
			'CHANNEL_SENTINEL', true, 'USER_NOTES_SENTINEL', NOW(), NOW(),
			'["TAG_SENTINEL"]'::jsonb, '[{"tag":"TAG_SENTINEL","source":"manual"}]'::jsonb,
			'pfc', 'TRAVEL', 'TRAVEL_FLIGHTS',
			'manual', $8,
			'EFFECTIVE_CATEGORY_NAME_SENTINEL',
			true, $9,
			$10, 'provider_removed_posted', 'DELETED_BY_SENTINEL',
			$11
		)
	`, txnID, userID, accountID, linkedItemID,
		"blind-scanmap-"+txnID, date, authorizedDate, categoryOverrideDate, reviewedAt, deletedAt,
		userAccountID)
	require.NoError(t, err, "seed transaction")

	repo := NewTransactionRepository(tx, newTestFieldEncryptor(t)).(*transactionRepository)

	// GetByID is deliberately NOT used: it filters deleted_at IS NULL, and the tombstone columns are
	// part of what has to round-trip. The query below is transactionSelectExpr verbatim, which is
	// exactly what every consolidated caller sends.
	var got domain.Transaction
	dest, state := repo.scanTransactionDestSliceWithState(&got)
	require.NoError(t, tx.QueryRow(ctx,
		"SELECT "+transactionSelectExpr+" FROM transactions WHERE id = $1", txnID).Scan(dest...))
	require.NoError(t, repo.postScanTransactionState(&got, state))

	deref := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}

	assert.Equal(t, txnID, got.ID)
	assert.Equal(t, userID, got.UserID)
	assert.Equal(t, accountID, got.AccountID)
	assert.Equal(t, linkedItemID, got.LinkedItemID)
	assert.Equal(t, domain.SourcePlaid, got.Source)
	assert.InDelta(t, 123.45, got.Amount, 0.0001)
	assert.True(t, got.Pending)
	assert.True(t, got.Reviewed)

	// --- *string group: the swap class. ---
	assert.Equal(t, "NAME_SENTINEL", got.Name)
	assert.Equal(t, "MERCHANT_NAME_SENTINEL", deref(got.MerchantName))
	assert.Equal(t, "FOOD_AND_DRINK", deref(got.PFCPrimary))
	assert.Equal(t, "FOOD_AND_DRINK_COFFEE", deref(got.PFCDetailed))
	assert.Equal(t, "VERY_HIGH", deref(got.PFCConfidenceLevel))
	assert.Equal(t, "ICON_URL_SENTINEL", deref(got.PFCCategoryIconURL))
	assert.Equal(t, "CHANNEL_SENTINEL", deref(got.PaymentChannel))
	assert.Equal(t, "USER_NOTES_SENTINEL", deref(got.UserNotes))
	assert.Equal(t, "pfc", deref(got.CategoryOverrideKind))
	assert.Equal(t, "TRAVEL", deref(got.PFCPrimaryOverride))
	assert.Equal(t, "TRAVEL_FLIGHTS", deref(got.PFCDetailedOverride))
	// Generated from the overrides (see the note above the INSERT), hence the shared values.
	assert.Equal(t, "TRAVEL", deref(got.EffectivePFCPrimary))
	assert.Equal(t, "TRAVEL_FLIGHTS", deref(got.EffectivePFCDetailed))
	assert.Equal(t, "EFFECTIVE_CATEGORY_NAME_SENTINEL", deref(got.EffectiveCategoryName))
	assert.Equal(t, "DELETED_BY_SENTINEL", deref(got.DeletedBy))
	// #1684: user_account_id is the tail column and deleted_by the one immediately before it —
	// both *string, so a swap between the two is invisible to the compiler, a mock and a length
	// check alike. Each sentinel must land in its own field.
	require.NotNil(t, got.UserAccountID)
	assert.Equal(t, userAccountID, *got.UserAccountID)
	assert.Equal(t, domain.CategorySourceManual, got.CategorySourceField)
	require.NotNil(t, got.DeletedReason)
	assert.Equal(t, domain.TxnDeletedProviderRemovedPosted, *got.DeletedReason)

	// --- *time.Time group: five same-typed destinations, five distinct days. ---
	assert.Equal(t, date.UTC(), got.Date.UTC())
	require.NotNil(t, got.AuthorizedDate)
	assert.Equal(t, authorizedDate.UTC(), got.AuthorizedDate.UTC())
	require.NotNil(t, got.CategoryOverrideDate)
	assert.Equal(t, categoryOverrideDate.UTC(), got.CategoryOverrideDate.UTC())
	require.NotNil(t, got.ReviewedAt)
	assert.Equal(t, reviewedAt.UTC(), got.ReviewedAt.UTC())
	require.NotNil(t, got.DeletedAt)
	assert.Equal(t, deletedAt.UTC(), got.DeletedAt.UTC())

	// --- []byte group: tags and tags_source are both JSONB and adjacent. ---
	assert.Equal(t, []string{"TAG_SENTINEL"}, got.Tags)
	require.Len(t, got.TagsSource, 1)
	assert.Equal(t, "TAG_SENTINEL", got.TagsSource[0].Tag)
	assert.Equal(t, "manual", got.TagsSource[0].Source)
}
