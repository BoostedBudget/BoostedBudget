//go:build integration

package database

// Issue #1609, Item 3 (task 3c, Addendum B) — the startup repair carried the HIDE forward and
// nothing else.
//
// Task 3a's carryLegacyHideForward closes the ordering hazard for `is_hidden`: container
// migrations run before the server boots, so an account whose user_account_id was still NULL when
// migration 000185 ran missed that migration's lift entirely, and the chain row this repair gives
// it moments later would be born empty. Under task 3b's chain-authoritative overlay the chain's
// value WINS, so an empty chain row erases the account's customization.
//
// `is_hidden` was fixed because it is the most damaging one (an account the user hid quietly
// reappears). The other five columns have exactly the same hazard and were not carried:
//
//	an account customized while UNCHAINED — through the legacy PATCH /api/v1/accounts/{id}
//	endpoint, which is precisely the endpoint the frontend uses for an unchained account — and
//	chained later by this repair loses its nickname, colour, icon, emoji AND order.
//
// This is a RUNTIME gap, not a data gap: 000185 backfills all six for rows that existed when it
// ran. Which is why the fix is here and not in another migration — the NULL user_account_id can
// be created at any time (a fresh link between two boots), whereas this repair runs on every boot.
//
// pgxmock cannot see any of it (it returns rows the test author invented), so these are
// integration tests against real PostgreSQL.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedCustomizedAccount inserts an account with a NULL user_account_id — one that MISSED migration
// 000185 — carrying legacy customization written the way the legacy endpoint writes it.
func seedCustomizedAccount(
	t *testing.T, pool *pgxpool.Pool, enc *appcrypto.FieldEncryptor,
	userID, liID, plainMask string,
	nickname, color, icon, emoji *string, displayOrder *int,
) string {
	t.Helper()
	ctx := context.Background()
	encMask, err := enc.EncryptString(plainMask)
	require.NoError(t, err)

	accID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (
			id, linked_item_id, user_id, plaid_account_id, name, type, subtype, mask, status,
			nickname, custom_color, custom_icon, custom_emoji, display_order
		)
		VALUES ($1, $2, $3, $4, 'Legacy Checking', 'depository', 'checking', $5, 'active',
		        $6, $7, $8, $9, $10)
	`, accID, liID, userID, "plaid-cust-"+accID, encMask,
		nickname, color, icon, emoji, displayOrder)
	require.NoError(t, err)
	return accID
}

type chainCustomization struct {
	uaID         string
	nickname     *string
	color        *string
	icon         *string
	emoji        *string
	displayOrder *int
}

func chainCustomizationForAccount(t *testing.T, pool *pgxpool.Pool, accID string) chainCustomization {
	t.Helper()
	ctx := context.Background()
	var ua *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT user_account_id FROM accounts WHERE id = $1`, accID).Scan(&ua))
	require.NotNil(t, ua, "the repair must have attached the account to a chain")

	got := chainCustomization{uaID: *ua}
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT nickname, custom_color, custom_icon, custom_emoji, display_order
		FROM user_accounts WHERE id = $1`, *ua,
	).Scan(&got.nickname, &got.color, &got.icon, &got.emoji, &got.displayOrder))
	return got
}

// The synthesis outcome: a customized account that had no chain at all gets one, and the chain
// must be born carrying what the account already had.
func TestBackfillUserAccountIDs_SynthesisCarriesLegacyCustomizationForward(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)

	userID, liID, _ := seedHideFixture(t, pool, enc, "custsynth")
	order := 4
	accID := seedCustomizedAccount(t, pool, enc, userID, liID, "5151",
		strp("Rainy Day"), strp("#123456"), strp("piggy-bank"), strp("🐷"), &order)

	require.NoError(t, BackfillUserAccountIDs(context.Background(), pool, enc))

	got := chainCustomizationForAccount(t, pool, accID)
	require.NotNil(t, got.nickname)
	assert.Equal(t, "Rainy Day", *got.nickname)
	require.NotNil(t, got.color)
	assert.Equal(t, "#123456", *got.color)
	require.NotNil(t, got.icon)
	assert.Equal(t, "piggy-bank", *got.icon)
	require.NotNil(t, got.emoji)
	assert.Equal(t, "🐷", *got.emoji)
	require.NotNil(t, got.displayOrder)
	assert.Equal(t, 4, *got.displayOrder)
}

// The match outcome: joining an EXISTING chain reopens the same hole, and it is the more common
// one — a re-link is exactly how an account comes to join a chain that is already there.
func TestBackfillUserAccountIDs_MatchCarriesLegacyCustomizationForward(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()

	userID, liID, plainInstID := seedHideFixture(t, pool, enc, "custmatch")
	plainMask := "5252"

	encInstID, err := enc.EncryptString(plainInstID)
	require.NoError(t, err)
	encMask, err := enc.EncryptString(plainMask)
	require.NoError(t, err)

	uaID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO user_accounts
			(id, user_id, institution_id, mask, institution_id_blind_idx, mask_blind_idx)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, uaID, userID, encInstID, encMask, enc.BlindIndex(plainInstID), enc.BlindIndex(plainMask))
	require.NoError(t, err)

	order := 7
	accID := seedCustomizedAccount(t, pool, enc, userID, liID, plainMask,
		strp("Joint Account"), strp("#ABCDEF"), strp("wallet"), strp("💰"), &order)

	require.NoError(t, BackfillUserAccountIDs(ctx, pool, enc))

	got := chainCustomizationForAccount(t, pool, accID)
	require.Equal(t, uaID, got.uaID, "fixture: the account must have MATCHED the existing chain")
	require.NotNil(t, got.nickname)
	assert.Equal(t, "Joint Account", *got.nickname)
	require.NotNil(t, got.color)
	assert.Equal(t, "#ABCDEF", *got.color)
	require.NotNil(t, got.icon)
	assert.Equal(t, "wallet", *got.icon)
	require.NotNil(t, got.emoji)
	assert.Equal(t, "💰", *got.emoji)
	require.NotNil(t, got.displayOrder)
	assert.Equal(t, 7, *got.displayOrder)
}

// ⚠ THE CHAIN'S OWN VALUE WINS, and this is the same discipline carryLegacyHideForward already
// uses. The chain is the source of truth; a legacy value only fills a HOLE. Overwriting would let
// a stale accounts-side label — written before the user renamed the account on the chain — defeat
// the rename, which is the very failure this batch exists to remove, just pointed the other way.
//
// Per COLUMN, not per row: a chain that has a nickname and no colour takes the colour and keeps
// its nickname.
func TestBackfillUserAccountIDs_CarryForwardNeverOverwritesAValueTheChainAlreadyHas(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()

	userID, liID, plainInstID := seedHideFixture(t, pool, enc, "custkeep")
	plainMask := "5353"

	encInstID, err := enc.EncryptString(plainInstID)
	require.NoError(t, err)
	encMask, err := enc.EncryptString(plainMask)
	require.NoError(t, err)

	uaID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO user_accounts
			(id, user_id, institution_id, mask, institution_id_blind_idx, mask_blind_idx,
			 nickname, display_order)
		VALUES ($1, $2, $3, $4, $5, $6, 'Chain Name', 1)
	`, uaID, userID, encInstID, encMask, enc.BlindIndex(plainInstID), enc.BlindIndex(plainMask))
	require.NoError(t, err)

	order := 9
	accID := seedCustomizedAccount(t, pool, enc, userID, liID, plainMask,
		strp("Legacy Name"), strp("#000111"), nil, nil, &order)

	require.NoError(t, BackfillUserAccountIDs(ctx, pool, enc))

	got := chainCustomizationForAccount(t, pool, accID)
	require.Equal(t, uaID, got.uaID)
	require.NotNil(t, got.nickname)
	assert.Equal(t, "Chain Name", *got.nickname, "the chain's own nickname must stand")
	require.NotNil(t, got.displayOrder)
	assert.Equal(t, 1, *got.displayOrder, "the chain's own order must stand")
	require.NotNil(t, got.color, "but a HOLE on the chain is filled from the legacy row")
	assert.Equal(t, "#000111", *got.color)
	assert.Nil(t, got.icon, "a column neither side has stays empty")
}

// An account with nothing to carry must not touch the chain at all — the repair runs on every
// boot, so a write per uncustomized account is a full-table updated_at churn for nothing.
func TestBackfillUserAccountIDs_CarryForwardIsANoOpForAnUncustomizedAccount(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()

	userID, liID, plainInstID := seedHideFixture(t, pool, enc, "custnoop")
	plainMask := "5454"

	encInstID, err := enc.EncryptString(plainInstID)
	require.NoError(t, err)
	encMask, err := enc.EncryptString(plainMask)
	require.NoError(t, err)

	uaID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO user_accounts
			(id, user_id, institution_id, mask, institution_id_blind_idx, mask_blind_idx, nickname)
		VALUES ($1, $2, $3, $4, $5, $6, 'Chain Name')
	`, uaID, userID, encInstID, encMask, enc.BlindIndex(plainInstID), enc.BlindIndex(plainMask))
	require.NoError(t, err)

	var before string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT updated_at::text FROM user_accounts WHERE id = $1`, uaID).Scan(&before))

	accID := seedCustomizedAccount(t, pool, enc, userID, liID, plainMask, nil, nil, nil, nil, nil)

	require.NoError(t, BackfillUserAccountIDs(ctx, pool, enc))

	got := chainCustomizationForAccount(t, pool, accID)
	require.Equal(t, uaID, got.uaID)

	var after string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT updated_at::text FROM user_accounts WHERE id = $1`, uaID).Scan(&after))
	assert.Equal(t, before, after, "an account with nothing to carry must not touch the chain row")
}
