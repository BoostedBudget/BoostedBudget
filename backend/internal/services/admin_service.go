package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/user"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Default TTL for role cache (5 minutes)
const defaultRoleCacheTTL = 5 * time.Minute

// Sentinel errors for AdminService
var (
	ErrClerkUserNotFound = errors.New("clerk user not found")
	ErrClerkAPIError     = errors.New("clerk API error")
)

// ClerkUserGetter is an interface for fetching Clerk users.
// This abstraction allows for mocking in tests.
type ClerkUserGetter interface {
	GetUser(ctx context.Context, clerkUserID string) (*clerk.User, error)
}

// AdminService defines the interface for admin role management.
type AdminService interface {
	// HasRole checks if a user has the specified role.
	// Roles are checked via Clerk privateMetadata.
	// Returns true if user has the role or is a hardcoded admin.
	HasRole(ctx context.Context, clerkUserID string, role domain.AdminRole) (bool, error)

	// GetRoles returns all roles for a user from Clerk privateMetadata.
	GetRoles(ctx context.Context, clerkUserID string) ([]domain.AdminRole, error)

	// InvalidateCache removes cached role data for a user.
	// Call this when role changes are detected (e.g., via webhook).
	InvalidateCache(clerkUserID string)

	// IsHardcodedAdmin checks if an email is in the hardcoded admin list.
	// Email matching is case-insensitive.
	IsHardcodedAdmin(email string) bool
}

// cachedRole stores cached role information with timestamp.
type cachedRole struct {
	roles    []domain.AdminRole
	cachedAt time.Time
}

// adminService implements AdminService with caching.
type adminService struct {
	mu          sync.RWMutex
	cache       map[string]*cachedRole
	ttl         time.Duration
	adminEmails map[string]bool // O(1) lookup for hardcoded admins (lowercase)
	userGetter  ClerkUserGetter
}

// defaultClerkUserGetter is the production implementation using Clerk SDK.
type defaultClerkUserGetter struct{}

func (d *defaultClerkUserGetter) GetUser(ctx context.Context, clerkUserID string) (*clerk.User, error) {
	clerkUser, err := user.Get(ctx, clerkUserID)
	if err != nil {
		if isClerkNotFoundError(err) {
			return nil, ErrClerkUserNotFound
		}
		return nil, ErrClerkAPIError
	}
	return clerkUser, nil
}

// NewAdminService creates a new AdminService instance.
//
// Parameters:
//   - secretKey: The Clerk secret key (sk_test_... or sk_live_...)
//   - adminEmails: List of hardcoded admin emails (case-insensitive)
func NewAdminService(secretKey string, adminEmails []string) AdminService {
	clerk.SetKey(secretKey)
	return NewAdminServiceWithUserGetterAndTTL(secretKey, adminEmails, &defaultClerkUserGetter{}, defaultRoleCacheTTL)
}

// NewAdminServiceWithUserGetter creates an AdminService with a custom user getter (for testing).
func NewAdminServiceWithUserGetter(secretKey string, adminEmails []string, userGetter ClerkUserGetter) AdminService {
	return NewAdminServiceWithUserGetterAndTTL(secretKey, adminEmails, userGetter, defaultRoleCacheTTL)
}

// NewAdminServiceWithUserGetterAndTTL creates an AdminService with custom user getter and TTL (for testing).
func NewAdminServiceWithUserGetterAndTTL(secretKey string, adminEmails []string, userGetter ClerkUserGetter, ttl time.Duration) AdminService {
	return newAdminServiceInternal(adminEmails, userGetter, ttl)
}

// newAdminServiceInternal is the internal constructor.
func newAdminServiceInternal(adminEmails []string, userGetter ClerkUserGetter, ttl time.Duration) *adminService {
	// Build email lookup map with lowercase keys
	emailMap := make(map[string]bool, len(adminEmails))
	for _, email := range adminEmails {
		emailMap[strings.ToLower(strings.TrimSpace(email))] = true
	}

	return &adminService{
		cache:       make(map[string]*cachedRole),
		ttl:         ttl,
		adminEmails: emailMap,
		userGetter:  userGetter,
	}
}

// IsHardcodedAdmin checks if an email is in the hardcoded admin list.
func (s *adminService) IsHardcodedAdmin(email string) bool {
	return s.adminEmails[strings.ToLower(strings.TrimSpace(email))]
}

// HasRole checks if a user has the specified role.
func (s *adminService) HasRole(ctx context.Context, clerkUserID string, role domain.AdminRole) (bool, error) {
	roles, err := s.GetRoles(ctx, clerkUserID)
	if err != nil {
		return false, err
	}

	for _, r := range roles {
		if r == role {
			return true, nil
		}
	}

	return false, nil
}

// GetRoles returns all roles for a user.
func (s *adminService) GetRoles(ctx context.Context, clerkUserID string) ([]domain.AdminRole, error) {
	// Check cache first
	s.mu.RLock()
	if cached, exists := s.cache[clerkUserID]; exists && time.Since(cached.cachedAt) < s.ttl {
		s.mu.RUnlock()
		return cached.roles, nil
	}
	s.mu.RUnlock()

	// Cache miss or expired - fetch from Clerk API
	clerkUser, err := s.userGetter.GetUser(ctx, clerkUserID)
	if err != nil {
		return nil, err
	}

	// Parse roles from privateMetadata and update cache
	roles := parseRolesFromMetadata(clerkUser.PrivateMetadata)
	s.mu.Lock()
	s.cache[clerkUserID] = &cachedRole{
		roles:    roles,
		cachedAt: time.Now(),
	}
	s.mu.Unlock()

	return roles, nil
}

// InvalidateCache removes cached role data for a user.
func (s *adminService) InvalidateCache(clerkUserID string) {
	s.mu.Lock()
	delete(s.cache, clerkUserID)
	s.mu.Unlock()
}

// parseRolesFromMetadata extracts roles from Clerk privateMetadata JSON.
func parseRolesFromMetadata(metadata json.RawMessage) []domain.AdminRole {
	if metadata == nil || len(metadata) == 0 {
		return []domain.AdminRole{}
	}

	var parsed domain.AdminPrivateMetadata
	if err := json.Unmarshal(metadata, &parsed); err != nil {
		return []domain.AdminRole{}
	}

	roles := make([]domain.AdminRole, len(parsed.Roles))
	for i, r := range parsed.Roles {
		roles[i] = domain.AdminRole(r)
	}

	return roles
}
