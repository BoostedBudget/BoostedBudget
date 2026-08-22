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

// positiveRoleCacheTTLCap bounds how long a role-GRANTING lookup may be served from cache.
const positiveRoleCacheTTLCap = 30 * time.Second

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
//
// Issue #1432 (batch ED, finding B2-3): there is NO DATABASE ROLE CHECK anywhere behind this
// interface, whatever older comments claimed. Roles are read from Clerk privateMetadata over the
// Clerk API and cached in process — see GetRoles and cacheTTLFor for the revocation window that
// follows from that.
type AdminService interface {
	// HasRole checks if a user has the specified role.
	// Roles are read from Clerk privateMetadata (not from the database) and may be served
	// from the in-process cache — see cacheTTLFor.
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
	ttl      time.Duration
}

// adminService implements AdminService with caching.
type adminService struct {
	mu          sync.RWMutex
	cache       map[string]*cachedRole
	ttl         time.Duration
	adminEmails map[string]bool // O(1) lookup for hardcoded admins (lowercase)
	userGetter  ClerkUserGetter
	// now is the clock, injectable so tests can cross the cache windows without sleeping.
	now func() time.Time
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

// NewDefaultClerkUserGetter returns the production ClerkUserGetter backed by the Clerk
// SDK. Exposed so other services can reuse the same Clerk user-fetch capability without
// duplicating the user.Get/error-mapping plumbing — e.g. household member profile
// enrichment (#1367 — Phase 3 Round 2, GT-3). Relies on clerk.SetKey having already been
// called (NewAdminService does this at startup, before the server accepts requests).
func NewDefaultClerkUserGetter() ClerkUserGetter {
	return &defaultClerkUserGetter{}
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
		now:         time.Now,
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

// GetRoles returns all roles for a user, reading Clerk privateMetadata over the Clerk API.
// A cached entry is served without contacting Clerk until its own TTL elapses; how long that is
// depends on whether the result grants anything — see cacheTTLFor (#1432 B2-3).
func (s *adminService) GetRoles(ctx context.Context, clerkUserID string) ([]domain.AdminRole, error) {
	// Check cache first
	s.mu.RLock()
	if cached, exists := s.cache[clerkUserID]; exists && s.now().Sub(cached.cachedAt) < cached.ttl {
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
		cachedAt: s.now(),
		ttl:      s.cacheTTLFor(roles),
	}
	s.mu.Unlock()

	return roles, nil
}

// cacheTTLFor returns how long a lookup result may be served from cache.
//
// Issue #1432 (batch ED, finding B2-3): a POSITIVE result — one that grants a role — is capped
// at positiveRoleCacheTTLCap, because serving it stale means an admin whose role was revoked in
// the Clerk dashboard keeps that authority for the rest of the window, and irreversible admin
// operations (the dead-letter bulk delete, subscription tier overrides) are reachable inside it.
// A NEGATIVE result keeps the full TTL: that is the overwhelmingly common case the cache exists
// to absorb, and serving it stale only delays a newly-GRANTED role, which is an availability
// inconvenience rather than an authority leak.
//
// It is a CAP, never a floor: a caller configuring a shorter TTL keeps it.
func (s *adminService) cacheTTLFor(roles []domain.AdminRole) time.Duration {
	if len(roles) == 0 || s.ttl <= positiveRoleCacheTTLCap {
		return s.ttl
	}
	return positiveRoleCacheTTLCap
}

// InvalidateCache removes cached role data for a user.
//
// Issue #1432 (batch ED, finding B2-3): this has NO non-test caller, and that is a deliberate,
// recorded state rather than an oversight. Roles live in Clerk privateMetadata and are granted
// and revoked in the Clerk dashboard, out of band from this application: nothing here writes
// privateMetadata (clerk_service.go's UpdateUserMetadata writes PUBLIC metadata — the
// subscription tier), and there is no Clerk webhook receiver to learn of a change from (the only
// webhook routes are /webhooks/paddle and /webhooks/plaid). So there is no event to wire this to
// without first building one. The revocation window is bounded by cacheTTLFor instead. If a
// Clerk user.updated webhook is ever added, call this from it and say so here.
func (s *adminService) InvalidateCache(clerkUserID string) {
	s.mu.Lock()
	delete(s.cache, clerkUserID)
	s.mu.Unlock()
}

// parseRolesFromMetadata extracts roles from Clerk privateMetadata JSON.
func parseRolesFromMetadata(metadata json.RawMessage) []domain.AdminRole {
	if len(metadata) == 0 {
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
