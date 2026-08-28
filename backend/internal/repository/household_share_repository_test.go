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
	identityType := "investment"
	identitySubtype := "brokerage"
	// #1569 round 11 task 4, coordinator round 3 fix: identity_type/identity_subtype are now
	// PART of the fingerprint (see AccountFingerprint's doc comment for why — two different real
	// accounts belonging to the same user that share institution+mask must not compute the same
	// fingerprint, and two DIFFERENT users' independently-computed fingerprints for the SAME real
	// account must agree, which requires identity in the concat either way).
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, institution_id_blind_idx, mask_blind_idx, identity_type, identity_subtype
		FROM user_accounts WHERE user_id = $1
	`)).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx", "identity_type", "identity_subtype"}).
			AddRow("acct-1", "inst-blind-1", &maskIdx, &identityType, &identitySubtype).
			AddRow("acct-2", "inst-blind-2", nil, nil, nil))

	fps, err := repo.ListActiveUserAccountFingerprints(context.Background(), "user-1")
	require.NoError(t, err)
	require.Len(t, fps, 2)
	assert.Equal(t, "acct-1", fps[0].UserAccountID)
	require.NotNil(t, fps[0].PhysicalBlindIdx)
	assert.Equal(t, "inst-blind-1"+maskIdx+":"+identityType+":"+identitySubtype, *fps[0].PhysicalBlindIdx)
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
	mock.ExpectBegin()
	// #1569 T7: the outgoing interval is CLOSED first, and only when it is a different state.
	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE household_account_shares SET revoked_at = now(), updated_at = now()
		WHERE household_id = $1 AND user_account_id = $2
		  AND revoked_at IS NULL AND visibility <> $3
	`)).
		WithArgs("hh-1", "acct-1", domain.ShareVisibilityShared).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
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
	mock.ExpectCommit()

	share, err := repo.SetShareVisibility(context.Background(), "hh-1", "acct-1", &physIdx, domain.ShareVisibilityShared)
	require.NoError(t, err)
	require.NotNil(t, share)
	assert.Equal(t, "share-1", share.ID)
	assert.Equal(t, domain.ShareVisibilityShared, share.Visibility)
	assert.Nil(t, share.RevokedAt, "the row SetShareVisibility returns is always the newly-OPEN interval")
}

func TestHouseholdShareRepository_SetShareVisibility_Hide_LeavesAnOpenRow(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdShareMockDB(t)
	defer mock.Close()

	now := time.Now()
	mock.ExpectBegin()
	// #1569 T7: the outgoing interval is CLOSED first, and only when it is a different state.
	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE household_account_shares SET revoked_at = now(), updated_at = now()
		WHERE household_id = $1 AND user_account_id = $2
		  AND revoked_at IS NULL AND visibility <> $3
	`)).
		WithArgs("hh-1", "acct-1", domain.ShareVisibilityHidden).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
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
	mock.ExpectCommit()

	share, err := repo.SetShareVisibility(context.Background(), "hh-1", "acct-1", nil, domain.ShareVisibilityHidden)
	require.NoError(t, err)
	// Hiding an account keeps an OPEN row (revoked_at NULL) so member-removal's
	// RevokeSharesForUser still catches and revokes it later. #1368 Phase 4. What #1569 T7
	// changed is the row the previous state left behind, not this one.
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

// ─── No delete counterpart (#1569 T7) ────────────────────────────────────────
//
// DeleteSharesForUser — the hard delete PurgeRemovedMembership ran 30 days after removal —
// was removed here. Closing a grant interval is now the only thing that ever happens to a
// household_account_shares row; see PurgeRemovedMembership's comment for the ruling, and
// TestHouseholdServiceIntegration_PurgeRemovedMembership_RetainsRevokedShareGrants for the
// behaviour it pins. Erasure of the row belongs to GDPR alone and reaches it by cascade
// from user_accounts.
