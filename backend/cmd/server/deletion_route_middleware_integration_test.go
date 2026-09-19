package main

// Issue #1560 acceptance criteria 2 and 4, and the check that was missing when #1560 shipped.
//
// #1560 was: `operations.RegisterDeletionRoutes` ran before any `humaAPI.UseMiddleware(...)`, and
// huma snapshots the middleware chain at `huma.Register` time — so `requestDeletion` ran with NO
// middleware and returned 401 forever. The fix moved registration below the middleware block.
//
// The problem with how that was verified: nothing anywhere issued a request to a REAL registered
// route through a REAL middleware chain. That category was empty for every route in the repository.
// The fix was covered only by (a) an AST scan of main.go's line ordering and (b) a synthetic probe
// operation. Both are real evidence about ORDERING; neither exercises the deletion endpoint. A gap
// analysis then found a SECOND 401 cause on the same endpoint (MCP-origin requests carry no session
// claims — see inputs/auth.go) that ordering checks could never have surfaced, which is the
// concrete argument for this test existing.
//
// So this test asserts the property the acceptance criteria actually asked for: a request reaches
// the real `RegisterDeletionRoutes` handler through the real read-only and cache-invalidation
// middlewares, the read-only middleware permits it (criterion 2 — previously "proven" only against
// a route the test itself invented), and a cache generation bump is genuinely issued.
//
// WHAT IS STUBBED, AND WHY: only Clerk's JWT verification, replaced by an injector that sets the
// same three context keys the real middleware sets (UserIDKey, ClerkUserIDKey, ClerkClaimsKey) —
// verifying a Clerk signature needs external crypto and is separately tested in
// internal/huma/middleware/clerk_*_test.go. Everything downstream is production code: the real
// read-only middleware, the real cache-invalidation middleware, the real route registration, the
// real input resolvers. Deliberately NOT integration-tagged: it needs no database and no Redis
// (redisclient.Fake observes the bump), so it runs in a plain `go test ./...` — the default suite is
// where a regression guard for a shipped production bug belongs.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	humaconfig "github.com/maxnussbaum/BoostedBudget/internal/huma"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/operations"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// stubDeletionService is the minimum of services.DeletionService that RequestDeletion needs. Only
// RequestDeletion is exercised; the rest satisfy the interface.
type stubDeletionService struct{ called bool }

func (s *stubDeletionService) RequestDeletion(_ context.Context, _, _ string) (string, time.Time, error) {
	s.called = true
	return "cancel-token", time.Now().Add(30 * 24 * time.Hour), nil
}

// The remainder satisfy services.DeletionService; only RequestDeletion is exercised here.
func (s *stubDeletionService) CancelDeletion(context.Context, string) error { return nil }
func (s *stubDeletionService) ExecuteDeletion(context.Context, string, repository.DeletionClaimKind) error {
	return nil
}
func (s *stubDeletionService) SetPlaidCleanupService(services.PlaidItemCleanupService)        {}
func (s *stubDeletionService) SetPaddleCleanupService(services.PaddleDeletionCleaner)         {}
func (s *stubDeletionService) SetAnalytics(phclient.Analytics)                                {}
func (s *stubDeletionService) SetHouseholdReader(services.HouseholdReader)                    {}
func (s *stubDeletionService) SetHouseholdInviteRevoker(services.HouseholdInviteRevoker)      {}
func (s *stubDeletionService) SetDB(repository.DB)                                            {}
func (s *stubDeletionService) SetWorkClaimsRepo(repository.WorkClaimsRepository)              {}
func (s *stubDeletionService) SetSubscriptionCleanupGuard(services.SubscriptionCleanupGuard)  {}
func (s *stubDeletionService) SetRevenueCatCleanupService(services.RevenueCatDeletionCleaner) {}
func (s *stubDeletionService) SetCache(cache.Purger, *cache.Generations, queue.JobQueue)      {}

// clerkStub injects exactly the context keys internal/huma/middleware/clerk.go's attach() sets on
// the JWT path. UserIDKey matters as much as the claims: the cache-invalidation middleware gates its
// bump on it, so a test that sets only the Clerk keys would silently observe no bump and "pass".
func clerkStub(userID, clerkUserID, email string) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		c := context.WithValue(ctx.Context(), inputs.UserIDKey, userID)
		c = context.WithValue(c, inputs.ClerkUserIDKey, clerkUserID)
		c = context.WithValue(c, inputs.ClerkClaimsKey, &clerk.SessionClaims{
			Custom: map[string]interface{}{"email": email},
		})
		// The effective subscription status the read-only middleware evaluates. Sourced from a
		// header so one API build can serve both the active and the read-only (lapsed) case —
		// mirroring internal/huma/middleware/read_only_test.go's X-Test-Subscription-Status idiom.
		if status := ctx.Header("X-Test-Subscription-Status"); status != "" {
			c = context.WithValue(c, inputs.EffectiveSubscriptionStatusKey, status)
		}
		next(huma.WithContext(ctx, c))
	}
}

// buildDeletionAPI assembles the API the way cmd/server/main.go does — humaecho.NewV4 with the real
// humaconfig, middlewares applied, THEN routes registered. registerBeforeMiddleware inverts that
// order to reproduce #1560.
func buildDeletionAPI(t *testing.T, registerBeforeMiddleware bool) (*echo.Echo, *stubDeletionService, *redisclient.Fake) {
	t.Helper()
	svc := &stubDeletionService{}
	e, fake := buildDeletionAPIWithService(t, svc, "user-uuid-1", "user_clerk_1", "someone@example.com", registerBeforeMiddleware)
	return e, svc, fake
}

// buildDeletionAPIWithService is buildDeletionAPI generalised over the service under test and the
// identity the Clerk stub injects. It exists so the integration variant in
// issue1560_deletion_endpoint_real_service_integration_test.go can drive the SAME wiring — same
// humaconfig, same middleware order, same real route registration — with a REAL
// services.DeletionService backed by a real ProfileRepository and a real Postgres, rather than
// standing up a parallel harness that could silently drift from this one.
func buildDeletionAPIWithService(
	t *testing.T,
	svc services.DeletionService,
	userID, clerkUserID, email string,
	registerBeforeMiddleware bool,
) (*echo.Echo, *redisclient.Fake) {
	t.Helper()

	e := echo.New()
	api := humaecho.NewV4(e, humaconfig.NewAPIConfig("test", "test"))

	handler := operations.NewDeletionHandler(svc)

	fake := redisclient.NewFake("cache")
	gens := cache.NewGenerations(fake)

	register := func() {
		operations.RegisterDeletionRoutes(api, handler)
		// A secured mutating route that is NOT in readOnlyPrefixAllowedPaths. Without it, asserting
		// "the read-only middleware permitted /api/v1/user/delete" is unfalsifiable: the middleware
		// permits that path, so its ABSENCE looks identical to its presence. This control makes the
		// middleware's participation observable — verified by probe: deleting the
		// NewReadOnlyMiddleware line leaves the deletion assertion green but flips this one.
		huma.Register(api, huma.Operation{
			OperationID: "controlNonWhitelistedMutation",
			Method:      http.MethodPost,
			Path:        "/api/v1/control/not-whitelisted",
			Security:    []map[string][]string{{"clerk": {}}},
		}, func(context.Context, *struct{}) (*struct{ Body struct{} }, error) {
			return &struct{ Body struct{} }{}, nil
		})
	}

	if registerBeforeMiddleware {
		register()
	}
	api.UseMiddleware(clerkStub(userID, clerkUserID, email))
	api.UseMiddleware(humamiddleware.NewReadOnlyMiddleware(nil))
	api.UseMiddleware(humamiddleware.NewCacheInvalidationMiddleware(gens, nil, nil))
	if !registerBeforeMiddleware {
		register()
	}

	return e, fake
}

func postDeletion(t *testing.T, e *echo.Echo) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/delete", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// TestDeletionRoute_ReachesHandlerThroughRealMiddlewareChain is #1560 criteria 2 and 4.
func TestDeletionRoute_ReachesHandlerThroughRealMiddlewareChain(t *testing.T) {
	e, svc, fake := buildDeletionAPI(t, false)

	rec := postDeletion(t, e)

	require.Less(t, rec.Code, 400,
		"POST /api/v1/user/delete must reach the handler through the real middleware chain; got %d "+
			"with body %s. A 401 here is #1560 reproducing: huma snapshots the middleware chain at "+
			"Register time, so a route registered too early gets no Clerk context at all.",
		rec.Code, rec.Body.String())

	assert.True(t, svc.called,
		"the real DeletionService.RequestDeletion must have been invoked — a 2xx produced without "+
			"reaching the service would mean the route is answered by something else entirely")

	// And the write is invalidated: a real generation bump was issued by production middleware, with
	// no test-authored Bump anywhere in this test.
	assert.Positive(t, fake.SetCalls()+fake.SetNXCalls(),
		"the cache-invalidation middleware must have issued a generation bump for this authenticated "+
			"mutation; zero writes to the generation store means the middleware did not run")

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "response must be the handler's JSON")
}

// TestDeletionRoute_ReadOnlyMiddlewareAppliesAndDiscriminates is #1560 criterion 2, done properly.
//
// A weaker version of this ("assert the deletion POST is not 403") passes whether or not the
// read-only middleware is in the chain, because the middleware PERMITS that path —
// readOnlyPrefixAllowedPaths lists /api/v1/user/delete precisely so a lapsed subscriber can still
// delete their account. Before #1560 that entry was unreachable config: the middleware never ran for
// this route at all, and nothing noticed.
//
// So this asserts the middleware DISCRIMINATES for a read-only user: the whitelisted deletion path
// is allowed, and a non-whitelisted mutating path is blocked. Only a middleware that is actually in
// the chain can produce both outcomes.
func TestDeletionRoute_ReadOnlyMiddlewareAppliesAndDiscriminates(t *testing.T) {
	e, _, _ := buildDeletionAPI(t, false)

	// "inactive" is read-only per domain.IsReadOnlySubscription.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/delete", nil)
	req.Header.Set("X-Test-Subscription-Status", "inactive")
	allowed := httptest.NewRecorder()
	e.ServeHTTP(allowed, req)

	blockedReq := httptest.NewRequest(http.MethodPost, "/api/v1/control/not-whitelisted", nil)
	blockedReq.Header.Set("X-Test-Subscription-Status", "inactive")
	blocked := httptest.NewRecorder()
	e.ServeHTTP(blocked, blockedReq)

	assert.Less(t, allowed.Code, 400,
		"a read-only (lapsed) user MUST still be able to request account deletion — that is why "+
			"/api/v1/user/delete is in readOnlyPrefixAllowedPaths; got %d: %s",
		allowed.Code, allowed.Body.String())
	assert.Equal(t, http.StatusForbidden, blocked.Code,
		"a read-only user must be BLOCKED on a non-whitelisted mutating route. If this is not 403 the "+
			"read-only middleware is not in the chain, and the assertion above proves nothing")
}

// TestDeletionRoute_RegisteredBeforeMiddleware_Reproduces1560 is the falsification: it asserts the
// test above can actually fail, by rebuilding the API in the pre-#1560 order. If this ever passes,
// either huma stopped snapshotting the chain or the test above proves nothing.
func TestDeletionRoute_RegisteredBeforeMiddleware_Reproduces1560(t *testing.T) {
	e, svc, fake := buildDeletionAPI(t, true)

	rec := postDeletion(t, e)

	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"registering the route BEFORE UseMiddleware must reproduce #1560's unconditional 401 — that "+
			"is the bug, and it is what makes the test above meaningful")
	assert.False(t, svc.called,
		"the deletion service must never be reached when the auth middleware was snapshotted out")
	assert.Zero(t, fake.SetCalls()+fake.SetNXCalls(),
		"no bump can happen when the cache-invalidation middleware never ran either")
}

// TestDeletionRoute_RealClerkMiddleware_RefusesUnauthenticated closes a gap in the test above,
// raised by adversarial review.
//
// The test above injects the Clerk context keys directly rather than running the real
// NewClerkMiddleware, because verifying a Clerk signature needs external crypto. That is a
// legitimate stub — but it means a production Clerk/identity regression that 401s every request
// would leave that test GREEN. It proves the route is reachable THROUGH the chain and that the
// downstream middlewares behave; it does not prove production authentication works.
//
// This companion runs the REAL NewClerkMiddleware (with a test secret and a stub identity repo, the
// pattern from internal/huma/middleware/clerk_subscription_status_test.go) and asserts an
// unauthenticated request is REFUSED. Together the two bracket the property: the real Clerk
// middleware is in the chain and enforcing, and once a request carries valid auth context it reaches
// the handler. Neither alone is sufficient, and #1560's criterion 1 — a 200 from a running server
// with a genuine Clerk JWT — remains the only thing that closes the middle.
func TestDeletionRoute_RealClerkMiddleware_RefusesUnauthenticated(t *testing.T) {
	e := echo.New()
	api := humaecho.NewV4(e, humaconfig.NewAPIConfig("test", "test"))

	svc := &stubDeletionService{}
	fake := redisclient.NewFake("cache")

	// The REAL Clerk middleware. No identity repo interaction should be reached: with no bearer token
	// and no MCP headers, it must refuse before resolving anything.
	api.UseMiddleware(humamiddleware.NewClerkMiddleware(
		"sk_test_secret", nil, nil, nil, nil, nil,
	))
	api.UseMiddleware(humamiddleware.NewReadOnlyMiddleware(nil))
	api.UseMiddleware(humamiddleware.NewCacheInvalidationMiddleware(cache.NewGenerations(fake), nil, nil))
	operations.RegisterDeletionRoutes(api, operations.NewDeletionHandler(svc))

	// A bearer token that is present but invalid, NOT a bare unauthenticated request. That
	// distinction is what makes this falsifiable: with no Authorization header at all, the input
	// resolver would 401 on a missing UserIDKey whether or not the Clerk middleware ran — so the
	// test would pass with Clerk deleted from the chain, proving nothing. Probed and confirmed.
	// A malformed token forces Clerk itself to answer, and its message is attributable to it.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/delete", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-jwt")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400,
		"a request with an invalid bearer token must be refused; got %d: %s. A 2xx here would mean the "+
			"deletion endpoint is reachable without valid authentication.", rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Invalid or expired token",
		"the refusal must come from the REAL Clerk middleware, which is the only thing that can produce "+
			"this message. If it instead reads like the input resolver's 'User not authenticated', Clerk "+
			"is not in the chain and this test would otherwise pass vacuously — the same trap the "+
			"read-only assertion above had to be rewritten to avoid. Body: %s", rec.Body.String())
	assert.False(t, svc.called,
		"the deletion service must never be reached by an unauthenticated request")
	assert.Zero(t, fake.SetCalls()+fake.SetNXCalls(),
		"no cache generation bump may be issued for a request that was refused")
}
