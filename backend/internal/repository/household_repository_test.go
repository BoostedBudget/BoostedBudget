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

// householdColumnNames / householdMemberColumnNames mirror the production Scan()
// column order in household_repository.go's householdScanArgs / householdMemberScanArgs
// helpers, so pgxmock row fixtures stay in sync across every test that builds a full
// households / household_members row.
var householdColumnNames = []string{
	"id", "owner_user_id", "name", "created_at", "updated_at",
	"over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at",
}

var householdMemberColumnNames = []string{
	"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
	"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
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
	overCapSince := now.Add(-24 * time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT h.id, h.owner_user_id, h.name, h.created_at, h.updated_at, h.over_capacity_since, h.over_capacity_start_notice_sent_at, h.over_capacity_warn3d_sent_at
		FROM household_members hm
		JOIN households h ON h.id = hm.household_id
		WHERE hm.user_id = $1 AND hm.status = 'active'
		FOR UPDATE OF hm
	`)).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows(householdColumnNames).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, &overCapSince, nil, nil))

	h, err := repo.LockActiveMembershipForUser(context.Background(), "user-1")
	require.NoError(t, err)
	assert.Equal(t, "hh-1", h.ID)
	assert.Equal(t, "owner-1", h.OwnerUserID)
	require.NotNil(t, h.OverCapacitySince)
	assert.WithinDuration(t, overCapSince, *h.OverCapacitySince, time.Second)
	assert.Nil(t, h.OverCapacityStartNoticeSentAt)
	assert.Nil(t, h.OverCapacityWarn3dSentAt)
}

func TestHouseholdRepository_LockActiveMembershipForUser_NotFound(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT h.id, h.owner_user_id, h.name, h.created_at, h.updated_at, h.over_capacity_since, h.over_capacity_start_notice_sent_at, h.over_capacity_warn3d_sent_at
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
		WillReturnRows(pgxmock.NewRows(householdMemberColumnNames).
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
		WillReturnRows(pgxmock.NewRows(householdMemberColumnNames).
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

// ─── LockHouseholdByID (#1371 — Phase 7 foundation) ──────────────────────────

func TestHouseholdRepository_LockHouseholdByID_Found(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, owner_user_id, name, created_at, updated_at, over_capacity_since, over_capacity_start_notice_sent_at, over_capacity_warn3d_sent_at
		FROM households WHERE id = $1 FOR UPDATE
	`)).
		WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows(householdColumnNames).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil))

	h, err := repo.LockHouseholdByID(context.Background(), "hh-1")
	require.NoError(t, err)
	assert.Equal(t, "hh-1", h.ID)
	assert.Equal(t, "owner-1", h.OwnerUserID)
	assert.Equal(t, "The Smiths", h.Name)
	assert.Nil(t, h.OverCapacitySince)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHouseholdRepository_LockHouseholdByID_NotFound(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, owner_user_id, name, created_at, updated_at, over_capacity_since, over_capacity_start_notice_sent_at, over_capacity_warn3d_sent_at
		FROM households WHERE id = $1 FOR UPDATE
	`)).
		WithArgs("hh-missing").
		WillReturnError(pgx.ErrNoRows)

	_, err := repo.LockHouseholdByID(context.Background(), "hh-missing")
	assert.ErrorIs(t, err, domain.ErrHouseholdNotFound)
}

// ─── DeleteRemovedMember (#1371 — Phase 7 foundation) ────────────────────────

func TestHouseholdRepository_DeleteRemovedMember_Success(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
		DELETE FROM household_members WHERE id = $1 AND status = 'removed'
		RETURNING household_id, user_id
	`)).
		WithArgs("member-row-1").
		WillReturnRows(pgxmock.NewRows([]string{"household_id", "user_id"}).AddRow("hh-1", "member-1"))

	householdID, userID, err := repo.DeleteRemovedMember(context.Background(), "member-row-1")
	require.NoError(t, err)
	assert.Equal(t, "hh-1", householdID)
	assert.Equal(t, "member-1", userID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHouseholdRepository_DeleteRemovedMember_NotFound(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
		DELETE FROM household_members WHERE id = $1 AND status = 'removed'
		RETURNING household_id, user_id
	`)).
		WithArgs("member-row-active").
		WillReturnError(pgx.ErrNoRows)

	_, _, err := repo.DeleteRemovedMember(context.Background(), "member-row-active")
	assert.ErrorIs(t, err, domain.ErrMemberNotFound)
}

// ─── SetOverCapacity / ClearOverCapacity / notice markers (#1371 — Task 1) ───

func TestHouseholdRepository_SetOverCapacity(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	since := time.Now()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE households SET over_capacity_since = $2 WHERE id = $1 AND over_capacity_since IS NULL`)).
		WithArgs("hh-1", since).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.SetOverCapacity(context.Background(), "hh-1", since)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// SetOverCapacity must be a silent no-op (not an error) when grace is already
// active, so the original deadline is never reset by a second call.
func TestHouseholdRepository_SetOverCapacity_AlreadyActive_NoOp(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	since := time.Now()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE households SET over_capacity_since = $2 WHERE id = $1 AND over_capacity_since IS NULL`)).
		WithArgs("hh-1", since).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.SetOverCapacity(context.Background(), "hh-1", since)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHouseholdRepository_ClearOverCapacity(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE households SET over_capacity_since = NULL, over_capacity_start_notice_sent_at = NULL, over_capacity_warn3d_sent_at = NULL WHERE id = $1`)).
		WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.ClearOverCapacity(context.Background(), "hh-1")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// ClearOverCapacity is idempotent: calling it on a household not currently in
// grace (0 rows changed by the NULL assignments, or the row simply already all-NULL)
// must not error.
func TestHouseholdRepository_ClearOverCapacity_NotInGrace_NoOp(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE households SET over_capacity_since = NULL, over_capacity_start_notice_sent_at = NULL, over_capacity_warn3d_sent_at = NULL WHERE id = $1`)).
		WithArgs("hh-missing").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.ClearOverCapacity(context.Background(), "hh-missing")
	require.NoError(t, err)
}

func TestHouseholdRepository_MarkOverCapacityStartNoticeSent(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	sentAt := time.Now()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE households SET over_capacity_start_notice_sent_at = $2 WHERE id = $1`)).
		WithArgs("hh-1", sentAt).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.MarkOverCapacityStartNoticeSent(context.Background(), "hh-1", sentAt)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHouseholdRepository_MarkOverCapacityWarn3dSent(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	sentAt := time.Now()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE households SET over_capacity_warn3d_sent_at = $2 WHERE id = $1`)).
		WithArgs("hh-1", sentAt).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.MarkOverCapacityWarn3dSent(context.Background(), "hh-1", sentAt)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// ─── ListHouseholdsWithMembers (#1371 — Task 1) ──────────────────────────────

func TestHouseholdRepository_ListHouseholdsWithMembers(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	now := time.Now()
	overCapSince := now.Add(-time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, owner_user_id, name, created_at, updated_at, over_capacity_since, over_capacity_start_notice_sent_at, over_capacity_warn3d_sent_at
		FROM households h
		WHERE EXISTS (SELECT 1 FROM household_members hm WHERE hm.household_id = h.id AND hm.status = 'active')
	`)).
		WillReturnRows(pgxmock.NewRows(householdColumnNames).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil).
			AddRow("hh-2", "owner-2", "The Joneses", now, now, &overCapSince, nil, nil))

	households, err := repo.ListHouseholdsWithMembers(context.Background())
	require.NoError(t, err)
	require.Len(t, households, 2)
	assert.Equal(t, "hh-1", households[0].ID)
	assert.Nil(t, households[0].OverCapacitySince)
	assert.Equal(t, "hh-2", households[1].ID)
	require.NotNil(t, households[1].OverCapacitySince)
	assert.WithinDuration(t, overCapSince, *households[1].OverCapacitySince, time.Second)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHouseholdRepository_ListHouseholdsWithMembers_Empty(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, owner_user_id, name, created_at, updated_at, over_capacity_since, over_capacity_start_notice_sent_at, over_capacity_warn3d_sent_at
		FROM households h
		WHERE EXISTS (SELECT 1 FROM household_members hm WHERE hm.household_id = h.id AND hm.status = 'active')
	`)).
		WillReturnRows(pgxmock.NewRows(householdColumnNames))

	households, err := repo.ListHouseholdsWithMembers(context.Background())
	require.NoError(t, err)
	assert.Empty(t, households)
	require.NoError(t, mock.ExpectationsWereMet())
}

// ─── GetRemovedMemberForUpdate / RestoreMember (#1371 — Task 1, used by re-invite in Task 4) ───

func TestHouseholdRepository_GetRemovedMemberForUpdate_Found(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	now := time.Now()
	removedAt := now
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, household_id, user_id, role, status, joined_at, removed_at, prior_subscription_tier, prior_subscription_status, created_at, updated_at
		FROM household_members WHERE household_id = $1 AND user_id = $2 AND status = 'removed'
		ORDER BY removed_at DESC, created_at DESC
		LIMIT 1
		FOR UPDATE
	`)).
		WithArgs("hh-1", "user-1").
		WillReturnRows(pgxmock.NewRows(householdMemberColumnNames).
			AddRow("mem-1", "hh-1", "user-1", "member", "removed", now, &removedAt, nil, nil, now, now))

	m, err := repo.GetRemovedMemberForUpdate(context.Background(), "hh-1", "user-1")
	require.NoError(t, err)
	assert.Equal(t, "mem-1", m.ID)
	assert.Equal(t, domain.HouseholdMemberStatusRemoved, m.Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHouseholdRepository_GetRemovedMemberForUpdate_NotFound(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, household_id, user_id, role, status, joined_at, removed_at, prior_subscription_tier, prior_subscription_status, created_at, updated_at
		FROM household_members WHERE household_id = $1 AND user_id = $2 AND status = 'removed'
		ORDER BY removed_at DESC, created_at DESC
		LIMIT 1
		FOR UPDATE
	`)).
		WithArgs("hh-1", "user-2").
		WillReturnError(pgx.ErrNoRows)

	_, err := repo.GetRemovedMemberForUpdate(context.Background(), "hh-1", "user-2")
	assert.ErrorIs(t, err, domain.ErrMemberNotFound)
}

func TestHouseholdRepository_RestoreMember(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE household_members SET status = 'active', removed_at = NULL, updated_at = now() WHERE id = $1 AND status = 'removed'`)).
		WithArgs("mem-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.RestoreMember(context.Background(), "mem-1")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHouseholdRepository_RestoreMember_NotRemoved(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE household_members SET status = 'active', removed_at = NULL, updated_at = now() WHERE id = $1 AND status = 'removed'`)).
		WithArgs("mem-active").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.RestoreMember(context.Background(), "mem-active")
	assert.ErrorIs(t, err, domain.ErrMemberNotFound)
}

// RestoreMember must map the same unique-constraint violations InsertMember does
// (Codex review finding, #1371 Task 1): restoring a removed row can collide with
// uq_household_members_active_user if the user has since become active elsewhere
// (that index is NOT scoped to household_id — see migration 000107), or with
// uq_household_members_active_owner in an owner-row edge case. Leaking the raw
// pgconn.PgError here would give Task 4's re-invite flow a different error contract
// than every other write path in this file.
func TestHouseholdRepository_RestoreMember_UserActiveElsewhere(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE household_members SET status = 'active', removed_at = NULL, updated_at = now() WHERE id = $1 AND status = 'removed'`)).
		WithArgs("mem-1").
		WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "uq_household_members_active_user"})

	err := repo.RestoreMember(context.Background(), "mem-1")
	assert.ErrorIs(t, err, domain.ErrAlreadyInHousehold)
}

func TestHouseholdRepository_RestoreMember_DuplicateOwnerRow(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE household_members SET status = 'active', removed_at = NULL, updated_at = now() WHERE id = $1 AND status = 'removed'`)).
		WithArgs("mem-1").
		WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "uq_household_members_active_owner"})

	err := repo.RestoreMember(context.Background(), "mem-1")
	assert.ErrorIs(t, err, domain.ErrAlreadyOwnsHousehold)
}

// ─── ListMembershipsPastPurgeWindow (#1371 — Task 2) ─────────────────────────

func TestHouseholdRepository_ListMembershipsPastPurgeWindow(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	now := time.Now()
	removedAt := now.Add(-31 * 24 * time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, household_id, user_id, role, status, joined_at, removed_at, prior_subscription_tier, prior_subscription_status, created_at, updated_at
		FROM household_members
		WHERE status = 'removed' AND removed_at < now() - $1::interval
		ORDER BY removed_at ASC
	`)).
		WithArgs("30 days").
		WillReturnRows(pgxmock.NewRows(householdMemberColumnNames).
			AddRow("mem-1", "hh-1", "user-1", "member", "removed", now, &removedAt, nil, nil, now, now))

	members, err := repo.ListMembershipsPastPurgeWindow(context.Background(), 30)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, "mem-1", members[0].ID)
	assert.Equal(t, domain.HouseholdMemberStatusRemoved, members[0].Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHouseholdRepository_ListMembershipsPastPurgeWindow_Empty(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, household_id, user_id, role, status, joined_at, removed_at, prior_subscription_tier, prior_subscription_status, created_at, updated_at
		FROM household_members
		WHERE status = 'removed' AND removed_at < now() - $1::interval
		ORDER BY removed_at ASC
	`)).
		WithArgs("30 days").
		WillReturnRows(pgxmock.NewRows(householdMemberColumnNames))

	members, err := repo.ListMembershipsPastPurgeWindow(context.Background(), 30)
	require.NoError(t, err)
	assert.Empty(t, members)
	require.NoError(t, mock.ExpectationsWereMet())
}
