package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
)

func TestRevenueCatCascadeGuardRunsBeforeLegacyEligibility(t *testing.T) {
	db, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer db.Close()
	repo := NewProfileRepository(db, newTestFieldEncryptor(t))
	repo.EnableRevenueCatRetentionGuard()
	db.ExpectBegin()
	db.ExpectQuery("SELECT h.id FROM households h").WithArgs("user").WillReturnError(context.DeadlineExceeded)
	db.ExpectRollback()
	deleted, _, err := repo.DeleteUserCascade(context.Background(), "user", nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, deleted)
	require.NoError(t, db.ExpectationsWereMet())
}

func TestRevenueCatCascadeGuardChecksLockedHouseholdCoverage(t *testing.T) {
	for _, tc := range []struct {
		name            string
		ownerPending    int
		ownerActive     bool
		ownerStale      bool
		changedCoverage bool
		want            bool
	}{
		{name: "pending owner grant", ownerPending: 1},
		{name: "active owner coverage", ownerActive: true},
		{name: "stale owner confirmation", ownerStale: true},
		{name: "fresh aggregate inactive", want: true},
		{name: "household changed during lock acquisition", changedCoverage: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer db.Close()
			repo := NewProfileRepository(db, newTestFieldEncryptor(t))
			now := time.Now()
			owner, household := "owner", "household"
			entitlementRows := func(householdID string) *pgxmock.Rows {
				return pgxmock.NewRows([]string{"id", "user_id", "source", "tier", "status", "payer_user_id", "household_id", "effective_from", "effective_until", "created_at", "updated_at"}).
					AddRow("entitlement", "user", domain.EntitlementSourceHousehold, "household_2", "active", &owner, &householdID, now.Add(-time.Hour), (*time.Time)(nil), now, now)
			}
			db.ExpectBegin()
			db.ExpectQuery("SELECT h.id FROM households h").WithArgs("user").WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(household))
			db.ExpectQuery("SELECT id, user_id, source").WithArgs("user", pgxmock.AnyArg()).WillReturnRows(entitlementRows(household))
			// Sorted customer locks fence both classes of operation before the user
			// lock and final coverage read. Query order is part of this contract.
			for _, customer := range []string{owner, "user"} {
				db.ExpectQuery("SELECT user_id FROM revenuecat_customers WHERE user_id=\\$1 FOR UPDATE").WithArgs(customer).WillReturnRows(pgxmock.NewRows([]string{"user_id"}).AddRow(customer))
			}
			db.ExpectQuery("SELECT id FROM users WHERE id=\\$1 FOR UPDATE").WithArgs("user").WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow("user"))
			currentHousehold := household
			if tc.changedCoverage {
				currentHousehold = "new-household"
			}
			db.ExpectQuery("SELECT id, user_id, source").WithArgs("user", pgxmock.AnyArg()).WillReturnRows(entitlementRows(currentHousehold))
			if !tc.changedCoverage {
				for _, customer := range []string{owner, "user"} {
					grants, updated, pending := `[]`, now, 0
					if customer == owner {
						pending = tc.ownerPending
						if tc.ownerActive {
							grants = `[{"key":"paid_household_2"}]`
						}
						if tc.ownerStale {
							updated = now.Add(-time.Hour)
						}
					}
					db.ExpectQuery("SELECT grants,access_updated_at,billing").WithArgs(customer).WillReturnRows(pgxmock.NewRows([]string{"grants", "access_updated_at", "billing", "billing_updated_at", "access_error", "billing_error", "erasure_started_at", "erasure_confirmed_at", "pending"}).AddRow([]byte(grants), &updated, "", (*time.Time)(nil), "", "", (*time.Time)(nil), (*time.Time)(nil), pending))
				}
			}
			db.ExpectRollback()
			tx, err := db.Begin(context.Background())
			require.NoError(t, err)
			allowed, err := repo.lockRevenueCatRetentionEligibility(context.Background(), tx, "user")
			require.NoError(t, err)
			require.Equal(t, tc.want, allowed)
			require.NoError(t, tx.Rollback(context.Background()))
			require.NoError(t, db.ExpectationsWereMet())
		})
	}
}

func TestRevenueCatCascadeGuardPendingIntentRefusesUnderLock(t *testing.T) {
	db, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer db.Close()
	repo := NewProfileRepository(db, newTestFieldEncryptor(t))
	repo.EnableRevenueCatRetentionGuard()
	db.ExpectBegin()
	db.ExpectQuery("SELECT h.id FROM households h").WithArgs("user").WillReturnRows(pgxmock.NewRows([]string{"id"}))
	db.ExpectQuery("SELECT id, user_id, source").WithArgs("user", pgxmock.AnyArg()).WillReturnError(pgx.ErrNoRows)
	db.ExpectQuery("SELECT user_id FROM revenuecat_customers WHERE user_id=\\$1 FOR UPDATE").WithArgs("user").WillReturnRows(pgxmock.NewRows([]string{"user_id"}).AddRow("user"))
	db.ExpectQuery("SELECT id FROM users WHERE id=\\$1 FOR UPDATE").WithArgs("user").WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow("user"))
	db.ExpectQuery("SELECT id, user_id, source").WithArgs("user", pgxmock.AnyArg()).WillReturnError(pgx.ErrNoRows)
	now := time.Now()
	db.ExpectQuery("SELECT grants,access_updated_at,billing").WithArgs("user").WillReturnRows(pgxmock.NewRows([]string{"grants", "access_updated_at", "billing", "billing_updated_at", "access_error", "billing_error", "erasure_started_at", "erasure_confirmed_at", "pending"}).AddRow([]byte(`[]`), &now, "", (*time.Time)(nil), "", "", (*time.Time)(nil), (*time.Time)(nil), 1))
	db.ExpectRollback()
	deleted, _, err := repo.DeleteUserCascade(context.Background(), "user", nil)
	require.NoError(t, err)
	require.False(t, deleted)
	require.NoError(t, db.ExpectationsWereMet())
}

func TestRevenueCatCascadeGuardDoesNotGateExplicitGDPR(t *testing.T) {
	db, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer db.Close()
	repo := NewProfileRepository(db, newTestFieldEncryptor(t))
	repo.EnableRevenueCatRetentionGuard()
	db.ExpectBegin()
	db.ExpectQuery("SELECT id FROM households WHERE owner_user_id").WithArgs("user").WillReturnError(context.DeadlineExceeded)
	db.ExpectRollback()
	now := time.Now()
	deleted, _, err := repo.DeleteUserCascade(context.Background(), "user", &now)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, deleted)
	require.NoError(t, db.ExpectationsWereMet())
}
