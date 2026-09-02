package middleware_test

// Issue #1531 Task 10b — the HTTP mutation cache-invalidation middleware. Task 10a wired cached
// reads for 17 analytics methods, dashboard prefs, and identity but nothing bumped a generation on
// write — this middleware is the completeness lever that closes that gap for every mutating,
// authenticated HTTP operation. See cache_invalidation.go for the full design rationale.
//
// These tests drive the middleware function DIRECTLY against a minimal, hand-built huma.Context
// fake (fakeHumaCtx below) rather than through a real HTTP adapter. That is a deliberate choice,
// not a shortcut: humaecho's Status()==0-until-written behavior is adapter-specific (verified
// against humaecho.go), and a real panicking-handler test through echo would additionally depend
// on exactly where Echo's own panic recovery sits relative to Huma's internal middleware chain —
// unverified and not what this test needs to pin. Driving fakeHumaCtx.Status() directly tests the
// ACTUAL branch this middleware takes on an unwritten/unknown status, independent of how any
// particular adapter or handler produces that value in practice.

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// fakeHumaCtx implements the subset of huma.Context this middleware touches (Operation, Context,
// Status/SetStatus) and panics on anything else — a deliberate signal that the middleware started
// depending on something this test doesn't model.
type fakeHumaCtx struct {
	op     *huma.Operation
	ctx    context.Context
	status int

	// bodyWriter models the response body writer. Left nil by every test that does not care
	// about the response-write path, so BodyWriter() keeps panicking for those — the tripwire
	// this fake was built around. Set it to assert on WHEN bytes reach the client relative to
	// the cache bump (see TestCacheInvalidation_UserGenerationIsBumpedBeforeTheResponseIsWritten).
	bodyWriter io.Writer
}

func (f *fakeHumaCtx) Operation() *huma.Operation { return f.op }
func (f *fakeHumaCtx) Context() context.Context   { return f.ctx }
func (f *fakeHumaCtx) SetStatus(code int)         { f.status = code }
func (f *fakeHumaCtx) Status() int                { return f.status }

func (f *fakeHumaCtx) TLS() *tls.ConnectionState                  { panic("not used") }
func (f *fakeHumaCtx) Version() huma.ProtoVersion                 { panic("not used") }
func (f *fakeHumaCtx) Method() string                             { return f.op.Method }
func (f *fakeHumaCtx) Host() string                               { panic("not used") }
func (f *fakeHumaCtx) RemoteAddr() string                         { panic("not used") }
func (f *fakeHumaCtx) URL() url.URL                               { panic("not used") }
func (f *fakeHumaCtx) Param(string) string                        { panic("not used") }
func (f *fakeHumaCtx) Query(string) string                        { panic("not used") }
func (f *fakeHumaCtx) Header(string) string                       { return "" }
func (f *fakeHumaCtx) EachHeader(func(name, value string))        {}
func (f *fakeHumaCtx) BodyReader() io.Reader                      { panic("not used") }
func (f *fakeHumaCtx) GetMultipartForm() (*multipart.Form, error) { panic("not used") }
func (f *fakeHumaCtx) SetReadDeadline(time.Time) error            { panic("not used") }
func (f *fakeHumaCtx) SetHeader(string, string)                   {}
func (f *fakeHumaCtx) AppendHeader(string, string)                {}
func (f *fakeHumaCtx) BodyWriter() io.Writer {
	if f.bodyWriter == nil {
		panic("not used")
	}
	return f.bodyWriter
}

var _ huma.Context = (*fakeHumaCtx)(nil)

func securedOp(method string) *huma.Operation {
	return &huma.Operation{Method: method, Security: []map[string][]string{{"clerk": {}}}}
}

func publicOp(method string) *huma.Operation {
	return &huma.Operation{Method: method}
}

func ctxWithUser(userID string) context.Context {
	if userID == "" {
		return context.Background()
	}
	return context.WithValue(context.Background(), inputs.UserIDKey, userID)
}

type fakeHouseholdLookup struct {
	household *domain.Household
	found     bool
	err       error
	calls     int
}

func (f *fakeHouseholdLookup) GetHouseholdForUser(_ context.Context, _ string) (*domain.Household, bool, bool, error) {
	f.calls++
	if f.err != nil {
		return nil, false, false, f.err
	}
	return f.household, false, f.found, nil
}

func newTestGenerations() (*cache.Generations, *redisclient.Fake) {
	fake := redisclient.NewFake("cache")
	return cache.NewGenerations(fake), fake
}

// ctxCheckingClient wraps redisclient.Fake and, unlike the bare Fake (which ignores its ctx
// argument entirely — verified by reading fake.go), actually returns ctx.Err() from Get/Set/SetNX
// when the passed-in context is already done. This is what a REAL go-redis-backed client does
// (context cancellation aborts in-flight and not-yet-issued commands), and it's the only way a
// unit test can distinguish "the middleware bumped using a live context" from "the middleware
// bumped using a context that happened to already be cancelled" — the bare Fake would pass either
// way, since it never even looks at ctx. Used exclusively by
// TestCacheInvalidation_BumpsAfterRequestContextCancelled.
type ctxCheckingClient struct {
	*redisclient.Fake
}

func (c *ctxCheckingClient) Get(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return c.Fake.Get(ctx, key)
}

func (c *ctxCheckingClient) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.Fake.Set(ctx, key, value, ttl)
}

func (c *ctxCheckingClient) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return c.Fake.SetNX(ctx, key, value, ttl)
}

// flakySetClient wraps redisclient.Fake, failing the first failCount Set calls and succeeding on
// every subsequent one — used exclusively by the item 3 (#1569 round 11) tests below to model a
// transient Redis blip. setCalls records every SET's key, in order, so a test can assert on WHICH
// scope was written first (the reorder) as well as whether a retry recovered (the fail-open fix).
type flakySetClient struct {
	*redisclient.Fake
	failCount int
	setCalls  []string // keys SET was called with, in order
}

func (c *flakySetClient) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	c.setCalls = append(c.setCalls, key)
	if c.failCount > 0 {
		c.failCount--
		return errors.New("redis: timeout")
	}
	return c.Fake.Set(ctx, key, value, ttl)
}

// runMiddleware invokes mw with a handler that sets responseStatus (0 means "never call
// SetStatus", modelling the unwritten/unknown case).
func runMiddleware(mw func(huma.Context, func(huma.Context)), op *huma.Operation, ctx context.Context, responseStatus int) {
	fc := &fakeHumaCtx{op: op, ctx: ctx}
	mw(fc, func(c huma.Context) {
		if responseStatus != 0 {
			c.SetStatus(responseStatus)
		}
	})
}

func TestCacheInvalidation_SuccessfulMutationBumpsUserGeneration(t *testing.T) {
	gens, _ := newTestGenerations()
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, nil, nil)

	before, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
	require.NoError(t, err)

	runMiddleware(mw, securedOp(http.MethodPost), ctxWithUser("u1"), http.StatusOK)

	after, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
	require.NoError(t, err)
	assert.NotEqual(t, before, after, "a successful mutation must bump the user's generation")
}

func TestCacheInvalidation_GetIsNeverBumped(t *testing.T) {
	gens, _ := newTestGenerations()
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, nil, nil)

	before, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
	require.NoError(t, err)

	runMiddleware(mw, securedOp(http.MethodGet), ctxWithUser("u1"), http.StatusOK)

	after, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
	require.NoError(t, err)
	assert.Equal(t, before, after, "a GET must never bump")
}

func TestCacheInvalidation_HeadAndOptionsAreNeverBumped(t *testing.T) {
	gens, _ := newTestGenerations()
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, nil, nil)

	for _, m := range []string{http.MethodHead, http.MethodOptions} {
		before, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
		require.NoError(t, err)

		runMiddleware(mw, securedOp(m), ctxWithUser("u1"), http.StatusOK)

		after, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
		require.NoError(t, err)
		assert.Equal(t, before, after, "%s must never bump", m)
	}
}

func TestCacheInvalidation_PublicRouteIsNeverBumped(t *testing.T) {
	gens, fake := newTestGenerations()
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, nil, nil)

	runMiddleware(mw, publicOp(http.MethodPost), ctxWithUser("u1"), http.StatusOK)

	assert.Equal(t, 0, fake.SetNXCalls()+fake.SetCalls(),
		"a public route must never touch the generation store at all")
}

// TestCacheInvalidation_4xxDoesNotBump pins the half of the pre-#1531-L8 behaviour that stays
// correct: a 4xx (validation, auth, business-rule rejection) is assumed to mean the handler never
// reached a commit, so skipping the bump is the right call — it costs nothing and saves the work.
// This is an ASSUMPTION, not a guarantee (see TestCacheInvalidation_5xxBumps below for the case
// where the equivalent assumption about 5xx turned out to be false), but narrowing it further is
// out of scope for #1531 L8.
func TestCacheInvalidation_4xxDoesNotBump(t *testing.T) {
	gens, _ := newTestGenerations()
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, nil, nil)

	before, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
	require.NoError(t, err)

	runMiddleware(mw, securedOp(http.MethodPost), ctxWithUser("u1"), http.StatusBadRequest)

	after, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
	require.NoError(t, err)
	assert.Equal(t, before, after, "a genuine 4xx must not bump — assumed to mean nothing committed")
}

// TestCacheInvalidation_5xxBumps is the #1531 L8 regression. This test previously asserted the
// OPPOSITE — that a 5xx must not bump either — under the name
// TestCacheInvalidation_GenuineFailureDoesNotBump. That assertion enshrined exactly the defect
// this fix closes: a handler that COMMITS and then fails on a later step (a post-commit side
// effect erroring out, surfaced as a 500) changed data with no bump, leaving it stale until the
// namespace TTL. The package doc comment already recognised this exact class of ambiguity for
// Status()==0 (unwritten/panicking handler) and chose to bump anyway, on the reasoning that
// over-invalidating only costs hit rate while under-invalidating serves stale financial data. A
// 5xx is the same ambiguity — "did the handler commit before it failed?" cannot be answered from
// the status code alone — so it now gets the same treatment for the same reason.
func TestCacheInvalidation_5xxBumps(t *testing.T) {
	gens, _ := newTestGenerations()
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, nil, nil)

	before, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
	require.NoError(t, err)

	runMiddleware(mw, securedOp(http.MethodPost), ctxWithUser("u1"), http.StatusInternalServerError)

	after, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
	require.NoError(t, err)
	assert.NotEqual(t, before, after,
		"a 5xx must still bump -- a handler can commit and then fail on a later step, and the "+
			"status code alone cannot prove otherwise (Issue #1531 L8)")
}

func TestCacheInvalidation_2xxAnd3xxBothBump(t *testing.T) {
	gens, _ := newTestGenerations()
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, nil, nil)

	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusNoContent, http.StatusFound} {
		before, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
		require.NoError(t, err)

		runMiddleware(mw, securedOp(http.MethodPost), ctxWithUser("u1"), status)

		after, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
		require.NoError(t, err)
		assert.NotEqual(t, before, after, "status %d must bump", status)
	}
}

// TestCacheInvalidation_UnwrittenStatusIsTreatedAsUnknownAndBumps is THE finding the brief flags
// explicitly: humaecho's Status() pointer initialises to 0 (new(int)), so an unwritten (or
// panicking-before-any-write) handler leaves Status() == 0. Over-invalidating costs hit rate;
// skipping a bump after a committed write is a stale-money bug — so 0 must be treated as
// "unknown, bump anyway", never as "falsy, skip".
func TestCacheInvalidation_UnwrittenStatusIsTreatedAsUnknownAndBumps(t *testing.T) {
	gens, _ := newTestGenerations()
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, nil, nil)

	before, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
	require.NoError(t, err)

	runMiddleware(mw, securedOp(http.MethodPost), ctxWithUser("u1"), 0 /* never call SetStatus */)

	after, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
	require.NoError(t, err)
	assert.NotEqual(t, before, after,
		"Status()==0 (unwritten/unknown) must be treated as UNKNOWN and bump anyway — skipping "+
			"the bump here risks serving stale data after a write that may have committed")
}

func TestCacheInvalidation_BumpsHouseholdWhenLookupSucceeds(t *testing.T) {
	gens, _ := newTestGenerations()
	hh := &fakeHouseholdLookup{household: &domain.Household{ID: "hh1"}, found: true}
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, hh, nil)

	beforeHH, err := gens.Get(context.Background(), cache.ScopeHousehold, "hh1")
	require.NoError(t, err)

	runMiddleware(mw, securedOp(http.MethodPost), ctxWithUser("u1"), http.StatusOK)

	afterHH, err := gens.Get(context.Background(), cache.ScopeHousehold, "hh1")
	require.NoError(t, err)
	assert.NotEqual(t, beforeHH, afterHH, "the user's household must also be bumped, synchronously")
	assert.Equal(t, 1, hh.calls, "the household lookup must run exactly once per mutation")
}

// TestCacheInvalidation_HouseholdLookupFailureStillBumpsUser — a household-lookup failure must
// never suppress the user-scope bump that already succeeded (void-bump semantics: an invalidation
// failure never fails or unwinds an already-committed write's effects).
func TestCacheInvalidation_HouseholdLookupFailureStillBumpsUser(t *testing.T) {
	gens, _ := newTestGenerations()
	hh := &fakeHouseholdLookup{err: errors.New("db down")}
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, hh, nil)

	beforeUser, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
	require.NoError(t, err)

	runMiddleware(mw, securedOp(http.MethodPost), ctxWithUser("u1"), http.StatusOK)

	afterUser, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
	require.NoError(t, err)
	assert.NotEqual(t, beforeUser, afterUser, "a household lookup failure must not suppress the user bump")
}

func TestCacheInvalidation_NotFoundHouseholdDoesNotBumpHousehold(t *testing.T) {
	gens, _ := newTestGenerations()
	hh := &fakeHouseholdLookup{found: false}
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, hh, nil)

	runMiddleware(mw, securedOp(http.MethodPost), ctxWithUser("u1"), http.StatusOK)

	assert.Equal(t, 1, hh.calls)
}

func TestCacheInvalidation_NoUserIDIsSkipped(t *testing.T) {
	gens, fake := newTestGenerations()
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, nil, nil)

	runMiddleware(mw, securedOp(http.MethodPost), ctxWithUser(""), http.StatusOK)

	assert.Equal(t, 0, fake.SetNXCalls()+fake.SetCalls(), "no user id means nothing to bump")
}

// TestCacheInvalidation_NilGenerationsIsANoOp mirrors noopCache.Generations() returning nil
// (disabled cache); the middleware must not panic and must simply pass through.
func TestCacheInvalidation_NilGenerationsIsANoOp(t *testing.T) {
	mw := humamiddleware.NewCacheInvalidationMiddleware(nil, nil, nil)
	called := false
	fc := &fakeHumaCtx{op: securedOp(http.MethodPost), ctx: ctxWithUser("u1")}
	mw(fc, func(huma.Context) { called = true })
	assert.True(t, called, "nil Generations must still call through to the handler")
}

// TestCacheInvalidation_BumpsAfterRequestContextCancelled is Issue #1531 Blocker 2's regression
// test. It models a client disconnecting (or a request deadline elapsing) in the narrow window
// AFTER the handler has already returned (i.e. after any DB commit inside it) but BEFORE this
// middleware's post-commit bump code runs — cancelling the SAME context object the handler,
// and therefore this middleware, both received. Before the fix, gens.Bump(reqCtx, ...) inherited
// that cancellation and the bump was silently lost (cache.Generations.Bump swallows its own
// errors) — a write had committed with no invalidation, stale until the namespace TTL.
//
// ADVERSARIAL VALIDATION: reverted the bumpCtx := context.WithTimeout(context.WithoutCancel(reqCtx),
// bumpTimeout) call to plain reqCtx (the pre-fix shape) — this test FAILED (before == after: the
// bump was lost to the cancelled context). Restored the fix — test PASSED.
func TestCacheInvalidation_BumpsAfterRequestContextCancelled(t *testing.T) {
	client := &ctxCheckingClient{Fake: redisclient.NewFake("cache")}
	gens := cache.NewGenerations(client)
	hh := &fakeHouseholdLookup{household: &domain.Household{ID: "hh1"}, found: true}
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, hh, nil)

	beforeUser, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
	require.NoError(t, err)
	beforeHH, err := gens.Get(context.Background(), cache.ScopeHousehold, "hh1")
	require.NoError(t, err)

	reqCtx, cancel := context.WithCancel(ctxWithUser("u1"))
	fc := &fakeHumaCtx{op: securedOp(http.MethodPost), ctx: reqCtx}
	mw(fc, func(c huma.Context) {
		c.SetStatus(http.StatusOK)
		// Simulate the write having committed, then the client disconnecting (or the request
		// deadline elapsing) the instant after — before this middleware's own post-next code runs.
		cancel()
	})

	afterUser, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
	require.NoError(t, err)
	assert.NotEqual(t, beforeUser, afterUser,
		"the user bump must survive the request context being cancelled after the handler returned")

	afterHH, err := gens.Get(context.Background(), cache.ScopeHousehold, "hh1")
	require.NoError(t, err)
	assert.NotEqual(t, beforeHH, afterHH,
		"the household bump must also survive the cancelled request context")
}

// TestCacheInvalidation_HouseholdBumpedBeforeUser (#1569 round 11 item 3) pins the reordering:
// within the post-next block, the household generation key is written to Redis BEFORE the user's.
// Before that fix the order was user-then-household, which meant a truncated bump budget landed
// on the higher-blast-radius household scope — see cache_invalidation.go's doc comment on the
// reorder for the full reasoning.
//
// ⚠ AMENDED BY #1600, and the amendment is the interesting part. The user scope is now ALSO
// bumped before the response is flushed, so the full SET sequence is:
//
//	user (pre-flush)  ->  household (post-next)  ->  user (post-next fallback)
//
// The literal "household is written first" no longer holds, and it cannot: closing #1600's
// read-after-write window requires the acting user's bump to precede the flush, while the
// household bump depends on a DB lookup that must not be on the response path. Those two
// requirements are simply incompatible.
//
// What #1569 actually protects IS preserved, and that is why this test was amended rather than
// deleted. Its concern was priority under a TRUNCATED budget: if the post-commit work is cut
// short (bumpTimeout, 5s), the scope with the wider blast radius — the household, whose staleness
// hits an uninvolved member — should already have been written. Inside the post-next block that
// ordering is untouched. The pre-flush user bump happens before that budget even starts and is
// strictly ADDITIONAL invalidation, never less, so it cannot cause a scope to go unbumped.
//
// Note also that the ordering never affected a household LOOKUP failure either way: the user
// bump runs unconditionally afterward, so a failed lookup leaves the household stale and the user
// fresh regardless of which SET came first.
func TestCacheInvalidation_HouseholdBumpedBeforeUser(t *testing.T) {
	client := &flakySetClient{Fake: redisclient.NewFake("cache")}
	gens := cache.NewGenerations(client)
	hh := &fakeHouseholdLookup{household: &domain.Household{ID: "hh1"}, found: true}
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, hh, nil)

	runMiddleware(mw, securedOp(http.MethodPost), ctxWithUser("u1"), http.StatusOK)

	userKey := cache.GenerationKey(cache.ScopeTag(cache.ScopeUser, "u1"))
	hhKey := cache.GenerationKey(cache.ScopeTag(cache.ScopeHousehold, "hh1"))
	require.Len(t, client.setCalls, 3,
		"expected the pre-flush user bump, then the post-next household bump, then the post-next "+
			"user fallback — got %v", client.setCalls)
	assert.Equal(t, userKey, client.setCalls[0],
		"the acting user's generation must be written FIRST, before the response is flushed (#1600)")

	// The property #1569 exists for: within the post-next block, household precedes user.
	assert.Equal(t, hhKey, client.setCalls[1],
		"inside the post-next block the household generation must still be written BEFORE the "+
			"user fallback, so a truncated bump budget favours the wider blast radius (#1569)")
	assert.Equal(t, userKey, client.setCalls[2],
		"the post-next user bump remains as a fallback for a failed pre-flush bump")
}

// TestCacheInvalidation_HouseholdBumpRecoversFromTransientSetFailure (#1569 round 11 item 3) is
// the fail-open half, independent of the reorder: a household bump whose FIRST Redis SET attempt
// fails must still land, because BumpWithRetry (not plain Bump) now backs the household half.
// Before this fix, a bare Bump would have logged and silently given up — this test would have
// failed on the "household generation actually changed" assertion.
func TestCacheInvalidation_HouseholdBumpRecoversFromTransientSetFailure(t *testing.T) {
	client := &flakySetClient{Fake: redisclient.NewFake("cache"), failCount: 1}
	gens := cache.NewGenerations(client)
	hh := &fakeHouseholdLookup{household: &domain.Household{ID: "hh1"}, found: true}
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, hh, nil)

	beforeHH, err := gens.Get(context.Background(), cache.ScopeHousehold, "hh1")
	require.NoError(t, err)

	runMiddleware(mw, securedOp(http.MethodPost), ctxWithUser("u1"), http.StatusOK)

	afterHH, err := gens.Get(context.Background(), cache.ScopeHousehold, "hh1")
	require.NoError(t, err)
	assert.NotEqual(t, beforeHH, afterHH,
		"the household bump must recover from a single transient Redis SET failure via retry")
}

// generationWatchingWriter records the user-scope generation token at the instant the FIRST byte
// of the response body is written. That instant is the one that matters: it is when a client can
// observe the write succeeded and issue its next read. Anything the middleware does after this
// point is racing that client.
type generationWatchingWriter struct {
	gens   *cache.Generations
	userID string
	seen   string
	wrote  bool
}

func (w *generationWatchingWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.wrote = true
		w.seen, _ = w.gens.Get(context.Background(), cache.ScopeUser, w.userID)
	}
	return len(p), nil
}

// Issue #1600. The acting user's cache generation must be bumped BEFORE the response body reaches
// the client, not after.
//
// The bug this pins: the middleware's bump work runs after next(ctx) returns, but the handler
// flushes the response INSIDE next(ctx). Between those two moments sit an uncached two-table JOIN
// (GetHouseholdForUser) and a retrying Redis write with 100ms backoff, so a client that reads
// immediately after its write can be served from the old generation — stale financial data.
//
// This was invisible for as long as CI ran on a loaded four-runner Mac Mini, where the client's
// next request never won that race. It reproduced deterministically (2 runs, 2 failures) the
// moment e2e-test moved to a dedicated Ubicloud VM, as
// apps/web/e2e/cache-freshness.auth.spec.ts:75 asserting "the dashboard is serving stale data".
//
// This test is the unit-level detector for it, because the E2E suite takes 16 minutes and cannot
// drive a fix.
func TestCacheInvalidation_UserGenerationIsBumpedBeforeTheResponseIsWritten(t *testing.T) {
	gens, _ := newTestGenerations()
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, nil, nil)

	const userID = "user-1600"
	before, err := gens.Get(context.Background(), cache.ScopeUser, userID)
	require.NoError(t, err)

	watcher := &generationWatchingWriter{gens: gens, userID: userID}
	fc := &fakeHumaCtx{op: securedOp(http.MethodPut), ctx: ctxWithUser(userID), bodyWriter: watcher}

	mw(fc, func(c huma.Context) {
		// Exactly what huma does for a normal response: set the status, then marshal the body
		// into the writer (huma.go:538-540). The handler's DB commit has already happened by the
		// time huma reaches either call.
		c.SetStatus(http.StatusOK)
		_, _ = c.BodyWriter().Write([]byte(`{"ok":true}`))
	})

	require.True(t, watcher.wrote, "the test handler never wrote a body, so this asserts nothing")
	assert.NotEqual(t, before, watcher.seen,
		"the user-scope generation was still the pre-write value at the moment the response body "+
			"was written — a client reading immediately after its own write would be served stale "+
			"data (#1600)")
}

// The pre-flush bump must NOT fire for a genuine 4xx, matching the policy the post-next block has
// always applied: a 4xx is assumed to mean the handler never reached a commit, so bumping only
// costs hit rate. Pinned separately because the pre-flush trigger sees the status at SetStatus
// time and could easily be written to bump unconditionally.
func TestCacheInvalidation_4xxIsNotBumpedBeforeTheResponseIsWritten(t *testing.T) {
	gens, _ := newTestGenerations()
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, nil, nil)

	const userID = "user-1600-4xx"
	before, err := gens.Get(context.Background(), cache.ScopeUser, userID)
	require.NoError(t, err)

	watcher := &generationWatchingWriter{gens: gens, userID: userID}
	fc := &fakeHumaCtx{op: securedOp(http.MethodPut), ctx: ctxWithUser(userID), bodyWriter: watcher}

	mw(fc, func(c huma.Context) {
		c.SetStatus(http.StatusUnprocessableEntity)
		_, _ = c.BodyWriter().Write([]byte(`{"error":"nope"}`))
	})

	require.True(t, watcher.wrote)
	assert.Equal(t, before, watcher.seen,
		"a genuine 4xx must not bump the generation before the response is written")

	after, err := gens.Get(context.Background(), cache.ScopeUser, userID)
	require.NoError(t, err)
	assert.Equal(t, before, after, "a genuine 4xx must not bump the generation at all")
}

// A handler that writes a body without ever calling SetStatus (the Status()==0 / unknown case the
// package doc concedes) must still be bumped before the write. Over-invalidating costs hit rate;
// under-invalidating serves stale money.
func TestCacheInvalidation_UnknownStatusIsBumpedBeforeTheResponseIsWritten(t *testing.T) {
	gens, _ := newTestGenerations()
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, nil, nil)

	const userID = "user-1600-unknown"
	before, err := gens.Get(context.Background(), cache.ScopeUser, userID)
	require.NoError(t, err)

	watcher := &generationWatchingWriter{gens: gens, userID: userID}
	fc := &fakeHumaCtx{op: securedOp(http.MethodPost), ctx: ctxWithUser(userID), bodyWriter: watcher}

	mw(fc, func(c huma.Context) {
		// No SetStatus at all — only a body write.
		_, _ = c.BodyWriter().Write([]byte(`{"ok":true}`))
	})

	require.True(t, watcher.wrote)
	assert.NotEqual(t, before, watcher.seen,
		"an unwritten/unknown status must still bump before the response is written")
}

// flushRecordingWriter is an io.Writer that also implements http.Flusher, so a test can prove the
// middleware's body-writer wrapper stays transparent to a streaming caller.
type flushRecordingWriter struct {
	flushed int
}

func (w *flushRecordingWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *flushRecordingWriter) Flush()                      { w.flushed++ }

// The wrapper added for #1600 sits between huma and the real response writer. huma's own
// StreamResponse example does `if f, ok := writer.(http.Flusher); ok { f.Flush() }`, so a wrapper
// that implements only io.Writer would make that assertion fail and silently disable flushing on
// any future streaming mutation — a stalled response with no error to trace. Nothing needs this
// today; the test exists so that stays true.
func TestCacheInvalidation_BodyWriterWrapperForwardsFlush(t *testing.T) {
	gens, _ := newTestGenerations()
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, nil, nil)

	inner := &flushRecordingWriter{}
	fc := &fakeHumaCtx{op: securedOp(http.MethodPost), ctx: ctxWithUser("u-flush"), bodyWriter: inner}

	mw(fc, func(c huma.Context) {
		w := c.BodyWriter()
		_, _ = w.Write([]byte("chunk"))
		f, ok := w.(http.Flusher)
		require.True(t, ok, "the wrapped body writer must still satisfy http.Flusher")
		f.Flush()
	})

	assert.Equal(t, 1, inner.flushed, "Flush must reach the underlying writer, not be swallowed")
}
