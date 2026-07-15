package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newTxTestService builds a HouseholdService whose db + non-tx repo fields all share
// one pgxmock pool, so transactional methods (which open db.Begin and construct fresh
// tx-scoped repository instances) route their SQL through the same expectation queue
// as the plain fields. This exercises the real repository SQL, not interface mocks —
// consistent with this codebase's existing transactional-test convention (see
// registration_service_test.go).
// The concrete *householdService return type (rather than the HouseholdService
// interface) lets tests exercise the narrow HouseholdAccountShareService methods
// (#1368 — Phase 4) too, without adding them to the broad HouseholdService interface
// implemented by operations/household_test.go's mockHouseholdService.
func newTxTestService(t *testing.T, userRepo userLoader, emailSvc EmailService) (pgxmock.PgxPoolIface, *householdService) {
	t.Helper()
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	enc := newTestFieldEncryptor(t)
	hhRepo := repository.NewHouseholdRepository(pool)
	entRepo := repository.NewEntitlementRepository(pool)
	entSvc := NewHouseholdEntitlementService(entRepo, userRepo, hhRepo, testTierConfig(t))
	svc := NewHouseholdService(pool, hhRepo, repository.NewHouseholdInviteRepository(pool, enc),
		repository.NewHouseholdShareRepository(pool), entRepo, entSvc, userRepo, testTierConfig(t), emailSvc, enc)
	return pool, svc.(*householdService)
}

// mockHouseholdInviteRepo and mockHouseholdShareRepo let the non-transactional
// household_service methods (which use the plain injected repo fields) be tested
// without a real DB. Transactional methods construct fresh tx-scoped repository
// instances internally and are tested via pgxmock against the real repository SQL.
type mockHouseholdInviteRepo struct{ mock.Mock }

func (m *mockHouseholdInviteRepo) Create(ctx context.Context, inv *domain.HouseholdInvite) error {
	args := m.Called(ctx, inv)
	return args.Error(0)
}
func (m *mockHouseholdInviteRepo) GetByTokenHashForUpdate(ctx context.Context, tokenHash string) (*domain.HouseholdInvite, error) {
	args := m.Called(ctx, tokenHash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.HouseholdInvite), args.Error(1)
}
func (m *mockHouseholdInviteRepo) GetHouseholdIDByTokenHash(ctx context.Context, tokenHash string) (string, error) {
	args := m.Called(ctx, tokenHash)
	return args.String(0), args.Error(1)
}
func (m *mockHouseholdInviteRepo) GetPreviewByTokenHash(ctx context.Context, tokenHash string) (*repository.HouseholdInvitePreview, error) {
	args := m.Called(ctx, tokenHash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*repository.HouseholdInvitePreview), args.Error(1)
}
func (m *mockHouseholdInviteRepo) ListPendingByHousehold(ctx context.Context, householdID string) ([]domain.HouseholdInvite, error) {
	args := m.Called(ctx, householdID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.HouseholdInvite), args.Error(1)
}
func (m *mockHouseholdInviteRepo) MarkAccepted(ctx context.Context, id, acceptedUserID string) error {
	args := m.Called(ctx, id, acceptedUserID)
	return args.Error(0)
}
func (m *mockHouseholdInviteRepo) MarkDeclined(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}
func (m *mockHouseholdInviteRepo) Revoke(ctx context.Context, householdID, id string) error {
	args := m.Called(ctx, householdID, id)
	return args.Error(0)
}
func (m *mockHouseholdInviteRepo) RevokeAllPendingForHousehold(ctx context.Context, householdID string) error {
	args := m.Called(ctx, householdID)
	return args.Error(0)
}

type mockHouseholdShareRepo struct{ mock.Mock }

func (m *mockHouseholdShareRepo) ListActiveUserAccountFingerprints(ctx context.Context, userID string) ([]repository.AccountFingerprint, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]repository.AccountFingerprint), args.Error(1)
}
func (m *mockHouseholdShareRepo) InsertShare(ctx context.Context, householdID, userAccountID string, physicalBlindIdx *string, visibility domain.ShareVisibility) error {
	args := m.Called(ctx, householdID, userAccountID, physicalBlindIdx, visibility)
	return args.Error(0)
}
func (m *mockHouseholdShareRepo) InsertConsent(ctx context.Context, c *domain.HouseholdShareConsent) error {
	args := m.Called(ctx, c)
	return args.Error(0)
}
func (m *mockHouseholdShareRepo) RevokeSharesForUser(ctx context.Context, householdID, userID string) error {
	args := m.Called(ctx, householdID, userID)
	return args.Error(0)
}
func (m *mockHouseholdShareRepo) DeleteSharesForUser(ctx context.Context, householdID, userID string) error {
	args := m.Called(ctx, householdID, userID)
	return args.Error(0)
}

// SetShareVisibility and ListMyAccountShares (#1368 — Phase 4 writes) are not
// exercised by these Phase 3 tests; stubbed to satisfy the expanded
// HouseholdShareRepository interface.
func (m *mockHouseholdShareRepo) SetShareVisibility(ctx context.Context, householdID, userAccountID string, physicalBlindIdx *string, visibility domain.ShareVisibility) (*domain.HouseholdAccountShare, error) {
	args := m.Called(ctx, householdID, userAccountID, physicalBlindIdx, visibility)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.HouseholdAccountShare), args.Error(1)
}
func (m *mockHouseholdShareRepo) ListMyAccountShares(ctx context.Context, householdID, userID string) ([]domain.UserAccountShareView, error) {
	args := m.Called(ctx, householdID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.UserAccountShareView), args.Error(1)
}

// testTierConfig loads the real tier config (same pattern as household_entitlement_service_test.go).
func testTierConfig(t *testing.T) *config.TierConfig {
	t.Helper()
	tc, err := config.LoadTierConfig()
	require.NoError(t, err)
	return tc
}

// purgeDateWithin30Days is a mock.MatchedBy predicate for the purgeDate argument passed
// to EmailService.SendHouseholdMemberRemoved. Unlike mock.AnythingOfType("time.Time")
// (which accepts ANY time.Time, including a wrong or zero one), this actually proves the
// value reaching the mock is ~30 days out — matching removalPurgeWindow — the same
// tolerance (1 minute) used by the existing assert.WithinDuration checks on
// QueuedRemovalEmail.PurgeDate. #1371 — Task 3 (Codex review follow-up).
func purgeDateWithin30Days(t time.Time) bool {
	diff := time.Until(t) - removalPurgeWindow
	if diff < 0 {
		diff = -diff
	}
	return diff < time.Minute
}

// ─── Non-transactional methods (plain mocked repos) ──────────────────────────

func TestHouseholdService_GetMyHousehold_NotFound(t *testing.T) {
	t.Parallel()
	hhRepo := &mockHouseholdRepo{}
	hhRepo.On("GetHouseholdForUser", mock.Anything, "user-1").Return(nil, false, false, nil)

	svc := NewHouseholdService(nil, hhRepo, &mockHouseholdInviteRepo{}, &mockHouseholdShareRepo{},
		&mockEntitlementRepo{}, nil, &mockUserLoader{}, testTierConfig(t), &MockEmailService{}, nil)

	household, _, found, err := svc.GetMyHousehold(context.Background(), "user-1")
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, household)
}

func TestHouseholdService_GetMyHousehold_Found(t *testing.T) {
	t.Parallel()
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "user-1", Name: "The Smiths"}
	hhRepo := &mockHouseholdRepo{}
	hhRepo.On("GetHouseholdForUser", mock.Anything, "user-1").Return(hh, true, true, nil)
	hhRepo.On("CountActiveMembers", mock.Anything, "hh-1").Return(1, nil)
	hhRepo.On("CountPendingInvites", mock.Anything, "hh-1").Return(0, nil)

	userRepo := &mockUserLoader{}
	user := &domain.User{ID: "user-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	userRepo.On("GetUserByID", mock.Anything, "user-1").Return(user, nil)
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "user-1").Return(nil, repository.ErrEntitlementNotFound)

	entSvc := NewHouseholdEntitlementService(entRepo, userRepo, hhRepo, testTierConfig(t))
	svc := NewHouseholdService(nil, hhRepo, &mockHouseholdInviteRepo{}, &mockHouseholdShareRepo{},
		entRepo, entSvc, userRepo, testTierConfig(t), &MockEmailService{}, nil)

	household, summary, found, err := svc.GetMyHousehold(context.Background(), "user-1")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "hh-1", household.ID)
	require.True(t, summary.IsOwner)
	require.Equal(t, 2, summary.UsersIncluded)
}

func TestHouseholdService_ListMembers_NotInHousehold(t *testing.T) {
	t.Parallel()
	hhRepo := &mockHouseholdRepo{}
	hhRepo.On("GetHouseholdForUser", mock.Anything, "user-1").Return(nil, false, false, nil)
	svc := NewHouseholdService(nil, hhRepo, &mockHouseholdInviteRepo{}, &mockHouseholdShareRepo{},
		&mockEntitlementRepo{}, nil, &mockUserLoader{}, testTierConfig(t), &MockEmailService{}, nil)

	_, err := svc.ListMembers(context.Background(), "user-1")
	require.ErrorIs(t, err, domain.ErrHouseholdNotFound)
}

// ─── #1367 — Phase 3 Round 2 (GT-3): member name/avatar enrichment ───────────

// mockMemberProfileResolver is a test double for services.MemberProfileResolver.
type mockMemberProfileResolver struct{ mock.Mock }

func (m *mockMemberProfileResolver) ResolveProfile(ctx context.Context, userID string) (string, string, error) {
	args := m.Called(ctx, userID)
	return args.String(0), args.String(1), args.Error(2)
}

func TestHouseholdService_ListMembers_NoResolverWired_ReturnsBareData(t *testing.T) {
	t.Parallel()
	hh := &domain.Household{ID: "hh-1"}
	hhRepo := &mockHouseholdRepo{}
	hhRepo.On("GetHouseholdForUser", mock.Anything, "user-1").Return(hh, true, true, nil)
	hhRepo.On("ListMembers", mock.Anything, "hh-1").Return([]domain.HouseholdMember{
		{UserID: "user-1", Role: domain.HouseholdRoleOwner, Status: domain.HouseholdMemberStatusActive},
	}, nil)
	svc := NewHouseholdService(nil, hhRepo, &mockHouseholdInviteRepo{}, &mockHouseholdShareRepo{},
		&mockEntitlementRepo{}, nil, &mockUserLoader{}, testTierConfig(t), &MockEmailService{}, nil)
	// No SetMemberProfileResolver call — must not error, must not panic.

	views, err := svc.ListMembers(context.Background(), "user-1")
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, "user-1", views[0].UserID)
	assert.Equal(t, domain.HouseholdRoleOwner, views[0].Role)
	assert.Empty(t, views[0].DisplayName, "no resolver wired — name must be blank, not an error")
	assert.Empty(t, views[0].AvatarURL)
}

func TestHouseholdService_ListMembers_EnrichesWithClerkProfile(t *testing.T) {
	t.Parallel()
	hh := &domain.Household{ID: "hh-1"}
	hhRepo := &mockHouseholdRepo{}
	hhRepo.On("GetHouseholdForUser", mock.Anything, "owner-1").Return(hh, true, true, nil)
	hhRepo.On("ListMembers", mock.Anything, "hh-1").Return([]domain.HouseholdMember{
		{UserID: "owner-1", Role: domain.HouseholdRoleOwner, Status: domain.HouseholdMemberStatusActive},
	}, nil)
	resolver := &mockMemberProfileResolver{}
	resolver.On("ResolveProfile", mock.Anything, "owner-1").Return("Jane Doe", "https://img.example/jane.png", nil)

	svc := NewHouseholdService(nil, hhRepo, &mockHouseholdInviteRepo{}, &mockHouseholdShareRepo{},
		&mockEntitlementRepo{}, nil, &mockUserLoader{}, testTierConfig(t), &MockEmailService{}, nil)
	svc.SetMemberProfileResolver(resolver)

	views, err := svc.ListMembers(context.Background(), "owner-1")
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, "Jane Doe", views[0].DisplayName)
	assert.Equal(t, "https://img.example/jane.png", views[0].AvatarURL)
}

func TestHouseholdService_ListMembers_ResolverError_FailsSoftPerMember(t *testing.T) {
	t.Parallel()
	hh := &domain.Household{ID: "hh-1"}
	hhRepo := &mockHouseholdRepo{}
	hhRepo.On("GetHouseholdForUser", mock.Anything, "owner-1").Return(hh, true, true, nil)
	hhRepo.On("ListMembers", mock.Anything, "hh-1").Return([]domain.HouseholdMember{
		{UserID: "owner-1", Role: domain.HouseholdRoleOwner, Status: domain.HouseholdMemberStatusActive},
		{UserID: "member-2", Role: domain.HouseholdRoleMember, Status: domain.HouseholdMemberStatusActive},
	}, nil)
	resolver := &mockMemberProfileResolver{}
	resolver.On("ResolveProfile", mock.Anything, "owner-1").Return("Jane Doe", "https://img.example/jane.png", nil)
	resolver.On("ResolveProfile", mock.Anything, "member-2").Return("", "", errors.New("clerk API down"))

	svc := NewHouseholdService(nil, hhRepo, &mockHouseholdInviteRepo{}, &mockHouseholdShareRepo{},
		&mockEntitlementRepo{}, nil, &mockUserLoader{}, testTierConfig(t), &MockEmailService{}, nil)
	svc.SetMemberProfileResolver(resolver)

	views, err := svc.ListMembers(context.Background(), "owner-1")
	require.NoError(t, err, "one member's Clerk lookup failing must not fail the whole list")
	require.Len(t, views, 2)
	assert.Equal(t, "Jane Doe", views[0].DisplayName)
	assert.Empty(t, views[1].DisplayName, "failed lookup leaves this member's name blank, not an error")
	assert.Empty(t, views[1].AvatarURL)
}

// ─── clerkMemberProfileResolver (production MemberProfileResolver) ──────────

type mockHouseholdIdentityLookup struct{ mock.Mock }

func (m *mockHouseholdIdentityLookup) GetByUserID(ctx context.Context, userID string) ([]*domain.UserIdentity, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.UserIdentity), args.Error(1)
}

func TestClerkMemberProfileResolver_NoLinkedIdentity_ReturnsBlank(t *testing.T) {
	t.Parallel()
	identityRepo := &mockHouseholdIdentityLookup{}
	identityRepo.On("GetByUserID", mock.Anything, "user-1").Return([]*domain.UserIdentity{}, nil)
	clerkGetter := &MockClerkUserGetter{}

	resolver := NewClerkMemberProfileResolver(identityRepo, clerkGetter)
	name, avatar, err := resolver.ResolveProfile(context.Background(), "user-1")
	require.NoError(t, err)
	assert.Empty(t, name)
	assert.Empty(t, avatar)
	clerkGetter.AssertNotCalled(t, "GetUser", mock.Anything, mock.Anything)
}

func TestClerkMemberProfileResolver_UsesPrimaryIdentity_UsernamePreferred(t *testing.T) {
	t.Parallel()
	identityRepo := &mockHouseholdIdentityLookup{}
	identityRepo.On("GetByUserID", mock.Anything, "user-1").Return([]*domain.UserIdentity{
		{ClerkUserID: "clerk_primary", IsPrimary: true},
		{ClerkUserID: "clerk_secondary", IsPrimary: false},
	}, nil)
	clerkGetter := &MockClerkUserGetter{}
	username := "janedoe"
	imageURL := "https://img.example/jane.png"
	clerkGetter.On("GetUser", mock.Anything, "clerk_primary").Return(&clerk.User{
		Username: &username, ImageURL: &imageURL,
	}, nil)

	resolver := NewClerkMemberProfileResolver(identityRepo, clerkGetter)
	name, avatar, err := resolver.ResolveProfile(context.Background(), "user-1")
	require.NoError(t, err)
	assert.Equal(t, "janedoe", name, "Username takes precedence when present")
	assert.Equal(t, "https://img.example/jane.png", avatar)
}

func TestClerkMemberProfileResolver_FallsBackToFirstLastName(t *testing.T) {
	t.Parallel()
	identityRepo := &mockHouseholdIdentityLookup{}
	identityRepo.On("GetByUserID", mock.Anything, "user-1").Return([]*domain.UserIdentity{
		{ClerkUserID: "clerk_1", IsPrimary: true},
	}, nil)
	clerkGetter := &MockClerkUserGetter{}
	first, last := "Jane", "Doe"
	clerkGetter.On("GetUser", mock.Anything, "clerk_1").Return(&clerk.User{
		FirstName: &first, LastName: &last,
	}, nil)

	resolver := NewClerkMemberProfileResolver(identityRepo, clerkGetter)
	name, avatar, err := resolver.ResolveProfile(context.Background(), "user-1")
	require.NoError(t, err)
	assert.Equal(t, "Jane Doe", name)
	assert.Empty(t, avatar)
}

func TestClerkMemberProfileResolver_NoNameFieldsSet_ReturnsBlankName(t *testing.T) {
	t.Parallel()
	identityRepo := &mockHouseholdIdentityLookup{}
	identityRepo.On("GetByUserID", mock.Anything, "user-1").Return([]*domain.UserIdentity{
		{ClerkUserID: "clerk_1", IsPrimary: true},
	}, nil)
	clerkGetter := &MockClerkUserGetter{}
	clerkGetter.On("GetUser", mock.Anything, "clerk_1").Return(&clerk.User{}, nil)

	resolver := NewClerkMemberProfileResolver(identityRepo, clerkGetter)
	name, avatar, err := resolver.ResolveProfile(context.Background(), "user-1")
	require.NoError(t, err)
	assert.Empty(t, name)
	assert.Empty(t, avatar)
}

func TestClerkMemberProfileResolver_IdentityLookupError_Propagates(t *testing.T) {
	t.Parallel()
	identityRepo := &mockHouseholdIdentityLookup{}
	identityRepo.On("GetByUserID", mock.Anything, "user-1").Return(nil, errors.New("db timeout"))
	clerkGetter := &MockClerkUserGetter{}

	resolver := NewClerkMemberProfileResolver(identityRepo, clerkGetter)
	_, _, err := resolver.ResolveProfile(context.Background(), "user-1")
	require.Error(t, err)
}

func TestClerkMemberProfileResolver_ClerkAPIError_Propagates(t *testing.T) {
	t.Parallel()
	identityRepo := &mockHouseholdIdentityLookup{}
	identityRepo.On("GetByUserID", mock.Anything, "user-1").Return([]*domain.UserIdentity{
		{ClerkUserID: "clerk_1", IsPrimary: true},
	}, nil)
	clerkGetter := &MockClerkUserGetter{}
	clerkGetter.On("GetUser", mock.Anything, "clerk_1").Return(nil, ErrClerkAPIError)

	resolver := NewClerkMemberProfileResolver(identityRepo, clerkGetter)
	_, _, err := resolver.ResolveProfile(context.Background(), "user-1")
	require.Error(t, err)
}

func TestHouseholdService_ListInvites_NotOwner(t *testing.T) {
	t.Parallel()
	hhRepo := &mockHouseholdRepo{}
	hhRepo.On("GetHouseholdForUser", mock.Anything, "member-1").Return(&domain.Household{ID: "hh-1"}, false, true, nil)
	svc := NewHouseholdService(nil, hhRepo, &mockHouseholdInviteRepo{}, &mockHouseholdShareRepo{},
		&mockEntitlementRepo{}, nil, &mockUserLoader{}, testTierConfig(t), &MockEmailService{}, nil)

	_, err := svc.ListInvites(context.Background(), "member-1")
	require.ErrorIs(t, err, domain.ErrNotHouseholdOwner)
}

func TestHouseholdService_RevokeInvite_Owner(t *testing.T) {
	t.Parallel()
	hhRepo := &mockHouseholdRepo{}
	hhRepo.On("GetHouseholdForUser", mock.Anything, "owner-1").Return(&domain.Household{ID: "hh-1"}, true, true, nil)
	inviteRepo := &mockHouseholdInviteRepo{}
	inviteRepo.On("Revoke", mock.Anything, "hh-1", "inv-1").Return(nil)
	svc := NewHouseholdService(nil, hhRepo, inviteRepo, &mockHouseholdShareRepo{},
		&mockEntitlementRepo{}, nil, &mockUserLoader{}, testTierConfig(t), &MockEmailService{}, nil)

	err := svc.RevokeInvite(context.Background(), "owner-1", "inv-1")
	require.NoError(t, err)
	inviteRepo.AssertExpectations(t)
}

func TestHouseholdService_GetInvitePreview_NotFound(t *testing.T) {
	t.Parallel()
	inviteRepo := &mockHouseholdInviteRepo{}
	inviteRepo.On("GetPreviewByTokenHash", mock.Anything, mock.AnythingOfType("string")).Return(nil, domain.ErrInviteNotFound)
	svc := NewHouseholdService(nil, &mockHouseholdRepo{}, inviteRepo, &mockHouseholdShareRepo{},
		&mockEntitlementRepo{}, nil, &mockUserLoader{}, testTierConfig(t), &MockEmailService{}, nil)

	_, err := svc.GetInvitePreview(context.Background(), "sometoken")
	require.ErrorIs(t, err, domain.ErrInviteNotFound)
}

// ─── Transactional methods (pgxmock, real repository SQL) ────────────────────

func TestHouseholdService_CreateHousehold_Success(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()
	now := time.Now()

	pool.ExpectBegin()
	pool.ExpectQuery("INSERT INTO households").
		WithArgs("owner-1", "The Smiths").
		WillReturnRows(pgxmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow("hh-1", now, now))
	pool.ExpectQuery("INSERT INTO household_members").
		WithArgs("hh-1", "owner-1", domain.HouseholdRoleOwner, domain.HouseholdMemberStatusActive, (*string)(nil), (*string)(nil)).
		WillReturnRows(pgxmock.NewRows([]string{"id", "joined_at", "created_at", "updated_at"}).AddRow("mem-1", now, now, now))
	pool.ExpectCommit()

	h, err := svc.CreateHousehold(context.Background(), "owner-1", "The Smiths")
	require.NoError(t, err)
	require.Equal(t, "hh-1", h.ID)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestHouseholdService_CreateHousehold_PanicMidTransaction_StillRollsBack closes a Gap
// Analysis Round 5 (6th Codex pass) finding, GT-7: the OLD `var cerr error; defer func() {
// if cerr != nil { rollback } }()` pattern only rolled back on an EXPLICIT returned error —
// a panic mid-transaction (nil pointer, index out of range, etc.) unwinds past every
// `cerr = ...; return cerr` assignment, so cerr stays nil and the deferred rollback never
// fires, leaking the pooled connection under the app's panic-recovery middleware
// (internal/middleware/recovery.go). Proves the fix (unconditional
// `defer func() { _ = tx.Rollback(ctx) }()`, matching the existing codebase idiom already
// used in registration_service.go/profile_service.go) rolls back even when a downstream
// call panics.
func TestHouseholdService_CreateHousehold_PanicMidTransaction_StillRollsBack(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("INSERT INTO households").
		WithArgs("owner-1", "The Smiths").
		WillPanic("simulated panic mid-transaction")
	pool.ExpectRollback()

	func() {
		defer func() { _ = recover() }()
		_, _ = svc.CreateHousehold(context.Background(), "owner-1", "The Smiths")
	}()

	require.NoError(t, pool.ExpectationsWereMet(), "the transaction must be rolled back even when a downstream call panics")
}

func TestHouseholdService_CreateHousehold_AlreadyOwnsHousehold_RollsBack(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("INSERT INTO households").
		WithArgs("owner-1", "The Smiths").
		WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "uq_households_owner_user_id"})
	pool.ExpectRollback()

	_, err := svc.CreateHousehold(context.Background(), "owner-1", "The Smiths")
	require.ErrorIs(t, err, domain.ErrAlreadyOwnsHousehold)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestHouseholdService_CreateHousehold_AlreadyInHousehold_RollsBack closes a Gap
// Analysis Round 2 (GT-5) test-hardening item (Codex finding 1): the sibling test above
// only covers the OWNER-uniqueness constraint (households.owner_user_id) firing on the
// first insert. This covers the second insert — the caller already being an ACTIVE
// MEMBER of some other household (uq_household_members_active_user) — which must map to
// ErrAlreadyInHousehold, not ErrAlreadyOwnsHousehold, and must still roll back the
// already-inserted households row.
func TestHouseholdService_CreateHousehold_AlreadyInHousehold_RollsBack(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()
	now := time.Now()

	pool.ExpectBegin()
	pool.ExpectQuery("INSERT INTO households").
		WithArgs("member-1", "The Smiths").
		WillReturnRows(pgxmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow("hh-1", now, now))
	pool.ExpectQuery("INSERT INTO household_members").
		WithArgs("hh-1", "member-1", domain.HouseholdRoleOwner, domain.HouseholdMemberStatusActive, (*string)(nil), (*string)(nil)).
		WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "uq_household_members_active_user"})
	pool.ExpectRollback()

	_, err := svc.CreateHousehold(context.Background(), "member-1", "The Smiths")
	require.ErrorIs(t, err, domain.ErrAlreadyInHousehold)
	require.NotErrorIs(t, err, domain.ErrAlreadyOwnsHousehold)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_DeleteHousehold_Success(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").
		WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "owner"))
	pool.ExpectQuery("FOR UPDATE").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"owner_user_id"}).AddRow("owner-1"))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	pool.ExpectExec("DELETE FROM households").WithArgs("hh-1", "owner-1").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("owner-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	pool.ExpectCommit()

	err := svc.DeleteHousehold(context.Background(), "owner-1")
	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_DeleteHousehold_HasActiveMembers_RollsBack(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "owner"))
	pool.ExpectQuery("FOR UPDATE").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"owner_user_id"}).AddRow("owner-1"))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(2))
	pool.ExpectRollback()

	err := svc.DeleteHousehold(context.Background(), "owner-1")
	require.ErrorIs(t, err, domain.ErrHouseholdHasActiveMembers)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_RemoveMember_SelfLeave_Success(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "member-1").
		Return(&domain.User{ID: "member-1", Email: "member@example.com"}, nil)
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days)).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("member-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "member"))
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 2))
	pool.ExpectCommit()

	err := svc.RemoveMember(context.Background(), "member-1", "member-1")
	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet())
	userRepo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)
}

func TestHouseholdService_RemoveMember_OwnerRemovesOther_Success(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "member-1").
		Return(&domain.User{ID: "member-1", Email: "member@example.com"}, nil)
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days)).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "owner"))
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	pool.ExpectCommit()

	err := svc.RemoveMember(context.Background(), "owner-1", "member-1")
	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet())
	userRepo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)
}

// TestHouseholdService_RemoveMember_Success_EmailSendFails_StillReturnsNoError proves
// the post-commit email dispatch is best-effort: RemoveMember has already committed the
// removal by the time SendHouseholdMemberRemoved is called, so a failed send must never
// be surfaced as a removal error. #1371 — Task 3.
func TestHouseholdService_RemoveMember_Success_EmailSendFails_StillReturnsNoError(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "member-1").
		Return(&domain.User{ID: "member-1", Email: "member@example.com"}, nil)
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days)).
		Return(errors.New("sendgrid unavailable"))
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("member-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "member"))
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 2))
	pool.ExpectCommit()

	err := svc.RemoveMember(context.Background(), "member-1", "member-1")
	require.NoError(t, err, "a failed best-effort email send must not surface as a removal error")
	require.NoError(t, pool.ExpectationsWereMet())
	emailSvc.AssertExpectations(t)
}

func TestHouseholdService_RemoveMember_NonOwnerCannotRemoveOther_RollsBack(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("member-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "member"))
	pool.ExpectRollback()

	err := svc.RemoveMember(context.Background(), "member-1", "member-2")
	require.ErrorIs(t, err, domain.ErrNotHouseholdOwner)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_RemoveMember_OwnerCannotRemoveSelf_RollsBack(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()
	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "owner"))
	pool.ExpectRollback()

	err := svc.RemoveMember(context.Background(), "owner-1", "owner-1")
	require.ErrorIs(t, err, domain.ErrCannotRemoveOwner)
	require.NoError(t, pool.ExpectationsWereMet())
}

// ─── OffboardExcessMembers (#1371 — Phase 7 foundation) ──────────────────────

func TestHouseholdService_OffboardExcessMembers_NoExcess_NoOp(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"} // 2 seats
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()
	now := time.Now()

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil))
	pool.ExpectQuery("status = 'active' ORDER BY joined_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
			"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
		}).
			AddRow("mem-owner", "hh-1", "owner-1", "owner", "active", now, nil, nil, nil, now, now).
			AddRow("mem-1", "hh-1", "member-1", "member", "active", now, nil, nil, nil, now, now))
	pool.ExpectCommit()

	queued, err := svc.OffboardExcessMembers(context.Background(), "hh-1")
	require.NoError(t, err)
	assert.Empty(t, queued, "2 active members on a 2-seat plan is not excess")
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_OffboardExcessMembers_RemovesMostRecentlyJoinedFirst(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"} // 2 seats
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)
	userRepo.On("GetUserByID", mock.Anything, "member-2").
		Return(&domain.User{ID: "member-2", Email: "member2@example.com"}, nil)
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member2@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days)).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	joinedOwner := time.Now().Add(-72 * time.Hour)
	joined1 := time.Now().Add(-48 * time.Hour)
	joined2 := time.Now().Add(-24 * time.Hour) // most recently joined -> removed first (LIFO)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", joinedOwner, joinedOwner, nil, nil, nil))
	pool.ExpectQuery("status = 'active' ORDER BY joined_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
			"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
		}).
			AddRow("mem-owner", "hh-1", "owner-1", "owner", "active", joinedOwner, nil, nil, nil, joinedOwner, joinedOwner).
			AddRow("mem-1", "hh-1", "member-1", "member", "active", joined1, nil, nil, nil, joined1, joined1).
			AddRow("mem-2", "hh-1", "member-2", "member", "active", joined2, nil, nil, nil, joined2, joined2))
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	queued, err := svc.OffboardExcessMembers(context.Background(), "hh-1")
	require.NoError(t, err)
	require.Len(t, queued, 1)
	assert.Equal(t, "member2@example.com", queued[0].ToEmail)
	assert.Equal(t, "The Smiths", queued[0].HouseholdName)
	assert.WithinDuration(t, time.Now().Add(30*24*time.Hour), queued[0].PurgeDate, time.Minute)
	require.NoError(t, pool.ExpectationsWereMet())
	userRepo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)
}

// TestHouseholdService_OffboardExcessMembers_EmailSendFails_StillReturnsNoError proves
// the post-commit email dispatch is best-effort: the removal is already committed by
// the time SendHouseholdMemberRemoved runs, so a failed send must never be surfaced as
// an OffboardExcessMembers error, and the queued result is still returned. #1371 — Task 3.
func TestHouseholdService_OffboardExcessMembers_EmailSendFails_StillReturnsNoError(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"} // 2 seats
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)
	userRepo.On("GetUserByID", mock.Anything, "member-2").
		Return(&domain.User{ID: "member-2", Email: "member2@example.com"}, nil)
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member2@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days)).
		Return(errors.New("sendgrid unavailable"))
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	joinedOwner := time.Now().Add(-72 * time.Hour)
	joined1 := time.Now().Add(-48 * time.Hour)
	joined2 := time.Now().Add(-24 * time.Hour)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", joinedOwner, joinedOwner, nil, nil, nil))
	pool.ExpectQuery("status = 'active' ORDER BY joined_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
			"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
		}).
			AddRow("mem-owner", "hh-1", "owner-1", "owner", "active", joinedOwner, nil, nil, nil, joinedOwner, joinedOwner).
			AddRow("mem-1", "hh-1", "member-1", "member", "active", joined1, nil, nil, nil, joined1, joined1).
			AddRow("mem-2", "hh-1", "member-2", "member", "active", joined2, nil, nil, nil, joined2, joined2))
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	queued, err := svc.OffboardExcessMembers(context.Background(), "hh-1")
	require.NoError(t, err, "a failed best-effort email send must not surface as an OffboardExcessMembers error")
	require.Len(t, queued, 1, "the queued result is unaffected by send failure")
	require.NoError(t, pool.ExpectationsWereMet())
	emailSvc.AssertExpectations(t)
}

func TestHouseholdService_OffboardExcessMembers_HouseholdNotFound_RollsBack(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-missing").
		WillReturnError(pgx.ErrNoRows)
	pool.ExpectRollback()

	_, err := svc.OffboardExcessMembers(context.Background(), "hh-missing")
	require.ErrorIs(t, err, domain.ErrHouseholdNotFound)
	require.NoError(t, pool.ExpectationsWereMet())
}

// ─── PurgeRemovedMembership (#1371 — Phase 7 foundation) ─────────────────────

func TestHouseholdService_PurgeRemovedMembership_Success(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("DELETE FROM household_members").WithArgs("member-row-1").
		WillReturnRows(pgxmock.NewRows([]string{"household_id", "user_id"}).AddRow("hh-1", "member-1"))
	// Not currently re-active in this household -> safe to clean up shares/entitlement.
	pool.ExpectQuery("status = 'active'").WithArgs("hh-1", "member-1").
		WillReturnError(pgx.ErrNoRows)
	pool.ExpectExec("DELETE FROM household_account_shares").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("DELETE", 2))
	pool.ExpectExec("source = 'household'").WithArgs("member-1", "hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	err := svc.PurgeRemovedMembership(context.Background(), "member-row-1")
	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_PurgeRemovedMembership_NotFound_RollsBack(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("DELETE FROM household_members").WithArgs("member-row-x").
		WillReturnError(pgx.ErrNoRows)
	pool.ExpectRollback()

	err := svc.PurgeRemovedMembership(context.Background(), "member-row-x")
	require.ErrorIs(t, err, domain.ErrMemberNotFound)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestHouseholdService_PurgeRemovedMembership_UserRejoined_SkipsShareAndEntitlementCleanup
// proves the Codex review fix: a stale removed membership row is keyed only by
// (household_id, user_id) once looked up, so if the user has SINCE rejoined the same
// household (a new active membership row exists) before this purge job runs, blindly
// deleting shares/expiring the entitlement by (household_id, user_id) would wipe the
// REJOINED member's current, live data. The purge must only touch shares/entitlement
// when the user has no active membership in this household — it always still deletes
// the stale historical row itself, since that's harmless regardless.
func TestHouseholdService_PurgeRemovedMembership_UserRejoined_SkipsShareAndEntitlementCleanup(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("DELETE FROM household_members").WithArgs("member-row-old").
		WillReturnRows(pgxmock.NewRows([]string{"household_id", "user_id"}).AddRow("hh-1", "member-1"))
	pool.ExpectQuery("status = 'active'").WithArgs("hh-1", "member-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
			"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
		}).AddRow("mem-new", "hh-1", "member-1", "member", "active", time.Now(), nil, nil, nil, time.Now(), time.Now()))
	// No DELETE FROM household_account_shares, no entitlement UPDATE — must not run.
	pool.ExpectCommit()

	err := svc.PurgeRemovedMembership(context.Background(), "member-row-old")
	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet())
}

// ─── ForceDisband (#1371 — Phase 7 foundation) ───────────────────────────────

func TestHouseholdService_ForceDisband_RemovesAllActiveMembersThenDissolves(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "member-1").
		Return(&domain.User{ID: "member-1", Email: "m1@example.com"}, nil)
	userRepo.On("GetUserByID", mock.Anything, "member-2").
		Return(&domain.User{ID: "member-2", Email: "m2@example.com"}, nil)
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "m1@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days)).
		Return(nil)
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "m2@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days)).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()
	now := time.Now()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil, "owner"))
	pool.ExpectQuery("FOR UPDATE").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"owner_user_id"}).AddRow("owner-1"))
	pool.ExpectQuery("status = 'active' ORDER BY joined_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
			"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
		}).
			AddRow("mem-owner", "hh-1", "owner-1", "owner", "active", now, nil, nil, nil, now, now).
			AddRow("mem-1", "hh-1", "member-1", "member", "active", now, nil, nil, nil, now, now).
			AddRow("mem-2", "hh-1", "member-2", "member", "active", now, nil, nil, nil, now, now))
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("DELETE FROM households").WithArgs("hh-1", "owner-1").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("owner-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	queued, err := svc.ForceDisband(context.Background(), "owner-1")
	require.NoError(t, err)
	require.Len(t, queued, 2)
	assert.Equal(t, "m1@example.com", queued[0].ToEmail)
	assert.Equal(t, "The Smiths", queued[0].HouseholdName)
	assert.WithinDuration(t, time.Now().Add(30*24*time.Hour), queued[0].PurgeDate, time.Minute)
	assert.Equal(t, "m2@example.com", queued[1].ToEmail)
	assert.Equal(t, "The Smiths", queued[1].HouseholdName)
	assert.WithinDuration(t, time.Now().Add(30*24*time.Hour), queued[1].PurgeDate, time.Minute)
	require.NoError(t, pool.ExpectationsWereMet())
	userRepo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)
}

func TestHouseholdService_ForceDisband_OwnerAlone_EquivalentToNormalDisband(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()
	now := time.Now()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil, "owner"))
	pool.ExpectQuery("FOR UPDATE").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"owner_user_id"}).AddRow("owner-1"))
	pool.ExpectQuery("status = 'active' ORDER BY joined_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
			"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
		}).
			AddRow("mem-owner", "hh-1", "owner-1", "owner", "active", now, nil, nil, nil, now, now))
	pool.ExpectExec("DELETE FROM households").WithArgs("hh-1", "owner-1").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("owner-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	queued, err := svc.ForceDisband(context.Background(), "owner-1")
	require.NoError(t, err)
	assert.Empty(t, queued)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_ForceDisband_NotOwner_RollsBack(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("member-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "member"))
	pool.ExpectRollback()

	_, err := svc.ForceDisband(context.Background(), "member-1")
	require.ErrorIs(t, err, domain.ErrNotHouseholdOwner)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_CreateInvite_Success(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdInvite", mock.Anything, "friend@example.com", "owner@example.com", "The Smiths", mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)

	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()
	now := time.Now()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil, "owner"))
	pool.ExpectQuery("FOR UPDATE").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"owner_user_id"}).AddRow("owner-1"))
	pool.ExpectQuery("SELECT COUNT.*household_members").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	pool.ExpectQuery("SELECT COUNT.*household_invites").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(0))
	pool.ExpectQuery("INSERT INTO household_invites").
		WithArgs("hh-1", "owner-1", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow("inv-1", now, now))
	pool.ExpectCommit()

	result, err := svc.CreateInvite(context.Background(), "owner-1", "friend@example.com", "https://app.example.com")
	require.NoError(t, err)
	require.Equal(t, "inv-1", result.Invite.ID)
	require.Contains(t, result.InviteLink, "https://app.example.com/households/invites/")
	require.NoError(t, pool.ExpectationsWereMet())
	emailSvc.AssertExpectations(t)
}

func TestHouseholdService_CreateInvite_SeatsFull_RollsBack(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "premium", SubscriptionStatus: "active"}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()
	now := time.Now()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil, "owner"))
	pool.ExpectQuery("FOR UPDATE").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"owner_user_id"}).AddRow("owner-1"))
	pool.ExpectQuery("SELECT COUNT.*household_members").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1)) // premium: UsersIncluded=1, active=1 -> full
	pool.ExpectQuery("SELECT COUNT.*household_invites").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(0))
	pool.ExpectRollback()

	_, err := svc.CreateInvite(context.Background(), "owner-1", "friend@example.com", "https://app.example.com")
	require.ErrorIs(t, err, domain.ErrHouseholdSeatsFull)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_CreateInvite_SelfInvite_NoTxOpened(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	owner := &domain.User{ID: "owner-1", Email: "Owner@Example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()
	// No Begin expected — self-invite is rejected before opening a transaction.

	_, err := svc.CreateInvite(context.Background(), "owner-1", "owner@example.com", "https://app.example.com")
	require.ErrorIs(t, err, domain.ErrCannotInviteSelf)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_CreateInvite_NotOwner_RollsBack(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	owner := &domain.User{ID: "member-1", Email: "member@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	userRepo.On("GetUserByID", mock.Anything, "member-1").Return(owner, nil)
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("member-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "member"))
	pool.ExpectRollback()

	_, err := svc.CreateInvite(context.Background(), "member-1", "friend@example.com", "https://app.example.com")
	require.ErrorIs(t, err, domain.ErrNotHouseholdOwner)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_DeclineInvite_Success(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	caller := &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: true}
	userRepo.On("GetUserByID", mock.Anything, "caller-1").Return(caller, nil)
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()
	now := time.Now()

	pool.ExpectBegin()
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "inviter_user_id", "invitee_email", "invitee_email_blind_idx", "token_hash",
			"status", "expires_at", "accepted_user_id", "accepted_at", "created_at", "updated_at",
		}).AddRow("inv-1", "hh-1", "owner-1", "friend@example.com", "blind-1", "hash-1", "pending", now.Add(48*time.Hour), nil, nil, now, now))
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("caller-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectExec("UPDATE household_invites SET status = 'declined'").WithArgs("inv-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	err := svc.DeclineInvite(context.Background(), "caller-1", "rawtoken")
	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_DeclineInvite_EmailMismatch_RollsBack(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	caller := &domain.User{ID: "caller-1", Email: "someone-else@example.com", EmailVerified: true}
	userRepo.On("GetUserByID", mock.Anything, "caller-1").Return(caller, nil)
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()
	now := time.Now()

	pool.ExpectBegin()
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "inviter_user_id", "invitee_email", "invitee_email_blind_idx", "token_hash",
			"status", "expires_at", "accepted_user_id", "accepted_at", "created_at", "updated_at",
		}).AddRow("inv-1", "hh-1", "owner-1", "friend@example.com", "blind-1", "hash-1", "pending", now.Add(48*time.Hour), nil, nil, now, now))
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("caller-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectRollback()

	err := svc.DeclineInvite(context.Background(), "caller-1", "rawtoken")
	require.ErrorIs(t, err, domain.ErrInviteEmailMismatch)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_DeclineInvite_UnverifiedEmail_RollsBack(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	caller := &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: false}
	userRepo.On("GetUserByID", mock.Anything, "caller-1").Return(caller, nil)
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()
	now := time.Now()

	pool.ExpectBegin()
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "inviter_user_id", "invitee_email", "invitee_email_blind_idx", "token_hash",
			"status", "expires_at", "accepted_user_id", "accepted_at", "created_at", "updated_at",
		}).AddRow("inv-1", "hh-1", "owner-1", "friend@example.com", "blind-1", "hash-1", "pending", now.Add(48*time.Hour), nil, nil, now, now))
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("caller-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectRollback()

	err := svc.DeclineInvite(context.Background(), "caller-1", "rawtoken")
	require.ErrorIs(t, err, domain.ErrInviteEmailMismatch, "an unverified email must not satisfy the match, even if it equals the invite email")
	require.NoError(t, pool.ExpectationsWereMet())
}

// ─── AcceptInvite — the critical consent-gated transaction ───────────────────

func acceptInviteRow(status string, expiresAt time.Time) *pgxmock.Rows {
	return pgxmock.NewRows([]string{
		"id", "household_id", "inviter_user_id", "invitee_email", "invitee_email_blind_idx", "token_hash",
		"status", "expires_at", "accepted_user_id", "accepted_at", "created_at", "updated_at",
	}).AddRow("inv-1", "hh-1", "owner-1", "friend@example.com", "blind-1", "hash-1", status, expiresAt, nil, nil, time.Now(), time.Now())
}

// expectLockCallerRow appends the pgxmock expectation for the FOR UPDATE lock every
// AcceptInvite/DeclineInvite call now takes on the caller's users row, once past
// invite-lock validation, before the email-match/paying-guard checks.
func expectLockCallerRow(pool pgxmock.PgxPoolIface, callerUserID string) {
	pool.ExpectExec("SELECT 1 FROM users").WithArgs(callerUserID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
}

// expectNoRemovedMember appends the pgxmock expectation for AcceptInvite's Task 4
// (#1371) re-invite-within-window lookup, GetRemovedMemberForUpdate, returning "no
// removed row" — i.e. the caller has never been a member of this household before (or
// their prior removed row was already purged), so AcceptInvite falls through to the
// original fresh-insert path unchanged.
func expectNoRemovedMember(pool pgxmock.PgxPoolIface, householdID, callerUserID string) {
	pool.ExpectQuery("household_members WHERE household_id").WithArgs(householdID, callerUserID).
		WillReturnError(pgx.ErrNoRows)
}

// expectHouseholdIDLookupAndLock appends the pgxmock expectations for the two
// household-first lock-acquisition steps every AcceptInvite call now takes BEFORE the
// invite-row lock (#1367 Gap Analysis Round 4B, GT-6): a non-locking household_id lookup
// by token hash, then the household row's FOR UPDATE lock. Locking the household before
// the invite avoids a lock-order-inversion deadlock against DeleteHousehold (which locks
// the household first, then cascade-deletes its invites).
func expectHouseholdIDLookupAndLock(pool pgxmock.PgxPoolIface, householdID, ownerUserID string) {
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"household_id"}).AddRow(householdID))
	pool.ExpectQuery("FOR UPDATE").WithArgs(householdID).
		WillReturnRows(pgxmock.NewRows([]string{"owner_user_id"}).AddRow(ownerUserID))
}

func TestHouseholdService_AcceptInvite_Success(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	caller := &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: true, SubscriptionTier: "premium", SubscriptionStatus: "cancelled"}
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	userRepo.On("GetUserByID", mock.Anything, "caller-1").Return(caller, nil)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)

	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()
	mask := "mask-blind-1"

	pool.ExpectBegin()
	expectHouseholdIDLookupAndLock(pool, "hh-1", "owner-1")
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(acceptInviteRow("pending", time.Now().Add(48*time.Hour)))
	expectLockCallerRow(pool, "caller-1")
	expectLockCallerRow(pool, "owner-1") // GT-8: owner row is now locked before its status is checked
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	expectNoRemovedMember(pool, "hh-1", "caller-1")
	pool.ExpectQuery("INSERT INTO household_members").
		WithArgs("hh-1", "caller-1", domain.HouseholdRoleMember, domain.HouseholdMemberStatusActive, pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id", "joined_at", "created_at", "updated_at"}).AddRow("mem-1", time.Now(), time.Now(), time.Now()))
	pool.ExpectQuery("user_accounts").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx"}).
			AddRow("acct-1", "inst-1", &mask).
			AddRow("acct-2", "inst-2", nil))
	pool.ExpectQuery("INSERT INTO household_share_consents").
		WithArgs("hh-1", "caller-1", domain.HouseholdShareConsentVersion, pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id", "consented_at", "created_at"}).AddRow("consent-1", time.Now(), time.Now()))
	pool.ExpectExec("INSERT INTO household_account_shares").
		WithArgs("hh-1", "acct-1", pgxmock.AnyArg(), domain.ShareVisibilityShared).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	pool.ExpectExec("INSERT INTO household_account_shares").
		WithArgs("hh-1", "acct-2", (*string)(nil), domain.ShareVisibilityShared).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	pool.ExpectExec("UPDATE subscription_entitlements SET effective_until").WithArgs("caller-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	pool.ExpectExec("INSERT INTO subscription_entitlements").
		WithArgs("caller-1", "household_2", "active", "owner-1", "hh-1").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	pool.ExpectExec("UPDATE household_invites SET status = 'accepted'").WithArgs("caller-1", "inv-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	result, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, nil)
	require.NoError(t, err)
	require.Equal(t, "hh-1", result.Household.ID)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_AcceptInvite_NoConsent_NoTxOpened(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	_, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", false, nil)
	require.ErrorIs(t, err, domain.ErrInvalidConsent)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_AcceptInvite_Expired_RollsBack(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	expectHouseholdIDLookupAndLock(pool, "hh-1", "owner-1")
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(acceptInviteRow("pending", time.Now().Add(-1*time.Hour)))
	pool.ExpectRollback()

	_, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, nil)
	require.ErrorIs(t, err, domain.ErrInviteExpired)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_AcceptInvite_NotPending_RollsBack(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	expectHouseholdIDLookupAndLock(pool, "hh-1", "owner-1")
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(acceptInviteRow("accepted", time.Now().Add(48*time.Hour)))
	pool.ExpectRollback()

	_, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, nil)
	require.ErrorIs(t, err, domain.ErrInviteNotPending)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_AcceptInvite_EmailMismatch_RollsBack(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	caller := &domain.User{ID: "caller-1", Email: "someone-else@example.com", EmailVerified: true}
	userRepo.On("GetUserByID", mock.Anything, "caller-1").Return(caller, nil)
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	expectHouseholdIDLookupAndLock(pool, "hh-1", "owner-1")
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(acceptInviteRow("pending", time.Now().Add(48*time.Hour)))
	expectLockCallerRow(pool, "caller-1")
	pool.ExpectRollback()

	_, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, nil)
	require.ErrorIs(t, err, domain.ErrInviteEmailMismatch)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_AcceptInvite_UnverifiedEmail_RollsBack(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	// Email matches the invite exactly, but is unverified — must still be rejected.
	caller := &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: false}
	userRepo.On("GetUserByID", mock.Anything, "caller-1").Return(caller, nil)
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	expectHouseholdIDLookupAndLock(pool, "hh-1", "owner-1")
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(acceptInviteRow("pending", time.Now().Add(48*time.Hour)))
	expectLockCallerRow(pool, "caller-1")
	pool.ExpectRollback()

	_, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, nil)
	require.ErrorIs(t, err, domain.ErrInviteEmailMismatch)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_AcceptInvite_PayingInvitee_Blocked(t *testing.T) {
	t.Parallel()
	blockedStatuses := []string{"active", "trialing", "past_due"}
	for _, status := range blockedStatuses {
		status := status
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			userRepo := &mockUserLoader{}
			subID := "sub_123"
			caller := &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: true, PaddleSubscriptionID: &subID, SubscriptionStatus: status}
			userRepo.On("GetUserByID", mock.Anything, "caller-1").Return(caller, nil)
			pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
			defer pool.Close()

			pool.ExpectBegin()
			expectHouseholdIDLookupAndLock(pool, "hh-1", "owner-1")
			pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
				WillReturnRows(acceptInviteRow("pending", time.Now().Add(48*time.Hour)))
			expectLockCallerRow(pool, "caller-1")
			pool.ExpectRollback()

			_, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, nil)
			require.ErrorIs(t, err, domain.ErrInviteeHasActivePaidSubscription)
			require.NoError(t, pool.ExpectationsWereMet())
		})
	}
}

// TestHouseholdService_AcceptInvite_PayingInvitee_SurfacesEligibilityDate verifies the
// Codex review Medium finding 5 fix: the 409 carries the caller's SubscriptionEndsAt so
// the client can tell the user when they'll be able to join.
func TestHouseholdService_AcceptInvite_PayingInvitee_SurfacesEligibilityDate(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	subID := "sub_123"
	endsAt := time.Now().Add(72 * time.Hour)
	caller := &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: true, PaddleSubscriptionID: &subID, SubscriptionStatus: "active", SubscriptionEndsAt: &endsAt}
	userRepo.On("GetUserByID", mock.Anything, "caller-1").Return(caller, nil)
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	expectHouseholdIDLookupAndLock(pool, "hh-1", "owner-1")
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(acceptInviteRow("pending", time.Now().Add(48*time.Hour)))
	expectLockCallerRow(pool, "caller-1")
	pool.ExpectRollback()

	_, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, nil)
	require.ErrorIs(t, err, domain.ErrInviteeHasActivePaidSubscription)
	var withDate *domain.InviteeHasActivePaidSubscriptionError
	require.ErrorAs(t, err, &withDate)
	require.NotNil(t, withDate.EligibleAfter)
	assert.WithinDuration(t, endsAt, *withDate.EligibleAfter, time.Second)
}

func TestHouseholdService_AcceptInvite_NonPayingInvitee_NotBlocked(t *testing.T) {
	t.Parallel()
	subID := "sub_123"
	cases := []struct {
		name   string
		caller *domain.User
	}{
		{"local trial no card", &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: true, SubscriptionStatus: "trial"}},
		{"cancelled paddle sub", &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: true, PaddleSubscriptionID: &subID, SubscriptionStatus: "cancelled"}},
		{"no subscription at all", &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: true, SubscriptionStatus: "inactive"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			userRepo := &mockUserLoader{}
			owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
			userRepo.On("GetUserByID", mock.Anything, "caller-1").Return(tc.caller, nil)
			userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)
			pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
			defer pool.Close()

			pool.ExpectBegin()
			expectHouseholdIDLookupAndLock(pool, "hh-1", "owner-1")
			pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
				WillReturnRows(acceptInviteRow("pending", time.Now().Add(48*time.Hour)))
			expectLockCallerRow(pool, "caller-1")
			expectLockCallerRow(pool, "owner-1") // GT-8: owner row is now locked before its status is checked
			pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
				WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(2)) // seats full -> short-circuits after the paying-invitee guard passes
			pool.ExpectRollback()

			_, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, nil)
			require.NotErrorIs(t, err, domain.ErrInviteeHasActivePaidSubscription)
			require.ErrorIs(t, err, domain.ErrHouseholdSeatsFull)
		})
	}
}

func TestHouseholdService_AcceptInvite_OwnerMarkedForDeletion_RollsBack(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	caller := &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: true, SubscriptionStatus: "trial"}
	deletedAt := time.Now()
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionStatus: "active", DeletionRequestedAt: &deletedAt}
	userRepo.On("GetUserByID", mock.Anything, "caller-1").Return(caller, nil)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	expectHouseholdIDLookupAndLock(pool, "hh-1", "owner-1")
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(acceptInviteRow("pending", time.Now().Add(48*time.Hour)))
	expectLockCallerRow(pool, "caller-1")
	expectLockCallerRow(pool, "owner-1") // GT-8: owner row is now locked before its status is checked
	pool.ExpectRollback()

	_, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, nil)
	require.ErrorIs(t, err, domain.ErrOwnerPlanInactive)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_AcceptInvite_OwnerReadOnlyStatus_RollsBack(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	caller := &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: true, SubscriptionStatus: "trial"}
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionStatus: "past_due"}
	userRepo.On("GetUserByID", mock.Anything, "caller-1").Return(caller, nil)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	expectHouseholdIDLookupAndLock(pool, "hh-1", "owner-1")
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(acceptInviteRow("pending", time.Now().Add(48*time.Hour)))
	expectLockCallerRow(pool, "caller-1")
	expectLockCallerRow(pool, "owner-1") // GT-8: owner row is now locked before its status is checked
	pool.ExpectRollback()

	_, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, nil)
	require.ErrorIs(t, err, domain.ErrOwnerPlanInactive)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_AcceptInvite_HiddenAccountIDsForeignID_RollsBack(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	caller := &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: true, SubscriptionStatus: "trial"}
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	userRepo.On("GetUserByID", mock.Anything, "caller-1").Return(caller, nil)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	expectHouseholdIDLookupAndLock(pool, "hh-1", "owner-1")
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(acceptInviteRow("pending", time.Now().Add(48*time.Hour)))
	expectLockCallerRow(pool, "caller-1")
	expectLockCallerRow(pool, "owner-1") // GT-8: owner row is now locked before its status is checked
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	expectNoRemovedMember(pool, "hh-1", "caller-1")
	pool.ExpectQuery("INSERT INTO household_members").
		WithArgs("hh-1", "caller-1", domain.HouseholdRoleMember, domain.HouseholdMemberStatusActive, pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id", "joined_at", "created_at", "updated_at"}).AddRow("mem-1", time.Now(), time.Now(), time.Now()))
	pool.ExpectQuery("user_accounts").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx"}).AddRow("acct-1", "inst-1", nil))
	pool.ExpectRollback()

	_, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, []string{"someone-elses-account"})
	require.ErrorIs(t, err, domain.ErrInvalidConsent)
	require.NoError(t, pool.ExpectationsWereMet())
}

// ─── AcceptInvite — re-invite-within-window restore path (#1371 Task 4) ──────

// removedMemberRow builds the pgxmock row set GetRemovedMemberForUpdate returns for a
// pre-existing 'removed' household_members row, matching the column order
// householdMemberScanArgs expects (id, household_id, user_id, role, status, joined_at,
// removed_at, prior_subscription_tier, prior_subscription_status, created_at, updated_at).
func removedMemberRow(id, householdID, userID string, removedAt time.Time) *pgxmock.Rows {
	joinedAt := removedAt.Add(-24 * time.Hour)
	return pgxmock.NewRows([]string{
		"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
		"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
	}).AddRow(id, householdID, userID, domain.HouseholdRoleMember, domain.HouseholdMemberStatusRemoved,
		joinedAt, &removedAt, (*string)(nil), (*string)(nil), joinedAt, removedAt)
}

// TestHouseholdService_AcceptInvite_RestoresRemovedMember_Success proves the Task 4
// orchestration: when GetRemovedMemberForUpdate finds an existing 'removed' row for this
// exact (household, user) pair, AcceptInvite calls RestoreMember on that row's ID instead
// of InsertMember — no "INSERT INTO household_members" query is issued at all (pgxmock
// would fail the test on an unexpected query), and the returned Member reflects the
// restored, now-active row.
func TestHouseholdService_AcceptInvite_RestoresRemovedMember_Success(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	caller := &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: true, SubscriptionTier: "premium", SubscriptionStatus: "cancelled"}
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	userRepo.On("GetUserByID", mock.Anything, "caller-1").Return(caller, nil)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)

	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()

	removedAt := time.Now().Add(-5 * 24 * time.Hour) // removed 5 days ago — well within the 30-day window
	pool.ExpectBegin()
	expectHouseholdIDLookupAndLock(pool, "hh-1", "owner-1")
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(acceptInviteRow("pending", time.Now().Add(48*time.Hour)))
	expectLockCallerRow(pool, "caller-1")
	expectLockCallerRow(pool, "owner-1")
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	pool.ExpectQuery("household_members WHERE household_id").WithArgs("hh-1", "caller-1").
		WillReturnRows(removedMemberRow("mem-1", "hh-1", "caller-1", removedAt))
	pool.ExpectExec("UPDATE household_members SET status = 'active'").WithArgs("mem-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectQuery("user_accounts").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx"}).AddRow("acct-1", "inst-1", nil))
	pool.ExpectQuery("INSERT INTO household_share_consents").
		WithArgs("hh-1", "caller-1", domain.HouseholdShareConsentVersion, pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id", "consented_at", "created_at"}).AddRow("consent-1", time.Now(), time.Now()))
	pool.ExpectExec("INSERT INTO household_account_shares").
		WithArgs("hh-1", "acct-1", pgxmock.AnyArg(), domain.ShareVisibilityShared).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	pool.ExpectExec("UPDATE subscription_entitlements SET effective_until").WithArgs("caller-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	pool.ExpectExec("INSERT INTO subscription_entitlements").
		WithArgs("caller-1", "household_2", "active", "owner-1", "hh-1").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	pool.ExpectExec("UPDATE household_invites SET status = 'accepted'").WithArgs("caller-1", "inv-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	result, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, nil)
	require.NoError(t, err)
	require.Equal(t, "hh-1", result.Household.ID)
	require.Equal(t, "mem-1", result.Member.ID, "the restored row's own id must be reused, not a fresh one")
	require.Equal(t, domain.HouseholdMemberStatusActive, result.Member.Status)
	require.Nil(t, result.Member.RemovedAt, "removed_at must be cleared on restore")
	require.NoError(t, pool.ExpectationsWereMet(), "no INSERT INTO household_members should have been issued")
}

// TestHouseholdService_AcceptInvite_RestoreConflict_RollsBack proves the conflict case:
// the caller has a removed row for THIS household, but has since become active in a
// DIFFERENT household — RestoreMember's existing unique-constraint mapping must surface
// as domain.ErrAlreadyInHousehold, not be swallowed or panic.
func TestHouseholdService_AcceptInvite_RestoreConflict_RollsBack(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	caller := &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: true, SubscriptionTier: "premium", SubscriptionStatus: "cancelled"}
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	userRepo.On("GetUserByID", mock.Anything, "caller-1").Return(caller, nil)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)

	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()

	removedAt := time.Now().Add(-5 * 24 * time.Hour)
	pool.ExpectBegin()
	expectHouseholdIDLookupAndLock(pool, "hh-1", "owner-1")
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(acceptInviteRow("pending", time.Now().Add(48*time.Hour)))
	expectLockCallerRow(pool, "caller-1")
	expectLockCallerRow(pool, "owner-1")
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	pool.ExpectQuery("household_members WHERE household_id").WithArgs("hh-1", "caller-1").
		WillReturnRows(removedMemberRow("mem-1", "hh-1", "caller-1", removedAt))
	pool.ExpectExec("UPDATE household_members SET status = 'active'").WithArgs("mem-1").
		WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "uq_household_members_active_user"})
	pool.ExpectRollback()

	_, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, nil)
	require.ErrorIs(t, err, domain.ErrAlreadyInHousehold)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_GetInvitePreview_Found(t *testing.T) {
	t.Parallel()
	preview := &repository.HouseholdInvitePreview{HouseholdName: "The Smiths", InviteeEmail: "friend@example.com", ExpiresAt: time.Now().Add(48 * time.Hour)}
	inviteRepo := &mockHouseholdInviteRepo{}
	inviteRepo.On("GetPreviewByTokenHash", mock.Anything, mock.AnythingOfType("string")).Return(preview, nil)
	svc := NewHouseholdService(nil, &mockHouseholdRepo{}, inviteRepo, &mockHouseholdShareRepo{},
		&mockEntitlementRepo{}, nil, &mockUserLoader{}, testTierConfig(t), &MockEmailService{}, nil)

	got, err := svc.GetInvitePreview(context.Background(), "sometoken")
	require.NoError(t, err)
	require.Equal(t, "The Smiths", got.HouseholdName)
}
