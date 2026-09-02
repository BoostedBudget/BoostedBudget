package middleware

// Issue #1531 Task 10b — the HTTP mutation cache-invalidation middleware. Task 10a wired cached
// reads for 17 analytics methods, dashboard prefs, and identity, but nothing bumped a generation
// on write — those reads could serve stale financial data for up to the namespace TTL after any
// mutation. This middleware is the completeness lever: it runs after every mutating, AUTHENTICATED
// HTTP operation and bumps the acting user's cache generation (and, when resolvable, their
// household's).
//
// Registration order (main.go) is load-bearing: AFTER the Clerk middleware (so inputs.UserIDKey is
// populated) and AFTER NewReadOnlyMiddleware (so a request the read-only gate blocked with 403/503
// never reaches this middleware and never bumps — nothing committed).
//
// ⚠ Status() == 0 means UNKNOWN, not "no write happened" — bump anyway. humaecho's Status()
// pointer is initialised via new(int) (humaecho.go), so an unwritten OR panicking handler leaves
// it at 0. Skipping the bump in that case risks serving stale data after a write that may have
// partially committed before the panic; over-invalidating merely costs hit rate. See
// TestCacheInvalidation_UnwrittenStatusIsTreatedAsUnknownAndBumps.
//
// ⚠ Both the user AND household bumps are SYNCHRONOUS, issued after next(ctx) returns (i.e. after
// the handler — and therefore any DB commit inside it — has already run). Do NOT make the
// household bump asynchronous/queued: a dropped or delayed household bump would leave a household
// member reading stale money after another member's write, which the product's own household
// specs assert moves immediately. See TestBumpBeforeCommit_ProducesPermanentStaleEntry /
// TestBumpAfterCommit_NoStaleEntry (internal/cache) for why "commit, then bump" (never the
// reverse) is the property that makes any of this safe at all — by construction here, the handler
// (and its commit) always completes before this middleware's code runs, since it runs AFTER next.
//
// ⚠ Issue #1531 Blocker 2 — the bump work below runs on context.WithoutCancel(reqCtx), NOT reqCtx
// itself, exactly like the rate limiter's fallback probe (internal/middleware/rate_limit_fallback.go)
// and every dependency health check (internal/health/check.go). reqCtx is the INBOUND HTTP
// request's context: it is cancelled the instant the client disconnects, or expires the instant
// its deadline (if any) elapses — either of which can land in the narrow window AFTER next(ctx)
// has already returned (the write has already committed) but BEFORE the bump below runs. Bumping
// on the request's own context would let a client that hangs up right after a successful write
// cancel its OWN cache invalidation, leaving stale financial data behind for the household/user
// scope until the namespace TTL — silently, since Bump swallows its own errors. WithoutCancel
// detaches from that cancellation/deadline; the explicit WithTimeout that follows re-introduces a
// bound (WithoutCancel strips the parent deadline too, so without it the bump would have none).
// See TestCacheInvalidation_BumpsAfterRequestContextCancelled for the regression test.
import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
)

// bumpTimeout bounds the detached post-commit bump work (user + household) once the request
// context's own cancellation/deadline has been stripped via context.WithoutCancel. Matches the
// rate limiter fallback's and health checker's own bounded-probe pattern — WithoutCancel alone
// would otherwise let a Redis outage hang this goroutine forever with no caller left to time it
// out (the HTTP response has already been written by this point).
const bumpTimeout = 5 * time.Second

// householdBumpAttempts/householdBumpBackoff bound the household bump's retry (see
// gens.BumpWithRetry and the reordering below, #1569 round 11 item 3). Two tries, 100ms apart,
// comfortably fits inside bumpTimeout's 5s budget alongside the household lookup and the
// user-scope bump that follows it.
const (
	householdBumpAttempts = 2
	householdBumpBackoff  = 100 * time.Millisecond
)

// HouseholdLookup resolves the household (if any) a user currently belongs to. Satisfied
// structurally by *repository.householdRepository's GetHouseholdForUser (see main.go wiring).
// This costs one indexed query, paid ONLY on the write path (mutating, authenticated requests) —
// never on reads.
type HouseholdLookup interface {
	GetHouseholdForUser(ctx context.Context, userID string) (household *domain.Household, isOwner bool, found bool, err error)
}

// NewCacheInvalidationMiddleware returns a Huma middleware that bumps the acting user's (and,
// when hh is non-nil and resolves one, their household's) cache generation after every mutating,
// authenticated operation whose response was not a genuine failure.
//
// gens may be nil (mirrors cache.Cache.Generations() returning nil for a disabled/noop cache):
// the middleware then passes every request through untouched, issuing zero work. hh may also be
// nil, in which case only the user-scope bump runs — the household side is simply skipped, never
// a hard failure.
func NewCacheInvalidationMiddleware(gens *cache.Generations, hh HouseholdLookup, logger *slog.Logger) func(ctx huma.Context, next func(huma.Context)) {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx huma.Context, next func(huma.Context)) {
		if gens == nil {
			next(ctx)
			return
		}

		// Public routes (no Security annotation): no authenticated actor, nothing to bump.
		if len(ctx.Operation().Security) == 0 {
			next(ctx)
			return
		}

		method := ctx.Operation().Method
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			next(ctx)
			return
		}

		// Run the handler (and, inside it, any DB commit) FIRST. Everything below runs only
		// after next returns — see the package-level doc comment on why that ordering is the
		// whole safety property here.
		next(ctx)

		status := ctx.Status()
		if status >= 400 && status < 500 {
			// A genuine 4xx (validation, auth, business-rule rejection): assumed to mean the
			// handler never reached a commit, so skipping the bump costs nothing and saves the
			// work. This is an assumption, not a guarantee -- see the 5xx case below, and the
			// package doc comment's Status()==0 case, for the class of failure where the
			// equivalent assumption turned out to be false.
			return
		}
		// Every other status bumps: 2xx/3xx (a real success), 0/UNKNOWN (unwritten or a
		// panicking handler — see the package doc comment), and 5xx.
		//
		// Issue #1531 L8: 5xx used to be treated the same as 4xx (skip, "nothing committed").
		// That assumption is false for a handler that COMMITS and then fails on a LATER step — a
		// post-commit side effect (an audit log write, an event publish, a downstream API call)
		// erroring out surfaces as a 500 despite the mutation itself having already landed. The
		// status code alone cannot distinguish "failed before any write" from "wrote, then failed
		// afterward," which is exactly the ambiguity the Status()==0 case already conceded and
		// chose to bump through — over-invalidating only costs hit rate, while under-invalidating
		// serves stale financial data until the namespace TTL. 5xx now gets the same treatment for
		// the same reason.

		reqCtx := ctx.Context()
		userID, _ := reqCtx.Value(inputs.UserIDKey).(string)
		if userID == "" {
			return
		}

		// Issue #1531 Blocker 2: detach from the request's own cancellation/deadline before
		// doing any post-commit work — see the package doc comment above for why.
		bumpCtx, cancel := context.WithTimeout(context.WithoutCancel(reqCtx), bumpTimeout)
		defer cancel()

		// #1569 round 11 item 3: household FIRST, then the user-scope bump — reordered from this
		// middleware's original "user, then household" shape. That original order meant a
		// household lookup failure (or, before this fix, a bare Bump's own silent Redis SET
		// failure) landed on the bump with the WORSE blast radius: it left ANOTHER, otherwise
		// uninvolved household member reading stale money for up to the namespace TTL, while the
		// acting user's own self-scoped bump had already gone through. Household work now runs
		// first and gets BumpWithRetry (see that method's doc comment); the user-scope bump below
		// still always runs, unconditionally, same as before.
		//
		// ⚠ Reordering alone does not survive a genuine Redis SET failure on the household bump —
		// that is why this also switches to BumpWithRetry rather than Bump for the household half.
		// The two are independent fixes for independent failure modes: the reorder addresses a
		// household LOOKUP failure (which order genuinely changes the blast radius of); the retry
		// addresses the household BUMP's own write failure (which order cannot affect at all, since
		// a failing SET fails wherever it runs).
		if hh != nil {
			household, _, found, err := hh.GetHouseholdForUser(bumpCtx, userID)
			if err != nil {
				// Void by design (see cache.Generations.Bump's own doc comment for the same
				// principle): the write already committed; a household lookup failure must never
				// fail the request (which has already been written to the client by this point).
				// The user-scope bump below still runs regardless.
				logger.Error("cache invalidation middleware: household lookup failed; household-scoped "+
					"cache may serve stale data until its namespace TTL", "user_id", userID, "error", err)
			} else if found && household != nil {
				gens.BumpWithRetry(bumpCtx, cache.ScopeHousehold, household.ID, "http_mutation",
					householdBumpAttempts, householdBumpBackoff)
			}
		}

		gens.Bump(bumpCtx, cache.ScopeUser, userID, "http_mutation")
	}
}
