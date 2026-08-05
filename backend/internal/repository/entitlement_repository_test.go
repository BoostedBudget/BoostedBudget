package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupEntitlementMockDB(t *testing.T) (pgxmock.PgxPoolIface, EntitlementRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	return mock, NewEntitlementRepository(mock)
}

// ─── GetActiveEntitlement — pins the current-state (not asOf-consistent) semantics ──
//
// The query is `WHERE user_id = $1 AND effective_until IS NULL AND effective_from <= $2`.
// Only effective_from is bound by asOf; effective_until IS NULL is evaluated at query
// time. These tests pin that: a row already closed (non-NULL effective_until) does not
// come back even though the caller's asOf predates the close, because the mocked query
// simply returns no rows for it — reproducing what the real partial-unique-indexed table
// would do. If the query is ever changed to also match closed epochs within
// [effective_from, effective_until) (the change this task's brief says NOT to make),
// TestEntitlementRepository_GetActiveEntitlement_ClosedEntitlement_NotReturned's
// regexp.QuoteMeta match on the exact WHERE clause breaks, flagging the change.

func TestEntitlementRepository_GetActiveEntitlement_Found(t *testing.T) {
	t.Parallel()
	mock, repo := setupEntitlementMockDB(t)
	defer mock.Close()

	asOf := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	effectiveFrom := asOf.Add(-24 * time.Hour)
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "source", "tier", "status", "payer_user_id", "household_id",
		"effective_from", "effective_until", "created_at", "updated_at",
	}).AddRow(
		"ent-1", "user-1", domain.EntitlementSourceHousehold, "premium", "active",
		(*string)(nil), (*string)(nil), effectiveFrom, (*time.Time)(nil), effectiveFrom, effectiveFrom,
	)
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, source, tier, status, payer_user_id, household_id,
		       effective_from, effective_until, created_at, updated_at
		FROM subscription_entitlements
		WHERE user_id = $1 AND effective_until IS NULL AND effective_from <= $2
		LIMIT 1
	`)).
		WithArgs("user-1", asOf).
		WillReturnRows(rows)

	e, err := repo.GetActiveEntitlement(context.Background(), "user-1", asOf)
	require.NoError(t, err)
	assert.Equal(t, "ent-1", e.ID)
	assert.Nil(t, e.EffectiveUntil)
}

func TestEntitlementRepository_GetActiveEntitlement_NotFound(t *testing.T) {
	t.Parallel()
	mock, repo := setupEntitlementMockDB(t)
	defer mock.Close()

	asOf := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, source, tier, status, payer_user_id, household_id,
		       effective_from, effective_until, created_at, updated_at
		FROM subscription_entitlements
		WHERE user_id = $1 AND effective_until IS NULL AND effective_from <= $2
		LIMIT 1
	`)).
		WithArgs("user-1", asOf).
		WillReturnError(pgx.ErrNoRows)

	_, err := repo.GetActiveEntitlement(context.Background(), "user-1", asOf)
	assert.ErrorIs(t, err, ErrEntitlementNotFound)
}

// TestEntitlementRepository_GetActiveEntitlement_ClosedEntitlement_NotReturned pins the
// current-state selection: a row whose effective_until is in the FUTURE relative to asOf
// (i.e. it was still open AT asOf, by the effective_from/effective_until epoch, but has
// since been closed by the time this query runs) is NOT returned. The real table's
// effective_until IS NULL filter excludes it regardless of asOf; this test reproduces that
// by having the mocked query return no rows, and exists to fail loudly if someone "fixes"
// GetActiveEntitlement to also match closed epochs without also handling the uniqueness
// implications (see the doc comment on GetActiveEntitlement and this task's brief).
func TestEntitlementRepository_GetActiveEntitlement_ClosedEntitlement_NotReturned(t *testing.T) {
	t.Parallel()
	mock, repo := setupEntitlementMockDB(t)
	defer mock.Close()

	asOf := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	// The entitlement was live at asOf (effective_from before asOf, effective_until after
	// asOf) but is closed as of query time, so effective_until IS NULL excludes it.
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, source, tier, status, payer_user_id, household_id,
		       effective_from, effective_until, created_at, updated_at
		FROM subscription_entitlements
		WHERE user_id = $1 AND effective_until IS NULL AND effective_from <= $2
		LIMIT 1
	`)).
		WithArgs("user-1", asOf).
		WillReturnError(pgx.ErrNoRows)

	_, err := repo.GetActiveEntitlement(context.Background(), "user-1", asOf)
	assert.ErrorIs(t, err, ErrEntitlementNotFound,
		"a row closed after asOf but before this query runs must not be returned — "+
			"effective_until IS NULL is a current-state predicate, not bound by asOf")
}

func TestEntitlementRepository_CreateHouseholdEntitlement(t *testing.T) {
	t.Parallel()
	mock, repo := setupEntitlementMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO subscription_entitlements (user_id, source, tier, status, payer_user_id, household_id, effective_from, effective_until)
		VALUES ($1, 'household', $2, $3, $4, $5, now(), NULL)
	`)).
		WithArgs("member-1", "household_2", "active", "owner-1", "hh-1").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.CreateHouseholdEntitlement(context.Background(), "member-1", "household_2", "active", "owner-1", "hh-1")
	require.NoError(t, err)
}

func TestEntitlementRepository_CreateHouseholdEntitlement_AlreadyOpen(t *testing.T) {
	t.Parallel()
	mock, repo := setupEntitlementMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO subscription_entitlements (user_id, source, tier, status, payer_user_id, household_id, effective_from, effective_until)
		VALUES ($1, 'household', $2, $3, $4, $5, now(), NULL)
	`)).
		WithArgs("member-1", "household_2", "active", "owner-1", "hh-1").
		WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "uq_subscription_entitlements_active_user"})

	err := repo.CreateHouseholdEntitlement(context.Background(), "member-1", "household_2", "active", "owner-1", "hh-1")
	assert.ErrorIs(t, err, domain.ErrAlreadyInHousehold)
}

func TestEntitlementRepository_ExpireActiveEntitlement(t *testing.T) {
	t.Parallel()
	mock, repo := setupEntitlementMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE subscription_entitlements SET effective_until = now(), updated_at = now()
		WHERE user_id = $1 AND effective_until IS NULL
	`)).
		WithArgs("user-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.ExpireActiveEntitlement(context.Background(), "user-1")
	require.NoError(t, err)
}

func TestEntitlementRepository_ExpireActiveEntitlement_NoneOpen_Idempotent(t *testing.T) {
	t.Parallel()
	mock, repo := setupEntitlementMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE subscription_entitlements SET effective_until = now(), updated_at = now()
		WHERE user_id = $1 AND effective_until IS NULL
	`)).
		WithArgs("user-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.ExpireActiveEntitlement(context.Background(), "user-1")
	require.NoError(t, err)
}

// ─── ExpireActiveHouseholdEntitlement (#1371 — Phase 7 foundation) ───────────
//
// Scoped by user_id AND household_id AND source='household', unlike the broad
// ExpireActiveEntitlement above (user_id only) — see the real-DB proof in
// entitlement_repository_integration_test.go that the scoping actually matters.

func TestEntitlementRepository_ExpireActiveHouseholdEntitlement(t *testing.T) {
	t.Parallel()
	mock, repo := setupEntitlementMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE subscription_entitlements SET effective_until = now(), updated_at = now()
		WHERE user_id = $1 AND household_id = $2 AND source = 'household' AND effective_until IS NULL
	`)).
		WithArgs("user-1", "hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.ExpireActiveHouseholdEntitlement(context.Background(), "user-1", "hh-1")
	require.NoError(t, err)
}

func TestEntitlementRepository_ExpireActiveHouseholdEntitlement_NoneOpen_Idempotent(t *testing.T) {
	t.Parallel()
	mock, repo := setupEntitlementMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE subscription_entitlements SET effective_until = now(), updated_at = now()
		WHERE user_id = $1 AND household_id = $2 AND source = 'household' AND effective_until IS NULL
	`)).
		WithArgs("user-1", "hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.ExpireActiveHouseholdEntitlement(context.Background(), "user-1", "hh-1")
	require.NoError(t, err)
}
