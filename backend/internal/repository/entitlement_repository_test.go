package repository

import (
	"context"
	"regexp"
	"testing"

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
