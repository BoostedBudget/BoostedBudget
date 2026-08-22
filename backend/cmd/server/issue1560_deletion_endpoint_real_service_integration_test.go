//go:build integration

package main

// Issue #1560 acceptance criterion 4: "At least one E2E or integration test exercises the real
// endpoint rather than mocking it."
//
// WHY THIS FILE EXISTS ALONGSIDE deletion_route_middleware_integration_test.go. That file drives
// POST /api/v1/user/delete through a real humaecho API and the real Clerk/read-only/cache
// middlewares — which is genuine evidence about the ROUTE — but it hands
// operations.NewDeletionHandler a *stub* services.DeletionService. So nothing there exercises what
// the endpoint actually DOES: a request could return 200 with the user's row untouched and every
// assertion in that file would still pass. An independent audit of #1560 called criterion 4 "met
// only weakly" for exactly that reason.
//
// This file closes it: one HTTP request goes through the real middleware chain, into the real
// registered route, into the real operations.DeletionHandler, into a REAL services.DeletionService,
// into a REAL repository.ProfileRepository, against a REAL Postgres — and the assertion is read
// back FROM THE DATABASE.
//
// WHAT IS REAL AND WHAT IS STUBBED:
//
//	real  — the huma API + humaconfig, the middleware chain, RegisterDeletionRoutes, the input
//	        resolvers, services.NewDeletionService, repository.NewProfileRepository against Postgres,
//	        repository.NewAuditLogRepository against Postgres (the audit trail row is written for
//	        real, not swallowed by a stub — the defect #1505 closed).
//	stub  — only the three genuinely-outbound dependencies: the email service (production's own
//	        services.NewEmailServiceMock, i.e. mockMode, no SendGrid call), Clerk's backend API
//	        (never touched by RequestDeletion at all), and the Plaid cleanup service (nil, likewise
//	        unused by RequestDeletion). Clerk JWT verification is injected the same way the sibling
//	        file documents, because verifying a real signature needs external crypto.
//
// UNLIKE the sibling file, this one IS build-tagged `integration`: it needs Postgres. The sibling
// is deliberately untagged so a regression guard for a shipped production bug runs in the default
// `go test ./...`; that is not available here, so this runs under `-tags integration -p 1`
// alongside the rest of the DB-backed suite.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/database"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// --- test doubles for the outbound-only dependencies ---

// stubClerkBackend stands in for services.ClerkService. RequestDeletion never calls it; it exists
// only because NewDeletionService requires a non-nil value. Every method fails loudly rather than
// returning nil, so a future change that starts calling Clerk from RequestDeletion shows up here
// instead of silently making a network call in the test suite.
type stubClerkBackend struct{ t *testing.T }

func (s stubClerkBackend) DeleteUser(context.Context, string) error {
	s.t.Error("Clerk DeleteUser must not be called by RequestDeletion")
	return nil
}

func (s stubClerkBackend) UpdateUserMetadata(context.Context, string, map[string]interface{}) error {
	s.t.Error("Clerk UpdateUserMetadata must not be called by RequestDeletion")
	return nil
}

func (s stubClerkBackend) UpdateUserEmail(context.Context, string, string) error {
	s.t.Error("Clerk UpdateUserEmail must not be called by RequestDeletion")
	return nil
}

// passthroughEncryption is a crypto.EncryptionService that does not encrypt. Field-level encryption
// fidelity is irrelevant to this test — the assertion reads deletion_requested_at, which is never
// encrypted — and a passthrough keeps the fixture legible in the database while a failure is being
// diagnosed. Mirrors internal/repository's own plainPassthroughEncryptionService.
type passthroughEncryption struct{}

func (passthroughEncryption) Encrypt(plaintext string) (string, error)  { return plaintext, nil }
func (passthroughEncryption) Decrypt(ciphertext string) (string, error) { return ciphertext, nil }

// noopMarkForDeletionRepo wraps a real ProfileRepository and neuters ONLY MarkForDeletion. It is the
// falsification instrument for the non-transactional path (see
// TestDeletionEndpoint_NeuteredWrite_NonTransactional_LeavesRowUnmarked): with the write removed the
// route, the middleware chain, the handler and the service all still behave identically and the
// endpoint still answers 200 — so a test that passes against this proves only that the route is
// wired, which the sibling file already covered.
type noopMarkForDeletionRepo struct {
	repository.ProfileRepository
}

func (noopMarkForDeletionRepo) MarkForDeletion(context.Context, string, time.Time) error {
	return nil
}

// noCommitDB wraps the real pool and hands out transactions whose Commit is a no-op, so the
// service's own `defer tx.Rollback` discards everything it wrote. It is the falsification instrument
// for the PRODUCTION path: requestDeletionTransactional does NOT use the injected profileRepo — it
// builds `repository.NewProfileRepository(tx, nil)` from its own transaction — so neutering the
// injected repository cannot reach that path. Neutering the commit can.
type noCommitDB struct{ *pgxpool.Pool }

func (d noCommitDB) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return noCommitTx{tx}, nil
}

type noCommitTx struct{ pgx.Tx }

func (noCommitTx) Commit(context.Context) error { return nil }

// --- fixtures ---

// connectDeletionTestDB opens the shared integration Postgres via the production pool constructor
// (so session settings match production, as internal/repository's helper documents).
func connectDeletionTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping the real-endpoint deletion integration test")
	}
	pool, err := database.NewPoolFromURL(context.Background(), dsn)
	require.NoError(t, err, "connect to the integration test database")
	t.Cleanup(pool.Close)
	return pool
}

// createDeletionFixtureUser inserts one live user through the real repository and registers cleanup.
//
// Cleanup deliberately uses context.Background(), NOT t.Context(): t.Context() is cancelled before
// t.Cleanup runs, so the DELETEs would silently fail and leak rows into every other package sharing
// this database. It removes the audit_logs row too, because the audit write on this path is REAL.
func createDeletionFixtureUser(t *testing.T, pool *pgxpool.Pool, label string) *domain.User {
	t.Helper()

	enc, err := appcrypto.NewFieldEncryptor(passthroughEncryption{}, []byte("12345678901234567890123456789012"))
	require.NoError(t, err)
	repo := repository.NewProfileRepository(pool, enc)

	suffix := fmt.Sprintf("%s_%d", label, time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_del_" + suffix,
		Email:              "del-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))

	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, "DELETE FROM audit_logs WHERE resource_type = 'user' AND resource_id = $1", user.ID)
		_, _ = pool.Exec(bg, "DELETE FROM users WHERE id = $1", user.ID)
	})

	// A fixture that arrived already marked would make the post-request assertion vacuous.
	requireDeletionState(t, pool, user.ID, false)
	return user
}

// requireDeletionState asserts, from the database, whether the user row is marked for deletion.
func requireDeletionState(t *testing.T, pool *pgxpool.Pool, userID string, wantMarked bool) {
	t.Helper()
	var requestedAt, scheduledAt *time.Time
	require.NoError(t, pool.QueryRow(context.Background(),
		"SELECT deletion_requested_at, deletion_scheduled_at FROM users WHERE id = $1", userID,
	).Scan(&requestedAt, &scheduledAt), "read the deletion columns back for user %s", userID)

	if wantMarked {
		require.NotNil(t, requestedAt,
			"users.deletion_requested_at must be set: the HTTP request reached the real DeletionService "+
				"and its MarkForDeletion write committed. A NULL here with a 2xx response is precisely the "+
				"failure mode the stub-service test in deletion_route_middleware_integration_test.go cannot "+
				"detect — the route answers, and nothing happens.")
		require.NotNil(t, scheduledAt, "users.deletion_scheduled_at must be set alongside deletion_requested_at")
		assert.WithinDuration(t, time.Now(), *requestedAt, time.Minute,
			"deletion_requested_at must have been written by THIS request, not left over from a fixture")
		assert.True(t, scheduledAt.After(time.Now()),
			"deletion_scheduled_at must be in the future (the grace period); got %s", scheduledAt)
		return
	}
	assert.Nil(t, requestedAt, "users.deletion_requested_at must still be NULL for user %s", userID)
	assert.Nil(t, scheduledAt, "users.deletion_scheduled_at must still be NULL for user %s", userID)
}

// realDeletionService builds the production DeletionService with only the outbound dependencies
// stubbed. db is what SetDB receives — the real pool on the production path, a noCommitDB when the
// write is being neutered for falsification. Passing nil skips SetDB entirely, which selects the
// service's non-transactional fallback branch (`if s.db != nil` in RequestDeletion) — the only
// branch that uses the INJECTED profileRepo.
func realDeletionService(t *testing.T, pool *pgxpool.Pool, profileRepo repository.ProfileRepository, db repository.DB) services.DeletionService {
	t.Helper()
	svc := services.NewDeletionService(
		profileRepo,
		services.NewEmailServiceMock("", "", "http://localhost:5173"), // production's own no-network mock
		stubClerkBackend{t: t},
		repository.NewAuditLogRepository(pool), // REAL audit trail write, against the test DB
		nil,                                   // plaidCleanupSvc: never used by RequestDeletion
		30,
		"integration-test-deletion-token-secret",
	)
	if db != nil {
		svc.SetDB(db)
	}
	return svc
}

func newRealProfileRepo(t *testing.T, pool *pgxpool.Pool) repository.ProfileRepository {
	t.Helper()
	enc, err := appcrypto.NewFieldEncryptor(passthroughEncryption{}, []byte("12345678901234567890123456789012"))
	require.NoError(t, err)
	return repository.NewProfileRepository(pool, enc)
}

// --- the tests ---

// TestDeletionEndpoint_RealService_MarksUserForDeletionInDatabase is #1560 criterion 4 on the
// PRODUCTION path: main.go always calls deletionService.SetDB(db), so RequestDeletion takes its
// transactional branch (requestDeletionTransactional — household lock/check, invite revocation, then
// MarkForDeletion, all in one transaction). The fixture user owns no household, so the guard finds
// nothing and the sequence proceeds to the write.
func TestDeletionEndpoint_RealService_MarksUserForDeletionInDatabase(t *testing.T) {
	pool := connectDeletionTestDB(t)
	user := createDeletionFixtureUser(t, pool, "prodpath")
	// A second live user nobody asked to delete. Without it, "the row is marked" could in principle
	// pass against a query or a write that touches every row; with it, the assertion is shown to be
	// specific to the authenticated caller.
	bystander := createDeletionFixtureUser(t, pool, "bystander")

	svc := realDeletionService(t, pool, newRealProfileRepo(t, pool), pool)
	e, fake := buildDeletionAPIWithService(t, svc, user.ID, user.ClerkUserID, user.Email, false)

	rec := postDeletion(t, e)

	require.Less(t, rec.Code, 400,
		"POST /api/v1/user/delete must reach the real service through the real middleware chain; got %d: %s",
		rec.Code, rec.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "response must be the handler's JSON")
	assert.NotEmpty(t, body["cancellation_token"],
		"the real service must have minted a cancellation JWT; an empty token means the response was "+
			"synthesised by something other than services.DeletionService")
	assert.NotEmpty(t, body["scheduled_at"], "the real service must have returned a scheduled deletion date")

	// THE assertion: the database, not the response.
	requireDeletionState(t, pool, user.ID, true)
	requireDeletionState(t, pool, bystander.ID, false)

	// The real audit trail entry was written by the real repository, not swallowed by a stub.
	var auditCount int
	require.NoError(t, pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM audit_logs WHERE action = 'user.deletion_requested' AND resource_id = $1", user.ID,
	).Scan(&auditCount))
	assert.Equal(t, 1, auditCount,
		"the GDPR audit trail must contain exactly one user.deletion_requested entry for this user")

	// And the production cache-invalidation middleware still bumped, as the sibling file asserts —
	// re-checked here because this is the first time it runs over a request that really mutated data.
	assert.Positive(t, fake.SetCalls()+fake.SetNXCalls(),
		"the cache-invalidation middleware must have issued a generation bump for this real mutation")
}

// TestDeletionEndpoint_RealService_NonTransactionalFallback_MarksUserForDeletion covers the other
// branch of RequestDeletion: with no db wired, the service calls the INJECTED
// repository.ProfileRepository directly. main.go wires SetDB, so this is the documented fallback
// rather than the shipped path — but it is the branch the injected repository participates in at
// all, and the one the falsification below neuters.
func TestDeletionEndpoint_RealService_NonTransactionalFallback_MarksUserForDeletion(t *testing.T) {
	pool := connectDeletionTestDB(t)
	user := createDeletionFixtureUser(t, pool, "fallback")

	svc := realDeletionService(t, pool, newRealProfileRepo(t, pool), nil) // nil => non-transactional branch
	e, _ := buildDeletionAPIWithService(t, svc, user.ID, user.ClerkUserID, user.Email, false)

	rec := postDeletion(t, e)
	require.Less(t, rec.Code, 400, "got %d: %s", rec.Code, rec.Body.String())

	requireDeletionState(t, pool, user.ID, true)
}

// TestDeletionEndpoint_NeuteredWrite_NonTransactional_LeavesRowUnmarked is the falsification for the
// non-transactional branch: the SAME route, middleware chain, handler and service, with only
// ProfileRepository.MarkForDeletion neutered. The endpoint still answers 2xx and still mints a
// cancellation token — which is exactly why a 2xx is not evidence, and why the database read in the
// tests above is the assertion that carries the criterion.
func TestDeletionEndpoint_NeuteredWrite_NonTransactional_LeavesRowUnmarked(t *testing.T) {
	pool := connectDeletionTestDB(t)
	user := createDeletionFixtureUser(t, pool, "neutered")

	neutered := noopMarkForDeletionRepo{ProfileRepository: newRealProfileRepo(t, pool)}
	svc := realDeletionService(t, pool, neutered, nil)
	e, _ := buildDeletionAPIWithService(t, svc, user.ID, user.ClerkUserID, user.Email, false)

	rec := postDeletion(t, e)
	require.Less(t, rec.Code, 400,
		"the neutered service must still answer 2xx — that is the whole point: a green route proves nothing")

	requireDeletionState(t, pool, user.ID, false)
}

// TestDeletionEndpoint_NeuteredWrite_ProductionPath_LeavesRowUnmarked is the falsification for the
// PRODUCTION (transactional) path. It cannot reuse noopMarkForDeletionRepo: requestDeletionTransactional
// builds its own repository from its own transaction and never touches the injected one. So the
// neuter here is a no-op Commit, which lets the service's unconditional `defer tx.Rollback` discard
// the write. Again the endpoint answers 2xx; again only the database read notices.
func TestDeletionEndpoint_NeuteredWrite_ProductionPath_LeavesRowUnmarked(t *testing.T) {
	pool := connectDeletionTestDB(t)
	user := createDeletionFixtureUser(t, pool, "neuteredtx")

	svc := realDeletionService(t, pool, newRealProfileRepo(t, pool), noCommitDB{Pool: pool})
	e, _ := buildDeletionAPIWithService(t, svc, user.ID, user.ClerkUserID, user.Email, false)

	rec := postDeletion(t, e)
	require.Less(t, rec.Code, 400,
		"a discarded transaction is invisible to the HTTP layer — the endpoint still answers 2xx")

	requireDeletionState(t, pool, user.ID, false)
}
