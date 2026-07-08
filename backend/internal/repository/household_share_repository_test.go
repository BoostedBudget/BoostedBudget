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
