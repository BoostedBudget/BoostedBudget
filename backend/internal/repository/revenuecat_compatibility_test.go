package repository

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	before := testutil.ToFloat64(observability.RevenueCatGrantsAppliedTotal.WithLabelValues(observability.RevenueCatGrantClassComp))
	require.NoError(t, s.FinishOperation(context.Background(), "op", "applied", "", "comp"))
	require.NoError(t, db.ExpectationsWereMet())
	assert.Equal(t, before+1, testutil.ToFloat64(observability.RevenueCatGrantsAppliedTotal.WithLabelValues(observability.RevenueCatGrantClassComp)),
		"the applied FLOW counter is incremented at this write site, not derived from a query")
}

// TestRevenueCatFinishFailureRecordsTheOperationFailureFlow is the other half: a last_error write is
// the failure write site, and it is the only place RevenueCatOperationFailuresTotal may increment.
func TestRevenueCatFinishFailureRecordsTheOperationFailureFlow(t *testing.T) {
	db, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer db.Close()
	s := &revenueCatSession{q: db, userID: "user"}
	db.ExpectExec("UPDATE revenuecat_operations SET stage").
		WithArgs("op", "user", "pending", "provider_unavailable").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	before := testutil.ToFloat64(observability.RevenueCatOperationFailuresTotal.WithLabelValues(observability.RevenueCatGrantClassComp))
	require.NoError(t, s.FinishOperation(context.Background(), "op", "pending", "provider_unavailable", "comp"))
	require.NoError(t, db.ExpectationsWereMet())
	assert.Equal(t, before+1, testutil.ToFloat64(observability.RevenueCatOperationFailuresTotal.WithLabelValues(observability.RevenueCatGrantClassComp)))
}

// TestRevenueCatFinishNonPendingRowCountsNothing guards against over-counting: an UPDATE that
// matched no pending row is a no-op, not an application or a failure.
func TestRevenueCatFinishNonPendingRowCountsNothing(t *testing.T) {
	db, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer db.Close()
	s := &revenueCatSession{q: db, userID: "user"}
	db.ExpectExec("UPDATE revenuecat_operations SET stage").
		WithArgs("op", "user", "applied", "").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	beforeApplied := testutil.ToFloat64(observability.RevenueCatGrantsAppliedTotal.WithLabelValues(observability.RevenueCatGrantClassComp))
	beforeFailed := testutil.ToFloat64(observability.RevenueCatOperationFailuresTotal.WithLabelValues(observability.RevenueCatGrantClassComp))
	require.NoError(t, s.FinishOperation(context.Background(), "op", "applied", "", "comp"))
	require.NoError(t, db.ExpectationsWereMet())
	assert.Equal(t, beforeApplied, testutil.ToFloat64(observability.RevenueCatGrantsAppliedTotal.WithLabelValues(observability.RevenueCatGrantClassComp)))
	assert.Equal(t, beforeFailed, testutil.ToFloat64(observability.RevenueCatOperationFailuresTotal.WithLabelValues(observability.RevenueCatGrantClassComp)))
}
