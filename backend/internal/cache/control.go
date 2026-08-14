package cache

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// DefaultControlPollInterval is how often Control.Run polls the control keys.
const DefaultControlPollInterval = 10 * time.Second

// LimiterClientName is the ONLY redisclient.Client name NewControl will accept. It must match the
// Name Task 7's main.go passes when constructing the limiter's no-eviction client
// (redisclient.New(redisclient.Options{Name: "limiter", ...})). This is a real runtime guard, not
// documentation: see NewControl's doc comment for why a name mismatch is refused at construction
// rather than merely warned about.
const LimiterClientName = "limiter"

// controlSnapshot is the immutable state atomically swapped in by each poll. observed[ns] is true
// once ns's control key has EVER been read successfully (whether "on" or "off"); enabled[ns] is
// only meaningful when observed[ns] is true.
type controlSnapshot struct {
	enabled  map[Namespace]bool
	observed map[Namespace]bool
}

// Control polls Redis-hosted per-namespace kill switches.
//
// ⚠ The client passed to NewControl MUST be the LIMITER's no-eviction Redis instance, never the
// cache's own (eviction-enabled) instance. If an "off" control key were evicted from the
// eviction-enabled cache instance, MGET would report it missing (not erroring), resolution would
// fall through to the compiled default ("on" for every non-hard-disabled namespace), and the
// emergency kill switch would silently re-enable caching. Wiring which client to pass here is
// Task 10's job (this package only builds Control); Task 9 just enforces the contract in tests.
//
// Resolution order (see cache.go's Enabled): hardDisabled constant -> Redis control key -> env var
// -> compiled default. Once a namespace has been OBSERVED as "off", a later missing value resolves
// to "off", not back to the default — see Poll's merge logic below. A poll FAILURE (the MGET call
// itself erroring) keeps the last snapshot untouched: fail-static, not fail-open-to-default.
type Control struct {
	client     redisclient.Client
	namespaces []Namespace
	snapshot   atomic.Pointer[controlSnapshot]
}

// NewControl returns a Control that will poll the control keys for the given namespaces against
// client.
//
// client.Name() MUST equal LimiterClientName ("limiter") — NewControl fails fast (returns an
// error) otherwise, rather than merely documenting the requirement. Construction happens once at
// boot, so failing there catches a wiring mistake at deploy time instead of leaving it to be
// discovered mid-incident: if Control were ever pointed at the cache's own (eviction-enabled)
// instance, an "off" control key evicted from it would read back as MISSING (not an error), fall
// through to the compiled default of "on", and silently re-enable caching right after an operator
// hit the emergency kill switch.
func NewControl(client redisclient.Client, namespaces []Namespace) (*Control, error) {
	if client == nil {
		return nil, fmt.Errorf("cache: Control requires a non-nil redisclient.Client")
	}
	if client.Name() != LimiterClientName {
		return nil, fmt.Errorf(
			"cache: Control must be constructed against the %q (no-eviction) Redis client, got %q — "+
				"wiring it to any other client risks the emergency kill switch silently re-enabling "+
				"caching if a control key is evicted", LimiterClientName, client.Name())
	}
	return &Control{client: client, namespaces: namespaces}, nil
}

// Poll runs one synchronous MGET against every configured namespace's control key and updates the
// snapshot. It is exported (rather than only reachable via Run's ticker) so tests — and Task 10's
// background goroutine — can both drive it deterministically without waiting on a timer.
//
// A poll failure (MGET itself returning an error — a real Redis error, not "key missing", which
// MGET never signals as an error) leaves the previous snapshot in place and returns the error.
func (c *Control) Poll(ctx context.Context) error {
	if c == nil || c.client == nil || len(c.namespaces) == 0 {
		return nil
	}

	keys := make([]string, len(c.namespaces))
	for i, ns := range c.namespaces {
		keys[i] = ControlKey(ns)
	}

	vals, found, err := c.client.MGet(ctx, keys...)
	if err != nil {
		// Fail-static: keep whatever snapshot already exists.
		return err
	}

	prev := c.snapshot.Load()
	next := &controlSnapshot{
		enabled:  make(map[Namespace]bool, len(c.namespaces)),
		observed: make(map[Namespace]bool, len(c.namespaces)),
	}
	for i, ns := range c.namespaces {
		if found[i] {
			next.enabled[ns] = vals[i] == "on"
			next.observed[ns] = true
			continue
		}
		// Missing this poll. If we have PREVIOUSLY observed a value for this namespace, carry it
		// forward rather than treating "vanished" as "never set" — this is exactly what makes a
		// missing-after-observed-off control key resolve to off instead of falling through to the
		// compiled default.
		if prev != nil && prev.observed[ns] {
			next.enabled[ns] = prev.enabled[ns]
			next.observed[ns] = true
		}
		// else: truly never observed — leave unobserved so Enabled() falls through to env var /
		// compiled default.
	}
	c.snapshot.Store(next)
	return nil
}

// Run polls every interval until ctx is done. Task 10 wires this into main.go's background
// goroutines; Task 9 only builds it. If interval <= 0, DefaultControlPollInterval is used.
func (c *Control) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultControlPollInterval
	}
	_ = c.Poll(ctx) // immediate first poll, matching internal/observability's Redis pool collector
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = c.Poll(ctx)
		}
	}
}

// Enabled reports the Redis-control-key-observed enablement for ns, and whether a value has EVER
// been observed. When observed is false, the caller must fall through to an env var / compiled
// default — there is no "unknown resolves to on" path anywhere in this method.
func (c *Control) Enabled(ns Namespace) (enabled bool, observed bool) {
	if c == nil {
		return false, false
	}
	snap := c.snapshot.Load()
	if snap == nil {
		return false, false
	}
	return snap.enabled[ns], snap.observed[ns]
}
