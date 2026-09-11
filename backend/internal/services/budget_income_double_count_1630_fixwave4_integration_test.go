//go:build integration

package services

// Issue #1630 fixwave 4 — a SECOND independent adversarial review found budget_income_service.go's
// CalculateBudgetIncome summed each configured income source's own query independently with NO
// cross-source deduplication. Since this branch widened GetIncomeSumByCategory to roll up custom
// children, a single inflow transaction could already be counted once via a `pfc_primary` source
// and again via a `tag` source that also matched it — the interface's own doc comment conceded
// this as a known limitation ("Users should configure non-overlapping income sources") instead of
// treating it as a defect, even though the budget's stated requirement is exact-once counting.
//
// This test seeds ONE real income deposit that is BOTH categorized pfc_primary=INCOME AND tagged
// "salary", configures a budget with both a pfc_primary=INCOME source and a tag=salary source, and
// asserts CalculateBudgetIncome returns the deposit's real amount exactly once — not doubled.
//
// Fixed by collecting all pfc_primary/tag sources into ONE GetIncomeSumForSources query whose
// predicates are OR-ed together (transaction_repository.go), so an overlapping row is still just
// one row in that query's result set. Not fixed by summing two separate per-source queries more
// cleverly — that shape cannot express "count this row once" no matter how the sums are combined
// afterward.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/services/... -tags integration -p 1 \
//	  -run TestClaim1630Fixwave4_IncomeSourcesDoNotDoubleCountAnOverlappingTransaction -v

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaim1630Fixwave4_IncomeSourcesDoNotDoubleCountAnOverlappingTransaction(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "claim1630-fixwave4-income-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token')
	`, linkedItemID, userID, "item-claim1630-fixwave4-income-"+linkedItemID)
	require.NoError(t, err, "seed linked item")

	accountID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-claim1630-fixwave4-income', 'Checking', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err, "seed account")

	periodStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.January, 31, 23, 59, 59, 0, time.UTC)
	depositDate := time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)

	// ONE real income deposit (inflow = negative amount under PFCv2), categorized pfc_primary=
	// INCOME AND tagged "salary" — matched by BOTH configured sources below.
	const seededIncome = 2750.00
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, source, amount, date, name, pending,
			 pfc_primary, tags)
		VALUES ($1, $2, $3, $4, 'plaid', $5, $6, 'Paycheck', false, 'INCOME', '["salary"]'::jsonb)
	`, uuid.New().String(), userID, accountID, linkedItemID, -seededIncome, depositDate)
	require.NoError(t, err, "seed the one income transaction both sources will independently match")

	enc := newTestFieldEncryptor(t)
	txnRepo := repository.NewTransactionRepository(tx, enc)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	incomeSvc := NewBudgetIncomeService(txnRepo.(IncomeTransactionGetter), logger)

	budget := &domain.Budget{
		ID:     uuid.New().String(),
		UserID: userID,
		IncomeSources: []domain.IncomeSource{
			{Type: domain.IncomeSourceTypePFCPrimary, Value: "INCOME"},
			{Type: domain.IncomeSourceTypeTag, Value: "salary"},
		},
	}

	total, err := incomeSvc.CalculateBudgetIncome(ctx, budget, periodStart, periodEnd)

	require.NoError(t, err)
	assert.Equal(t, seededIncome, total,
		"#1630 fixwave 4: the one seeded deposit matches BOTH the pfc_primary=INCOME source and "+
			"the tag=salary source, and must be counted exactly ONCE ($2750.00) — not doubled to "+
			"$5500.00 by summing two independent per-source queries")
}
