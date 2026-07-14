package repository

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupHouseholdShareMockDB(t *testing.T) (pgxmock.PgxPoolIface, HouseholdShareRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	return mock, NewHouseholdShareRepository(mock)
}

func TestHouseholdShareRepository_ListActiveUserAccountFingerprints(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdShareMockDB(t)
	defer mock.Close()

	maskIdx := "mask-blind-1"
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, institution_id_blind_idx, mask_blind_idx FROM user_accounts WHERE user_id = $1
	`)).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx"}).
			AddRow("acct-1", "inst-blind-1", &maskIdx).
			AddRow("acct-2", "inst-blind-2", nil))

	fps, err := repo.ListActiveUserAccountFingerprints(context.Background(), "user-1")
	require.NoError(t, err)
	require.Len(t, fps, 2)
	assert.Equal(t, "acct-1", fps[0].UserAccountID)
	require.NotNil(t, fps[0].PhysicalBlindIdx)
	assert.Equal(t, "inst-blind-1"+maskIdx, *fps[0].PhysicalBlindIdx)
	assert.Nil(t, fps[1].PhysicalBlindIdx, "no mask -> nil physical fingerprint")
}

func TestHouseholdShareRepository_InsertShare(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdShareMockDB(t)
	defer mock.Close()

	physIdx := "combined-fp"
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO household_account_shares (household_id, user_account_id, physical_account_blind_idx, visibility)
		VALUES ($1, $2, $3, $4)
	`)).
		WithArgs("hh-1", "acct-1", &physIdx, domain.ShareVisibilityShared).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.InsertShare(context.Background(), "hh-1", "acct-1", &physIdx, domain.ShareVisibilityShared)
	require.NoError(t, err)
}

func TestHouseholdShareRepository_InsertConsent(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdShareMockDB(t)
	defer mock.Close()

	c := &domain.HouseholdShareConsent{
		HouseholdID:    "hh-1",
		UserID:         "user-1",
		ConsentVersion: domain.HouseholdShareConsentVersion,
		Snapshot:       json.RawMessage(`{"shared":["acct-1"],"hidden":[]}`),
	}
	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO household_share_consents (household_id, user_id, consent_version, snapshot)
		VALUES ($1, $2, $3, $4)
		RETURNING id, consented_at, created_at
	`)).
		WithArgs(c.HouseholdID, c.UserID, c.ConsentVersion, []byte(c.Snapshot)).
		WillReturnRows(pgxmock.NewRows([]string{"id", "consented_at", "created_at"}).AddRow("consent-1", now, now))

	err := repo.InsertConsent(context.Background(), c)
	require.NoError(t, err)
	assert.Equal(t, "consent-1", c.ID)
}

func TestHouseholdShareRepository_SetShareVisibility_Share(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdShareMockDB(t)
	defer mock.Close()

	physIdx := "combined-fp"
	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO household_account_shares (household_id, user_account_id, physical_account_blind_idx, visibility)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (household_id, user_account_id) WHERE revoked_at IS NULL
		DO UPDATE SET visibility = EXCLUDED.visibility,
		              physical_account_blind_idx = EXCLUDED.physical_account_blind_idx,
		              updated_at = now()
		RETURNING id, household_id, user_account_id, visibility, shared_at, revoked_at,
		          physical_account_blind_idx, created_at, updated_at
	`)).
		WithArgs("hh-1", "acct-1", &physIdx, domain.ShareVisibilityShared).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_account_id", "visibility", "shared_at", "revoked_at",
			"physical_account_blind_idx", "created_at", "updated_at",
		}).AddRow("share-1", "hh-1", "acct-1", domain.ShareVisibilityShared, now, nil, &physIdx, now, now))

	share, err := repo.SetShareVisibility(context.Background(), "hh-1", "acct-1", &physIdx, domain.ShareVisibilityShared)
	require.NoError(t, err)
	require.NotNil(t, share)
	assert.Equal(t, "share-1", share.ID)
	assert.Equal(t, domain.ShareVisibilityShared, share.Visibility)
	assert.Nil(t, share.RevokedAt, "PATCH share/hide must never write revoked_at")
}

func TestHouseholdShareRepository_SetShareVisibility_Hide_NeverTouchesRevokedAt(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdShareMockDB(t)
	defer mock.Close()

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO household_account_shares (household_id, user_account_id, physical_account_blind_idx, visibility)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (household_id, user_account_id) WHERE revoked_at IS NULL
		DO UPDATE SET visibility = EXCLUDED.visibility,
		              physical_account_blind_idx = EXCLUDED.physical_account_blind_idx,
		              updated_at = now()
		RETURNING id, household_id, user_account_id, visibility, shared_at, revoked_at,
		          physical_account_blind_idx, created_at, updated_at
	`)).
		WithArgs("hh-1", "acct-1", (*string)(nil), domain.ShareVisibilityHidden).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_account_id", "visibility", "shared_at", "revoked_at",
			"physical_account_blind_idx", "created_at", "updated_at",
		}).AddRow("share-1", "hh-1", "acct-1", domain.ShareVisibilityHidden, now, nil, nil, now, now))

	share, err := repo.SetShareVisibility(context.Background(), "hh-1", "acct-1", nil, domain.ShareVisibilityHidden)
	require.NoError(t, err)
	// Hiding an account keeps it "active" (revoked_at NULL) so member-removal's
	// RevokeSharesForUser still catches and revokes it later. #1368 Phase 4.
	assert.Nil(t, share.RevokedAt)
	assert.Equal(t, domain.ShareVisibilityHidden, share.Visibility)
}

func TestHouseholdShareRepository_ListMyAccountShares(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdShareMockDB(t)
	defer mock.Close()

	hiddenStr := string(domain.ShareVisibilityHidden)
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT ua.id AS user_account_id, has.visibility, has.revoked_at
		FROM user_accounts ua
		LEFT JOIN household_account_shares has
			ON has.user_account_id = ua.id AND has.household_id = $1 AND has.revoked_at IS NULL
		WHERE ua.user_id = $2
		ORDER BY ua.id
	`)).
		WithArgs("hh-1", "user-1").
		WillReturnRows(pgxmock.NewRows([]string{"user_account_id", "visibility", "revoked_at"}).
			AddRow("acct-1", &hiddenStr, nil).
			AddRow("acct-2", nil, nil)) // acct-2: linked after joining, no grant row yet

	views, err := repo.ListMyAccountShares(context.Background(), "hh-1", "user-1")
	require.NoError(t, err)
	require.Len(t, views, 2)
	require.NotNil(t, views[0].Visibility)
	assert.Equal(t, domain.ShareVisibilityHidden, *views[0].Visibility)
	assert.Nil(t, views[1].Visibility, "no grant row yet -> nil visibility, not a default")
}

func TestHouseholdShareRepository_RevokeSharesForUser(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdShareMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE household_account_shares SET revoked_at = now(), updated_at = now()
		WHERE household_id = $1 AND revoked_at IS NULL
		  AND user_account_id IN (SELECT id FROM user_accounts WHERE user_id = $2)
	`)).
		WithArgs("hh-1", "user-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 2))

	err := repo.RevokeSharesForUser(context.Background(), "hh-1", "user-1")
	require.NoError(t, err)
}

// ─── DeleteSharesForUser (#1371 — Phase 7 foundation) ────────────────────────
//
// Unlike RevokeSharesForUser (soft-revoke, sets revoked_at, run at removal time),
// DeleteSharesForUser is a hard delete run at purge time (30 days later) by
// PurgeRemovedMembership, permanently removing the already-revoked share rows.

func TestHouseholdShareRepository_DeleteSharesForUser(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdShareMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`
		DELETE FROM household_account_shares
		WHERE household_id = $1
		  AND user_account_id IN (SELECT id FROM user_accounts WHERE user_id = $2)
	`)).
		WithArgs("hh-1", "user-1").
		WillReturnResult(pgxmock.NewResult("DELETE", 2))

	err := repo.DeleteSharesForUser(context.Background(), "hh-1", "user-1")
	require.NoError(t, err)
}
