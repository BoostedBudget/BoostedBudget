//go:build integration

package repository

// Issue #1569 round 6, G-F — a closed account's rendered "Closed on {date}" must not move when the
// user renames or reorders it.
//
// THE DEFECT. `expired` was the one terminal status with no paired timestamp on the wire, so
// apps/web/src/features/accounts/utils.ts's getAccountClosedDate fell through to `updated_at`.
// But `updated_at` is bumped with NO status predicate by two ordinary cosmetic writes —
// accountRepository.Update (nickname / emoji / colour / hide) and UpdateDisplayOrders
// (drag-reorder). So renaming or reordering a closed account moved its rendered closure date
// forward to today, while the true instant sat recorded in BOTH accounts.expired_at (migration
// 000155, this branch) and account_status_history, unread by anything.
//
// This test is the backend half: the two reads whose rows reach that helper must carry expired_at,
// and it must survive a cosmetic write that moves updated_at. The frontend half — the precedence
// change itself — is pinned in utils.test.ts.
//
// WHY INTEGRATION. The claim is about what Postgres stores and what the projection carries. A
// pgxmock test asserts the SQL string and would pass against a scan list that never populates the
// field; and the whole point is the RELATIONSHIP between two timestamps the database writes.
//
// ⚠ WHY THE POOL AND NOT ONE ROLLED-BACK TRANSACTION, which is this package's usual shape.
// `accounts` carries a BEFORE UPDATE trigger, update_accounts_updated_at, that overwrites
// updated_at with NOW() on EVERY update — and NOW() is the TRANSACTION start timestamp. Inside a
// single test transaction the expiry and the later rename therefore both stamp the same instant,
// updated_at never moves past expired_at, and the divergence this test exists to demonstrate is
// invisible. Measured, not assumed: the first draft of this test was tx-scoped and its own
// fixture precondition failed with expired_at == updated_at to the microsecond. In production
// each request is its own transaction, so the clock really does advance. Rows are cleaned up in
// dependency order by t.Cleanup rather than by a rollback.
//
// FIXTURE THROUGH PRODUCTION WRITE PATHS. The account is created with CreateBatch, expired with
// ExpireAccountsByFingerprint (the only production writer of expired_at), and touched with
// Update / UpdateDisplayOrders. A hand-set `status = 'expired'` would not stamp expired_at at all
// — migration 000155's accounts_expired_at_consistency CHECK would reject the row, which is the
// schema doing its job.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)


func TestAccountRepository_ExpiredAtSurvivesACosmeticWrite_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	repo := NewAccountRepository(pool, newTestFieldEncryptor(t))

	userID := uuid.New()
	linkedItemID := uuid.New()
	userAccountID := uuid.New()
	prefix := "expiredat-" + userID.String()

	// ⚠ context.Background(), not t.Context(): a cancelled context makes these DELETEs silently
	// no-op and leaks fixture rows into a shared database, where a global-scan test in ANOTHER
	// package picks them up and fails on a mismatched entity.
	t.Cleanup(func() {
		cctx := context.Background()
		for _, stmt := range []string{
			`DELETE FROM balance_snapshots WHERE user_id = $1`,
			`DELETE FROM accounts WHERE user_id = $1`,
			`DELETE FROM user_accounts WHERE user_id = $1`,
			`DELETE FROM linked_items WHERE user_id = $1`,
			`DELETE FROM users WHERE id = $1`,
		} {
			if _, err := pool.Exec(cctx, stmt, userID); err != nil {
				t.Errorf("cleanup %q: %v", stmt, err)
			}
		}
	})

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+prefix, prefix+"@example.com")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')`, linkedItemID, userID, prefix+"-item")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, $3, 'depository')`, userAccountID, userID, prefix+"-inst")
	require.NoError(t, err)

	uaID := userAccountID.String()
	bal := 250.00
	accountID := uuid.New().String()
	fingerprint := prefix + "-fp"

	require.NoError(t, repo.CreateBatch(ctx, userID.String(), []domain.Account{{
		ID: accountID, UserID: userID.String(), LinkedItemID: linkedItemID.String(),
		UserAccountID: &uaID, PlaidAccountID: prefix + "-1", Name: "expired-then-renamed",
		Type: "depository", CurrentBalance: &bal, AvailableBalance: &bal, CurrencyCode: "USD",
		FallbackFingerprintBlindIdx: &fingerprint,
	}}))

	// The real transition, through the only production writer of expired_at.
	expiredIDs, err := repo.ExpireAccountsByFingerprint(ctx, fingerprint)
	require.NoError(t, err)
	require.Equal(t, []string{accountID}, expiredIDs, "fixture: the fingerprint must actually match")

	var expiredAt time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT expired_at FROM accounts WHERE id = $1`, accountID).Scan(&expiredAt))

	// ── THE COSMETIC WRITE. A rename, after the account expired. `Update` carries no status
	// predicate — it will happily bump a closed account's updated_at, and that is not a bug in
	// Update: the nickname really did change. The bug was reading updated_at as a closure date.
	nickname := "Old savings"
	require.NoError(t, repo.Update(ctx, &domain.Account{
		ID: accountID, Nickname: &nickname,
	}, userID.String()))
	// And a drag-reorder, the other unpredicated writer.
	require.NoError(t, repo.UpdateDisplayOrders(ctx, userID.String(),
		[]domain.AccountOrder{{AccountID: accountID, DisplayOrder: 3}}))

	var updatedAt time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT updated_at FROM accounts WHERE id = $1`, accountID).Scan(&updatedAt))
	require.True(t, updatedAt.After(expiredAt),
		"fixture: the cosmetic writes must actually have moved updated_at past expired_at "+
			"(expired_at=%s updated_at=%s); if they no longer do, this test proves nothing",
		expiredAt, updatedAt)

	// ── The two reads whose rows reach getAccountClosedDate. Both must carry expired_at, or the
	// client falls through to updated_at and renders the rename's timestamp as the closure date.
	t.Run("GetAccountsWithInstitutionFiltered carries expired_at", func(t *testing.T) {
		rows, err := repo.GetAccountsWithInstitutionFiltered(ctx, userID.String(),
			AccountFilterOptions{AllStatuses: true})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		got := rows[0]
		require.Equal(t, domain.AccountStatusExpired, got.Status, "fixture: it must come back expired")
		require.NotNil(t, got.ExpiredAt, "the accounts-list read must carry expired_at")
		assert.WithinDuration(t, expiredAt, *got.ExpiredAt, time.Millisecond)
		assert.True(t, got.ExpiredAt.Before(got.UpdatedAt),
			"and it must be the CLOSURE instant, not the rename's updated_at")
	})

	t.Run("GetByIDAndUser carries expired_at", func(t *testing.T) {
		// This read's result is returned over the wire verbatim by PATCH /api/v1/accounts/{id} —
		// i.e. by the very request that performs the rename. Omitting the column here made the
		// PATCH response itself the place a user first saw the wrong date.
		got, err := repo.GetByIDAndUser(ctx, accountID, userID.String())
		require.NoError(t, err)
		require.NotNil(t, got.ExpiredAt, "the single-account read must carry expired_at")
		assert.WithinDuration(t, expiredAt, *got.ExpiredAt, time.Millisecond)
		assert.True(t, got.ExpiredAt.Before(got.UpdatedAt))
	})

	t.Run("an active account carries no expired_at", func(t *testing.T) {
		// The pairing runs both ways: migration 000155's CHECK forbids expired_at on a row that is
		// not 'expired', so a non-nil value here would mean the scan is reading the wrong column.
		activeID := uuid.New().String()
		require.NoError(t, repo.CreateBatch(ctx, userID.String(), []domain.Account{{
			ID: activeID, UserID: userID.String(), LinkedItemID: linkedItemID.String(),
			UserAccountID: &uaID, PlaidAccountID: prefix + "-2", Name: "still-active",
			Type: "depository", CurrentBalance: &bal, AvailableBalance: &bal, CurrencyCode: "USD",
		}}))
		got, err := repo.GetByIDAndUser(ctx, activeID, userID.String())
		require.NoError(t, err)
		assert.Nil(t, got.ExpiredAt)
	})
}
