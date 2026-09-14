package repository

import (
	"context"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestRevenueCatSaveWritesConfirmedCompatibilityInSameSession(t *testing.T) {
	db, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer db.Close()
	s := &revenueCatSession{q: db, userID: "user", enc: newTestFieldEncryptor(t)}
	db.ExpectExec("UPDATE revenuecat_customers SET grants").WithArgs("user", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	db.ExpectExec("WITH candidate AS.*UPDATE users.*candidate.subscription_status IN").WithArgs("user", "household_3", "active", pgxmock.AnyArg(), 6).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	require.NoError(t, s.Save(context.Background(), domain.RevenueCatState{AccessUpdatedAt: time.Now(), Grants: []domain.RevenueCatGrant{{Key: "comp_household_3"}}}))
	require.NoError(t, db.ExpectationsWereMet())
}
func TestRevenueCatFinishAppliedSchedulesCompatibilityRepair(t *testing.T) {
	db, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer db.Close()
	s := &revenueCatSession{q: db, userID: "user"}
	db.ExpectExec("UPDATE revenuecat_operations SET stage").WithArgs("op", "user", "applied", "").WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	db.ExpectExec("UPDATE revenuecat_customers SET next_refresh_at").WithArgs("user").WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	require.NoError(t, s.FinishOperation(context.Background(), "op", "applied", ""))
	require.NoError(t, db.ExpectationsWereMet())
}
