package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// methodResolveInternalUserID is the cache method constant for
// cachedIdentityResolver.ResolveInternalUserID.
const methodResolveInternalUserID = "ResolveInternalUserID"

// InternalUserIDResolver resolves a Clerk user ID to the internal user's UUID ONLY — never the
// full identity record. repository.UserIdentityRepository.GetByClerkID returns a *domain.UserIdentity
// carrying the (decrypted) email, provider ID, and verification state; caching that transparently
// would put all of it in Redis. This narrow interface exists so the ONLY thing a cache consumer
// can ever put in NamespaceIdentity is the scalar UUID. Issue #1531 Task 10a.
type InternalUserIDResolver interface {
	ResolveInternalUserID(ctx context.Context, clerkUserID string) (uuid.UUID, error)
}

// cachedIdentityResolver implements InternalUserIDResolver over a
// repository.UserIdentityRepository, caching only the resolved UUID.
//
// ⚠ Cache successful lookups only. repository.ErrIdentityNotFound must never be negative-cached —
// a fresh registration would otherwise be invisible for the identity namespace's full 600s TTL.
// This falls out of cache.GetOrLoad's own contract (it only Sets on a nil loader error), so no
// special-casing is needed here beyond simply propagating the error unencoded.
type cachedIdentityResolver struct {
	inner repository.UserIdentityRepository
	c     cache.Cache
}

// NewCachedIdentityResolver returns an InternalUserIDResolver backed by inner, caching resolved
// UUIDs under NamespaceIdentity (600s TTL, ScopeClerkID — clerkUserID is hashed into the scope
// tag, never stored raw, per ScopeTag's doc comment).
func NewCachedIdentityResolver(inner repository.UserIdentityRepository, c cache.Cache) InternalUserIDResolver {
	return &cachedIdentityResolver{inner: inner, c: c}
}

func (r *cachedIdentityResolver) ResolveInternalUserID(ctx context.Context, clerkUserID string) (uuid.UUID, error) {
	k := cache.NewKey(cache.NamespaceIdentity, cache.Scope{Kind: cache.ScopeClerkID, ID: clerkUserID}, methodResolveInternalUserID).
		Enc(func(e *cache.ArgEncoder) {})
	return cache.GetOrLoad(ctx, r.c, k, func(ctx context.Context) (uuid.UUID, error) {
		identity, err := r.inner.GetByClerkID(ctx, clerkUserID)
		if err != nil {
			// repository.ErrIdentityNotFound (or any other error) propagates unencoded and is
			// NEVER cached — see this type's doc comment.
			return uuid.Nil, err
		}
		id, err := uuid.Parse(identity.UserID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("identity resolver: user_id %q is not a valid UUID: %w", identity.UserID, err)
		}
		return id, nil
	})
}

var _ InternalUserIDResolver = (*cachedIdentityResolver)(nil)
