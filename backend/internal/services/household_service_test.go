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

// ptrStringEquals implements pgxmock.Argument to match a *string-typed query argument (e.g.
// household_members.prior_subscription_tier/prior_subscription_status, both *string fields on
// domain.HouseholdMember) against an expected non-pointer value — a bare string literal in
// WithArgs would fail with a type mismatch, since the actual bound driver value is a pointer.
type ptrStringEquals string

func (want ptrStringEquals) Match(v interface{}) bool {
	got, ok := v.(*string)
	return ok && got != nil && *got == string(want)
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
	entRepo.On("GetActiveEntitlement", mock.Anything, "user-1", mock.Anything).Return(nil, repository.ErrEntitlementNotFound)

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
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), false).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("member-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "member"))
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil))
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
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), false).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "owner"))
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil))
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
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), false).
		Return(errors.New("sendgrid unavailable"))
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("member-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "member"))
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil))
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
	// Household-row lock is taken before the auth check inside removeMemberTx runs —
	// authorization failures roll back AFTER the lock, not before it.
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil))
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
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil))
	pool.ExpectRollback()

	err := svc.RemoveMember(context.Background(), "owner-1", "owner-1")
	require.ErrorIs(t, err, domain.ErrCannotRemoveOwner)
	require.NoError(t, pool.ExpectationsWereMet())
}

// ─── removeMemberTx grace-clear on in-app removal (#1371 — Phase 7, Task 6, Part D) ──

// TestHouseholdService_RemoveMember_ClearsGrace_WhenBackWithinCapacity proves an owner
// manually removing a member during over-capacity grace gets immediate credit — the
// household is re-counted POST-removal, and since it's now within the owner's current
// seat count, ClearOverCapacity fires in the SAME transaction as the removal.
func TestHouseholdService_RemoveMember_ClearsGrace_WhenBackWithinCapacity(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "member-1").
		Return(&domain.User{ID: "member-1", Email: "member@example.com"}, nil)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").
		Return(&domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"}, nil) // 2 seats
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), false).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	graceStart := time.Now().Add(-2 * 24 * time.Hour)
	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), &graceStart, nil, nil, "owner"))
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), &graceStart, nil, nil))
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// #1389: owner-row lock (lockAndGetUser) before grace-clear check.
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("owner-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	// Grace-clear: post-removal count is back within the owner's 2-seat plan.
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(2))
	pool.ExpectExec("over_capacity_since = NULL, over_capacity_start_notice_sent_at = NULL").WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	err := svc.RemoveMember(context.Background(), "owner-1", "member-1")
	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet())
	userRepo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)
}

// TestHouseholdService_RemoveMember_DoesNotClearGrace_WhenStillOverCapacity proves the
// grace flag is left alone when the household is STILL over capacity after the removal
// — ClearOverCapacity must not be called at all.
func TestHouseholdService_RemoveMember_DoesNotClearGrace_WhenStillOverCapacity(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "member-1").
		Return(&domain.User{ID: "member-1", Email: "member@example.com"}, nil)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").
		Return(&domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"}, nil) // 2 seats
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), false).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	graceStart := time.Now().Add(-2 * 24 * time.Hour)
	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), &graceStart, nil, nil, "owner"))
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), &graceStart, nil, nil))
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// #1389: owner-row lock (lockAndGetUser) before grace-clear check.
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("owner-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	// Grace-clear check: post-removal count is STILL over the owner's 2-seat plan —
	// no ExpectExec for ClearOverCapacity should follow.
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(3))
	pool.ExpectCommit()

	err := svc.RemoveMember(context.Background(), "owner-1", "member-1")
	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet(), "ClearOverCapacity must not be called while still over capacity")
	userRepo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)
}

// TestHouseholdService_RemoveMember_GraceClear_OwnerMalformedOverride_FallsBackToRawTier is the
// RED test for Issue #1413 (this task — tier axis): the grace-clear branch used to pass
// owner.ComputeEffectiveTier() straight into s.tierConfig.GetTier(), so a malformed
// manual_tier_override (an unconstrained VARCHAR(50) with no CHECK constraint) would error the
// whole RemoveMember call. It must instead fall back to the owner's raw tier.
func TestHouseholdService_RemoveMember_GraceClear_OwnerMalformedOverride_FallsBackToRawTier(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "member-1").
		Return(&domain.User{ID: "member-1", Email: "member@example.com"}, nil)
	futureEnd := time.Now().Add(24 * time.Hour)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").
		Return(&domain.User{
			ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active", // raw tier: 2 seats
			ManualTierOverride: strPtr("not-a-real-tier"), ManualTierEndsAt: &futureEnd, // garbage — no CHECK constraint stops this
		}, nil)
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), false).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	graceStart := time.Now().Add(-2 * 24 * time.Hour)
	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), &graceStart, nil, nil, "owner"))
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), &graceStart, nil, nil))
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("owner-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	// 2 remaining active members is within the owner's RAW 2-seat tier (the malformed override
	// must not be treated as some other seat count) -> grace clears.
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(2))
	pool.ExpectExec("over_capacity_since = NULL, over_capacity_start_notice_sent_at = NULL").WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	err := svc.RemoveMember(context.Background(), "owner-1", "member-1")
	require.NoError(t, err, "a malformed owner override must not error the grace-clear seat check")
	require.NoError(t, pool.ExpectationsWereMet(), "the seat check must fall back to the owner's raw tier")
	userRepo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)
}

// TestHouseholdService_OffboardExcessMembers_OwnerMalformedOverride_FallsBackToRawTier is the
// RED test for Issue #1413 (this task — tier axis) at the OffboardExcessMembers seat-capacity
// check: before this fix, a malformed manual_tier_override reached
// s.tierConfig.GetTier(owner.ComputeEffectiveTier()) as an unrecognized tier name and errored the
// whole daily auto-offboard run for this household. It must instead fall back to the owner's raw
// tier.
func TestHouseholdService_OffboardExcessMembers_OwnerMalformedOverride_FallsBackToRawTier(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	futureEnd := time.Now().Add(24 * time.Hour)
	owner := &domain.User{
		ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active", // raw tier: 2 seats
		ManualTierOverride: strPtr("not-a-real-tier"), ManualTierEndsAt: &futureEnd,
	}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()
	now := time.Now()
	anchor := time.Now().Add(-2 * 24 * time.Hour)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, &anchor, nil, nil))
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("owner-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectQuery("status = 'active' ORDER BY joined_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
			"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
		}).
			AddRow("mem-owner", "hh-1", "owner-1", "owner", "active", now, nil, nil, nil, now, now).
			AddRow("mem-1", "hh-1", "member-1", "member", "active", now, nil, nil, nil, now, now))
	pool.ExpectCommit()

	queued, err := svc.OffboardExcessMembers(context.Background(), "hh-1", anchor)
	require.NoError(t, err, "a malformed owner override must not error the seat-capacity check")
	assert.Empty(t, queued, "2 active members on the owner's RAW 2-seat plan is not excess")
	require.NoError(t, pool.ExpectationsWereMet())
	userRepo.AssertExpectations(t)
}

// TestHouseholdService_RemoveMember_LocksOwnerBeforeGraceClear is a dedicated lock-ordering
// regression test for #1389 (follow-up from Round 9/10 Gap Analysis on PR #1387): the
// grace-clear branch inside removeMemberTx reads the owner via the SAME unlocked-read
// pattern OffboardExcessMembers had before its Round 9 fix, reached here through
// RemoveMember (and, unlocked, ForceDisband) rather than through
// OffboardExcessMembers's own lockAndGetUser call. The owner's users row must now be
// locked (lockAndGetUser's `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`) BEFORE the
// post-removal CountActiveMembers/ClearOverCapacity decision — mirroring
// TestHouseholdService_OffboardExcessMembers_LocksOwnerBeforeTierAndRemoval's identical
// rationale. pgxmock enforces expectations in declaration order by default, so declaring
// the owner-lock ExpectExec AFTER the member-removal mutations (RemoveMember/
// ExpireActiveEntitlement/RevokeSharesForUser — which must run first, per removeMemberTx's
// own sequencing) but BEFORE the CountActiveMembers query proves the lock is taken exactly
// where the grace-clear decision needs it. If the implementation regressed to a plain,
// unlocked userRepo.GetUserByID, pgxmock would see the unexpected `SELECT COUNT` query
// arrive before the owner-lock Exec it's still waiting on, and fail with a clear ordering
// mismatch, not a false pass. This is the pgxmock-level complement to Round 9's real-
// Postgres concurrency proof (household_service_integration_test.go:726) — per this
// codebase's established rule (memory feedback_pgxmock_integration_coverage.md), pgxmock
// alone can prove query SHAPE/ORDER but never a genuine row-lock/concurrency guarantee, and
// lockAndGetUser's actual serialization is already proven end-to-end by that same SQL
// there — no new real-Postgres test is added for this narrower, lower-severity call site.
func TestHouseholdService_RemoveMember_LocksOwnerBeforeGraceClear(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "member-1").
		Return(&domain.User{ID: "member-1", Email: "member@example.com"}, nil)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").
		Return(&domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"}, nil) // 2 seats
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), false).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	graceStart := time.Now().Add(-2 * 24 * time.Hour)
	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), &graceStart, nil, nil, "owner"))
	// Household-row lock — always taken first.
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), &graceStart, nil, nil))
	// Member-removal mutations — must run BEFORE the owner lock below (removeMemberTx's own
	// sequencing: soft-remove, expire entitlement, revoke shares, THEN grace-clear).
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// Owner-row lock (lockAndGetUser) — must precede the CountActiveMembers query below.
	// This is the query whose declaration position this test exists to pin down. The
	// pattern requires FOR UPDATE explicitly (not just "SELECT 1 FROM users"), so a
	// regression that dropped the lock clause but kept a plain SELECT would still fail
	// this test rather than passing on a substring-only match.
	pool.ExpectExec("SELECT 1 FROM users.*FOR UPDATE").WithArgs("owner-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(2))
	pool.ExpectExec("over_capacity_since = NULL, over_capacity_start_notice_sent_at = NULL").WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	err := svc.RemoveMember(context.Background(), "owner-1", "member-1")
	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet(),
		"owner-row lock must be acquired before the CountActiveMembers/ClearOverCapacity decision — see the numbered comments above")
	userRepo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)
}

// TestHouseholdService_RemoveMember_GraceClearOwnerLockFails_RollsBackNoEmail is a
// regression test for #1389 (Codex adversarial review, Low finding): if the grace-clear
// branch's new owner-lock Exec (lockAndGetUser's `SELECT 1 FROM users ... FOR UPDATE`)
// fails AFTER the member/entitlement/share mutations have already been issued on this
// transaction, removeMemberTx must return the error, RemoveMember's deferred
// tx.Rollback(ctx) must undo everything (pgxmock's ExpectRollback pins this), and — since
// the removal never committed — no removal email may be queued or sent. This pins the
// existing deferred-rollback + no-email-on-error contract against the NEW failure point
// this change introduces (lockAndGetUser can fail where the old unlocked GetUserByID read
// could also fail, but this proves the contract holds for the locked variant specifically).
func TestHouseholdService_RemoveMember_GraceClearOwnerLockFails_RollsBackNoEmail(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{} // no stubs: the failure occurs inside lockAndGetUser's
	// own FOR UPDATE Exec, before it ever calls userRepo.GetUserByID — so no owner or
	// target GetUserByID call should happen at all on this path.
	emailSvc := &MockEmailService{} // no stubs: a rolled-back removal must never send email.
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	graceStart := time.Now().Add(-2 * 24 * time.Hour)
	lockErr := errors.New("simulated lock timeout")
	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), &graceStart, nil, nil, "owner"))
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), &graceStart, nil, nil))
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// The owner-lock Exec fails here — AFTER the mutations above, proving they get rolled
	// back rather than left dangling.
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("owner-1").WillReturnError(lockErr)
	pool.ExpectRollback()

	err := svc.RemoveMember(context.Background(), "owner-1", "member-1")
	require.ErrorIs(t, err, lockErr)
	require.NoError(t, pool.ExpectationsWereMet(), "the tx must roll back after the owner-lock failure, with no further queries issued")
	userRepo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)
}

// TestHouseholdService_RemoveMember_NotInGrace_NoOverCapacityChecks proves removeMemberTx
// doesn't even CHECK over-capacity state when the household isn't currently in grace —
// no CountActiveMembers/ClearOverCapacity calls, and no extra owner lookup beyond the
// baseline removal flow.
func TestHouseholdService_RemoveMember_NotInGrace_NoOverCapacityChecks(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "member-1").
		Return(&domain.User{ID: "member-1", Email: "member@example.com"}, nil)
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), false).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "owner")) // not in grace
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil)) // not in grace
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// No SELECT COUNT, no ClearOverCapacity/SetOverCapacity — over-capacity state is
	// never even inspected when the household isn't in grace.
	pool.ExpectCommit()

	err := svc.RemoveMember(context.Background(), "owner-1", "member-1")
	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet(), "over-capacity state must not be checked at all when not in grace")
	// Only the target's own GetUserByID (for the removal email) should have been called
	// — no extra owner lookup, since the grace-check branch is never entered.
	userRepo.AssertExpectations(t)
	userRepo.AssertNumberOfCalls(t, "GetUserByID", 1)
	emailSvc.AssertExpectations(t)
}

// TestHouseholdService_RemoveMember_LocksHouseholdBeforeMutations is a dedicated
// lock-ordering regression test (Gap Analysis Round 3, GT-1): RemoveMember must take the
// household-row FOR UPDATE lock (via LockHouseholdByID) BEFORE issuing any
// household_members/subscription_entitlements/household_account_shares mutation —
// matching the "household-row lock always taken first" convention already established by
// OffboardExcessMembers/ForceDisband/PurgeRemovedMembership. Without this, an owner
// manually removing a member (which touches household_members first, then LATER wants the
// households lock for the grace-clear) can circular-wait against the daily
// OffboardExcessMembers worker (which locks households FIRST, then household_members for
// the same member) — a real deadlock risk. pgxmock enforces expectations in declaration
// order by default, so declaring the households-row lock query here, between the
// non-locking GetHouseholdForUser resolution and the first mutation, makes this test FAIL
// against the pre-fix implementation (which never issues that query at all) and PASS once
// RemoveMember acquires the lock in the right place.
func TestHouseholdService_RemoveMember_LocksHouseholdBeforeMutations(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "member-1").
		Return(&domain.User{ID: "member-1", Email: "member@example.com"}, nil)
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), false).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	pool.ExpectBegin()
	// (1) Non-locking resolution of which household actorUserID belongs to.
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "owner"))
	// (2) Household-row lock — must come BEFORE any household_members/entitlements/shares
	// mutation below. If the implementation regressed to never locking (or locked after
	// mutating), this expectation would go unmet/mismatched and the test would fail.
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil))
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	pool.ExpectCommit()

	err := svc.RemoveMember(context.Background(), "owner-1", "member-1")
	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet(), "household-row lock must be acquired before any household_members/entitlements/shares mutation")
	userRepo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)
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
	// A concrete, matching over-capacity anchor: production only ever calls this method
	// with h.OverCapacitySince already set (Case 4 in processHousehold only fires once
	// it's non-nil), so the locked row's own anchor here must match whatever the caller
	// passes as expectedOverCapacitySince — otherwise the new Round 9 anchor-mismatch
	// guard (tested separately below) would short-circuit this test before it ever
	// reaches the excess-computation logic under test.
	anchor := time.Now().Add(-2 * 24 * time.Hour)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, &anchor, nil, nil))
	// Round 9 Gap Analysis fix (6th round-9 fix): the owner is now read via lockAndGetUser
	// (FOR UPDATE), not a plain userRepo.GetUserByID — see OffboardExcessMembers's doc
	// comment above the owner read.
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("owner-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectQuery("status = 'active' ORDER BY joined_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
			"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
		}).
			AddRow("mem-owner", "hh-1", "owner-1", "owner", "active", now, nil, nil, nil, now, now).
			AddRow("mem-1", "hh-1", "member-1", "member", "active", now, nil, nil, nil, now, now))
	pool.ExpectCommit()

	queued, err := svc.OffboardExcessMembers(context.Background(), "hh-1", anchor)
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
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member2@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), false).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	joinedOwner := time.Now().Add(-72 * time.Hour)
	joined1 := time.Now().Add(-48 * time.Hour)
	joined2 := time.Now().Add(-24 * time.Hour) // most recently joined -> removed first (LIFO)
	// Matching over-capacity anchor — see NoExcess_NoOp's comment above for why this must
	// now be non-nil and match the call's expectedOverCapacitySince argument.
	anchor := time.Now().Add(-2 * 24 * time.Hour)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", joinedOwner, joinedOwner, &anchor, nil, nil))
	// Round 9 Gap Analysis fix (6th round-9 fix): the owner is now read via lockAndGetUser
	// (FOR UPDATE), not a plain userRepo.GetUserByID — see OffboardExcessMembers's doc
	// comment above the owner read.
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("owner-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
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
	// #1389: removeMemberTx's own grace-clear branch now re-locks the owner (lockAndGetUser)
	// before its CountActiveMembers/ClearOverCapacity decision, even though the owner is
	// already locked (above) for the remainder of this transaction — a harmless, reentrant
	// FOR UPDATE on the same tx, accepted as a small redundant cost for a self-protecting
	// removeMemberTx (see #1389's PR description for the full rationale).
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("owner-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	// removeMemberTx's own Task 6 Part D grace-clear check (see RemoveMember's identical
	// pattern in TestHouseholdService_RemoveMember_ClearsGrace_WhenBackWithinCapacity)
	// fires here too: household.OverCapacitySince is now realistically non-nil (it must
	// match the anchor above to satisfy the Round 9 precondition check), and post-removal
	// active count (2: owner + member-1) is back within the 2-seat tier, so this in-flight
	// removal transaction clears over-capacity itself — a harmless, idempotent duplicate
	// of autoOffboard's own unconditional ClearOverCapacity call after this method returns.
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(2))
	pool.ExpectExec("over_capacity_since = NULL, over_capacity_start_notice_sent_at = NULL").WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	queued, err := svc.OffboardExcessMembers(context.Background(), "hh-1", anchor)
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
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member2@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), false).
		Return(errors.New("sendgrid unavailable"))
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	joinedOwner := time.Now().Add(-72 * time.Hour)
	joined1 := time.Now().Add(-48 * time.Hour)
	joined2 := time.Now().Add(-24 * time.Hour)
	// Matching over-capacity anchor — see NoExcess_NoOp's comment above for why this must
	// now be non-nil and match the call's expectedOverCapacitySince argument.
	anchor := time.Now().Add(-2 * 24 * time.Hour)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", joinedOwner, joinedOwner, &anchor, nil, nil))
	// Round 9 Gap Analysis fix (6th round-9 fix): the owner is now read via lockAndGetUser
	// (FOR UPDATE), not a plain userRepo.GetUserByID — see OffboardExcessMembers's doc
	// comment above the owner read.
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("owner-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
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
	// #1389: owner re-lock + grace-clear (see RemovesMostRecentlyJoinedFirst).
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("owner-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(2))
	pool.ExpectExec("over_capacity_since = NULL, over_capacity_start_notice_sent_at = NULL").WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	queued, err := svc.OffboardExcessMembers(context.Background(), "hh-1", anchor)
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

	// The lock itself fails before any row is ever read, so the anchor argument's value
	// is irrelevant here — it's never compared against anything.
	_, err := svc.OffboardExcessMembers(context.Background(), "hh-missing", time.Time{})
	require.ErrorIs(t, err, domain.ErrHouseholdNotFound)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestHouseholdService_OffboardExcessMembers_OwnerFreshlyReadOnly_NoRemovals proves the
// Gap Analysis Round 3, GT-2 fix: household_over_capacity_check.go's processHousehold
// reads the owner's subscription status ONCE at the top of a run, then may call
// OffboardExcessMembers (via autoOffboard) some time later in that SAME run. If the
// owner's subscription lapses via a concurrent Paddle webhook in that window, the
// caller's cached owner read would be stale. OffboardExcessMembers already re-fetches the
// owner fresh under its own LockHouseholdByID lock for its tier computation — this test
// proves that fresh read is ALSO used to skip removal entirely (no ListMembers call, no
// removeMemberTx, no seat-limit computation) when the freshly-fetched owner is read-only:
// grace is moot once the owner is read-only, since members already fall back to their own
// tier via the entitlement resolver's owner-read-only branch, so there's nothing to
// LIFO-remove. Returns an empty queue and no error — a legitimate no-op, not a failure.
func TestHouseholdService_OffboardExcessMembers_OwnerFreshlyReadOnly_NoRemovals(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	readOnlyOwner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "cancelled"}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(readOnlyOwner, nil)
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()
	now := time.Now()
	// A matching over-capacity anchor: the new Round 9 anchor-mismatch guard runs BEFORE
	// the owner-read-only check (it's the very first thing after LockHouseholdByID), so
	// the locked row's anchor must match expectedOverCapacitySince here or this test
	// would short-circuit on the anchor guard instead of ever reaching (and proving) the
	// read-only-owner behavior it's actually testing.
	anchor := time.Now().Add(-2 * 24 * time.Hour)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, &anchor, nil, nil))
	// Round 9 Gap Analysis fix (6th round-9 fix): the owner is now read via lockAndGetUser
	// (FOR UPDATE), not a plain userRepo.GetUserByID — see OffboardExcessMembers's doc
	// comment above the owner read. The read-only check still runs immediately after this
	// lock, before any of the queries below.
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("owner-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	// No ListMembers query, no removal mutations, no tier lookup consequence — the
	// read-only check short-circuits before any of that. If the implementation
	// regressed to computing a seat limit and removing members anyway, pgxmock would
	// see an unexpected ListMembers/UPDATE query here and fail.
	pool.ExpectCommit()

	queued, err := svc.OffboardExcessMembers(context.Background(), "hh-1", anchor)
	require.NoError(t, err)
	assert.Empty(t, queued, "a freshly read-only owner means grace is moot — nothing should be removed")
	require.NoError(t, pool.ExpectationsWereMet(), "no ListMembers/removal query should have been issued for a read-only owner")
	userRepo.AssertExpectations(t)
}

// TestHouseholdService_OffboardExcessMembers_CompedOwnerRawInactive_RunsCapacityMachine
// proves the #1413 fix's mirror image of OwnerFreshlyReadOnly_NoRemovals above: a comped
// owner whose REAL subscription has lapsed (raw "inactive") must NOT be treated as
// read-only here either — the capacity machine must run (ListMembers is queried, excess is
// computed from the owner's COMP tier), not short-circuit into an early commit. This
// household has exactly enough active members for the comp tier's seat count, so no
// removal is expected — the point of this test is that the machine RAN at all (reached
// ListMembers), not that it removed anyone.
func TestHouseholdService_OffboardExcessMembers_CompedOwnerRawInactive_RunsCapacityMachine(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	owner := &domain.User{
		ID: "owner-1", SubscriptionTier: "premium", SubscriptionStatus: "inactive", // real subscription lapsed
		ManualTierOverride: strPtr("household_2"), ManualTierEndsAt: &futureEnd, // admin comp, still active — 2 seats
	}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()
	now := time.Now()
	anchor := time.Now().Add(-2 * 24 * time.Hour)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, &anchor, nil, nil))
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("owner-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	// The critical assertion: ListMembers IS queried (unlike the read-only-owner
	// short-circuit test above) — if the implementation regressed to reading raw status,
	// this query would never be reached and pgxmock would fail with an unmet expectation.
	pool.ExpectQuery("status = 'active' ORDER BY joined_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
			"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
		}).
			AddRow("mem-owner", "hh-1", "owner-1", "owner", "active", now, nil, nil, nil, now, now).
			AddRow("mem-1", "hh-1", "member-1", "member", "active", now, nil, nil, nil, now, now))
	pool.ExpectCommit()

	queued, err := svc.OffboardExcessMembers(context.Background(), "hh-1", anchor)
	require.NoError(t, err)
	assert.Empty(t, queued, "2 active members on the comp tier's 2-seat limit is not excess")
	require.NoError(t, pool.ExpectationsWereMet(), "the capacity machine must run (ListMembers reached) for a comped owner, not short-circuit on raw status")
	userRepo.AssertExpectations(t)
}

// TestHouseholdService_OffboardExcessMembers_CompedOwnerDownToSmallerTier_RemovesExcessMember
// documents the consequence #1413's fix unlocks: once the owner-lapse gate stops
// short-circuiting on raw status, a comp that moves the owner DOWN to a smaller tier than
// their real one now reaches the seat-capacity machine for the first time — comping a
// household owner down to a 1-seat tier while they have more active members than that
// starts (and, once its grace period is exhausted, executes) real LIFO removal, exactly as
// it would for a genuine downgrade. This test uses a single excess member for a tractable,
// focused proof of the mechanism (the same mechanism scales to any excess count, e.g. the
// household_4 -> premium, 3-members-removed scenario called out in the issue).
func TestHouseholdService_OffboardExcessMembers_CompedOwnerDownToSmallerTier_RemovesExcessMember(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	owner := &domain.User{
		ID: "owner-1", SubscriptionTier: "household_4", SubscriptionStatus: "inactive", // real tier: 4 seats, real subscription lapsed
		ManualTierOverride: strPtr("premium"), ManualTierEndsAt: &futureEnd, // admin comp DOWN to 1 seat, still active
	}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)
	userRepo.On("GetUserByID", mock.Anything, "member-1").
		Return(&domain.User{ID: "member-1", Email: "member1@example.com"}, nil)
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member1@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), false).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	joinedOwner := time.Now().Add(-72 * time.Hour)
	joined1 := time.Now().Add(-48 * time.Hour) // the household's only non-owner member
	anchor := time.Now().Add(-2 * 24 * time.Hour)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", joinedOwner, joinedOwner, &anchor, nil, nil))
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("owner-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectQuery("status = 'active' ORDER BY joined_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
			"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
		}).
			AddRow("mem-owner", "hh-1", "owner-1", "owner", "active", joinedOwner, nil, nil, nil, joinedOwner, joinedOwner).
			AddRow("mem-1", "hh-1", "member-1", "member", "active", joined1, nil, nil, nil, joined1, joined1))
	// Excess = 2 active members - 1 comp seat (premium, NOT the owner's real household_4
	// 4-seat tier) = 1 -> the only non-owner member is removed.
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// removeMemberTx's own grace-clear branch: post-removal activeCount (1, just the
	// owner) is back within the comp tier's 1-seat limit, so this in-flight removal
	// clears over-capacity itself (mirrors RemovesMostRecentlyJoinedFirst's identical tail).
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("owner-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	pool.ExpectExec("over_capacity_since = NULL, over_capacity_start_notice_sent_at = NULL").WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	queued, err := svc.OffboardExcessMembers(context.Background(), "hh-1", anchor)
	require.NoError(t, err)
	require.Len(t, queued, 1, "the owner's COMP tier (1 seat) — not their real household_4 tier (4 seats) — must drive the excess computation")
	assert.Equal(t, "member1@example.com", queued[0].ToEmail)
	require.NoError(t, pool.ExpectationsWereMet())
	userRepo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)
}

// TestHouseholdService_OffboardExcessMembers_LocksOwnerBeforeTierAndRemoval is a dedicated
// lock-ordering regression test for the Round 9 Gap Analysis fix (6th round-9 fix, a later
// Codex adversarial pass than the anchor-mismatch check above): the owner's users row must
// be locked (lockAndGetUser's `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`) BEFORE this
// method computes the owner's tier/seat limit and BEFORE it issues any ListMembers/removal
// query — mirroring PurgeRemovedMembership/OffboardExcessMembers's existing "household-row
// lock always taken first" convention, extended here to the owner's row specifically.
// pgxmock enforces expectations in declaration order by default (see
// TestHouseholdService_PurgeRemovedMembership_LocksHouseholdBeforeDelete's identical
// rationale), so declaring the owner-lock ExpectExec BEFORE the ListMembers ExpectQuery and
// the removal ExpectExecs below proves the lock genuinely precedes them — if the
// implementation regressed to a plain, unlocked userRepo.GetUserByID (dropping the FOR
// UPDATE statement entirely) or reordered the lock to AFTER the tier/removal logic, pgxmock
// would see an unexpected query and fail with a clear ordering mismatch, not a false pass.
// This is the pgxmock-level complement to the real-Postgres concurrency proof in
// household_service_integration_test.go
// (TestHouseholdServiceIntegration_OffboardExcessMembers_OwnerLockedDuringRemoval_ConcurrentTierUpgradeWaits)
// — per this codebase's established rule (memory feedback_pgxmock_integration_coverage.md),
// pgxmock alone can prove query SHAPE/ORDER but never a genuine row-lock/concurrency
// guarantee, so this test is deliberately NOT a substitute for that real-DB proof.
func TestHouseholdService_OffboardExcessMembers_LocksOwnerBeforeTierAndRemoval(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"} // 2 seats
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)
	userRepo.On("GetUserByID", mock.Anything, "member-2").
		Return(&domain.User{ID: "member-2", Email: "member2@example.com"}, nil)
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member2@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), false).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	joinedOwner := time.Now().Add(-72 * time.Hour)
	joined1 := time.Now().Add(-48 * time.Hour)
	joined2 := time.Now().Add(-24 * time.Hour)
	anchor := time.Now().Add(-2 * 24 * time.Hour)

	pool.ExpectBegin()
	// 1. Household-row lock (LockHouseholdByID) — always taken first.
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", joinedOwner, joinedOwner, &anchor, nil, nil))
	// 2. Owner-row lock (lockAndGetUser) — must precede BOTH the ListMembers query (3) and
	// the removal mutations (4) below. This is the query whose declaration position this
	// test exists to pin down.
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("owner-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	// 3. ListMembers — the seat/excess computation the (now lock-protected) owner tier
	// feeds into.
	pool.ExpectQuery("status = 'active' ORDER BY joined_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
			"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
		}).
			AddRow("mem-owner", "hh-1", "owner-1", "owner", "active", joinedOwner, nil, nil, nil, joinedOwner, joinedOwner).
			AddRow("mem-1", "hh-1", "member-1", "member", "active", joined1, nil, nil, nil, joined1, joined1).
			AddRow("mem-2", "hh-1", "member-2", "member", "active", joined2, nil, nil, nil, joined2, joined2))
	// 4. Removal mutations — must never be reachable before the owner lock (2) above.
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// 5. #1389: removeMemberTx's own grace-clear branch re-locks the owner (lockAndGetUser)
	// before its own CountActiveMembers/ClearOverCapacity decision — reentrant on the same
	// tx as the owner lock from (2) above, harmless, accepted as a small redundant cost so
	// removeMemberTx is self-protecting for every caller (#1389).
	pool.ExpectExec("SELECT 1 FROM users").WithArgs("owner-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(2))
	pool.ExpectExec("over_capacity_since = NULL, over_capacity_start_notice_sent_at = NULL").WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	queued, err := svc.OffboardExcessMembers(context.Background(), "hh-1", anchor)
	require.NoError(t, err)
	require.Len(t, queued, 1)
	require.NoError(t, pool.ExpectationsWereMet(),
		"owner-row lock must be acquired before any ListMembers/removal query — see the numbered comments above")
	userRepo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)
}

// TestHouseholdService_OffboardExcessMembers_AnchorMismatchSinceCallerDecided_NoRemovals
// proves the Round 9 Gap Analysis fix: the final Codex adversarial review found that even
// after household_over_capacity_check.go's processHousehold re-reads the fresh
// over_capacity_since immediately before deciding the grace deadline has passed, a
// SEPARATE race window still exists between that decision and THIS method's own
// LockHouseholdByID call a few lines later — a concurrent clear-then-re-flag cycle (e.g.
// a subscription webhook) can land in that gap and set a genuinely FRESH
// over_capacity_since under this method's lock, which the pre-fix implementation
// completely ignored, proceeding with LIFO removal anyway based on the caller's
// now-superseded deadline decision. OffboardExcessMembers now takes the caller's
// expectedOverCapacitySince and, immediately after taking its own lock, verifies the
// CURRENT locked row's OverCapacitySince still matches it — treating a mismatch as a
// legitimate no-op, but NOT a silent one: it returns domain.ErrOverCapacityAnchorMismatch
// so the caller (autoOffboard) can distinguish "no-op, safe to clear the flag" from
// "no-op, DO NOT touch the flag — a fresh grace period is already running" (Round 9 Gap
// Analysis fix, third Codex adversarial pass — see domain.ErrOverCapacityAnchorMismatch's
// doc comment). This test seeds the locked household row with a DIFFERENT (fresher)
// over_capacity_since than what the caller passes as its expected anchor, and asserts NO
// ListMembers query and NO removal mutation ever happen — proving the real member-removal
// action never executes despite 4 active members on a 2-seat plan (which would otherwise
// trigger LIFO removal).
func TestHouseholdService_OffboardExcessMembers_AnchorMismatchSinceCallerDecided_NoRemovals(t *testing.T) {
	t.Parallel()
	// No owner mock registrations: the anchor-mismatch check runs immediately after the
	// lock, BEFORE the owner is ever fetched (mirroring the read-only-owner check's own
	// position), so GetUserByID must never be called in this scenario. An unexpected call
	// here would fail via mockUserLoader's own strict mock.Mock behavior.
	userRepo := &mockUserLoader{}
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()

	// The caller (the daily worker) made its removal decision based on THIS anchor —
	// e.g. read fresh moments ago, deadline already passed.
	callerDecidedAnchor := time.Now().Add(-15 * 24 * time.Hour)
	// But the row actually locked under THIS method's own LockHouseholdByID reflects a
	// concurrent clear-then-re-flag cycle that landed in the race window: a genuinely
	// fresh anchor, nowhere near its own deadline. Passed as a *time.Time (not a bare
	// time.Time) to AddRow — pgxmock/pgx's scan plan for a nullable timestamp column
	// requires either nil or a pointer, matching this file's other nullable-timestamp
	// AddRow calls (see household_repository_test.go's removedAt pattern).
	currentDBAnchor := time.Now().Add(-1 * time.Hour)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), &currentDBAnchor, nil, nil))
	// No ListMembers query, no removal mutations, no owner lookup — the anchor-mismatch
	// check short-circuits before any of that, exactly like the read-only-owner branch
	// above. If the implementation regressed to ignoring the mismatch, pgxmock would see
	// an unexpected ListMembers/UPDATE query here and fail.
	pool.ExpectCommit()

	queued, err := svc.OffboardExcessMembers(context.Background(), "hh-1", callerDecidedAnchor)
	assert.ErrorIs(t, err, domain.ErrOverCapacityAnchorMismatch, "a mismatched over-capacity anchor must surface the sentinel so the caller knows NOT to clear the flag")
	assert.Nil(t, queued, "no removal queue should be returned on an anchor mismatch")
	require.NoError(t, pool.ExpectationsWereMet(), "no ListMembers/removal query should have been issued when the anchor no longer matches")
	userRepo.AssertExpectations(t)
}

// TestHouseholdService_OffboardExcessMembers_AnchorNilMismatch_NoRemovals covers the
// other half of the mismatch check: the household's CURRENT over_capacity_since has been
// cleared entirely (nil) by the time this method's lock is acquired — e.g. the owner
// manually removed enough members via RemoveMember's own grace-clear branch in the race
// window — while the caller still passes the anchor it originally decided on. A nil
// current value must also be treated as "no longer matches", not skip the check via a
// nil-pointer short-circuit, and must ALSO surface domain.ErrOverCapacityAnchorMismatch
// rather than a silent empty-no-error result.
func TestHouseholdService_OffboardExcessMembers_AnchorNilMismatch_NoRemovals(t *testing.T) {
	t.Parallel()
	// No owner mock registrations — see AnchorMismatchSinceCallerDecided_NoRemovals's
	// comment above: GetUserByID must never be called when the anchor check short-
	// circuits first.
	userRepo := &mockUserLoader{}
	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()

	callerDecidedAnchor := time.Now().Add(-15 * 24 * time.Hour)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil))
	pool.ExpectCommit()

	queued, err := svc.OffboardExcessMembers(context.Background(), "hh-1", callerDecidedAnchor)
	assert.ErrorIs(t, err, domain.ErrOverCapacityAnchorMismatch, "a nil current anchor must also surface the sentinel, not a silent empty-no-error result")
	assert.Nil(t, queued, "no removal queue should be returned on an anchor mismatch")
	require.NoError(t, pool.ExpectationsWereMet(), "no ListMembers/removal query should have been issued when the current anchor is nil")
	userRepo.AssertExpectations(t)
}

// ─── PurgeRemovedMembership (#1371 — Phase 7 foundation) ─────────────────────

// householdLockRow returns a pgxmock rows result satisfying LockHouseholdByID's
// SELECT ... FOR UPDATE column shape (id, owner_user_id, name, created_at, updated_at,
// over_capacity_since, over_capacity_start_notice_sent_at, over_capacity_warn3d_sent_at).
func householdLockRow(householdID, ownerUserID string) *pgxmock.Rows {
	now := time.Now()
	return pgxmock.NewRows([]string{
		"id", "owner_user_id", "name", "created_at", "updated_at",
		"over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at",
	}).AddRow(householdID, ownerUserID, "The Smiths", now, now, nil, nil, nil)
}

func TestHouseholdService_PurgeRemovedMembership_Success(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	// Household-row lock must be taken FIRST, per this codebase's lock-ordering
	// convention — pgxmock enforces expectation order, so declaring this query before
	// the DELETE below proves the lock precedes it.
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(householdLockRow("hh-1", "owner-1"))
	pool.ExpectQuery("DELETE FROM household_members").WithArgs("member-row-1").
		WillReturnRows(pgxmock.NewRows([]string{"household_id", "user_id"}).AddRow("hh-1", "member-1"))
	// Not currently re-active in this household -> safe to clean up shares/entitlement.
	pool.ExpectQuery("status = 'active'").WithArgs("hh-1", "member-1").
		WillReturnError(pgx.ErrNoRows)
	// #1569 T7: the purge CLOSES any still-open grant instead of hard-deleting the
	// household's record of what it was shown. An `ExpectExec("DELETE FROM
	// household_account_shares")` here would now go unmatched.
	pool.ExpectExec("UPDATE household_account_shares SET revoked_at").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 2))
	pool.ExpectExec("source = 'household'").WithArgs("member-1", "hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	err := svc.PurgeRemovedMembership(context.Background(), "hh-1", "member-row-1")
	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestHouseholdService_PurgeRemovedMembership_LocksHouseholdBeforeDelete is a dedicated
// lock-ordering regression test: proves the household row is locked (FOR UPDATE)
// BEFORE the removed-member row is deleted, matching OffboardExcessMembers/ForceDisband's
// documented "household-row lock always taken first" convention. pgxmock enforces
// expectations in declaration order by default, so reversing the two ExpectQuery calls
// below would make this test fail if the implementation's ordering regressed.
func TestHouseholdService_PurgeRemovedMembership_LocksHouseholdBeforeDelete(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(householdLockRow("hh-1", "owner-1"))
	pool.ExpectQuery("DELETE FROM household_members").WithArgs("member-row-1").
		WillReturnRows(pgxmock.NewRows([]string{"household_id", "user_id"}).AddRow("hh-1", "member-1"))
	pool.ExpectQuery("status = 'active'").WithArgs("hh-1", "member-1").
		WillReturnError(pgx.ErrNoRows)
	// #1569 T7: the purge CLOSES any still-open grant instead of hard-deleting the
	// household's record of what it was shown. An `ExpectExec("DELETE FROM
	// household_account_shares")` here would now go unmatched.
	pool.ExpectExec("UPDATE household_account_shares SET revoked_at").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 2))
	pool.ExpectExec("source = 'household'").WithArgs("member-1", "hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	err := svc.PurgeRemovedMembership(context.Background(), "hh-1", "member-row-1")
	require.NoError(t, err)
	// If the implementation locked AFTER deleting (or not at all), the queued
	// expectations above would be consumed out of order and pgxmock would report an
	// unmet/mismatched expectation here.
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestHouseholdService_PurgeRemovedMembership_NotFound_RollsBack(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(householdLockRow("hh-1", "owner-1"))
	pool.ExpectQuery("DELETE FROM household_members").WithArgs("member-row-x").
		WillReturnError(pgx.ErrNoRows)
	pool.ExpectRollback()

	err := svc.PurgeRemovedMembership(context.Background(), "hh-1", "member-row-x")
	require.ErrorIs(t, err, domain.ErrMemberNotFound)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestHouseholdService_PurgeRemovedMembership_HouseholdLockNotFound_RollsBack proves
// that if the household row itself can't be locked (e.g. a data-integrity edge case
// where the household no longer exists), the method fails fast on the lock step and
// never reaches the delete — the delete-by-membership-ID call is never made.
func TestHouseholdService_PurgeRemovedMembership_HouseholdLockNotFound_RollsBack(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-missing").
		WillReturnError(pgx.ErrNoRows)
	pool.ExpectRollback()

	err := svc.PurgeRemovedMembership(context.Background(), "hh-missing", "member-row-1")
	require.ErrorIs(t, err, domain.ErrHouseholdNotFound)
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
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(householdLockRow("hh-1", "owner-1"))
	pool.ExpectQuery("DELETE FROM household_members").WithArgs("member-row-old").
		WillReturnRows(pgxmock.NewRows([]string{"household_id", "user_id"}).AddRow("hh-1", "member-1"))
	pool.ExpectQuery("status = 'active'").WithArgs("hh-1", "member-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
			"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
		}).AddRow("mem-new", "hh-1", "member-1", "member", "active", time.Now(), nil, nil, nil, time.Now(), time.Now()))
	// No share write, no entitlement UPDATE — must not run.
	pool.ExpectCommit()

	err := svc.PurgeRemovedMembership(context.Background(), "hh-1", "member-row-old")
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
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "m1@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), true).
		Return(nil)
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "m2@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), true).
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
	// Round 4 Gap Analysis fix: ForceDisband must flag every queued email as disbanded so
	// sendRemovalEmails sends the corrected copy (no future-cleanup reference) rather than
	// the individual-removal copy's "cleaned up after 30 days" language.
	assert.True(t, queued[0].Disbanded)
	assert.Equal(t, "m2@example.com", queued[1].ToEmail)
	assert.Equal(t, "The Smiths", queued[1].HouseholdName)
	assert.WithinDuration(t, time.Now().Add(30*24*time.Hour), queued[1].PurgeDate, time.Minute)
	assert.True(t, queued[1].Disbanded)
	require.NoError(t, pool.ExpectationsWereMet())
	userRepo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)
}

// TestHouseholdService_ForceDisband_SucceedsWithGraceActive closes a coverage gap
// surfaced by a Codex adversarial review during a /requirements-gap-analysis pass on
// #1389: the #1389 fix made removeMemberTx's grace-clear branch (household.
// OverCapacitySince != nil) lock the owner via lockAndGetUser before reading it, and that
// branch is reached by 3 callers (RemoveMember, ForceDisband, OffboardExcessMembers's own
// removal loop) — but only RemoveMember and OffboardExcessMembers had dedicated tests that
// actually exercise the branch (over_capacity_since != nil). ForceDisband's own existing
// test, TestHouseholdService_ForceDisband_RemovesAllActiveMembersThenDissolves above, sets
// over_capacity_since = nil in its household fixture row, so the grace-clear branch never
// runs via the ForceDisband caller path in any test before this one.
//
// This matters because ForceDisband commits ALL its removals plus DeleteHousehold/
// ExpireActiveEntitlement in ONE shared transaction (see removeMemberTx's own doc comment):
// if the grace-clear branch's owner lock, tier lookup, or CountActiveMembers query ever
// errors, that error propagates out of removeMemberTx and aborts the entire ForceDisband
// transaction — a failure mode specific to this caller (RemoveMember/OffboardExcessMembers
// only ever abort their own single-member-removal transaction, never a whole
// disband-in-progress). This test doesn't assert on that failure path (a rollback test
// would just re-prove TestHouseholdService_RemoveMember_GraceClearOwnerLockFails_RollsBackNoEmail's
// contract) — it instead proves the mainline success path: ForceDisband still returns no
// error and the expected 2 queued removal emails when the household IS in grace and the
// branch's owner-lock/tier/count machinery genuinely runs on every loop iteration (household
// is passed unchanged into every removeMemberTx call, per ForceDisband's own loop, so the
// branch fires for member-1 AND member-2, not just once).
//
// Owner is fixed at "household_2" (2 seats) for the whole test, so CountActiveMembers's
// post-removal count (2, then 1) is always <= the seat limit — ClearOverCapacity fires on
// both iterations. That's an intentionally simpler fixture than trying to keep the
// household over capacity across the entire loop (impossible here regardless of tier: the
// household only has 3 members total, so the count necessarily drops to 1 after the second
// removal, which is <= any real tier's seat count) — the seat-count arithmetic itself is
// already covered by the RemoveMember/OffboardExcessMembers tests referenced above; this
// test only needs the branch to execute without erroring.
func TestHouseholdService_ForceDisband_SucceedsWithGraceActive(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").
		Return(&domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"}, nil) // 2 seats
	userRepo.On("GetUserByID", mock.Anything, "member-1").
		Return(&domain.User{ID: "member-1", Email: "m1@example.com"}, nil)
	userRepo.On("GetUserByID", mock.Anything, "member-2").
		Return(&domain.User{ID: "member-2", Email: "m2@example.com"}, nil)
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "m1@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), true).
		Return(nil)
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "m2@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), true).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()
	now := time.Now()
	graceStart := now.Add(-2 * 24 * time.Hour)

	pool.ExpectBegin()
	// Household fixture is in over-capacity grace (over_capacity_since != nil) — this is
	// the row that flows unchanged into removeMemberTx for every loop iteration below.
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, &graceStart, nil, nil, "owner"))
	// LockHouseholdForUpdate — only returns a bare owner-ID string, irrelevant to the grace
	// fixture (see task facts: OverCapacitySince only matters on the row read above).
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
	// --- Loop iteration 1: member-1 ---
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// Grace-clear branch fires (household.OverCapacitySince != nil): owner-lock, then
	// tier/count decision, then ClearOverCapacity since post-removal count (2) <= seats (2).
	pool.ExpectExec("SELECT 1 FROM users.*FOR UPDATE").WithArgs("owner-1").
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(2))
	pool.ExpectExec("over_capacity_since = NULL, over_capacity_start_notice_sent_at = NULL").WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// --- Loop iteration 2: member-2 ---
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// Grace-clear branch fires again — household is passed unchanged into every iteration,
	// so OverCapacitySince is still non-nil here even though the branch already cleared it
	// (in-memory) on iteration 1.
	pool.ExpectExec("SELECT 1 FROM users.*FOR UPDATE").WithArgs("owner-1").
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	pool.ExpectExec("over_capacity_since = NULL, over_capacity_start_notice_sent_at = NULL").WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// --- Dissolve ---
	pool.ExpectExec("DELETE FROM households").WithArgs("hh-1", "owner-1").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("owner-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	queued, err := svc.ForceDisband(context.Background(), "owner-1")
	require.NoError(t, err)
	require.Len(t, queued, 2)
	assert.Equal(t, "m1@example.com", queued[0].ToEmail)
	assert.True(t, queued[0].Disbanded)
	assert.Equal(t, "m2@example.com", queued[1].ToEmail)
	assert.True(t, queued[1].Disbanded)
	require.NoError(t, pool.ExpectationsWereMet(),
		"ForceDisband must succeed end-to-end even when the grace-clear branch's owner-lock/tier/count machinery runs on every removal in the loop")
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

// TestHouseholdService_CreateInvite_OwnerMalformedOverride_FallsBackToRawTier is the RED test
// for Issue #1413 (this task — tier axis) at CreateInvite's seat-capacity check: before this
// fix, a malformed manual_tier_override reached s.tierConfig.GetTier(owner.ComputeEffectiveTier())
// as an unrecognized tier name and errored the invite entirely. It must instead fall back to
// the owner's raw tier.
func TestHouseholdService_CreateInvite_OwnerMalformedOverride_FallsBackToRawTier(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	futureEnd := time.Now().Add(24 * time.Hour)
	owner := &domain.User{
		ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active", // raw tier: 2 seats
		ManualTierOverride: strPtr("not-a-real-tier"), ManualTierEndsAt: &futureEnd,
	}
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
	// 1 active member is within the owner's RAW 2-seat tier (household_2), not the garbage override.
	pool.ExpectQuery("SELECT COUNT.*household_members").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	pool.ExpectQuery("SELECT COUNT.*household_invites").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(0))
	pool.ExpectQuery("INSERT INTO household_invites").
		WithArgs("hh-1", "owner-1", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow("inv-1", now, now))
	pool.ExpectCommit()

	result, err := svc.CreateInvite(context.Background(), "owner-1", "friend@example.com", "https://app.example.com")
	require.NoError(t, err, "a malformed owner override must not error the seat-capacity check")
	require.Equal(t, "inv-1", result.Invite.ID)
	require.NoError(t, pool.ExpectationsWereMet(), "the seat check must fall back to the owner's raw tier")
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
	pool.ExpectQuery("household_members WHERE household_id").WithArgs(householdID, callerUserID, "30 days").
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
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx", "identity_type", "identity_subtype"}).
			AddRow("acct-1", "inst-1", &mask, nil, nil).
			AddRow("acct-2", "inst-2", nil, nil, nil))
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

// TestCallerHasLivePaddleSubscription_CompedUser_NoPaddleBillingRelationship_JoinsFreely
// documents (but does NOT pin the raw-vs-effective distinction of) the nil-PaddleSubscriptionID
// short-circuit in callerHasLivePaddleSubscription (household_service.go:1674-1676): a comped
// user with no Paddle billing relationship at all has no refund/double-charge risk and must join
// freely. Because PaddleSubscriptionID is nil here, the function returns at that check BEFORE
// ever reaching the status switch at :1677 — so this case can never fail on a raw-vs-effective
// switch there, regardless of ManualTierOverride/effective status. The actual #1413 pin for that
// switch is TestCallerHasLivePaddleSubscription_CompedUser_WithPaddleSub_RawCancelled_PinnedRaw
// below, which sets a real PaddleSubscriptionID so the switch is actually exercised.
func TestCallerHasLivePaddleSubscription_CompedUser_NoPaddleBillingRelationship_JoinsFreely(t *testing.T) {
	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	comped := &domain.User{
		ID:                 "user-1",
		SubscriptionStatus: "inactive", // real subscription lapsed
		ManualTierOverride: strPtr("household_4"),
		ManualTierEndsAt:   &futureEnd, // admin comp, still active
		// PaddleSubscriptionID intentionally nil: no Paddle billing relationship at all.
	}
	assert.False(t, callerHasLivePaddleSubscription(comped),
		"a comped user with no PaddleSubscriptionID has no refund/double-charge risk and must join freely, regardless of effective status/tier")
}

// TestCallerHasLivePaddleSubscription_CompedUser_WithPaddleSub_RawCancelled_PinnedRaw is the
// #1413 PinnedRaw pin test for the status switch at household_service.go:1677. Unlike the test
// above, PaddleSubscriptionID is set here so execution actually reaches that switch instead of
// returning early at the nil check. Raw SubscriptionStatus is "cancelled" (the real Paddle
// subscription is genuinely terminated — no refund/double-charge risk), while a LIVE manual
// override is also set, so EFFECTIVE status would read "active". If callerHasLivePaddleSubscription
// were switched from raw to effective status, this would flip from false to true — incorrectly
// blocking a user whose real billing relationship poses no risk, contradicting the function's own
// doc comment ("cancelled"/"inactive"... and manual-tier-override comps ... all join freely).
func TestCallerHasLivePaddleSubscription_CompedUser_WithPaddleSub_RawCancelled_PinnedRaw(t *testing.T) {
	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	paddleSubID := "sub_01abc123"
	comped := &domain.User{
		ID:                   "user-1",
		SubscriptionStatus:   "cancelled",  // raw: the real Paddle subscription is genuinely terminated
		PaddleSubscriptionID: &paddleSubID, // real Paddle billing relationship -> reaches the status switch
		ManualTierOverride:   strPtr("household_4"),
		ManualTierEndsAt:     &futureEnd, // admin comp, still active -> effective status would read "active"
	}
	assert.False(t, callerHasLivePaddleSubscription(comped),
		`raw status "cancelled" has no refund/double-charge risk even though the comp override would make effective status read "active"`)
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
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx", "identity_type", "identity_subtype"}).AddRow("acct-1", "inst-1", nil, nil, nil))
	pool.ExpectRollback()

	_, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, []string{"someone-elses-account"})
	require.ErrorIs(t, err, domain.ErrInvalidConsent)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestHouseholdService_AcceptInvite_CompedOwnerRawInactive_SucceedsAndPersistsEffectiveStatus
// proves the #1413 fix at two sites in the same flow: (1) the owner-lapse gate
// (`owner.DeletionRequestedAt != nil || ...IsReadOnlySubscription()`) must read the OWNER's
// EFFECTIVE status, not raw — a comped owner whose real subscription lapsed (raw
// "inactive") must NOT be treated as ErrOwnerPlanInactive; and (2)
// CreateHouseholdEntitlement must persist the owner's EFFECTIVE status ("active") onto the
// entitlement row, not the raw "inactive" — otherwise the stored row would be
// self-inconsistent with the comp tier stored alongside it. Mirrors
// TestHouseholdService_AcceptInvite_Success's flow exactly, with the owner comped instead
// of genuinely active.
func TestHouseholdService_AcceptInvite_CompedOwnerRawInactive_SucceedsAndPersistsEffectiveStatus(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	caller := &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: true, SubscriptionTier: "premium", SubscriptionStatus: "cancelled"}
	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	owner := &domain.User{
		ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "inactive", // real subscription lapsed
		ManualTierOverride: strPtr("household_4"), ManualTierEndsAt: &futureEnd, // admin comp, still active
	}
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
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx", "identity_type", "identity_subtype"}).
			AddRow("acct-1", "inst-1", &mask, nil, nil).
			AddRow("acct-2", "inst-2", nil, nil, nil))
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
	// The critical assertion: "active" (owner's EFFECTIVE status), never "inactive" (raw).
	pool.ExpectExec("INSERT INTO subscription_entitlements").
		WithArgs("caller-1", "household_4", "active", "owner-1", "hh-1").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	pool.ExpectExec("UPDATE household_invites SET status = 'accepted'").WithArgs("caller-1", "inv-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	result, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, nil)
	require.NoError(t, err, "a comped owner's raw-inactive status must not block AcceptInvite with ErrOwnerPlanInactive")
	require.Equal(t, "hh-1", result.Household.ID)
	require.NoError(t, pool.ExpectationsWereMet(), "the persisted entitlement status must be the owner's EFFECTIVE status")
}

// TestHouseholdService_AcceptInvite_OwnerActiveRawStatus_MalformedOverride_SeatCheckFallsBackToRawTier
// is the RED test for Issue #1413 (this task — tier axis) at AcceptInvite's seat-capacity check.
// The owner's RAW status is deliberately "active" here (never forced there by the override — it
// was already active), so the owner-lapse gate above is a no-op and this isolates the TIER axis
// leak distinctly from the status axis: before this fix, a malformed manual_tier_override
// reached s.tierConfig.GetTier(owner.ComputeEffectiveTier()) as an unrecognized tier name and
// errored the whole AcceptInvite call — even for an owner who is actively paying.
func TestHouseholdService_AcceptInvite_OwnerActiveRawStatus_MalformedOverride_SeatCheckFallsBackToRawTier(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	caller := &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: true, SubscriptionTier: "premium", SubscriptionStatus: "cancelled"}
	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	owner := &domain.User{
		ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active", // raw status genuinely active
		ManualTierOverride: strPtr("not-a-real-tier"), ManualTierEndsAt: &futureEnd,
	}
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
	expectLockCallerRow(pool, "owner-1")
	// household_2 (owner's RAW tier, 2 seats) — not the garbage override — must drive the check.
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	expectNoRemovedMember(pool, "hh-1", "caller-1")
	pool.ExpectQuery("INSERT INTO household_members").
		WithArgs("hh-1", "caller-1", domain.HouseholdRoleMember, domain.HouseholdMemberStatusActive, pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id", "joined_at", "created_at", "updated_at"}).AddRow("mem-1", time.Now(), time.Now(), time.Now()))
	pool.ExpectQuery("user_accounts").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx", "identity_type", "identity_subtype"}).
			AddRow("acct-1", "inst-1", &mask, nil, nil).
			AddRow("acct-2", "inst-2", nil, nil, nil))
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
	require.NoError(t, err, "a malformed owner override must not error the seat-capacity check")
	require.Equal(t, "hh-1", result.Household.ID)
	require.NoError(t, pool.ExpectationsWereMet(), "the seat check and persisted tier must use the owner's raw tier, not the garbage override")
}

// TestHouseholdService_AcceptInvite_CallerMalformedOverride_PriorTierFallsBackToRaw is the RED
// test for Issue #1413 (this task — tier axis): the fresh-insert branch used to write
// caller.ComputeEffectiveTier() straight into household_members.prior_subscription_tier — a
// malformed manual_tier_override would propagate the garbage string into a SECOND table, later
// read back to revert the member's tier when they leave the household. It must instead fall back
// to the caller's raw tier.
func TestHouseholdService_AcceptInvite_CallerMalformedOverride_PriorTierFallsBackToRaw(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	caller := &domain.User{
		ID: "caller-1", Email: "friend@example.com", EmailVerified: true,
		SubscriptionTier: "premium", SubscriptionStatus: "cancelled",
		ManualTierOverride: strPtr("not-a-real-tier"), ManualTierEndsAt: &futureEnd,
	}
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
	expectLockCallerRow(pool, "owner-1")
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	expectNoRemovedMember(pool, "hh-1", "caller-1")
	// The critical assertion: prior_subscription_tier (5th INSERT param) must be the caller's
	// RAW tier "premium", never the garbage override string.
	pool.ExpectQuery("INSERT INTO household_members").
		WithArgs("hh-1", "caller-1", domain.HouseholdRoleMember, domain.HouseholdMemberStatusActive, ptrStringEquals("premium"), ptrStringEquals("cancelled")).
		WillReturnRows(pgxmock.NewRows([]string{"id", "joined_at", "created_at", "updated_at"}).AddRow("mem-1", time.Now(), time.Now(), time.Now()))
	pool.ExpectQuery("user_accounts").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx", "identity_type", "identity_subtype"}).
			AddRow("acct-1", "inst-1", &mask, nil, nil).
			AddRow("acct-2", "inst-2", nil, nil, nil))
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
	require.NoError(t, pool.ExpectationsWereMet(), "prior_subscription_tier must fall back to the caller's raw tier, not the garbage override")
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
	pool.ExpectQuery("household_members WHERE household_id").WithArgs("hh-1", "caller-1", "30 days").
		WillReturnRows(removedMemberRow("mem-1", "hh-1", "caller-1", removedAt))
	freshUpdatedAt := time.Now()
	pool.ExpectQuery("UPDATE household_members SET status = 'active'").WithArgs("mem-1").
		WillReturnRows(pgxmock.NewRows([]string{"updated_at"}).AddRow(freshUpdatedAt))
	pool.ExpectQuery("user_accounts").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx", "identity_type", "identity_subtype"}).AddRow("acct-1", "inst-1", nil, nil, nil))
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
	require.True(t, result.Member.UpdatedAt.Equal(freshUpdatedAt),
		"UpdatedAt must be the freshly-written value RestoreMember RETURNs, not the stale pre-restore removedMemberRow value (#1371 Task 4 Codex finding)")
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
	pool.ExpectQuery("household_members WHERE household_id").WithArgs("hh-1", "caller-1", "30 days").
		WillReturnRows(removedMemberRow("mem-1", "hh-1", "caller-1", removedAt))
	pool.ExpectQuery("UPDATE household_members SET status = 'active'").WithArgs("mem-1").
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
