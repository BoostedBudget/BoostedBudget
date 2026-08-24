// household_share_repository_integration_test.go — DB-backed behavior for the
// household_account_shares toggle/upsert added by #1368 (Phase 4).
//
// Per project rule: pgxmock never catches constraint/type errors or exercises real
// concurrency/locking — only a real Postgres proves the ON CONFLICT partial-index
// upsert is race-safe and that Plaid relink survival actually holds (see memory
// feedback_pgxmock_integration_coverage.md).
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/repository/ -run TestHouseholdShareRepository -v
package repository_test

import (
	"context"
	"sync"
	"time"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertTestLinkedItem inserts a minimal linked_items row and returns its UUID.
func insertTestLinkedItem(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO linked_items (user_id, item_id, access_token_encrypted)
		VALUES ($1, 'item_'||gen_random_uuid()::text, 'enc_test_token')
		RETURNING id::text
	`, userID).Scan(&id)
	if err != nil {
		t.Fatalf("insertTestLinkedItem: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM linked_items WHERE id = $1`, id) //nolint:errcheck
	})
	return id
}

// insertTestAccount inserts a minimal accounts row pointing at userAccountID (the
// stable reference that survives Plaid re-links) and returns its UUID.
func insertTestAccount(t *testing.T, pool *pgxpool.Pool, linkedItemID, userID, userAccountID string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type, user_account_id)
		VALUES ($1, $2, 'plaid_'||gen_random_uuid()::text, 'Test Account', 'depository', $3)
		RETURNING id::text
	`, linkedItemID, userID, userAccountID).Scan(&id)
	if err != nil {
		t.Fatalf("insertTestAccount: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1`, id) //nolint:errcheck
	})
	return id
}

func TestHouseholdShareRepository_SetShareVisibility_ShareAndHide_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdShareRepository(pool)

	owner := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ua := insertTestUserAccount(t, pool, owner)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2`, hh, ua) //nolint:errcheck
	})

	shared, err := repo.SetShareVisibility(ctx, hh, ua, nil, domain.ShareVisibilityShared)
	require.NoError(t, err)
	assert.Equal(t, domain.ShareVisibilityShared, shared.Visibility)
	assert.Nil(t, shared.RevokedAt)

	hidden, err := repo.SetShareVisibility(ctx, hh, ua, nil, domain.ShareVisibilityHidden)
	require.NoError(t, err)
	assert.Equal(t, domain.ShareVisibilityHidden, hidden.Visibility)
	// Hiding must NOT revoke — the row stays active (revoked_at NULL) so member
	// removal's RevokeSharesForUser still finds and revokes it later.
	assert.Nil(t, hidden.RevokedAt)
}

func TestHouseholdShareRepository_SetShareVisibility_ToggleClosesAndOpensIntervals_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdShareRepository(pool)

	owner := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ua := insertTestUserAccount(t, pool, owner)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2`, hh, ua) //nolint:errcheck
	})

	hidden, err := repo.SetShareVisibility(ctx, hh, ua, nil, domain.ShareVisibilityHidden)
	require.NoError(t, err)

	reShared, err := repo.SetShareVisibility(ctx, hh, ua, nil, domain.ShareVisibilityShared)
	require.NoError(t, err)

	// #1569 T7 replaced the old contract here ("re-share must toggle the same active row").
	// Toggling in place was exactly the defect: it rewrote the account's whole history to
	// whatever the member last chose. A toggle is a new state starting now.
	assert.NotEqual(t, hidden.ID, reShared.ID, "a genuine state change must OPEN a new interval, not overwrite the old one")
	assert.Equal(t, domain.ShareVisibilityShared, reShared.Visibility)
	assert.Nil(t, reShared.RevokedAt, "the incoming state is the open row")

	var closedVisibility string
	var closedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT visibility, revoked_at FROM household_account_shares WHERE id = $1`, hidden.ID).
		Scan(&closedVisibility, &closedAt))
	assert.Equal(t, string(domain.ShareVisibilityHidden), closedVisibility, "the outgoing state must be retained as it was")
	require.NotNil(t, closedAt, "and its interval must be CLOSED, not deleted")

	var open, total int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE revoked_at IS NULL), COUNT(*)
		FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2
	`, hh, ua).Scan(&open, &total))
	assert.Equal(t, 1, open, "exactly one OPEN row may exist for this (household, user_account) pair")
	assert.Equal(t, 2, total, "and the closed predecessor stays as the record of the hidden period")
}

func TestHouseholdShareRepository_SetShareVisibility_ConcurrentToggle_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdShareRepository(pool)

	owner := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ua := insertTestUserAccount(t, pool, owner) // newly-linked account — no share row exists yet
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2`, hh, ua) //nolint:errcheck
	})

	const contenders = 10
	var wg sync.WaitGroup
	var ready sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, contenders)
	ready.Add(contenders)
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		visibility := domain.ShareVisibilityShared
		if i%2 == 0 {
			visibility = domain.ShareVisibilityHidden
		}
		go func(idx int, v domain.ShareVisibility) {
			defer wg.Done()
			ready.Done()
			<-start // block until every goroutine has reached this point, forcing genuine overlap
			_, err := repo.SetShareVisibility(ctx, hh, ua, nil, v)
			errs[idx] = err
		}(i, visibility)
	}
	ready.Wait() // all 10 goroutines are now parked on <-start
	close(start) // release them all at once
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "concurrent PATCH #%d must never surface a 23505 — the upsert must absorb the race", i)
	}

	var activeCount int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM household_account_shares
		WHERE household_id = $1 AND user_account_id = $2 AND revoked_at IS NULL
	`, hh, ua).Scan(&activeCount)
	require.NoError(t, err)
	assert.Equal(t, 1, activeCount, "exactly one active row must survive concurrent toggles")
}

func TestHouseholdShareRepository_RelinkSurvival_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdShareRepository(pool)

	owner := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ua := insertTestUserAccount(t, pool, owner)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2`, hh, ua) //nolint:errcheck
	})

	shared, err := repo.SetShareVisibility(ctx, hh, ua, nil, domain.ShareVisibilityShared)
	require.NoError(t, err)

	// Simulate a Plaid re-link: the OLD accounts row is soft-replaced and a NEW
	// accounts row is created, but both point at the same stable user_account_id.
	linkedItem := insertTestLinkedItem(t, pool, owner)
	oldAccount := insertTestAccount(t, pool, linkedItem, owner, ua)
	_, err = pool.Exec(ctx, `UPDATE accounts SET status = 'replaced', replaced_at = now() WHERE id = $1`, oldAccount)
	require.NoError(t, err)
	newAccount := insertTestAccount(t, pool, linkedItem, owner, ua)

	// The share grant is keyed to user_accounts, not accounts — it must be
	// completely unaffected by the relink and still resolve via user_account_id.
	var visibility string
	var resolvedUserAccountID string
	err = pool.QueryRow(ctx, `
		SELECT has.visibility, a.user_account_id
		FROM household_account_shares has
		JOIN accounts a ON a.user_account_id = has.user_account_id
		WHERE has.id = $1 AND a.id = $2
	`, shared.ID, newAccount).Scan(&visibility, &resolvedUserAccountID)
	require.NoError(t, err, "share must still resolve through the new post-relink accounts row via user_account_id")
	assert.Equal(t, string(domain.ShareVisibilityShared), visibility)
	assert.Equal(t, ua, resolvedUserAccountID)
}

func TestHouseholdShareRepository_RevokeSharesForUser_CatchesHiddenRows_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdShareRepository(pool)

	owner := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ua := insertTestUserAccount(t, pool, owner)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2`, hh, ua) //nolint:errcheck
	})

	hidden, err := repo.SetShareVisibility(ctx, hh, ua, nil, domain.ShareVisibilityHidden)
	require.NoError(t, err)
	require.Nil(t, hidden.RevokedAt, "sanity: hidden row starts active")

	err = repo.RevokeSharesForUser(ctx, hh, owner)
	require.NoError(t, err)

	var revokedAt *string
	err = pool.QueryRow(ctx, `SELECT revoked_at::text FROM household_account_shares WHERE id = $1`, hidden.ID).Scan(&revokedAt)
	require.NoError(t, err)
	require.NotNil(t, revokedAt, "member removal must revoke hidden rows too, not just shared ones")
}

func TestHouseholdShareRepository_Rejoin_DistinctRow_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdShareRepository(pool)

	owner := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ua := insertTestUserAccount(t, pool, owner)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2`, hh, ua) //nolint:errcheck
	})

	shared, err := repo.SetShareVisibility(ctx, hh, ua, nil, domain.ShareVisibilityShared)
	require.NoError(t, err)

	// Member leaves — RevokeSharesForUser is the real production revocation path.
	require.NoError(t, repo.RevokeSharesForUser(ctx, hh, owner))

	// Member rejoins — the accept fan-out uses InsertShare, not SetShareVisibility.
	// The old revoked row does not satisfy the partial unique index's predicate
	// (revoked_at IS NULL), so this plain INSERT must succeed as a fresh row.
	require.NoError(t, repo.InsertShare(ctx, hh, ua, nil, domain.ShareVisibilityShared))

	var rows []struct {
		id      string
		revoked bool
	}
	pgRows, err := pool.Query(ctx, `SELECT id::text, revoked_at IS NOT NULL FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2 ORDER BY created_at`, hh, ua)
	require.NoError(t, err)
	defer pgRows.Close()
	for pgRows.Next() {
		var r struct {
			id      string
			revoked bool
		}
		require.NoError(t, pgRows.Scan(&r.id, &r.revoked))
		rows = append(rows, r)
	}
	require.NoError(t, pgRows.Err())

	require.Len(t, rows, 2, "the old revoked row and the new active row must both exist")
	assert.Equal(t, shared.ID, rows[0].id)
	assert.True(t, rows[0].revoked, "old row stays revoked")
	assert.NotEqual(t, shared.ID, rows[1].id, "rejoin creates a distinct new row")
	assert.False(t, rows[1].revoked, "new row is active")
}

func TestHouseholdShareRepository_ListMyAccountShares_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdShareRepository(pool)

	owner := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	sharedAccount := insertTestUserAccount(t, pool, owner)
	newAccount := insertTestUserAccount(t, pool, owner) // linked after joining — no grant row yet
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM household_account_shares WHERE household_id = $1`, hh) //nolint:errcheck
	})

	_, err := repo.SetShareVisibility(ctx, hh, sharedAccount, nil, domain.ShareVisibilityHidden)
	require.NoError(t, err)

	views, err := repo.ListMyAccountShares(ctx, hh, owner)
	require.NoError(t, err)
	require.Len(t, views, 2)

	byID := make(map[string]domain.UserAccountShareView, len(views))
	for _, v := range views {
		byID[v.UserAccountID] = v
	}

	require.Contains(t, byID, sharedAccount)
	require.NotNil(t, byID[sharedAccount].Visibility)
	assert.Equal(t, domain.ShareVisibilityHidden, *byID[sharedAccount].Visibility)

	require.Contains(t, byID, newAccount)
	assert.Nil(t, byID[newAccount].Visibility, "no grant row yet -> nil, not a default")
}

// TestHouseholdShareRepository_RevokeSharesForUser_CrossUserIsolation_Integration proves
// RevokeSharesForUser's subquery join (`user_account_id IN (SELECT id FROM user_accounts
// WHERE user_id = $2)`) is actually correct against real tables: it must close the target
// user's own grants within the household, and must NEVER touch another user's grants in
// that SAME household — a wrong join/column here would still "pass" a pgxmock regex match
// on the query text alone.
//
// This test carries over from DeleteSharesForUser, which it used to cover; #1569 T7 removed
// the hard delete and left RevokeSharesForUser as the only writer of that subquery.
func TestHouseholdShareRepository_RevokeSharesForUser_CrossUserIsolation_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdShareRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	ownerAccount := insertTestUserAccount(t, pool, owner)
	memberAccount := insertTestUserAccount(t, pool, member)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM household_account_shares WHERE household_id = $1`, hh) //nolint:errcheck
	})

	_, err := repo.SetShareVisibility(ctx, hh, ownerAccount, nil, domain.ShareVisibilityShared)
	require.NoError(t, err)
	_, err = repo.SetShareVisibility(ctx, hh, memberAccount, nil, domain.ShareVisibilityShared)
	require.NoError(t, err)

	require.NoError(t, repo.RevokeSharesForUser(ctx, hh, member))

	var memberOpen, memberTotal int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE revoked_at IS NULL), COUNT(*)
		FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2
	`, hh, memberAccount).Scan(&memberOpen, &memberTotal))
	assert.Equal(t, 0, memberOpen, "the target member's grant must be CLOSED")
	assert.Equal(t, 1, memberTotal, "and RETAINED — closing is not deleting (#1569 T7)")

	var ownerOpen int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2 AND revoked_at IS NULL`,
		hh, ownerAccount).Scan(&ownerOpen))
	assert.Equal(t, 1, ownerOpen, "the owner's grant in the SAME household must be completely untouched")
}
