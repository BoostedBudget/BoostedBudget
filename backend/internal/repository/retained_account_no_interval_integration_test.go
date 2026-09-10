//go:build integration

package repository

// Issue #1626 Phase 2 — behavioural proof for accountWithNoActiveIntervalLeg.
//
// WHY THIS IS AN INTEGRATION TEST AND NOT A SQL-SHAPE ONE. The shape test next door proves the
// fragment ASKS the right question; only Postgres can prove the row actually comes back. And the
// first version of that shape test passed BEFORE the leg existed, because its assertions ("contains
// NOT EXISTS", "has three ORs") were already satisfied by the two pre-existing legs — so a
// behavioural check is what makes this requirement falsifiable at all.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestUnifiedFeed_AccountWithNoActiveInterval_StillShowsItsTransactions_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	enc := newTestFieldEncryptor(t)
	repo := NewTransactionRepository(tx, enc)

	userID := uuid.New()
	userAccountID := uuid.New()
	linkedItemID := uuid.New()
	// terminalOnly: the backfill artifact — a removed account whose status history has NO 'active'
	// row, only a terminal one. governed: the ordinary shape, active-then-terminal.
	terminalOnly := uuid.New()
	governed := uuid.New()

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	mustExec("user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1,$2,$3,'premium','active','iad')`, userID, "test|"+userID.String(), "noint-1626-"+userID.String()+"@example.com")
	mustExec("user_account", `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1,$2,'ins-noint-1626','depository')`, userAccountID, userID)
	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1,$2,$3,'enc-test-token')`, linkedItemID, userID, "noint-item-"+linkedItemID.String())

	// Both accounts are REMOVED, so neither has a live chain sibling and the first leg is out.
	for _, a := range []struct {
		id   uuid.UUID
		name string
	}{{terminalOnly, "Terminal Only"}, {governed, "Governed"}} {
		mustExec("account "+a.name, `
			INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status, removed_at, removed_reason)
			VALUES ($1,$2,$3,$4,$5,$6,'depository','removed', now(), 'user_disconnect')`,
			a.id, linkedItemID, userID, userAccountID, "plaid-"+a.id.String(), a.name)
	}

	// ⚠ No manual history insert for terminalOnly. `account_status_history_insert_trigger` fires on
	// the accounts INSERT and, for status='removed', writes exactly ONE row: an OPEN 'removed'
	// interval and no 'active' row. That IS the backfill artifact this leg exists for — the shape
	// is reproduced by the ordinary write path, not contrived. (`uq_ash_open` also permits only one
	// open interval per account, so adding another by hand would collide.)

	// governed gets the ordinary shape: a CLOSED 'active' interval alongside the trigger's open
	// 'removed' one, with the active window ENDING long before its transaction's date — so the
	// historical leg deliberately excludes it and the narrowness of the new leg is observable.
	mustExec("history governed active", `
		INSERT INTO account_status_history (id, account_id, user_id, user_account_id, status, valid_from, valid_to, provenance)
		VALUES ($1,$2,$3,$4,'active', now() - interval '400 days', now() - interval '300 days', 'recorded')`,
		uuid.New(), governed, userID, userAccountID)

	seedTxn := func(id, accountID uuid.UUID, name string) {
		mustExec("txn "+name, `
			INSERT INTO transactions
				(id, user_id, account_id, user_account_id, linked_item_id, source,
				 external_transaction_id, external_transaction_id_blind_idx,
				 amount, date, name, pending, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,'plaid',$6,$7, 12.00, CURRENT_DATE - 1, $8, false, now(), now())`,
			id, userID, accountID, userAccountID, linkedItemID,
			"ext-"+id.String(), "blind-"+id.String(), name)
	}
	visibleTxn := uuid.New()
	hiddenTxn := uuid.New()
	seedTxn(visibleTxn, terminalOnly, "On terminal-only account")
	seedTxn(hiddenTxn, governed, "On governed account")

	items, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(),
		domain.TransactionFilters{IncludeInvestmentActivity: false}, nil, 50)
	require.NoError(t, err)

	got := map[string]bool{}
	for _, it := range items {
		got[it.ID] = true
	}

	assert.True(t, got[visibleTxn.String()],
		"a transaction on an account with NO 'active' interval must be VISIBLE. Absence of interval "+
			"data is a backfill artifact, not evidence the row should be hidden — and while it was "+
			"hidden, no Sankey node's amount could equal its navigated list's total (#1626)")

	assert.False(t, got[hiddenTxn.String()],
		"the fix must stay narrow: an account with a real 'active' interval that ENDED before this "+
			"date is still governed by the historical leg's date bound. If this row appears, the new "+
			"leg is too broad and is resurrecting deliberately disconnected accounts")
}
