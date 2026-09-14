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

func TestRevenueCatCheckoutIntentLocksCustomerAndPreventsSecondWindow(t *testing.T) {
	for _, alreadyOpen := range []bool{false, true} {
		db, err := pgxmock.NewPool()
		require.NoError(t, err)
		now := time.Now()
		db.ExpectBegin()
		db.ExpectExec("INSERT INTO revenuecat_customers").WithArgs("user").WillReturnResult(pgxmock.NewResult("INSERT", 0))
		db.ExpectQuery("SELECT user_id FROM revenuecat_customers.*FOR UPDATE").WithArgs("user").WillReturnRows(pgxmock.NewRows([]string{"user_id"}).AddRow("user"))
		db.ExpectQuery("SELECT grants,access_updated_at,billing").WithArgs("user").WillReturnRows(pgxmock.NewRows([]string{"grants", "access_updated_at", "billing", "billing_updated_at", "access_error", "billing_error", "erasure_started_at", "erasure_confirmed_at", "pending"}).AddRow([]byte(`[]`), &now, "", &now, "", "", (*time.Time)(nil), (*time.Time)(nil), 0))
		open := db.ExpectQuery("SELECT id FROM revenuecat_checkout_intents.*closed_at IS NULL.*expires_at >").WithArgs("user", pgxmock.AnyArg())
		if alreadyOpen {
			open.WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow("existing"))
			db.ExpectRollback()
		} else {
			open.WillReturnError(pgx.ErrNoRows)
			db.ExpectExec("INSERT INTO revenuecat_checkout_intents").WithArgs(pgxmock.AnyArg(), "user", pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
			db.ExpectCommit()
		}
		repo := NewRevenueCatCheckoutRepository(db, nil)
		intent, err := repo.Begin(context.Background(), "user")
		if alreadyOpen {
			require.ErrorIs(t, err, domain.ErrRevenueCatCheckoutInProgress)
		} else {
			require.NoError(t, err)
			require.NotEmpty(t, intent.ID)
			require.Equal(t, "web", intent.Offering)
			require.True(t, intent.ExpiresAt.After(now))
		}
		require.NoError(t, db.ExpectationsWereMet())
		db.Close()
	}
}

func TestRevenueCatCheckoutIntentReleaseIsScopedToAccountAndIntent(t *testing.T) {
	db, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer db.Close()
	db.ExpectExec("UPDATE revenuecat_checkout_intents SET closed_at.*WHERE id=\\$1 AND user_id=\\$2 AND closed_at IS NULL").WithArgs("intent", "user").WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	require.NoError(t, NewRevenueCatCheckoutRepository(db, nil).Release(context.Background(), "user", "intent"))
	require.NoError(t, db.ExpectationsWereMet())
}
