package services

// household_account_share_service_test.go — tests for the #1368 (Phase 4) narrow
// HouseholdAccountShareService methods on *householdService: SetAccountShareVisibility
// (transactional, TOCTOU-safe against RemoveMember) and ListMyAccountShares.

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
)

func TestHouseholdService_SetAccountShareVisibility_NotActiveMember_RollsBack(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("FOR UPDATE OF hm").WithArgs("caller-1").WillReturnError(pgx.ErrNoRows)
	pool.ExpectRollback()

	_, err := svc.SetAccountShareVisibility(context.Background(), "caller-1", "acct-1", domain.ShareVisibilityShared)
	require.ErrorIs(t, err, domain.ErrHouseholdNotFound)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_SetAccountShareVisibility_AccountNotOwned_RollsBack(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()
	now := time.Now()

	pool.ExpectBegin()
	pool.ExpectQuery("FOR UPDATE OF hm").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil))
	pool.ExpectQuery("FROM user_accounts WHERE user_id").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx"}).
			AddRow("acct-other", "inst-blind", nil))
	pool.ExpectRollback()

	_, err := svc.SetAccountShareVisibility(context.Background(), "caller-1", "acct-not-mine", domain.ShareVisibilityHidden)
	require.ErrorIs(t, err, domain.ErrAccountNotOwned)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestHouseholdService_SetAccountShareVisibility_MalformedID_ReturnsAccountNotOwned documents,
// as an explicit regression test (#1368 Phase 4 deeper-review finding), that a garbage/malformed
// account ID can never reach a SQL UUID-typed column: the target ID is only ever compared in Go
// against the caller's real fingerprints (string equality), so this fails closed to
// ErrAccountNotOwned/403 exactly like a well-formed-but-foreign ID — never a 500 or a
// UUID-cast error. Same code path as AccountNotOwned above; kept as its own test purely so the
// "malformed input is safe" guarantee is documented and can't silently regress.
func TestHouseholdService_SetAccountShareVisibility_MalformedID_ReturnsAccountNotOwned(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()
	now := time.Now()

	pool.ExpectBegin()
	pool.ExpectQuery("FOR UPDATE OF hm").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil))
	pool.ExpectQuery("FROM user_accounts WHERE user_id").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx"}).
			AddRow("acct-1", "inst-blind", nil))
	pool.ExpectRollback()

	_, err := svc.SetAccountShareVisibility(context.Background(), "caller-1", "not-a-uuid-at-all", domain.ShareVisibilityHidden)
	require.ErrorIs(t, err, domain.ErrAccountNotOwned)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_SetAccountShareVisibility_Share_Success(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()
	now := time.Now()
	instIdx := "inst-blind"
	maskIdx := "mask-blind"
	combined := instIdx + maskIdx

	pool.ExpectBegin()
	pool.ExpectQuery("FOR UPDATE OF hm").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil))
	pool.ExpectQuery("FROM user_accounts WHERE user_id").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx"}).
			AddRow("acct-1", instIdx, &maskIdx))
	pool.ExpectQuery("ON CONFLICT").
		WithArgs("hh-1", "acct-1", &combined, domain.ShareVisibilityShared).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_account_id", "visibility", "shared_at", "revoked_at",
			"physical_account_blind_idx", "created_at", "updated_at",
		}).AddRow("share-1", "hh-1", "acct-1", domain.ShareVisibilityShared, now, nil, &combined, now, now))
	pool.ExpectCommit()

	share, err := svc.SetAccountShareVisibility(context.Background(), "caller-1", "acct-1", domain.ShareVisibilityShared)
	require.NoError(t, err)
	require.Equal(t, "share-1", share.ID)
	require.Equal(t, domain.ShareVisibilityShared, share.Visibility)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_SetAccountShareVisibility_Hide_Success(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()
	now := time.Now()

	pool.ExpectBegin()
	pool.ExpectQuery("FOR UPDATE OF hm").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil))
	pool.ExpectQuery("FROM user_accounts WHERE user_id").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx"}).
			AddRow("acct-1", "inst-blind", nil))
	pool.ExpectQuery("ON CONFLICT").
		WithArgs("hh-1", "acct-1", (*string)(nil), domain.ShareVisibilityHidden).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_account_id", "visibility", "shared_at", "revoked_at",
			"physical_account_blind_idx", "created_at", "updated_at",
		}).AddRow("share-1", "hh-1", "acct-1", domain.ShareVisibilityHidden, now, nil, nil, now, now))
	pool.ExpectCommit()

	share, err := svc.SetAccountShareVisibility(context.Background(), "caller-1", "acct-1", domain.ShareVisibilityHidden)
	require.NoError(t, err)
	require.Equal(t, domain.ShareVisibilityHidden, share.Visibility)
	require.Nil(t, share.RevokedAt)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_ListMyAccountShares_NotInHousehold(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("FOR UPDATE OF hm").WithArgs("caller-1").WillReturnError(pgx.ErrNoRows)
	pool.ExpectRollback()

	_, err := svc.ListMyAccountShares(context.Background(), "caller-1")
	require.ErrorIs(t, err, domain.ErrHouseholdNotFound)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_ListMyAccountShares_Success(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()
	now := time.Now()

	pool.ExpectBegin()
	pool.ExpectQuery("FOR UPDATE OF hm").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil))
	pool.ExpectQuery("LEFT JOIN household_account_shares").WithArgs("hh-1", "caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"user_account_id", "visibility", "revoked_at"}).
			AddRow("acct-1", nil, nil))
	pool.ExpectCommit()

	views, err := svc.ListMyAccountShares(context.Background(), "caller-1")
	require.NoError(t, err)
	require.Len(t, views, 1)
	require.Equal(t, "acct-1", views[0].UserAccountID)
	require.Nil(t, views[0].Visibility)
	require.NoError(t, pool.ExpectationsWereMet())
}
