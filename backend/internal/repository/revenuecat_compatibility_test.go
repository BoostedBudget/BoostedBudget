package repository

import (
	"context"
	"regexp"
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

// TestRevenueCatFinishAbandonedRecordsTerminalFlow pins the terminal write: an abandoned operation
// increments the abandonment counter and must NOT increment the retryable-failure counter, whose
// stock companion only counts rows still stage='pending'.
func TestRevenueCatFinishAbandonedRecordsTerminalFlow(t *testing.T) {
	db, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer db.Close()
	s := &revenueCatSession{q: db, userID: "user"}
	db.ExpectExec("UPDATE revenuecat_operations SET stage").
		WithArgs("op", "user", "abandoned", "provider_unavailable").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	beforeAbandoned := testutil.ToFloat64(observability.RevenueCatOperationAbandonmentsTotal.WithLabelValues(observability.RevenueCatGrantClassComp))
	beforeFailed := testutil.ToFloat64(observability.RevenueCatOperationFailuresTotal.WithLabelValues(observability.RevenueCatGrantClassComp))
	require.NoError(t, s.FinishOperation(context.Background(), "op", "abandoned", "provider_unavailable", "comp"))
	require.NoError(t, db.ExpectationsWereMet())
	assert.Equal(t, beforeAbandoned+1, testutil.ToFloat64(observability.RevenueCatOperationAbandonmentsTotal.WithLabelValues(observability.RevenueCatGrantClassComp)),
		"an abandoned operation is a terminal outcome and must increment the abandonment counter")
	assert.Equal(t, beforeFailed, testutil.ToFloat64(observability.RevenueCatOperationFailuresTotal.WithLabelValues(observability.RevenueCatGrantClassComp)),
		"an abandoned operation must not count as a retryable failure: it has left the pending population")
}

func TestRevenueCatPendingOperationsQuery_ExcludesTerminalStages(t *testing.T) {
	assert.Contains(t, revenueCatPendingOperationsQuery, "stage='pending'")
	assert.NotContains(t, revenueCatPendingOperationsQuery, "'abandoned'",
		"an abandoned operation must never be selected for retry")
	assert.NotContains(t, revenueCatPendingOperationsQuery, "'applied'")
	assert.NotContains(t, revenueCatPendingOperationsQuery, "'superseded'")
}

// TestRevenueCatAbandonExhaustedOperationsQuery_PinsDeterministicSweep pins the SQL that makes the
// attempt cap a hard bound. PendingOperations orders by attempts ASC, so a cap-adjacent operation
// sorts LAST and is never reached when more than one page of lower-attempt work exists. The sweep
// must instead select the HIGHEST-attempt pending rows (attempts DESC), atomically write the
// terminal stage, stamp the cap reason only when no provider error was recorded, and bump attempts
// so the row leaves the pending population exactly once.
func TestRevenueCatAbandonExhaustedOperationsQuery_PinsDeterministicSweep(t *testing.T) {
	sql := revenueCatAbandonExhaustedOperationsQuery
	assert.Contains(t, sql, "SET stage='abandoned'")
	assert.Contains(t, sql, "attempts=attempts+1")
	assert.Contains(t, sql, "last_error=CASE WHEN last_error='' THEN 'attempt_cap_exceeded' ELSE last_error END")
	assert.Contains(t, sql, "WHERE stage='pending' AND attempts >= $1")
	assert.Contains(t, sql, "ORDER BY attempts DESC, created_at")
	assert.Contains(t, sql, "RETURNING class")
}

// TestRevenueCatAbandonExhaustedOperationsRecordsTerminalFlow pins that the sweep is a terminal
// write site equal in effect to FinishOperation: one abandonment metric per returned class, and the
// returned count is exactly the number of rows retired.
func TestRevenueCatAbandonExhaustedOperationsRecordsTerminalFlow(t *testing.T) {
	db, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer db.Close()
	r := &revenueCatRepository{db: db}
	db.ExpectQuery(regexp.QuoteMeta("UPDATE revenuecat_operations SET stage='abandoned'")).
		WithArgs(500, 100).
		WillReturnRows(pgxmock.NewRows([]string{"class"}).AddRow("comp").AddRow("trial"))

	beforeComp := testutil.ToFloat64(observability.RevenueCatOperationAbandonmentsTotal.WithLabelValues(observability.RevenueCatGrantClassComp))
	beforeTrial := testutil.ToFloat64(observability.RevenueCatOperationAbandonmentsTotal.WithLabelValues(observability.RevenueCatGrantClassTrial))
	n, err := r.AbandonExhaustedOperations(context.Background(), 500, 100)
	require.NoError(t, err)
	require.NoError(t, db.ExpectationsWereMet())
	require.Equal(t, 2, n)
	assert.Equal(t, beforeComp+1, testutil.ToFloat64(observability.RevenueCatOperationAbandonmentsTotal.WithLabelValues(observability.RevenueCatGrantClassComp)))
	assert.Equal(t, beforeTrial+1, testutil.ToFloat64(observability.RevenueCatOperationAbandonmentsTotal.WithLabelValues(observability.RevenueCatGrantClassTrial)))
}
