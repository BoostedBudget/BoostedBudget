package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// tombstoneTTL bounds how long a purge tombstone blocks new writes into a scope. It MUST be at
// least as long as the longest registered namespace TTL (600s, NamespaceIdentity) — a write that
// was in flight (already past the generation-read step) when the tombstone was set must never be
// able to land after the tombstone itself has expired, or the closed race would reopen.
const tombstoneTTL = 900 * time.Second

// tombstoneValue is a fixed literal — its content is never read, only its presence.
const tombstoneValue = "1"

// TombstoneKey returns the Redis key marking scopeTag as under deletion: bb:c:{<scopeTag>}:tomb
func TombstoneKey(scopeTag string) string {
	return fmt.Sprintf("%s:{%s}:tomb", keyPrefix, scopeTag)
}

// scanPattern returns the glob matching every key (data, generation, AND the tombstone itself)
// rendered under scopeTag.
func scanPattern(scopeTag string) string {
	return fmt.Sprintf("%s:{%s}:*", keyPrefix, scopeTag)
}

// Purger is implemented by a real Redis-backed Cache and used ONLY by the GDPR deletion path
// (Issue #1531 Task 10b) to erase a deleted user's own cache footprint immediately, rather than
// merely orphaning it via a generation bump and waiting out the TTL like every other invalidation
// in this package. NewNoopCache also implements this (as a no-op) so callers never need a type
// switch to find out whether purging is possible.
//
// This is deliberately NOT folded into the Cache interface's read/write surface: GetOrLoad's
// callers (Task 10a's cached services) have no reason to ever see this method, and keeping it on
// a separate interface makes "who is allowed to call this" a compile-time-visible decision (only
// deletion_service.go performs the type assertion).
type Purger interface {
	// PurgeUser deletes EVERY key (data + generation) rendered under the u:<userID> scope tag and,
	// if clerkUserID is non-empty, under the k:<hash(clerkUserID)> identity scope tag too — both
	// scopes exist ONLY for this one user, so unlike a household scope there is no other party's
	// legitimate data to preserve, and unlike the household-orphaning generation bump, this is a
	// real, immediate delete (see the package-level doc comment on why: the codebase's existing
	// GDPR posture already deletes the 24h idempotency cache outright rather than trusting a TTL —
	// profile_repository.go's DeleteUserCascade — and this purge matches that posture).
	//
	// A per-scope deletion tombstone is written BEFORE the scan/delete, closing the race where an
	// in-flight fill — one that read stale (pre-deletion) data and computes its SET call after this
	// method's scan has already passed over that key — would otherwise resurrect it. Every cache
	// write (redisCache.set) checks for this tombstone and refuses to write while it is present.
	//
	// Returns a non-nil error if ANY Redis operation failed, so the caller (deletion_service.go)
	// can enqueue a durable retry (River) rather than silently completing the deletion with an
	// un-purged cache — a Redis purge failure must never silently drop the erasure. Partial
	// progress (e.g. the user scope purged, the identity scope failed) is retried in full: PurgeUser
	// is idempotent — deleting an already-empty scope and re-setting an already-set tombstone are
	// both no-ops.
	PurgeUser(ctx context.Context, userID, clerkUserID string) error
}

func (c *redisCache) PurgeUser(ctx context.Context, userID, clerkUserID string) error {
	tags := []string{ScopeTag(ScopeUser, userID)}
	if clerkUserID != "" {
		tags = append(tags, ScopeTag(ScopeClerkID, clerkUserID))
	}

	var errs []error
	for _, tag := range tags {
		if err := c.purgeScope(ctx, tag); err != nil {
			errs = append(errs, fmt.Errorf("purge scope %q: %w", tag, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// purgeScope tombstones tag FIRST (closing the write race for anything that lands from this
// moment on), then deletes every key that already existed under tag at scan time — EXCLUDING the
// tombstone key itself, which must survive for its own TTL.
func (c *redisCache) purgeScope(ctx context.Context, tag string) error {
	tombKey := TombstoneKey(tag)
	if err := c.client.Set(ctx, tombKey, tombstoneValue, tombstoneTTL); err != nil {
		return fmt.Errorf("set tombstone: %w", err)
	}

	keys, err := c.client.Scan(ctx, scanPattern(tag))
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	toDelete := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == tombKey {
			continue
		}
		toDelete = append(toDelete, k)
	}
	if len(toDelete) == 0 {
		return nil
	}
	if err := c.client.Del(ctx, toDelete...); err != nil {
		return fmt.Errorf("del: %w", err)
	}
	return nil
}

// isTombstoned reports whether tag currently has an active deletion tombstone. Checked by
// redisCache.set before every write — see that method's doc comment.
//
// Issue #1531 Blocker 3: this MUST fail CLOSED, not open. A genuine Redis read error here (a
// timeout, the circuit breaker being open, a connection drop — anything other than the tombstone
// key genuinely not existing) used to be treated identically to "no tombstone", which is exactly
// backwards for the one flow that must refuse a write it cannot positively clear: a write that
// lands during a GDPR deletion's purge window, while this check could not confirm the tombstone's
// absence, is the same class of bug the purge/tombstone mechanism exists to prevent (a deleted
// user's data resurrected past the deletion). This mirrors how Generations.Get already treats a
// genuine read error on the generation key — propagated as-is, forcing the caller to bypass the
// cache entirely rather than serve under an unconfirmed generation (see that method's doc
// comment). Concretely:
//   - Get succeeds (err == nil): the tombstone key exists — tombstoned, refuse the write.
//   - Get fails with ErrNotFound: the key genuinely does not exist — not tombstoned, allow the
//     write.
//   - Get fails with any OTHER error: unknown state — treated as tombstoned (fail closed), so the
//     write is refused rather than risking a resurrection this check could not rule out. The cost
//     is a spuriously-skipped cache write during a Redis blip, which is free; the alternative
//     (fail open) is a GDPR-relevant data-resurrection risk, which is not.
func (c *redisCache) isTombstoned(ctx context.Context, tag string) bool {
	_, err := c.client.Get(ctx, TombstoneKey(tag))
	if err == nil {
		return true
	}
	return !errors.Is(err, redisclient.ErrNotFound)
}

// PurgeUser on noopCache is a no-op: there is no Redis client to purge, and nothing was ever
// cached in the first place.
func (noopCache) PurgeUser(context.Context, string, string) error { return nil }

var (
	_ Purger = (*redisCache)(nil)
	_ Purger = noopCache{}
)
