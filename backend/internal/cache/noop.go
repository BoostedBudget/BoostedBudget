package cache

import "context"

// noopCache is the disabled-cache implementation. It holds no Redis client of any kind, of any
// shape — there is structurally nothing here capable of issuing a command — so "zero Redis
// commands" is a property of the type, not merely an observed runtime behaviour. See
// TestNoopCache_IssuesZeroRedisCommands.
type noopCache struct{}

// NewNoopCache returns a Cache that always calls the loader directly and issues zero Redis
// commands. Use it when the top-level cache is disabled (CACHE_ENABLED=false, or no cache Redis
// client could be constructed) — Task 10 decides when to construct this instead of NewCache.
func NewNoopCache() Cache {
	return noopCache{}
}

func (noopCache) GetOrLoad(
	ctx context.Context, _ Key, loader func(context.Context) ([]byte, error),
) ([]byte, bool, error) {
	v, err := loader(ctx)
	return v, false, err
}

// Generations returns nil: there is no generation manager for a cache with no Redis client.
func (noopCache) Generations() *Generations { return nil }

// Enabled always reports false: a disabled cache has no enabled namespaces.
func (noopCache) Enabled(Namespace) bool { return false }

var _ Cache = noopCache{}
