package repository

import (
	"context"
	"errors"
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

func setupHouseholdMockDB(t *testing.T) (pgxmock.PgxPoolIface, HouseholdRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	return mock, NewHouseholdRepository(mock)
}

func TestHouseholdRepository_CreateHousehold(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	h := &domain.Household{OwnerUserID: "owner-1", Name: "My Household"}
	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO households (owner_user_id, name) VALUES ($1, $2) RETURNING id, created_at, updated_at`)).
		WithArgs(h.OwnerUserID, h.Name).
		WillReturnRows(pgxmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow("hh-1", now, now))

	err := repo.CreateHousehold(context.Background(), h)
	require.NoError(t, err)
	assert.Equal(t, "hh-1", h.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHouseholdRepository_CreateHousehold_DuplicateOwner(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	h := &domain.Household{OwnerUserID: "owner-1", Name: "My Household"}
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO households (owner_user_id, name) VALUES ($1, $2) RETURNING id, created_at, updated_at`)).
		WithArgs(h.OwnerUserID, h.Name).
		WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "uq_households_owner_user_id"})

	err := repo.CreateHousehold(context.Background(), h)
	assert.ErrorIs(t, err, domain.ErrAlreadyOwnsHousehold)
}

func TestHouseholdRepository_LockHouseholdForUpdate(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT owner_user_id FROM households WHERE id = $1 FOR UPDATE`)).
		WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"owner_user_id"}).AddRow("owner-1"))

	ownerID, err := repo.LockHouseholdForUpdate(context.Background(), "hh-1")
	require.NoError(t, err)
	assert.Equal(t, "owner-1", ownerID)
}

func TestHouseholdRepository_LockHouseholdForUpdate_NotFound(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT owner_user_id FROM households WHERE id = $1 FOR UPDATE`)).
		WithArgs("hh-missing").
		WillReturnError(pgx.ErrNoRows)

	_, err := repo.LockHouseholdForUpdate(context.Background(), "hh-missing")
	assert.ErrorIs(t, err, domain.ErrHouseholdNotFound)
}

func TestHouseholdRepository_LockActiveMembershipForUser_Found(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT h.id, h.owner_user_id, h.name, h.created_at, h.updated_at
		FROM household_members hm
		JOIN households h ON h.id = hm.household_id
		WHERE hm.user_id = $1 AND hm.status = 'active'
		FOR UPDATE OF hm
	`)).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now))

	h, err := repo.LockActiveMembershipForUser(context.Background(), "user-1")
	require.NoError(t, err)
	assert.Equal(t, "hh-1", h.ID)
	assert.Equal(t, "owner-1", h.OwnerUserID)
}

func TestHouseholdRepository_LockActiveMembershipForUser_NotFound(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT h.id, h.owner_user_id, h.name, h.created_at, h.updated_at
		FROM household_members hm
		JOIN households h ON h.id = hm.household_id
		WHERE hm.user_id = $1 AND hm.status = 'active'
		FOR UPDATE OF hm
	`)).
		WithArgs("user-solo").
		WillReturnError(pgx.ErrNoRows)

	_, err := repo.LockActiveMembershipForUser(context.Background(), "user-solo")
	assert.ErrorIs(t, err, domain.ErrHouseholdNotFound)
}

func TestHouseholdRepository_UpdateHouseholdName(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE households SET name = $1, updated_at = now() WHERE id = $2 AND owner_user_id = $3`)).
		WithArgs("New Name", "hh-1", "owner-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.UpdateHouseholdName(context.Background(), "hh-1", "owner-1", "New Name")
	require.NoError(t, err)
}

func TestHouseholdRepository_UpdateHouseholdName_NotOwner(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE households SET name = $1, updated_at = now() WHERE id = $2 AND owner_user_id = $3`)).
		WithArgs("New Name", "hh-1", "not-owner").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.UpdateHouseholdName(context.Background(), "hh-1", "not-owner", "New Name")
	assert.ErrorIs(t, err, domain.ErrNotHouseholdOwner)
}

func TestHouseholdRepository_DeleteHousehold(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM households WHERE id = $1 AND owner_user_id = $2`)).
		WithArgs("hh-1", "owner-1").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	err := repo.DeleteHousehold(context.Background(), "hh-1", "owner-1")
	require.NoError(t, err)
}

func TestHouseholdRepository_DeleteHousehold_NotOwner(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM households WHERE id = $1 AND owner_user_id = $2`)).
		WithArgs("hh-1", "not-owner").
		WillReturnResult(pgxmock.NewResult("DELETE", 0))

	err := repo.DeleteHousehold(context.Background(), "hh-1", "not-owner")
	assert.ErrorIs(t, err, domain.ErrNotHouseholdOwner)
}

func TestHouseholdRepository_InsertMember(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	m := &domain.HouseholdMember{HouseholdID: "hh-1", UserID: "user-1", Role: domain.HouseholdRoleMember, Status: domain.HouseholdMemberStatusActive}
	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO household_members (household_id, user_id, role, status, prior_subscription_tier, prior_subscription_status) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, joined_at, created_at, updated_at`)).
		WithArgs(m.HouseholdID, m.UserID, m.Role, m.Status, m.PriorSubscriptionTier, m.PriorSubscriptionStatus).
		WillReturnRows(pgxmock.NewRows([]string{"id", "joined_at", "created_at", "updated_at"}).AddRow("mem-1", now, now, now))

	err := repo.InsertMember(context.Background(), m)
	require.NoError(t, err)
	assert.Equal(t, "mem-1", m.ID)
}

func TestHouseholdRepository_InsertMember_AlreadyActiveElsewhere(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	m := &domain.HouseholdMember{HouseholdID: "hh-1", UserID: "user-1", Role: domain.HouseholdRoleMember, Status: domain.HouseholdMemberStatusActive}
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO household_members (household_id, user_id, role, status, prior_subscription_tier, prior_subscription_status) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, joined_at, created_at, updated_at`)).
		WithArgs(m.HouseholdID, m.UserID, m.Role, m.Status, m.PriorSubscriptionTier, m.PriorSubscriptionStatus).
		WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "uq_household_members_active_user"})

	err := repo.InsertMember(context.Background(), m)
	assert.ErrorIs(t, err, domain.ErrAlreadyInHousehold)
}

func TestHouseholdRepository_InsertMember_DuplicateOwnerRow(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	m := &domain.HouseholdMember{HouseholdID: "hh-1", UserID: "owner-1", Role: domain.HouseholdRoleOwner, Status: domain.HouseholdMemberStatusActive}
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO household_members (household_id, user_id, role, status, prior_subscription_tier, prior_subscription_status) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, joined_at, created_at, updated_at`)).
		WithArgs(m.HouseholdID, m.UserID, m.Role, m.Status, m.PriorSubscriptionTier, m.PriorSubscriptionStatus).
		WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "uq_household_members_active_owner"})

	err := repo.InsertMember(context.Background(), m)
	assert.ErrorIs(t, err, domain.ErrAlreadyOwnsHousehold)
}

func TestHouseholdRepository_ListMembers(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, household_id, user_id, role, status, joined_at, removed_at, prior_subscription_tier, prior_subscription_status, created_at, updated_at
		FROM household_members WHERE household_id = $1 AND status = 'active' ORDER BY joined_at ASC`)).
		WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at", "prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at"}).
			AddRow("mem-1", "hh-1", "owner-1", "owner", "active", now, nil, nil, nil, now, now))

	members, err := repo.ListMembers(context.Background(), "hh-1")
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, "owner-1", members[0].UserID)
}

func TestHouseholdRepository_GetActiveMember(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, household_id, user_id, role, status, joined_at, removed_at, prior_subscription_tier, prior_subscription_status, created_at, updated_at
		FROM household_members WHERE household_id = $1 AND user_id = $2 AND status = 'active'`)).
		WithArgs("hh-1", "user-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at", "prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at"}).
			AddRow("mem-1", "hh-1", "user-1", "member", "active", now, nil, nil, nil, now, now))

	member, err := repo.GetActiveMember(context.Background(), "hh-1", "user-1")
	require.NoError(t, err)
	assert.Equal(t, "mem-1", member.ID)
}

func TestHouseholdRepository_GetActiveMember_NotFound(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, household_id, user_id, role, status, joined_at, removed_at, prior_subscription_tier, prior_subscription_status, created_at, updated_at
		FROM household_members WHERE household_id = $1 AND user_id = $2 AND status = 'active'`)).
		WithArgs("hh-1", "user-1").
		WillReturnError(pgx.ErrNoRows)

	_, err := repo.GetActiveMember(context.Background(), "hh-1", "user-1")
	assert.ErrorIs(t, err, domain.ErrMemberNotFound)
}

func TestHouseholdRepository_RemoveMember(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE household_members SET status = 'removed', removed_at = now(), updated_at = now() WHERE household_id = $1 AND user_id = $2 AND status = 'active' AND role = 'member'`)).
		WithArgs("hh-1", "user-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.RemoveMember(context.Background(), "hh-1", "user-1")
	require.NoError(t, err)
}

func TestHouseholdRepository_RemoveMember_NotFoundOrIsOwner(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE household_members SET status = 'removed', removed_at = now(), updated_at = now() WHERE household_id = $1 AND user_id = $2 AND status = 'active' AND role = 'member'`)).
		WithArgs("hh-1", "owner-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.RemoveMember(context.Background(), "hh-1", "owner-1")
	assert.ErrorIs(t, err, domain.ErrMemberNotFound)
}

// sanity: pgconn errors that are not 23505 propagate unmapped (not swallowed).
func TestHouseholdRepository_InsertMember_OtherDBError(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	m := &domain.HouseholdMember{HouseholdID: "hh-1", UserID: "user-1", Role: domain.HouseholdRoleMember, Status: domain.HouseholdMemberStatusActive}
	dbErr := errors.New("connection reset")
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO household_members (household_id, user_id, role, status, prior_subscription_tier, prior_subscription_status) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, joined_at, created_at, updated_at`)).
		WithArgs(m.HouseholdID, m.UserID, m.Role, m.Status, m.PriorSubscriptionTier, m.PriorSubscriptionStatus).
		WillReturnError(dbErr)

	err := repo.InsertMember(context.Background(), m)
	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrAlreadyInHousehold)
	assert.NotErrorIs(t, err, domain.ErrAlreadyOwnsHousehold)
}
