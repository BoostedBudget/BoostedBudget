package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeUserIdentityRepository is a minimal repository.UserIdentityRepository double for exercising
// cachedIdentityResolver. It counts GetByClerkID calls so tests can assert a cache hit skipped it.
type fakeUserIdentityRepository struct {
	byClerkID       map[string]*domain.UserIdentity
	getByClerkCalls int
}

func newFakeUserIdentityRepository() *fakeUserIdentityRepository {
	return &fakeUserIdentityRepository{byClerkID: map[string]*domain.UserIdentity{}}
}

func (f *fakeUserIdentityRepository) GetByClerkID(ctx context.Context, clerkUserID string) (*domain.UserIdentity, error) {
	f.getByClerkCalls++
	if id, ok := f.byClerkID[clerkUserID]; ok {
		return id, nil
	}
	return nil, repository.ErrIdentityNotFound
}

func (f *fakeUserIdentityRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.UserIdentity, error) {
	return nil, nil
}
func (f *fakeUserIdentityRepository) Create(ctx context.Context, identity *domain.UserIdentity) error {
	return nil
}
func (f *fakeUserIdentityRepository) CreateTx(ctx context.Context, tx pgx.Tx, identity *domain.UserIdentity) error {
	return nil
}
func (f *fakeUserIdentityRepository) UpdateLastUsed(ctx context.Context, clerkUserID string) error {
	return nil
}
func (f *fakeUserIdentityRepository) UpdatePrimaryIdentityEmailTx(ctx context.Context, tx pgx.Tx, userID, plainEmail string) error {
	return nil
}

var _ repository.UserIdentityRepository = (*fakeUserIdentityRepository)(nil)

func TestCachedIdentityResolver_CachesSuccessfulLookup(t *testing.T) {
	internalID := uuid.New()
	repo := newFakeUserIdentityRepository()
	repo.byClerkID["clerk-1"] = &domain.UserIdentity{
		UserID:   internalID.String(),
		Email:    "should-never-be-cached@example.com",
		Provider: "clerk",
	}
	c := newFakeCache()
	resolver := NewCachedIdentityResolver(repo, c)

	ctx := context.Background()
	got, err := resolver.ResolveInternalUserID(ctx, "clerk-1")
	require.NoError(t, err)
	assert.Equal(t, internalID, got)

	got2, err := resolver.ResolveInternalUserID(ctx, "clerk-1")
	require.NoError(t, err)
	assert.Equal(t, internalID, got2)

	assert.Equal(t, 1, repo.getByClerkCalls, "second lookup for the same Clerk ID should have been served from cache")
}

func TestCachedIdentityResolver_DoesNotNegativeCacheNotFound(t *testing.T) {
	repo := newFakeUserIdentityRepository() // empty: every clerk ID is "not found"
	c := newFakeCache()
	resolver := NewCachedIdentityResolver(repo, c)

	ctx := context.Background()
	_, err := resolver.ResolveInternalUserID(ctx, "clerk-new")
	require.ErrorIs(t, err, repository.ErrIdentityNotFound)

	// A fresh registration between the two calls must be visible immediately — not cached-miss
	// for the identity namespace's 600s TTL.
	internalID := uuid.New()
	repo.byClerkID["clerk-new"] = &domain.UserIdentity{UserID: internalID.String()}

	got, err := resolver.ResolveInternalUserID(ctx, "clerk-new")
	require.NoError(t, err)
	assert.Equal(t, internalID, got)
	assert.Equal(t, 2, repo.getByClerkCalls, "the not-found result must not have been cached")
}

func TestCachedIdentityResolver_NeverCachesEmailOrOtherIdentityFields(t *testing.T) {
	internalID := uuid.New()
	repo := newFakeUserIdentityRepository()
	repo.byClerkID["clerk-1"] = &domain.UserIdentity{
		UserID:        internalID.String(),
		Email:         "secret@example.com",
		Provider:      "clerk",
		EmailVerified: true,
	}
	c := newFakeCache()
	resolver := NewCachedIdentityResolver(repo, c)

	_, err := resolver.ResolveInternalUserID(context.Background(), "clerk-1")
	require.NoError(t, err)

	require.Len(t, c.store, 1)
	for _, raw := range c.store {
		assert.NotContains(t, string(raw), "secret@example.com", "cached identity payload must never contain the email")
		assert.NotContains(t, string(raw), "EmailVerified", "cached identity payload must never contain identity struct fields — scalar UUID only")
	}
}

func TestCachedIdentityResolver_DifferentClerkIDsAreDifferentCacheEntries(t *testing.T) {
	id1, id2 := uuid.New(), uuid.New()
	repo := newFakeUserIdentityRepository()
	repo.byClerkID["clerk-1"] = &domain.UserIdentity{UserID: id1.String()}
	repo.byClerkID["clerk-2"] = &domain.UserIdentity{UserID: id2.String()}
	c := newFakeCache()
	resolver := NewCachedIdentityResolver(repo, c)

	ctx := context.Background()
	got1, err := resolver.ResolveInternalUserID(ctx, "clerk-1")
	require.NoError(t, err)
	got2, err := resolver.ResolveInternalUserID(ctx, "clerk-2")
	require.NoError(t, err)

	assert.Equal(t, id1, got1)
	assert.Equal(t, id2, got2)
	assert.Equal(t, 2, repo.getByClerkCalls)
}
