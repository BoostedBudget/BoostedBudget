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
