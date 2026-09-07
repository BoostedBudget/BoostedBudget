//go:build integration

package repository

// Issue #1609, Item 3 (task 3b) — account customization is WRITTEN to user_accounts and READ
// from accounts, so hiding / renaming / recolouring / reordering an account reverts on refresh.
//
// PATCH /api/v1/user-accounts/{id} writes only user_accounts (userAccountRepository.Update), but
// every account read projected the six customization fields straight off `accounts` with no join
// to the chain row. This file pins the fix: a CHAIN-AUTHORITATIVE overlay,
//
//	CASE WHEN a.user_account_id IS NOT NULL THEN a_ua.<field> ELSE a.<field> END
//
// deliberately NOT COALESCE. The update API writes NULL to mean "the user cleared this"
// (userAccountService.UpdateUserAccount), and COALESCE cannot tell that apart from "never
// overridden" — it would resurrect the value the user just deleted. That distinction is the
// reason for the CASE, and it has its own test below
// (…_ClearedOverrideDoesNotResurrectLegacyValue_Integration).
//
// ⚠ WHY INTEGRATION AND NOT pgxmock. Every claim here is about what Postgres computes: which leg
// of a CASE fires, whether a join condition is right, and whether a one-user_account-to-many-
// accounts join fans out. pgxmock asserts the SQL *string* and then hands back rows the test
// author invented, so it is structurally incapable of catching a wrong CASE, a wrong ON clause,
// or a duplicated row. The anti-fan-out case in particular cannot even be expressed in pgxmock.
//
// FIXTURES GO THROUGH PRODUCTION WRITE PATHS where one exists: accounts are created with
// CreateBatch, chain-side values written with userAccountRepository.Update, and accounts-side
// (legacy) values written with accountRepository.Update — which is exactly the writer that made
// those legacy values exist in the first place.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// overlayFixture is the shared skeleton: one user, one linked_item, and cleanup in dependency
// order. Cleanup uses context.Background() rather than t.Context() on purpose — a cancelled
// context makes the DELETEs silently no-op and leaks fixture rows into the shared database,
// where a global-scan test in ANOTHER package picks them up and fails on a mismatched entity.
type overlayFixture struct {
	pool         *pgxpool.Pool
	userID       uuid.UUID
	linkedItemID uuid.UUID
	prefix       string
	repo         AccountRepository
	uaRepo       UserAccountRepository
	nextPlaidID  int
}

func newOverlayFixture(t *testing.T) (*overlayFixture, context.Context) {
	t.Helper()
	pool := connectTestDB(t)
	ctx := context.Background()

	userID := uuid.New()
	linkedItemID := uuid.New()
	prefix := "custovl-" + userID.String()

	t.Cleanup(func() {
		cctx := context.Background()
		for _, stmt := range []string{
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

	return &overlayFixture{
		pool:         pool,
		userID:       userID,
		linkedItemID: linkedItemID,
		prefix:       prefix,
		repo:         NewAccountRepository(pool, newTestFieldEncryptor(t)),
		uaRepo:       NewUserAccountRepository(pool, newTestFieldEncryptor(t)),
	}, ctx
}

// newUserAccount inserts a chain row directly. institution_id is written raw rather than through
// userAccountRepository.Create because nothing here reads it back — the six customization columns
// are NOT encrypted on either table (see decryptUserAccountPIIFields / decryptAccountPIIFields,
// which cover institution_id / mask / name only), which is what makes the overlay a plain SQL
// question rather than a crypto one.
func (f *overlayFixture) newUserAccount(t *testing.T, ctx context.Context) string {
	t.Helper()
	id := uuid.New()
	_, err := f.pool.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, $3, 'depository')`, id, f.userID, f.prefix+"-inst-"+id.String()[:8])
	require.NoError(t, err)
	return id.String()
}

// newAccount creates an account through CreateBatch, the production write path.
func (f *overlayFixture) newAccount(t *testing.T, ctx context.Context, name string, userAccountID *string) string {
	t.Helper()
	f.nextPlaidID++
	id := uuid.New().String()
	bal := 100.00
	require.NoError(t, f.repo.CreateBatch(ctx, f.userID.String(), []domain.Account{{
		ID: id, UserID: f.userID.String(), LinkedItemID: f.linkedItemID.String(),
		UserAccountID: userAccountID, PlaidAccountID: f.prefix + "-" + name,
		Name: name, Type: "depository",
		CurrentBalance: &bal, AvailableBalance: &bal, CurrencyCode: "USD",
	}}))
	return id
}

func strptr(s string) *string { return &s }
func intptr(i int) *int       { return &i }

// ── 1. The chain wins. ────────────────────────────────────────────────────────────────────────
//
// All six fields written through the API's own writer (userAccountRepository.Update) must come
// back from the accounts list. Before this change every one of them came back as the accounts-
// side value, which is the reported bug: the rename reverts on refresh.
func TestAccountRepository_CustomizationOverlay_UserAccountWins_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)

	uaID := f.newUserAccount(t, ctx)
	accountID := f.newAccount(t, ctx, "chained", &uaID)

	// The LEGACY values, written by the writer that created legacy values: accountRepository.Update.
	require.NoError(t, f.repo.Update(ctx, &domain.Account{
		ID: accountID, Nickname: strptr("legacy nickname"), CustomColor: strptr("#111111"),
		CustomIcon: strptr("wallet"), CustomEmoji: strptr("🏚"), DisplayOrder: intptr(9),
		IsHidden: false,
	}, f.userID.String()))

	// The CHAIN values, written by the writer PATCH /user-accounts/{id} actually uses.
	require.NoError(t, f.uaRepo.Update(ctx, &domain.UserAccount{
		ID: uaID, UserID: f.userID.String(),
		Nickname: strptr("chain nickname"), CustomColor: strptr("#222222"),
		CustomIcon: strptr("piggy-bank"), CustomEmoji: strptr("🏦"), DisplayOrder: intptr(1),
		IsHidden: false,
	}))

	rows, err := f.repo.GetAccountsWithInstitutionFiltered(ctx, f.userID.String(),
		AccountFilterOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	got := rows[0]

	require.NotNil(t, got.Nickname)
	assert.Equal(t, "chain nickname", *got.Nickname)
	require.NotNil(t, got.CustomColor)
	assert.Equal(t, "#222222", *got.CustomColor)
	require.NotNil(t, got.CustomIcon)
	assert.Equal(t, "piggy-bank", *got.CustomIcon)
	require.NotNil(t, got.CustomEmoji)
	assert.Equal(t, "🏦", *got.CustomEmoji)
	require.NotNil(t, got.DisplayOrder)
	assert.Equal(t, 1, *got.DisplayOrder)
	assert.False(t, got.IsHidden)
}

// ── 2. The no-regression half. ────────────────────────────────────────────────────────────────
//
// accounts.user_account_id is NULLABLE and a real production population carries unchained rows:
// the startup repair skips no-match and decryption-failure rows (database/backfill.go). Those
// accounts must behave EXACTLY as they did before — the ELSE leg — or this fix breaks manual and
// un-repaired accounts to fix chained ones.
func TestAccountRepository_CustomizationOverlay_UnchainedAccountFallsBackToAccounts_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)

	accountID := f.newAccount(t, ctx, "unchained", nil)
	require.NoError(t, f.repo.Update(ctx, &domain.Account{
		ID: accountID, Nickname: strptr("only nickname"), CustomColor: strptr("#333333"),
		CustomIcon: strptr("briefcase"), CustomEmoji: strptr("📦"), DisplayOrder: intptr(7),
		IsHidden: false,
	}, f.userID.String()))

	rows, err := f.repo.GetAccountsWithInstitutionFiltered(ctx, f.userID.String(),
		AccountFilterOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	got := rows[0]

	require.Nil(t, got.UserAccountID, "fixture: this account must be unchained")
	require.NotNil(t, got.Nickname)
	assert.Equal(t, "only nickname", *got.Nickname)
	require.NotNil(t, got.CustomColor)
	assert.Equal(t, "#333333", *got.CustomColor)
	require.NotNil(t, got.CustomIcon)
	assert.Equal(t, "briefcase", *got.CustomIcon)
	require.NotNil(t, got.CustomEmoji)
	assert.Equal(t, "📦", *got.CustomEmoji)
	require.NotNil(t, got.DisplayOrder)
	assert.Equal(t, 7, *got.DisplayOrder)
}

// ── 3. THE REASON FOR THE CASE. ───────────────────────────────────────────────────────────────
//
// The update API writes NULL to mean "the user cleared this". Under COALESCE the read would fall
// through to the legacy accounts value and hand the user back the nickname they just deleted —
// an edit that silently does nothing, which is indistinguishable from the bug this task fixes.
// The CASE is chain-AUTHORITATIVE: once the row is chained, the chain's NULL is the answer.
func TestAccountRepository_CustomizationOverlay_ClearedOverrideDoesNotResurrectLegacyValue_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)

	uaID := f.newUserAccount(t, ctx)
	accountID := f.newAccount(t, ctx, "cleared", &uaID)

	require.NoError(t, f.repo.Update(ctx, &domain.Account{
		ID: accountID, Nickname: strptr("legacy nickname"), CustomColor: strptr("#444444"),
		CustomIcon: strptr("wallet"), CustomEmoji: strptr("🧾"), DisplayOrder: intptr(4),
	}, f.userID.String()))

	// The user clears every override. All five nullable fields go to NULL on the chain row.
	require.NoError(t, f.uaRepo.Update(ctx, &domain.UserAccount{
		ID: uaID, UserID: f.userID.String(),
	}))

	// Precondition, stated rather than assumed: the legacy values really are still on `accounts`.
	var legacyNickname *string
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT nickname FROM accounts WHERE id = $1`, accountID).Scan(&legacyNickname))
	require.NotNil(t, legacyNickname, "fixture: the legacy value must still be present, or this proves nothing")

	rows, err := f.repo.GetAccountsWithInstitutionFiltered(ctx, f.userID.String(),
		AccountFilterOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	got := rows[0]

	assert.Nil(t, got.Nickname, "a cleared override must NOT resurrect accounts.nickname (this is what COALESCE gets wrong)")
	assert.Nil(t, got.CustomColor)
	assert.Nil(t, got.CustomIcon)
	assert.Nil(t, got.CustomEmoji)
	assert.Nil(t, got.DisplayOrder)
}

// ── 4. The hidden FILTER, not just the projected column. ─────────────────────────────────────
//
// Hiding is the most visible half of the bug: the WHERE clause read accounts.is_hidden, so a hide
// written to user_accounts never removed the row from the list. The filter must use the same
// overlay expression the projection does.
//
// A WHERE over a LEFT-JOINed column is safe here specifically because the ELSE leg (a.is_hidden)
// is NOT NULL — the expression is never NULL, so there is no three-valued-logic trap.
func TestAccountRepository_CustomizationOverlay_HiddenFilterUsesTheOverlay_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)

	uaID := f.newUserAccount(t, ctx)
	accountID := f.newAccount(t, ctx, "hidden", &uaID)

	// accounts says visible, the chain says hidden. This is exactly the post-PATCH state.
	require.NoError(t, f.uaRepo.Update(ctx, &domain.UserAccount{
		ID: uaID, UserID: f.userID.String(), IsHidden: true,
	}))
	var accountsHidden bool
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT is_hidden FROM accounts WHERE id = $1`, accountID).Scan(&accountsHidden))
	require.False(t, accountsHidden, "fixture: accounts must still say visible, or this proves nothing")

	t.Run("IncludeHidden=false drops it", func(t *testing.T) {
		rows, err := f.repo.GetAccountsWithInstitutionFiltered(ctx, f.userID.String(),
			AccountFilterOptions{})
		require.NoError(t, err)
		assert.Empty(t, rows, "a hide written to user_accounts must remove the account from the default list")
	})

	t.Run("IncludeHidden=true returns it, flagged hidden", func(t *testing.T) {
		rows, err := f.repo.GetAccountsWithInstitutionFiltered(ctx, f.userID.String(),
			AccountFilterOptions{IncludeHidden: true})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.True(t, rows[0].IsHidden, "and the projected column must agree with the filter")
	})
}

// ── 5. ORDER BY reads the overlay too. ───────────────────────────────────────────────────────
//
// A drag-reorder writes user_accounts.display_order. If ORDER BY keeps reading accounts, the list
// snaps back to the old order on refresh even when the numbers on screen are right.
func TestAccountRepository_CustomizationOverlay_DisplayOrderOverlayOrdersTheResult_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)

	uaA := f.newUserAccount(t, ctx)
	uaB := f.newUserAccount(t, ctx)
	acctA := f.newAccount(t, ctx, "alpha", &uaA)
	acctB := f.newAccount(t, ctx, "bravo", &uaB)

	// accounts-side order says A then B …
	require.NoError(t, f.repo.Update(ctx, &domain.Account{ID: acctA, DisplayOrder: intptr(1)}, f.userID.String()))
	require.NoError(t, f.repo.Update(ctx, &domain.Account{ID: acctB, DisplayOrder: intptr(2)}, f.userID.String()))
	// … and the chain says the opposite. The chain wins.
	require.NoError(t, f.uaRepo.Update(ctx, &domain.UserAccount{ID: uaA, UserID: f.userID.String(), DisplayOrder: intptr(2)}))
	require.NoError(t, f.uaRepo.Update(ctx, &domain.UserAccount{ID: uaB, UserID: f.userID.String(), DisplayOrder: intptr(1)}))

	rows, err := f.repo.GetAccountsWithInstitutionFiltered(ctx, f.userID.String(),
		AccountFilterOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, acctB, rows[0].ID, "the user_accounts display_order must decide the order")
	assert.Equal(t, acctA, rows[1].ID)
}

// ── 6. THE ANTI-FAN-OUT TEST. ────────────────────────────────────────────────────────────────
//
// One user_account has MANY accounts rows — that is the whole point of the chain (1 active + N
// replaced after re-links). The join must not multiply the account's own result row. Cardinality
// is provably safe (a.user_account_id is an FK to user_accounts(id), the PRIMARY KEY, so at most
// one row matches) but "provably safe" is what every fan-out was believed to be before it
// shipped, and pgxmock cannot express this case at all: it returns whatever rows the author
// invented, so a duplicated row is literally unrepresentable in a mock.
func TestAccountRepository_CustomizationOverlay_ChainWithReplacedSiblingsReturnsOneRowPerAccount_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)

	uaID := f.newUserAccount(t, ctx)
	live := f.newAccount(t, ctx, "live", &uaID)
	old1 := f.newAccount(t, ctx, "old1", &uaID)
	old2 := f.newAccount(t, ctx, "old2", &uaID)

	// CreateBatch's own supersedeStrandedChainSiblings does the transition: adding a second
	// account to a chain that already has an active one is precisely a re-link, so the earlier
	// siblings are marked 'replaced' by the production path rather than by a hand-written UPDATE.
	// Stated as a precondition because the shape of the fixture is what the assertion rests on.
	var active, replaced int
	require.NoError(t, f.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'active'), count(*) FILTER (WHERE status = 'replaced')
		FROM accounts WHERE user_account_id = $1`, uaID).Scan(&active, &replaced))
	require.Equal(t, 1, active, "fixture: the chain must hold exactly one active account")
	require.Equal(t, 2, replaced, "fixture: and two replaced siblings, or this proves nothing")

	rows, err := f.repo.GetAccountsWithInstitutionFiltered(ctx, f.userID.String(),
		AccountFilterOptions{AllStatuses: true, IncludeHidden: true})
	require.NoError(t, err)
	require.Len(t, rows, 3, "three accounts on one chain must yield exactly three rows, not nine")

	seen := map[string]int{}
	for _, r := range rows {
		seen[r.ID]++
	}
	assert.Equal(t, map[string]int{live: 1, old1: 1, old2: 1}, seen)
}

// ── 7. A2: the PATCH response body itself. ───────────────────────────────────────────────────
//
// GetByIDAndUser's row IS the PATCH /api/v1/accounts/{id} response body
// (PlaidHandler.UpdateAccount assigns it straight in). Fixing only the list read would leave the
// modal showing pre-write values in the response to the successful write — the bug reappearing
// at the exact moment the user is looking for confirmation.
func TestAccountRepository_CustomizationOverlay_GetByIDAndUserOverlays_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)

	uaID := f.newUserAccount(t, ctx)
	accountID := f.newAccount(t, ctx, "patched", &uaID)

	require.NoError(t, f.repo.Update(ctx, &domain.Account{
		ID: accountID, Nickname: strptr("legacy nickname"), CustomColor: strptr("#555555"),
		CustomIcon: strptr("wallet"), CustomEmoji: strptr("🗄"), DisplayOrder: intptr(8),
	}, f.userID.String()))
	require.NoError(t, f.uaRepo.Update(ctx, &domain.UserAccount{
		ID: uaID, UserID: f.userID.String(),
		Nickname: strptr("chain nickname"), CustomColor: strptr("#666666"),
		CustomIcon: strptr("piggy-bank"), CustomEmoji: strptr("💳"), DisplayOrder: intptr(2),
		IsHidden: true,
	}))

	got, err := f.repo.GetByIDAndUser(ctx, accountID, f.userID.String())
	require.NoError(t, err)
	require.NotNil(t, got.Nickname)
	assert.Equal(t, "chain nickname", *got.Nickname)
	require.NotNil(t, got.CustomColor)
	assert.Equal(t, "#666666", *got.CustomColor)
	require.NotNil(t, got.CustomIcon)
	assert.Equal(t, "piggy-bank", *got.CustomIcon)
	require.NotNil(t, got.CustomEmoji)
	assert.Equal(t, "💳", *got.CustomEmoji)
	require.NotNil(t, got.DisplayOrder)
	assert.Equal(t, 2, *got.DisplayOrder)
	assert.True(t, got.IsHidden)
}

// ── 8. A3/A4/A5, the remaining three reads. ──────────────────────────────────────────────────
//
// GetByUserAccountIDAndUser is the transfer IDOR probe; GetByUserAccountAndStatus and
// GetByUserAccountID are latent today (no caller renders their customization) — which is exactly
// why they are covered here rather than left: a latent stale field becomes a live one the moment
// somebody renders it, with nothing to say it was ever wrong.
func TestAccountRepository_CustomizationOverlay_RemainingChainReadsOverlay_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)

	uaID := f.newUserAccount(t, ctx)
	accountID := f.newAccount(t, ctx, "remaining", &uaID)

	require.NoError(t, f.repo.Update(ctx, &domain.Account{
		ID: accountID, Nickname: strptr("legacy nickname"), CustomColor: strptr("#777777"),
		CustomIcon: strptr("wallet"), CustomEmoji: strptr("📁"), DisplayOrder: intptr(6),
	}, f.userID.String()))
	require.NoError(t, f.uaRepo.Update(ctx, &domain.UserAccount{
		ID: uaID, UserID: f.userID.String(),
		Nickname: strptr("chain nickname"), CustomColor: strptr("#888888"),
		CustomIcon: strptr("piggy-bank"), CustomEmoji: strptr("🏧"), DisplayOrder: intptr(3),
		IsHidden: true,
	}))

	assertOverlaid := func(t *testing.T, got domain.Account) {
		t.Helper()
		require.NotNil(t, got.Nickname)
		assert.Equal(t, "chain nickname", *got.Nickname)
		require.NotNil(t, got.CustomColor)
		assert.Equal(t, "#888888", *got.CustomColor)
		require.NotNil(t, got.CustomIcon)
		assert.Equal(t, "piggy-bank", *got.CustomIcon)
		require.NotNil(t, got.CustomEmoji)
		assert.Equal(t, "🏧", *got.CustomEmoji)
		require.NotNil(t, got.DisplayOrder)
		assert.Equal(t, 3, *got.DisplayOrder)
		assert.True(t, got.IsHidden)
	}

	t.Run("A3 GetByUserAccountIDAndUser", func(t *testing.T) {
		got, err := f.repo.GetByUserAccountIDAndUser(ctx, uaID, f.userID.String())
		require.NoError(t, err)
		assertOverlaid(t, *got)
	})

	t.Run("A4 GetByUserAccountAndStatus", func(t *testing.T) {
		rows, err := f.repo.GetByUserAccountAndStatus(ctx, uaID, string(domain.AccountStatusActive), f.userID.String())
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assertOverlaid(t, rows[0])
	})

	t.Run("A5 GetByUserAccountID", func(t *testing.T) {
		rows, err := f.repo.GetByUserAccountID(ctx, uaID)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assertOverlaid(t, rows[0])
	})
}
