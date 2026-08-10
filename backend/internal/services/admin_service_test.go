package services

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockClerkUserGetter is a mock implementation for fetching Clerk users.
type MockClerkUserGetter struct {
	mock.Mock
}

func (m *MockClerkUserGetter) GetUser(ctx context.Context, clerkUserID string) (*clerk.User, error) {
	args := m.Called(ctx, clerkUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*clerk.User), args.Error(1)
}

// Helper to create a mock Clerk user with privateMetadata
func createMockClerkUser(clerkUserID, email string, roles []string) *clerk.User {
	metadata := domain.AdminPrivateMetadata{Roles: roles}
	metadataJSON, _ := json.Marshal(metadata)
	return &clerk.User{
		ID: clerkUserID,
		EmailAddresses: []*clerk.EmailAddress{
			{EmailAddress: email},
		},
		PrivateMetadata: metadataJSON,
	}
}

// TestAdminService_IsHardcodedAdmin_MatchesEmail tests that hardcoded emails are recognized
func TestAdminService_IsHardcodedAdmin_MatchesEmail(t *testing.T) {
	// GIVEN: An admin service with hardcoded admin emails
	adminEmails := []string{"admin@example.com", "ceo@company.com"}
	mockClerk := new(MockClerkUserGetter)
	svc := NewAdminServiceWithUserGetter("test-secret", adminEmails, mockClerk)

	// WHEN: Checking if a hardcoded email is an admin
	result := svc.IsHardcodedAdmin("admin@example.com")

	// THEN: Should return true
	assert.True(t, result, "Hardcoded admin email should be recognized")
}

// TestAdminService_IsHardcodedAdmin_CaseInsensitive tests case-insensitive email matching
func TestAdminService_IsHardcodedAdmin_CaseInsensitive(t *testing.T) {
	// GIVEN: An admin service with hardcoded admin emails
	adminEmails := []string{"admin@example.com"}
	mockClerk := new(MockClerkUserGetter)
	svc := NewAdminServiceWithUserGetter("test-secret", adminEmails, mockClerk)

	// WHEN: Checking with different case variations
	testCases := []struct {
		email    string
		expected bool
	}{
		{"ADMIN@EXAMPLE.COM", true},
		{"Admin@Example.Com", true},
		{"admin@example.com", true},
		{"other@example.com", false},
	}

	for _, tc := range testCases {
		result := svc.IsHardcodedAdmin(tc.email)
		assert.Equal(t, tc.expected, result, "Email %s should match: %v", tc.email, tc.expected)
	}
}

// TestAdminService_IsHardcodedAdmin_NotInList tests that non-admin emails return false
func TestAdminService_IsHardcodedAdmin_NotInList(t *testing.T) {
	// GIVEN: An admin service with hardcoded admin emails
	adminEmails := []string{"admin@example.com"}
	mockClerk := new(MockClerkUserGetter)
	svc := NewAdminServiceWithUserGetter("test-secret", adminEmails, mockClerk)

	// WHEN: Checking a non-admin email
	result := svc.IsHardcodedAdmin("user@example.com")

	// THEN: Should return false
	assert.False(t, result, "Non-admin email should not be recognized")
}

// TestAdminService_HasRole_AdminRole_ReturnsTrue tests Clerk privateMetadata role check
func TestAdminService_HasRole_AdminRole_ReturnsTrue(t *testing.T) {
	// GIVEN: A user with admin role in Clerk privateMetadata
	mockClerk := new(MockClerkUserGetter)
	svc := NewAdminServiceWithUserGetter("test-secret", []string{}, mockClerk)

	clerkUserID := "user_123"
	mockUser := createMockClerkUser(clerkUserID, "user@example.com", []string{"admin"})
	mockClerk.On("GetUser", mock.Anything, clerkUserID).Return(mockUser, nil)

	// WHEN: Checking if user has admin role
	hasRole, err := svc.HasRole(context.Background(), clerkUserID, domain.RoleAdmin)

	// THEN: Should return true
	assert.NoError(t, err)
	assert.True(t, hasRole, "User with admin role should be recognized")
	mockClerk.AssertExpectations(t)
}

// TestAdminService_HasRole_NoAdminRole_ReturnsFalse tests user without admin role
func TestAdminService_HasRole_NoAdminRole_ReturnsFalse(t *testing.T) {
	// GIVEN: A user without admin role in Clerk privateMetadata
	mockClerk := new(MockClerkUserGetter)
	svc := NewAdminServiceWithUserGetter("test-secret", []string{}, mockClerk)

	clerkUserID := "user_456"
	mockUser := createMockClerkUser(clerkUserID, "user@example.com", []string{"support"})
	mockClerk.On("GetUser", mock.Anything, clerkUserID).Return(mockUser, nil)

	// WHEN: Checking if user has admin role
	hasRole, err := svc.HasRole(context.Background(), clerkUserID, domain.RoleAdmin)

	// THEN: Should return false
	assert.NoError(t, err)
	assert.False(t, hasRole, "User without admin role should not be recognized")
	mockClerk.AssertExpectations(t)
}

// TestAdminService_HasRole_EmptyRoles_ReturnsFalse tests user with empty roles array
func TestAdminService_HasRole_EmptyRoles_ReturnsFalse(t *testing.T) {
	// GIVEN: A user with empty roles array
	mockClerk := new(MockClerkUserGetter)
	svc := NewAdminServiceWithUserGetter("test-secret", []string{}, mockClerk)

	clerkUserID := "user_789"
	mockUser := createMockClerkUser(clerkUserID, "user@example.com", []string{})
	mockClerk.On("GetUser", mock.Anything, clerkUserID).Return(mockUser, nil)

	// WHEN: Checking if user has admin role
	hasRole, err := svc.HasRole(context.Background(), clerkUserID, domain.RoleAdmin)

	// THEN: Should return false
	assert.NoError(t, err)
	assert.False(t, hasRole, "User with empty roles should not be recognized")
	mockClerk.AssertExpectations(t)
}

// TestAdminService_HasRole_NilMetadata_ReturnsFalse tests user with nil privateMetadata
func TestAdminService_HasRole_NilMetadata_ReturnsFalse(t *testing.T) {
	// GIVEN: A user with nil privateMetadata
	mockClerk := new(MockClerkUserGetter)
	svc := NewAdminServiceWithUserGetter("test-secret", []string{}, mockClerk)

	clerkUserID := "user_nil"
	mockUser := &clerk.User{
		ID:              clerkUserID,
		PrivateMetadata: nil,
	}
	mockClerk.On("GetUser", mock.Anything, clerkUserID).Return(mockUser, nil)

	// WHEN: Checking if user has admin role
	hasRole, err := svc.HasRole(context.Background(), clerkUserID, domain.RoleAdmin)

	// THEN: Should return false
	assert.NoError(t, err)
	assert.False(t, hasRole, "User with nil metadata should not be recognized as admin")
	mockClerk.AssertExpectations(t)
}

// TestAdminService_HasRole_CacheHit_SkipsAPI tests that cached results are used
func TestAdminService_HasRole_CacheHit_SkipsAPI(t *testing.T) {
	// GIVEN: A user whose role is already cached
	mockClerk := new(MockClerkUserGetter)
	svc := NewAdminServiceWithUserGetter("test-secret", []string{}, mockClerk)

	clerkUserID := "user_cached"
	mockUser := createMockClerkUser(clerkUserID, "user@example.com", []string{"admin"})
	// Should only be called once
	mockClerk.On("GetUser", mock.Anything, clerkUserID).Return(mockUser, nil).Once()

	// WHEN: Checking role twice
	ctx := context.Background()
	result1, err1 := svc.HasRole(ctx, clerkUserID, domain.RoleAdmin)
	result2, err2 := svc.HasRole(ctx, clerkUserID, domain.RoleAdmin)

	// THEN: API should only be called once, both results should be true
	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.True(t, result1)
	assert.True(t, result2)
	mockClerk.AssertNumberOfCalls(t, "GetUser", 1)
}

// TestAdminService_HasRole_CacheExpired_CallsAPI tests that expired cache triggers API call
func TestAdminService_HasRole_CacheExpired_CallsAPI(t *testing.T) {
	// GIVEN: An admin service with a very short TTL for testing
	mockClerk := new(MockClerkUserGetter)
	svc := NewAdminServiceWithUserGetterAndTTL("test-secret", []string{}, mockClerk, 10*time.Millisecond)

	clerkUserID := "user_expire"
	mockUser := createMockClerkUser(clerkUserID, "user@example.com", []string{"admin"})
	// Should be called twice (initial + after expiry)
	mockClerk.On("GetUser", mock.Anything, clerkUserID).Return(mockUser, nil)

	// WHEN: Checking role, waiting for expiry, then checking again
	ctx := context.Background()
	result1, err1 := svc.HasRole(ctx, clerkUserID, domain.RoleAdmin)
	assert.NoError(t, err1)
	assert.True(t, result1)

	// Wait for cache to expire
	time.Sleep(20 * time.Millisecond)

	result2, err2 := svc.HasRole(ctx, clerkUserID, domain.RoleAdmin)

	// THEN: API should be called twice
	assert.NoError(t, err2)
	assert.True(t, result2)
	mockClerk.AssertNumberOfCalls(t, "GetUser", 2)
}

// TestAdminService_InvalidateCache_RemovesCachedEntry tests cache invalidation
func TestAdminService_InvalidateCache_RemovesCachedEntry(t *testing.T) {
	// GIVEN: A cached user role
	mockClerk := new(MockClerkUserGetter)
	svc := NewAdminServiceWithUserGetter("test-secret", []string{}, mockClerk)

	clerkUserID := "user_invalidate"
	mockUser := createMockClerkUser(clerkUserID, "user@example.com", []string{"admin"})
	mockClerk.On("GetUser", mock.Anything, clerkUserID).Return(mockUser, nil)

	// First call to populate cache
	ctx := context.Background()
	_, _ = svc.HasRole(ctx, clerkUserID, domain.RoleAdmin)
	mockClerk.AssertNumberOfCalls(t, "GetUser", 1)

	// WHEN: Invalidating cache and checking again
	svc.InvalidateCache(clerkUserID)
	_, _ = svc.HasRole(ctx, clerkUserID, domain.RoleAdmin)

	// THEN: API should be called again
	mockClerk.AssertNumberOfCalls(t, "GetUser", 2)
}

// TestAdminService_HasRole_UserNotFound_ReturnsError tests handling of user not found
func TestAdminService_HasRole_UserNotFound_ReturnsError(t *testing.T) {
	// GIVEN: A Clerk API that returns user not found
	mockClerk := new(MockClerkUserGetter)
	svc := NewAdminServiceWithUserGetter("test-secret", []string{}, mockClerk)

	clerkUserID := "user_not_found"
	mockClerk.On("GetUser", mock.Anything, clerkUserID).Return(nil, ErrClerkUserNotFound)

	// WHEN: Checking if user has admin role
	hasRole, err := svc.HasRole(context.Background(), clerkUserID, domain.RoleAdmin)

	// THEN: Should return error and false
	assert.Error(t, err)
	assert.False(t, hasRole)
	assert.ErrorIs(t, err, ErrClerkUserNotFound)
	mockClerk.AssertExpectations(t)
}

// TestAdminService_HasRole_APIError_ReturnsError tests handling of Clerk API errors
func TestAdminService_HasRole_APIError_ReturnsError(t *testing.T) {
	// GIVEN: A Clerk API that returns an error
	mockClerk := new(MockClerkUserGetter)
	svc := NewAdminServiceWithUserGetter("test-secret", []string{}, mockClerk)

	clerkUserID := "user_api_error"
	mockClerk.On("GetUser", mock.Anything, clerkUserID).Return(nil, ErrClerkAPIError)

	// WHEN: Checking if user has admin role
	hasRole, err := svc.HasRole(context.Background(), clerkUserID, domain.RoleAdmin)

	// THEN: Should return error and false
	assert.Error(t, err)
	assert.False(t, hasRole)
	mockClerk.AssertExpectations(t)
}

// TestAdminService_GetRoles_ReturnsAllRoles tests GetRoles returns all user roles
func TestAdminService_GetRoles_ReturnsAllRoles(t *testing.T) {
	// GIVEN: A user with multiple roles
	mockClerk := new(MockClerkUserGetter)
	svc := NewAdminServiceWithUserGetter("test-secret", []string{}, mockClerk)

	clerkUserID := "user_multi_role"
	mockUser := createMockClerkUser(clerkUserID, "user@example.com", []string{"admin", "support", "viewer"})
	mockClerk.On("GetUser", mock.Anything, clerkUserID).Return(mockUser, nil)

	// WHEN: Getting all roles for user
	roles, err := svc.GetRoles(context.Background(), clerkUserID)

	// THEN: Should return all roles
	assert.NoError(t, err)
	assert.Len(t, roles, 3)
	assert.Contains(t, roles, domain.AdminRole("admin"))
	assert.Contains(t, roles, domain.AdminRole("support"))
	assert.Contains(t, roles, domain.AdminRole("viewer"))
	mockClerk.AssertExpectations(t)
}

// TestAdminService_ConcurrentAccess_ThreadSafe tests thread safety of cache
func TestAdminService_ConcurrentAccess_ThreadSafe(t *testing.T) {
	// GIVEN: An admin service with multiple concurrent callers
	mockClerk := new(MockClerkUserGetter)
	svc := NewAdminServiceWithUserGetter("test-secret", []string{}, mockClerk)

	clerkUserID := "user_concurrent"
	mockUser := createMockClerkUser(clerkUserID, "user@example.com", []string{"admin"})
	mockClerk.On("GetUser", mock.Anything, clerkUserID).Return(mockUser, nil)

	// WHEN: Making concurrent calls
	var wg sync.WaitGroup
	numGoroutines := 100
	results := make(chan bool, numGoroutines)
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hasRole, err := svc.HasRole(context.Background(), clerkUserID, domain.RoleAdmin)
			if err != nil {
				errors <- err
			} else {
				results <- hasRole
			}
		}()
	}

	wg.Wait()
	close(results)
	close(errors)

	// THEN: All results should be true, no errors
	for err := range errors {
		t.Errorf("Unexpected error: %v", err)
	}
	for result := range results {
		assert.True(t, result)
	}
}

// TestNewAdminService_ReturnsNonNilService verifies that NewAdminService returns a valid service
// without calling any external APIs. It uses a fake Clerk key to avoid a real API call.
func TestNewAdminService_ReturnsNonNilService(t *testing.T) {
	// NewAdminService calls clerk.SetKey internally — that's a no-op for key format validation.
	// It does NOT call the Clerk API; it just initializes the service struct.
	svc := NewAdminService("sk_test_fakekeyfortest", []string{"admin@example.com"})

	require.NotNil(t, svc, "NewAdminService must return a non-nil service")
	// Verify hardcoded admin emails are respected
	assert.True(t, svc.IsHardcodedAdmin("admin@example.com"), "hardcoded admin must be recognized")
	assert.False(t, svc.IsHardcodedAdmin("other@example.com"), "non-admin must not be recognized")
}

// Issue #1432 (batch ED, finding B2-3): revoking a Clerk admin role used to leave the revoked
// person in authority for up to the FULL cache TTL — five minutes — because GetRoles served a
// positive (role-granting) result straight out of the cache without contacting Clerk.
//
// Two reviewers disagreed on the severity: a Claude reviewer graded it Low ("self-correcting",
// the window closes on its own); Codex graded it High. Codex's reasoning governs and is
// recorded here so the next reader does not re-derive it: the window matters PRECISELY because
// you revoke when you have stopped trusting the person, and irreversible admin operations are
// reachable inside it — the dead-letter bulk delete (no undo outside a Postgres PITR) and
// subscription tier overrides among them.
//
// The fix caps how long a POSITIVE result is served from cache (positiveRoleCacheTTLCap),
// leaving the negative/empty case on the full TTL. Choosing to shorten rather than to wire
// InvalidateCache is not a shortcut: there is no event to wire it TO. Roles live in Clerk
// privateMetadata, this app has no Clerk webhook endpoint (the only webhook routes are
// /webhooks/paddle and /webhooks/plaid), and nothing in the codebase writes privateMetadata —
// clerk_service.go's UpdateUserMetadata writes PUBLIC metadata (subscription tier). A role is
// granted or revoked in the Clerk dashboard, out of band, so InvalidateCache has no caller it
// could acquire without first building a Clerk webhook receiver, which is a separate feature.

// fakeClockAdminService builds an adminService whose clock the test controls, so the
// five-minute and thirty-second windows can be crossed without sleeping.
func fakeClockAdminService(t *testing.T, getter ClerkUserGetter, ttl time.Duration) (*adminService, func(time.Duration)) {
	t.Helper()

	svc := newAdminServiceInternal([]string{}, getter, ttl)
	now := time.Now()
	svc.now = func() time.Time { return now }
	return svc, func(d time.Duration) { now = now.Add(d) }
}

// TestAdminService_HasRole_PositiveResultExpiresWellBeforeTheFullTTL is the RED for B2-3.
func TestAdminService_HasRole_PositiveResultExpiresWellBeforeTheFullTTL(t *testing.T) {
	mockClerk := new(MockClerkUserGetter)
	svc, advance := fakeClockAdminService(t, mockClerk, defaultRoleCacheTTL)

	clerkUserID := "user_still_admin"
	mockClerk.On("GetUser", mock.Anything, clerkUserID).Return(
		createMockClerkUser(clerkUserID, "admin@example.com", []string{"admin"}), nil)

	ctx := context.Background()
	hasRole, err := svc.HasRole(ctx, clerkUserID, domain.RoleAdmin)
	require.NoError(t, err)
	require.True(t, hasRole)
	mockClerk.AssertNumberOfCalls(t, "GetUser", 1)

	// Control: inside the cap the cache is still doing its job — this is what makes the
	// assertion below evidence of a SHORTER window rather than of caching being broken.
	advance(positiveRoleCacheTTLCap / 2)
	_, err = svc.HasRole(ctx, clerkUserID, domain.RoleAdmin)
	require.NoError(t, err)
	mockClerk.AssertNumberOfCalls(t, "GetUser", 1)

	// Past the cap but FAR short of the full five-minute TTL: Clerk must be consulted again,
	// so a role revoked in the Clerk dashboard takes effect here.
	advance(positiveRoleCacheTTLCap)
	hasRole, err = svc.HasRole(ctx, clerkUserID, domain.RoleAdmin)
	require.NoError(t, err)
	assert.True(t, hasRole, "a legitimately-current admin must still pass after the cache re-checks")
	mockClerk.AssertNumberOfCalls(t, "GetUser", 2)

	require.Less(t, positiveRoleCacheTTLCap, defaultRoleCacheTTL,
		"the positive-result cap must be strictly shorter than the general TTL, or it caps nothing")
}

// TestAdminService_HasRole_RevokedRoleStopsWorkingAfterTheCap binds the fix to the harm it is
// about: the Clerk answer CHANGES, and authority must follow it.
func TestAdminService_HasRole_RevokedRoleStopsWorkingAfterTheCap(t *testing.T) {
	mockClerk := new(MockClerkUserGetter)
	svc, advance := fakeClockAdminService(t, mockClerk, defaultRoleCacheTTL)

	clerkUserID := "user_revoked"
	mockClerk.On("GetUser", mock.Anything, clerkUserID).Return(
		createMockClerkUser(clerkUserID, "ex-admin@example.com", []string{"admin"}), nil).Once()
	mockClerk.On("GetUser", mock.Anything, clerkUserID).Return(
		createMockClerkUser(clerkUserID, "ex-admin@example.com", []string{}), nil)

	ctx := context.Background()
	hasRole, err := svc.HasRole(ctx, clerkUserID, domain.RoleAdmin)
	require.NoError(t, err)
	require.True(t, hasRole)

	advance(positiveRoleCacheTTLCap + time.Second)

	hasRole, err = svc.HasRole(ctx, clerkUserID, domain.RoleAdmin)
	require.NoError(t, err)
	assert.False(t, hasRole, "a role revoked in Clerk must stop granting admin authority once the positive cache cap elapses")
}

// TestAdminService_HasRole_NegativeResultKeepsTheFullTTL is the sibling that stops the fix from
// degenerating into "disable the cache". The overwhelmingly common case — an ordinary user with
// no roles — is what the cache exists to absorb, and caching it longer costs nothing:
// a newly-GRANTED role arriving late is an availability inconvenience, not an authority leak.
func TestAdminService_HasRole_NegativeResultKeepsTheFullTTL(t *testing.T) {
	mockClerk := new(MockClerkUserGetter)
	svc, advance := fakeClockAdminService(t, mockClerk, defaultRoleCacheTTL)

	clerkUserID := "user_ordinary"
	mockClerk.On("GetUser", mock.Anything, clerkUserID).Return(
		createMockClerkUser(clerkUserID, "user@example.com", []string{}), nil)

	ctx := context.Background()
	hasRole, err := svc.HasRole(ctx, clerkUserID, domain.RoleAdmin)
	require.NoError(t, err)
	require.False(t, hasRole)
	mockClerk.AssertNumberOfCalls(t, "GetUser", 1)

	// Well past the positive cap, still inside the general TTL: no second Clerk call.
	advance(positiveRoleCacheTTLCap * 4)
	_, err = svc.HasRole(ctx, clerkUserID, domain.RoleAdmin)
	require.NoError(t, err)
	mockClerk.AssertNumberOfCalls(t, "GetUser", 1)

	// Past the general TTL: refreshed as before.
	advance(defaultRoleCacheTTL)
	_, err = svc.HasRole(ctx, clerkUserID, domain.RoleAdmin)
	require.NoError(t, err)
	mockClerk.AssertNumberOfCalls(t, "GetUser", 2)
}

// TestAdminService_ShortConfiguredTTLIsNotLengthenedByTheCap: the cap is a CAP, never a floor.
// A caller that asks for a 10ms TTL (the existing expiry test does) must not have positive
// results held for thirty seconds instead.
func TestAdminService_ShortConfiguredTTLIsNotLengthenedByTheCap(t *testing.T) {
	mockClerk := new(MockClerkUserGetter)
	svc, advance := fakeClockAdminService(t, mockClerk, 10*time.Millisecond)

	clerkUserID := "user_short_ttl"
	mockClerk.On("GetUser", mock.Anything, clerkUserID).Return(
		createMockClerkUser(clerkUserID, "admin@example.com", []string{"admin"}), nil)

	ctx := context.Background()
	_, err := svc.HasRole(ctx, clerkUserID, domain.RoleAdmin)
	require.NoError(t, err)

	advance(20 * time.Millisecond)
	_, err = svc.HasRole(ctx, clerkUserID, domain.RoleAdmin)
	require.NoError(t, err)
	mockClerk.AssertNumberOfCalls(t, "GetUser", 2)
}
