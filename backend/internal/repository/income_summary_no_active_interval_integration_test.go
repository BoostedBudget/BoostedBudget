//go:build integration

package repository

// Issue #1626 Phase 2 — accountWithNoActiveIntervalLeg widens IncomeSummary too, and that is a
// MONEY number, not a list.
//
// WHY THIS TEST EXISTS. The leg was added for the transactions feed, to make the reports Sankey's
// click-through checkable. But it is rendered inside retainedAccountVisibilityLegs, which has five
// call sites, and three of them are not the transactions feed — one of which is IncomeSummary, a
// `COALESCE(-SUM(it.amount),0) ... GROUP BY income_type` aggregate whose output is dividend and
// interest income shown to the user. Every rationale comment, both other integration tests and the
// PR narrative were about list/Sankey PARITY, so a reported total changed with no test naming it.
// This file names it.
//
// The behaviour is CORRECT — a disconnected chain keeps its history (#1569 task 2), and an account
// whose 'active' interval was never recorded is a backfill artifact rather than evidence the income
// should be hidden. What was missing was a test that would notice if it ever changed, in either
// direction.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIncomeSummary_AccountWithNoActiveInterval_StillCountsItsIncome_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	prefix := uniquePrefix("income-nointerval")
	userID, linkedItemID, _, secID := seedInvestmentFixture(ctx, t, tx, prefix)

	// Both accounts are REMOVED and UNCHAINED (user_account_id IS NULL), so activeChainSiblingLeg
	// is off for each by its own `IS NOT NULL` guard and cannot mask what is being measured.
	//
	// terminalOnly: the backfill artifact. Inserted directly at status='removed', so
	// record_account_status_change() writes exactly ONE history row — an open 'removed' interval,
	// no 'active' one. Migration 000152's removed branch produces the same shape in production
	// whenever removed_at IS NULL or <= created_at.
	//
	// governed: the ordinary shape. A CLOSED 'active' interval that ENDS long before its dividend's
	// date, so the historical leg deliberately excludes it and the narrowness of the new leg is
	// observable rather than asserted.
	terminalOnly := uuid.New()
	governed := uuid.New()
	for _, a := range []struct {
		id   uuid.UUID
		name string
	}{{terminalOnly, "Terminal Only"}, {governed, "Governed"}} {
		_, err = tx.Exec(ctx, `
			INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, removed_at, removed_reason)
			VALUES ($1,$2,$3,$4,$5,'investment','removed', now(), 'user_disconnect')`,
			a.id, linkedItemID, userID, prefix+"-acct-"+a.id.String(), a.name)
		require.NoError(t, err, "seed account %s", a.name)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO account_status_history (id, account_id, user_id, status, valid_from, valid_to, provenance)
		VALUES ($1,$2,$3,'active', now() - interval '900 days', now() - interval '800 days', 'recorded')`,
		uuid.New(), governed, userID)
	require.NoError(t, err, "seed governed active interval")

	seedDividend := func(accountID uuid.UUID, amount float64, label string) {
		t.Helper()
		id := uuid.New()
		_, execErr := tx.Exec(ctx, `
			INSERT INTO investment_transactions
				(id, user_id, account_id, security_id, plaid_investment_transaction_id,
				 type, subtype, name, amount, date)
			VALUES ($1,$2,$3,$4,$5,'cash','dividend',$6,$7,'2026-03-15')`,
			id, userID, accountID, secID, prefix+"-div-"+id.String(), label, amount)
		require.NoError(t, execErr, "seed dividend %s", label)
	}
	// Plaid stores income as a cash CREDIT, i.e. a negative amount; IncomeSummary negates it.
	seedDividend(terminalOnly, -140.00, "Retained Dividend")
	seedDividend(governed, -55.00, "Out-Of-Window Dividend")

	repo := NewInvestmentAnalyticsRepository(tx)
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 31, 23, 59, 59, 0, time.UTC)

	result, err := repo.IncomeSummary(ctx, userID.String(), start, end)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.InDelta(t, 140.0, result.TotalIncome, 0.01,
		"IncomeSummary must count the dividend on an account with NO recorded 'active' interval "+
			"(140.00) and must NOT count the one whose recorded interval closed before the dividend's "+
			"date (55.00). A total of 195.00 means the new leg is too broad and has overridden a "+
			"recorded interval; 55.00 or 0.00 means the leg regressed and a user's retained income "+
			"silently vanished from a reported number (#1626, #1569 task 2)")

	require.Len(t, result.ByType, 1, "exactly one income bucket should survive")
	assert.Equal(t, "dividend", result.ByType[0].IncomeType)
	assert.InDelta(t, 140.0, result.ByType[0].TotalAmount, 0.01)
	assert.Equal(t, 1, result.ByType[0].Count,
		"the leg is an EXISTS semi-join over accounts.id (the PK), so it cannot fan out and "+
			"double-count the one contributing row")
}
