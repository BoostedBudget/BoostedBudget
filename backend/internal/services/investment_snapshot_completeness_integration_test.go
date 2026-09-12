//go:build integration

package services

// investment_snapshot_completeness_integration_test.go — Issue #1569 Task 4, fix round 1.
//
// WHAT THIS GUARDS. Task 4's completeness gate correctly stops SyncHoldings from wiping an
// account's holdings when that account is absent from Plaid's response. The fix-round-1 defect
// was reusing domain.HoldingSyncResult.Errors to record that skip: Errors also gates the
// portfolio snapshot upsert (SyncHoldings, "Issue #138" block), so an account Plaid has
// PERMANENTLY stopped returning — Plaid's own AccountBase.AccountId doc comment: "If an account
// ... disappears instead of changing, the account is likely closed. Closed accounts are not
// returned by the Plaid API" — would suppress this user's portfolio snapshot on every sync,
// forever, even though the preserved holdings are exactly what the snapshot should be summing.
//
// The fix moved that bookkeeping to its own field, HoldingSyncResult.SkippedIncomplete, which
// still drives ProcessWebhook's retry decision but does NOT gate the snapshot. This test proves
// the fix end to end against a REAL Postgres: a real holdings row, a real portfolio_snapshots
// aggregate computed by portfolio_snapshot_repository.go's SQL (SUM(institution_value) joined
// through account_status_history), and the real investmentSyncService.SyncHoldings orchestration.
// A pgxmock/unit-level test cannot show this, because the "total" in question is computed by
// Postgres from committed rows, not passed through Go by the service under test.
//
// Run with:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/... \
//	    -run TestSyncHoldings_PermanentlyAbsentAccount_StillWritesSnapshot \
//	    -tags integration -p 1 -count=1 -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/plaid/plaid-go/v23/plaid"
)

// TestSyncHoldings_PermanentlyAbsentAccount_StillWritesSnapshot is the fix-round-1 regression:
// an account whose holdings clear is skipped because Plaid's response never mentions it (the
// stable "closed account" case, not a transient partial response) must still let today's
// portfolio snapshot get written, and that snapshot's total must include the preserved holding.
func TestSyncHoldings_PermanentlyAbsentAccount_StillWritesSnapshot(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	linkedItemID := uuid.New().String()
	accountID := uuid.New().String()
	securityID := uuid.New().String()
	plaidAccountID := "plaid-acct-closed-" + accountID

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "int-snap|"+userID, "int-snap-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-snap-test-token')
	`, linkedItemID, userID, "snap-item-"+linkedItemID)
	require.NoError(t, err, "seed linked_item")

	// Seeded with status 'active' — this account has NOT been marked removed. Task 5 (not this
	// task) is what eventually flips that once an accounts-list response confirms it's gone; this
	// test is specifically about the WINDOW before that happens, where the account is still
	// 'active' in our DB but Plaid has already stopped returning it from /investments/holdings/get.
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Closed Brokerage', 'investment', 'active')
	`, accountID, linkedItemID, userID, plaidAccountID)
	require.NoError(t, err, "seed account")

	_, err = tx.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, ticker_symbol)
		VALUES ($1, $2, 'Preserved Security', 'PRSV')
	`, securityID, "plaid-sec-"+securityID)
	require.NoError(t, err, "seed security")

	const preservedValue = 5000.00
	holdingID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
		VALUES ($1, $2, $3, $4, 10, 4000.00, $5)
	`, holdingID, userID, accountID, securityID, preservedValue)
	require.NoError(t, err, "seed pre-existing holding — this is what the fix must preserve AND the snapshot must sum")

	enc, err := appcrypto.NewFieldEncryptor(nil, []byte("12345678901234567890123456789012"))
	require.NoError(t, err, "nil EncryptionService is safe here — every seeded PII field above is "+
		"plaintext (no ENC$ prefix), so DecryptString takes the passthrough branch and never calls "+
		"the underlying service")

	accountRepo := repository.NewAccountRepository(tx, enc)
	holdingRepo := repository.NewHoldingRepository(tx)
	securityRepo := repository.NewSecurityRepository(tx)
	snapshotRepo := repository.NewPortfolioSnapshotRepository(tx)

	// Plaid's response for this call: the account is not merely holding-less, it is ABSENT from
	// `accounts` entirely — the permanently-closed-account shape, not a genuine empty portfolio.
	mockPlaid := new(MockPlaidService)
	holdingsResp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{},
		[]plaid.Holding{},
		[]plaid.Security{},
		makeMinimalPlaidItem(),
		"req-snap-completeness",
	)
	mockPlaid.On("InvestmentsHoldingsGet", ctx, "tok").Return(holdingsResp, nil)

	svc := NewInvestmentSyncService(
		nil, // webhookRepo — unused by SyncHoldings
		nil, // linkedItemRepo — unused by SyncHoldings (linkedItem is passed in directly)
		accountRepo,
		securityRepo,
		holdingRepo,
		nil, // investmentTxnRepo — unused by SyncHoldings
		snapshotRepo,
		nil, // userAccountRepo — only SyncInvestmentTransactions consumes backfill obligations
		mockPlaid,
		nil, // logger
	)

	linkedItem := &domain.LinkedItem{ID: linkedItemID, UserID: userID, AccessToken: "tok"}

	result, err := svc.SyncHoldings(ctx, linkedItem)
	require.NoError(t, err)

	// The wipe-prevention itself (Task 4 core fix): no correctness error, and the skip is
	// recorded on the NEW field, not the one that gates the snapshot.
	assert.Empty(t, result.Errors, "a permanently-absent account is not a correctness failure")
	require.Len(t, result.SkippedIncomplete, 1)
	assert.Contains(t, result.SkippedIncomplete[0], plaidAccountID)
	assert.Equal(t, 0, result.AccountsProcessed)

	// The holding itself must still be there — untouched by the skipped clear.
	preserved, err := holdingRepo.GetByAccountID(ctx, accountID)
	require.NoError(t, err)
	require.Len(t, preserved, 1, "the pre-existing holding must survive the skipped clear")
	require.NotNil(t, preserved[0].InstitutionValue)
	assert.InDelta(t, preservedValue, *preserved[0].InstitutionValue, 0.001)

	// THE FIX-ROUND-1 REGRESSION: the snapshot must have been written for today (not suppressed),
	// and its total must include the preserved holding's value.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	snap, err := snapshotRepo.GetLatestBefore(ctx, userID, today.AddDate(0, 0, 1))
	require.NoError(t, err)
	require.NotNil(t, snap, "portfolio snapshot must be written even though one account's holdings "+
		"clear was skipped — SkippedIncomplete must not suppress it the way Errors does")
	assert.InDelta(t, preservedValue, snap.TotalValue, 0.001,
		"the snapshot total must include the preserved holding — it is exactly what should be summed")
}

// TestSyncHoldings_NonInvestmentHoldingIsSkippedWithoutRewritingHistory pins the #1640/#1616
// retention decision against real Postgres. A Plaid holding attached to a depository account is
// outside the investment universe, but the provider reporting it is not evidence that a retained
// legacy row was liquidated. The sync therefore ignores the new payload without replacing the live
// row, recording a false empty observation, or creating a discontinuity in the legacy aggregate.
func TestSyncHoldings_NonInvestmentHoldingIsSkippedWithoutRewritingHistory(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	linkedItemID := uuid.New().String()
	accountID := uuid.New().String()
	securityID := uuid.New().String()
	plaidAccountID := "plaid-depository-" + accountID
	plaidSecurityID := "plaid-security-" + securityID

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "int-type-gate|"+userID, "int-type-gate-"+userID+"@example.com")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-type-gate-token')
	`, linkedItemID, userID, "type-gate-item-"+linkedItemID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Cash Management', 'depository', 'active')
	`, accountID, linkedItemID, userID, plaidAccountID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, ticker_symbol)
		VALUES ($1, $2, 'Legacy Cash Position', 'CASH')
	`, securityID, plaidSecurityID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
		VALUES ($1, $2, $3, $4, 100, 100.00, 100.00)
	`, uuid.New().String(), userID, accountID, securityID)
	require.NoError(t, err)

	enc, err := appcrypto.NewFieldEncryptor(nil, []byte("12345678901234567890123456789012"))
	require.NoError(t, err)
	accountRepo := repository.NewAccountRepository(tx, enc)
	holdingRepo := repository.NewHoldingRepository(tx)
	securityRepo := repository.NewSecurityRepository(tx)
	snapshotRepo := repository.NewPortfolioSnapshotRepository(tx)

	mockPlaid := new(MockPlaidService)
	response := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{{AccountId: plaidAccountID}},
		[]plaid.Holding{makePlaidHolding(plaidAccountID, plaidSecurityID, 999, 1, 999, 999)},
		[]plaid.Security{makePlaidSecurity(plaidSecurityID, "Provider Cash Position", "CASH")},
		makeMinimalPlaidItem(),
		"req-non-investment-skip",
	)
	mockPlaid.On("InvestmentsHoldingsGet", ctx, "tok").Return(response, nil)

	svc := NewInvestmentSyncService(
		nil, nil, accountRepo, securityRepo, holdingRepo, nil, snapshotRepo, nil, mockPlaid, nil,
	)
	result, err := svc.SyncHoldings(ctx, &domain.LinkedItem{ID: linkedItemID, UserID: userID, AccessToken: "tok"})
	require.NoError(t, err)

	assert.Equal(t, 1, result.AccountsSkippedNonInvestment)
	assert.Equal(t, 1, result.HoldingsSkippedNonInvestment)
	assert.Zero(t, result.AccountsProcessed)
	assert.Empty(t, result.Errors)
	assert.Empty(t, result.SkippedIncomplete)

	retained, err := holdingRepo.GetByAccountID(ctx, accountID)
	require.NoError(t, err)
	require.Len(t, retained, 1)
	assert.InDelta(t, 100, retained[0].Quantity, 0.001)
	require.NotNil(t, retained[0].InstitutionValue)
	assert.InDelta(t, 100, *retained[0].InstitutionValue, 0.001)

	var observations int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT count(*) FROM holding_observations WHERE user_id = $1 AND account_id = $2`,
		userID, accountID,
	).Scan(&observations))
	assert.Zero(t, observations, "a type-policy skip must not claim Plaid observed an empty account")

	today := time.Now().UTC().Truncate(24 * time.Hour)
	snapshot, err := snapshotRepo.GetLatestBefore(ctx, userID, today.AddDate(0, 0, 1))
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	assert.InDelta(t, 100, snapshot.TotalValue, 0.001,
		"the legacy ungated series retains its prior universe instead of stepping down at deployment")
}
