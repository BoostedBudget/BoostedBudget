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
func (f *fakeHumaCtx) BodyWriter() io.Writer                      { panic("not used") }

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

func TestCacheInvalidation_GenuineFailureDoesNotBump(t *testing.T) {
	gens, _ := newTestGenerations()
	mw := humamiddleware.NewCacheInvalidationMiddleware(gens, nil, nil)

	before, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
	require.NoError(t, err)

	runMiddleware(mw, securedOp(http.MethodPost), ctxWithUser("u1"), http.StatusBadRequest)

	after, err := gens.Get(context.Background(), cache.ScopeUser, "u1")
	require.NoError(t, err)
	assert.Equal(t, before, after, "a genuine 4xx must not bump — nothing committed")

	// 5xx must not bump either.
	before2, _ := gens.Get(context.Background(), cache.ScopeUser, "u1")
	runMiddleware(mw, securedOp(http.MethodPost), ctxWithUser("u1"), http.StatusInternalServerError)
	after2, _ := gens.Get(context.Background(), cache.ScopeUser, "u1")
	assert.Equal(t, before2, after2, "a genuine 5xx must not bump either")
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
