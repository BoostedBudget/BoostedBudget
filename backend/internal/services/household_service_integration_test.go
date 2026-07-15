package services

// household_service_integration_test.go — end-to-end proof of HouseholdService.AcceptInvite
// against a real Postgres instance: real encryption round-trip, real constraints, real
// FOR UPDATE locking. Complements the pgxmock-based unit tests in household_service_test.go
// (which verify orchestration/branching) and the repository-layer concurrency proof in
// internal/repository/household_accept_concurrency_integration_test.go.
//
// Per project rule: pgxmock never catches constraint/type errors — this is the real-DB
// counterpart (see memory feedback_pgxmock_integration_coverage.md).
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/ -run TestHouseholdServiceIntegration -v

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/require"
)

func setupHouseholdServiceIntegrationDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// integrationServiceFixture bundles the service and repos needed by integration tests,
// reducing setup duplication across the three concurrent AcceptInvite scenarios.
type integrationServiceFixture struct {
	pool        *pgxpool.Pool
	profileRepo repository.ProfileRepository
	// svc is the concrete type (not the HouseholdService interface) so integration
	// tests can also exercise the narrow HouseholdAccountShareService methods
	// (#1368 — Phase 4) without adding them to the broad HouseholdService interface.
	svc *householdService
}

func setupHouseholdServiceIntegration(t *testing.T) *integrationServiceFixture {
	t.Helper()
	pool := setupHouseholdServiceIntegrationDB(t)
	enc := newTestFieldEncryptor(t)

	profileRepo := repository.NewProfileRepository(pool, enc)
	householdRepo := repository.NewHouseholdRepository(pool)
	inviteRepo := repository.NewHouseholdInviteRepository(pool, enc)
	shareRepo := repository.NewHouseholdShareRepository(pool)
	entRepo := repository.NewEntitlementRepository(pool)
	tierConfig := testTierConfig(t)
	entSvc := NewHouseholdEntitlementService(entRepo, profileRepo, householdRepo, tierConfig)
	emailSvc := NewEmailServiceMock("", "", "https://app.example.com")

	svc := NewHouseholdService(pool, householdRepo, inviteRepo, shareRepo, entRepo, entSvc, profileRepo, tierConfig, emailSvc, enc)

	return &integrationServiceFixture{
		pool:        pool,
		profileRepo: profileRepo,
		svc:         svc.(*householdService),
	}
}

// insertIntegrationTestUser creates a real user row via the real ProfileRepository
// (so email encryption/blind-indexing round-trips exactly as production does). Callers
// are responsible for cleanup (see the DELETE FROM users t.Cleanup in the test body).
func insertIntegrationTestUser(t *testing.T, profileRepo repository.ProfileRepository, email, tier, status string) *domain.User {
	t.Helper()
	user := &domain.User{
		ClerkUserID:        "clerk_" + email,
		Email:              email,
		EmailVerified:      true,
		SubscriptionTier:   tier,
		SubscriptionStatus: status,
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, profileRepo.CreateUser(context.Background(), user))
	return user
}

func TestHouseholdServiceIntegration_AcceptInvite_FullHappyPath(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	owner := insertIntegrationTestUser(t, f.profileRepo, "owner-"+t.Name()+"@example.com", "household_2", "active")
	invitee := insertIntegrationTestUser(t, f.profileRepo, "invitee-"+t.Name()+"@example.com", "premium", "trial")
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []string{owner.ID, invitee.ID}) //nolint:errcheck
	})

	household, err := f.svc.CreateHousehold(ctx, owner.ID, "Integration Test Household")
	require.NoError(t, err)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) //nolint:errcheck
	})

	// Give the invitee one account so the default-share fan-out has something to do.
	var acctID string
	err = f.pool.QueryRow(ctx, `
		INSERT INTO user_accounts (user_id, institution_id, institution_id_blind_idx)
		VALUES ($1, 'enc_inst', 'blind_inst_'||gen_random_uuid()::text)
		RETURNING id::text
	`, invitee.ID).Scan(&acctID)
	require.NoError(t, err)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM user_accounts WHERE id = $1`, acctID) //nolint:errcheck
	})

	inviteResult, err := f.svc.CreateInvite(ctx, owner.ID, invitee.Email, "https://app.example.com")
	require.NoError(t, err)
	rawToken := inviteResult.InviteLink[len("https://app.example.com/households/invites/"):]

	acceptResult, err := f.svc.AcceptInvite(ctx, invitee.ID, rawToken, true, nil)
	require.NoError(t, err)
	require.Equal(t, household.ID, acceptResult.Household.ID)

	// Membership row exists and is active.
	householdRepo := repository.NewHouseholdRepository(f.pool)
	member, err := householdRepo.GetActiveMember(ctx, household.ID, invitee.ID)
	require.NoError(t, err)
	require.Equal(t, domain.HouseholdRoleMember, member.Role)

	// Consent row was written with a non-empty snapshot.
	var snapshotRaw []byte
	err = f.pool.QueryRow(ctx, `SELECT snapshot FROM household_share_consents WHERE household_id = $1 AND user_id = $2`,
		household.ID, invitee.ID).Scan(&snapshotRaw)
	require.NoError(t, err)
	var snapshot map[string][]string
	require.NoError(t, json.Unmarshal(snapshotRaw, &snapshot))
	require.Contains(t, snapshot["shared"], acctID)

	// Account share was created as 'shared' (not hidden).
	var visibility string
	err = f.pool.QueryRow(ctx, `SELECT visibility FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2`,
		household.ID, acctID).Scan(&visibility)
	require.NoError(t, err)
	require.Equal(t, "shared", visibility)

	// Household entitlement was written for the invitee, sourced from the owner.
	entRepo := repository.NewEntitlementRepository(f.pool)
	entitlement, err := entRepo.GetActiveEntitlement(ctx, invitee.ID)
	require.NoError(t, err)
	require.Equal(t, domain.EntitlementSourceHousehold, entitlement.Source)
	require.Equal(t, owner.ID, *entitlement.PayerUserID)
	require.Equal(t, "household_2", entitlement.Tier)

	// Invite flipped to accepted.
	var inviteStatus string
	var acceptedAt *time.Time
	err = f.pool.QueryRow(ctx, `SELECT status, accepted_at FROM household_invites WHERE household_id = $1 AND inviter_user_id = $2`,
		household.ID, owner.ID).Scan(&inviteStatus, &acceptedAt)
	require.NoError(t, err)
	require.Equal(t, "accepted", inviteStatus)
	require.NotNil(t, acceptedAt)
}

// waitForBlockedByGate polls, via a dedicated connection independent of the test's own
// `pool`, until `want` distinct backends are shown transitively blocked on gatePID, or
// fails the test after a timeout. Used to PROVE a FOR UPDATE lock actually forces
// serialization between two concurrent AcceptInvite calls — without this, a naive "just
// start two goroutines" concurrency test can pass purely because the Go scheduler happened
// to run them one after another, silently proving nothing about the lock (Codex
// adversarial review, Gap Analysis Round 2, GT-5).
//
// Three non-obvious pitfalls this design works around (earlier iterations hit all three,
// empirically, against a real Postgres instance):
//  1. Naively counting `pg_locks WHERE relation = X AND granted = false` undercounts: when
//     N>1 backends queue for the SAME row lock, only the FIRST waiter shows a
//     `locktype='tuple'` wait (with `relation` populated); every SUBSEQUENT queued waiter
//     shows `wait_event='transactionid'` (waiting on the first waiter's xid to resolve) —
//     a lock type that never has `relation` populated, so that filter can never count past
//     1 once real contention queues up.
//  2. Polling pg_stat_activity from WITHIN the gate's own already-open transaction reads
//     as a stale/frozen snapshot of other backends' activity — the blocked waiters never
//     became visible even though an independent connection saw them immediately.
//  3. Matching on the blocked query's own raw SQL text (e.g. via ILIKE) fixes 1 but not 2,
//     and introduces a NEW risk: when the full suite runs via `go test ./...`, an
//     unrelated concurrent test in another package (e.g. the repository-level analog in
//     internal/repository) can be blocked on a similarly-worded `FOR UPDATE` query against
//     the same shared dev database and get miscounted as this test's own contention.
//
// pg_blocking_pids(pid) precisely identifies backends blocked BY THIS GATE specifically
// (fixes 3, immune to unrelated concurrent tests/packages hitting a similarly-worded
// query), but — verified empirically — it only reports the IMMEDIATE next link in the
// queue, not the full chain: in a 2-waiter queue, the second waiter's pg_blocking_pids
// returns the FIRST waiter's pid, not the original gate's pid. The recursive CTE below
// walks that chain to compute the full transitive closure, so every queued waiter is
// counted regardless of queue depth (fixes 1, without pg_locks' relation/locktype
// blind spot). Using a dedicated pgx.Connect observer (not the shared `pool`, and not the
// gate's own transaction) fixes 2 and also guarantees this observation can never contend
// with `pool` for a connection-pool slot, regardless of how the test environment's
// DATABASE_URL configures pool_max_conns.
func waitForBlockedByGate(t *testing.T, gatePID int32, want int) {
	t.Helper()
	ctx := context.Background()
	observer, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	require.NoError(t, err)
	defer observer.Close(ctx)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		err := observer.QueryRow(ctx, `
			WITH RECURSIVE blocked AS (
				SELECT pid AS waiter, unnest(pg_blocking_pids(pid)) AS blocker
				FROM pg_stat_activity WHERE wait_event_type = 'Lock'
			),
			chain AS (
				SELECT waiter, blocker FROM blocked
				UNION
				SELECT c.waiter, b.blocker FROM chain c JOIN blocked b ON c.blocker = b.waiter
			)
			SELECT count(DISTINCT waiter) FROM chain WHERE blocker = $1
		`, gatePID).Scan(&count)
		require.NoError(t, err)
		if count >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d backend(s) to be transitively blocked by gate pid %d — the lock may not be contended", want, gatePID)
}

// TestHouseholdServiceIntegration_AcceptInvite_ConcurrentSameToken_InviteRowLockEnforced
// closes a Gap Analysis Round 2 (GT-5) test-hardening item: the existing real-DB
// concurrency proof (repository.TestHouseholdAcceptConcurrency_SeatCapEnforced) only
// exercises a repo-level analog (Lock -> Count -> Insert), not the FULL service-level
// AcceptInvite transaction racing on the SAME invite token. This proves two concurrent
// accepts of one token (e.g. a double-click) are fully serialized — the second call must
// see the flipped status, not double-accept.
//
// Gates the invite row specifically (Codex Round 4B/GT-6 note: since AcceptInvite now
// locks the household before the invite — see GT-6 — both callers actually contend on the
// SAME household row first; whichever wins then reaches this gate and blocks on the
// invite-row lock, while the loser is only transitively blocked, via the winner's held
// household lock, on this same gate. waitForBlockedByGate's recursive pg_blocking_pids CTE
// correctly counts both as gate-blocked either way, so this still proves full end-to-end
// serialization — just via a household-then-invite chain rather than two direct invite-row
// waiters).
func TestHouseholdServiceIntegration_AcceptInvite_ConcurrentSameToken_InviteRowLockEnforced(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	owner := insertIntegrationTestUser(t, f.profileRepo, "owner-"+t.Name()+"@example.com", "household_2", "active")
	invitee := insertIntegrationTestUser(t, f.profileRepo, "invitee-"+t.Name()+"@example.com", "premium", "trial")
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []string{owner.ID, invitee.ID}) //nolint:errcheck
	})

	household, err := f.svc.CreateHousehold(ctx, owner.ID, "Concurrency Test Household (same token)")
	require.NoError(t, err)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) //nolint:errcheck
	})

	inviteResult, err := f.svc.CreateInvite(ctx, owner.ID, invitee.Email, "https://app.example.com")
	require.NoError(t, err)
	rawToken := inviteResult.InviteLink[len("https://app.example.com/households/invites/"):]

	// Gate: hold the invite row's lock BEFORE starting the two real accept attempts, so
	// both are guaranteed to actually contend for it — proving real lock contention
	// rather than hoping goroutine scheduling happens to overlap them.
	gateTx, err := f.pool.Begin(ctx)
	require.NoError(t, err)
	gateReleased := false
	defer func() {
		// Safety net: if waitForBlockedByGate below fails via t.Fatalf, execution never
		// reaches the explicit release — without this the gate stays open, wedging the two
		// blocked goroutines (and, transitively, test cleanup / pool.Close) forever
		// (Codex adversarial review, Gap Analysis Round 2, GT-5).
		if !gateReleased {
			_ = gateTx.Rollback(ctx)
		}
	}()
	var gatePID int32
	require.NoError(t, gateTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&gatePID))
	var lockedID string
	require.NoError(t, gateTx.QueryRow(ctx,
		`SELECT id::text FROM household_invites WHERE id = $1 FOR UPDATE`, inviteResult.Invite.ID,
	).Scan(&lockedID))

	const attempts = 2
	var wg sync.WaitGroup
	errs := make([]error, attempts)
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = f.svc.AcceptInvite(ctx, invitee.ID, rawToken, true, nil)
		}(i)
	}

	waitForBlockedByGate(t, gatePID, attempts)
	require.NoError(t, gateTx.Rollback(ctx)) // release the gate — both goroutines now race for real
	gateReleased = true

	wg.Wait()

	successes, notPending := 0, 0
	for _, e := range errs {
		switch {
		case e == nil:
			successes++
		case errors.Is(e, domain.ErrInviteNotPending):
			notPending++
		default:
			t.Fatalf("unexpected error from concurrent accept: %v", e)
		}
	}
	require.Equal(t, 1, successes, "exactly one concurrent accept of the same token must succeed")
	require.Equal(t, 1, notPending, "the loser must see the invite already flipped, not silently double-accept")
}

// TestHouseholdServiceIntegration_AcceptInvite_ConcurrentDifferentTokens_SeatCapEnforced
// closes the other half of the GT-5 concurrency gap: two DIFFERENT invitees, each with
// their own pending invite to the same household, racing for the household's LAST active
// seat. This reproduces the exact scenario the accept-time seat re-check exists for (see
// plan decision — "downgrade-between-invite-and-accept guard"): both invites were created
// while the owner had 3 seats (household_3), then the owner's plan was downgraded to 2
// seats (e.g. a Paddle downgrade) before either invitee accepted. Note the accept-time
// re-check (CountActiveMembers >= included) only counts ACTIVE members, not pending
// invites — this is why two pending invites can legitimately coexist on a now-2-seat
// household (the invite-creation-time gate, which DOES count pending, only blocks
// creating a 3rd pending invite going forward; it cannot retroactively revoke the two
// already issued). Proves the household-row FOR UPDATE + seat re-check in the full
// AcceptInvite transaction — not just the repository-level Lock->Count->Insert analog —
// prevents seat overflow when both racing invitees try to claim the single remaining seat.
func TestHouseholdServiceIntegration_AcceptInvite_ConcurrentDifferentTokens_SeatCapEnforced(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	// Owner starts on household_3 (3 seats) so both invites can be created — the
	// invite-creation seat gate counts active+pending, so a 2-seat household could never
	// have had two simultaneously-pending invites in the first place.
	owner := insertIntegrationTestUser(t, f.profileRepo, "owner-"+t.Name()+"@example.com", "household_3", "active")
	inviteeA := insertIntegrationTestUser(t, f.profileRepo, "invitee-a-"+t.Name()+"@example.com", "premium", "trial")
	inviteeB := insertIntegrationTestUser(t, f.profileRepo, "invitee-b-"+t.Name()+"@example.com", "premium", "trial")
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []string{owner.ID, inviteeA.ID, inviteeB.ID}) //nolint:errcheck
	})

	household, err := f.svc.CreateHousehold(ctx, owner.ID, "Concurrency Test Household (seat race)")
	require.NoError(t, err)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) //nolint:errcheck
	})

	inviteA, err := f.svc.CreateInvite(ctx, owner.ID, inviteeA.Email, "https://app.example.com")
	require.NoError(t, err)
	inviteB, err := f.svc.CreateInvite(ctx, owner.ID, inviteeB.Email, "https://app.example.com")
	require.NoError(t, err)
	tokenA := inviteA.InviteLink[len("https://app.example.com/households/invites/"):]
	tokenB := inviteB.InviteLink[len("https://app.example.com/households/invites/"):]

	// Simulate the owner downgrading from household_3 (3 seats) to household_2 (2 seats)
	// after both invites were issued but before either invitee accepts. Owner (1 active)
	// + one more accept = 2 = included; the second concurrent accept must be rejected.
	_, err = f.pool.Exec(ctx, `UPDATE users SET subscription_tier = 'household_2' WHERE id = $1`, owner.ID)
	require.NoError(t, err)

	// Gate: hold the household row's lock BEFORE starting the two real accept attempts.
	// Each invitee's own invite row is distinct (no contention there); the real
	// contention point this test proves is the shared household-row FOR UPDATE +
	// seat re-check, so the gate must target `households`, not `household_invites`.
	gateTx, err := f.pool.Begin(ctx)
	require.NoError(t, err)
	gateReleased := false
	defer func() {
		// Safety net — see the sibling ConcurrentSameToken test for why this matters.
		if !gateReleased {
			_ = gateTx.Rollback(ctx)
		}
	}()
	var gatePID int32
	require.NoError(t, gateTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&gatePID))
	var lockedID string
	require.NoError(t, gateTx.QueryRow(ctx,
		`SELECT id::text FROM households WHERE id = $1 FOR UPDATE`, household.ID,
	).Scan(&lockedID))

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = f.svc.AcceptInvite(ctx, inviteeA.ID, tokenA, true, nil)
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = f.svc.AcceptInvite(ctx, inviteeB.ID, tokenB, true, nil)
	}()

	waitForBlockedByGate(t, gatePID, 2)
	require.NoError(t, gateTx.Rollback(ctx)) // release the gate — both goroutines now race for real
	gateReleased = true

	wg.Wait()

	successes, seatsFull := 0, 0
	for _, e := range errs {
		switch {
		case e == nil:
			successes++
		case errors.Is(e, domain.ErrHouseholdSeatsFull):
			seatsFull++
		default:
			t.Fatalf("unexpected error from concurrent accept: %v", e)
		}
	}
	require.Equal(t, 1, successes, "exactly one of the two racing invitees must win the last seat")
	require.Equal(t, 1, seatsFull, "the loser must see the household as full, never overfilling seats")
}

// TestHouseholdServiceIntegration_AcceptInvite_ConcurrentWithDeleteHousehold_NoDeadlock
// closes a lock-order-inversion deadlock found by a user-requested final Codex adversarial
// review (Gap Analysis Round 4B, GT-6): AcceptInvite used to lock the invite row before the
// household row, while DeleteHousehold locks the household row first and then (via
// household_invites.household_id ON DELETE CASCADE) needs to lock/remove that same invite
// row. A concurrent accept + disband on the same household could deadlock — Postgres aborts
// one side with an unmapped SQLSTATE 40P01, which fell through errors/mapper.go's default
// case as a raw 500 instead of a clean domain sentinel.
//
// The fix reorders AcceptInvite to lock the household BEFORE the invite (matching
// CreateInvite/DeleteHousehold), via a new non-locking GetHouseholdIDByTokenHash lookup
// used only to learn which household to lock first. Both paths now contend for the SAME
// FIRST lock (the household row), so there are exactly two valid, race-safe outcomes —
// never a deadlock or an unmapped error:
//   - Accept wins the household lock first, inserts the member, and commits BEFORE Delete's
//     CountActiveMembers check runs → Delete correctly sees 2 active members and gets
//     domain.ErrHouseholdHasActiveMembers.
//   - Delete wins the household lock first, sees only 1 active member (Accept hasn't
//     inserted anything yet), and deletes the household (cascading away the pending
//     invite) → Accept's own LockHouseholdForUpdate then finds no such row and gets
//     domain.ErrHouseholdNotFound.
func TestHouseholdServiceIntegration_AcceptInvite_ConcurrentWithDeleteHousehold_NoDeadlock(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	owner := insertIntegrationTestUser(t, f.profileRepo, "owner-"+t.Name()+"@example.com", "household_2", "active")
	invitee := insertIntegrationTestUser(t, f.profileRepo, "invitee-"+t.Name()+"@example.com", "premium", "trial")
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []string{owner.ID, invitee.ID}) //nolint:errcheck
	})

	household, err := f.svc.CreateHousehold(ctx, owner.ID, "Concurrency Test Household (accept vs disband)")
	require.NoError(t, err)
	t.Cleanup(func() {
		// DeleteHousehold may have already removed this row (one of the two valid
		// outcomes) — a 0-row DELETE is harmless.
		f.pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) //nolint:errcheck
	})

	inviteResult, err := f.svc.CreateInvite(ctx, owner.ID, invitee.Email, "https://app.example.com")
	require.NoError(t, err)
	rawToken := inviteResult.InviteLink[len("https://app.example.com/households/invites/"):]

	// Gate: hold the household row's lock BEFORE starting the accept and the disband, so
	// both are guaranteed to actually contend for it — this is the FIRST lock either path
	// takes now that AcceptInvite has been reordered, so no separate invite-row gate is
	// needed (unlike the sibling seat-cap test above, which gates a lock acquired only
	// partway through each transaction).
	gateTx, err := f.pool.Begin(ctx)
	require.NoError(t, err)
	gateReleased := false
	defer func() {
		if !gateReleased {
			_ = gateTx.Rollback(ctx)
		}
	}()
	var gatePID int32
	require.NoError(t, gateTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&gatePID))
	var lockedID string
	require.NoError(t, gateTx.QueryRow(ctx,
		`SELECT id::text FROM households WHERE id = $1 FOR UPDATE`, household.ID,
	).Scan(&lockedID))

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = f.svc.AcceptInvite(ctx, invitee.ID, rawToken, true, nil)
	}()
	go func() {
		defer wg.Done()
		errs[1] = f.svc.DeleteHousehold(ctx, owner.ID)
	}()

	waitForBlockedByGate(t, gatePID, 2)
	require.NoError(t, gateTx.Rollback(ctx)) // release the gate — both goroutines now race for real
	gateReleased = true

	wg.Wait()

	acceptErr, deleteErr := errs[0], errs[1]
	switch {
	case acceptErr == nil && errors.Is(deleteErr, domain.ErrHouseholdHasActiveMembers):
		// Accept won: inserted the member before Delete's active-count check ran.
	case deleteErr == nil && errors.Is(acceptErr, domain.ErrHouseholdNotFound):
		// Delete won: removed the household (cascading the invite) before Accept
		// re-acquired the household lock.
	default:
		t.Fatalf("expected exactly one of the two race-safe outcomes, got acceptErr=%v deleteErr=%v — a raw/unmapped error here means the lock-order deadlock regressed", acceptErr, deleteErr)
	}
}

// TestHouseholdServiceIntegration_AcceptInvite_OwnerStatusRace_NoStaleRead closes a Gap
// Analysis Round 5 (6th Codex pass) finding, GT-8: AcceptInvite used to read the owner's
// deletion/subscription status via a plain, non-locking read, so a concurrent Paddle webhook
// committing an active->cancelled transition on the owner's row could race between that read
// and AcceptInvite's commit, letting a membership + household entitlement get inserted for an
// owner whose plan just lapsed. The fix locks the owner's users row (via the same
// lockAndGetUser helper already used for the caller) before reading their status.
//
// PROVING THIS DETERMINISTICALLY — two earlier, simpler versions of this test were tried and
// both turned out NOT to discriminate a fixed implementation from a broken one, discovered
// empirically by running each against the fix manually reverted:
//  1. "Start both concurrently, accept either outcome as valid": the naive assumption was
//     that a stale-read success would still eventually contend on the owner's row (thought to
//     be via an implicit FOR KEY SHARE lock from the entitlement insert's FK). That assumption
//     was wrong — the entitlement's FK is a *composite* one to households(id, owner_user_id),
//     not to users(id) — so a broken AcceptInvite never touches the owner's row lock at all
//     and simply completes, unblocked, before the simulated cancel even gets a chance to
//     commit — which is actually a legitimate ordering, not a bug, so it can't distinguish
//     anything.
//  2. "Force the cancel to queue for the owner-row lock before accept starts, then release":
//     same root problem — without a lock, AcceptInvite's read/decision/commit all happen
//     before the test ever lets the cancel commit, so the owner genuinely was still active at
//     the moment AcceptInvite committed. Not a race at all, let alone the TOCTOU one.
//
// Both attempts foundered on the same fact: Postgres MVCC always shows the latest COMMITTED
// data to a fresh read, locked or not — a lock's only real effect is to force a *concurrent
// writer* to wait, not to change what a reader sees once that writer has already committed.
// So the only way to deterministically prove "the owner-status decision happens under a lock
// that a concurrent writer must wait on" is to check that directly, not infer it from
// AcceptInvite's returned error. This test uses a test-only hook
// (acceptInviteAfterOwnerCheckHook, nil in production) fired immediately after AcceptInvite's
// owner-status decision, while its transaction (and, in the fixed code, its owner-row lock)
// is still open. While paused there, it starts a concurrent "Paddle cancel" UPDATE against the
// owner's row and asserts, within a bounded window, that the cancel does NOT complete — i.e.
// is genuinely blocked waiting for AcceptInvite's own transaction to end. A broken
// implementation holds no lock on the owner's row at this point, so the concurrent UPDATE
// would complete immediately, uncontended — the test asserts that must never happen.
func TestHouseholdServiceIntegration_AcceptInvite_OwnerStatusRace_NoStaleRead(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	impl := f.svc // f.svc is already the concrete householdService type — this test installs a synchronization hook on it.
	ctx := context.Background()

	owner := insertIntegrationTestUser(t, f.profileRepo, "owner-"+t.Name()+"@example.com", "household_2", "active")
	invitee := insertIntegrationTestUser(t, f.profileRepo, "invitee-"+t.Name()+"@example.com", "premium", "trial")
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []string{owner.ID, invitee.ID}) //nolint:errcheck
	})

	household, err := f.svc.CreateHousehold(ctx, owner.ID, "Concurrency Test Household (owner status race)")
	require.NoError(t, err)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) //nolint:errcheck
	})

	inviteResult, err := f.svc.CreateInvite(ctx, owner.ID, invitee.Email, "https://app.example.com")
	require.NoError(t, err)
	rawToken := inviteResult.InviteLink[len("https://app.example.com/households/invites/"):]

	reachedHook := make(chan struct{})
	releaseHook := make(chan struct{})
	impl.acceptInviteAfterOwnerCheckHook = func() {
		close(reachedHook)
		<-releaseHook
	}
	t.Cleanup(func() { impl.acceptInviteAfterOwnerCheckHook = nil })

	// Safety net: if any assertion below fails (t.Fatal unwinds via runtime.Goexit, skipping
	// the rest of the function body), releaseHookOnce still fires via this defer, so the
	// AcceptInvite goroutine — paused inside the hook, holding an open transaction/connection
	// — is never leaked. Without this, a failing assertion here would hang the whole test
	// binary until pool cleanup's connection wait times out.
	var releaseHookOnce sync.Once
	releaseHookFn := func() { releaseHookOnce.Do(func() { close(releaseHook) }) }
	defer releaseHookFn()

	var wg sync.WaitGroup
	var acceptErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, acceptErr = f.svc.AcceptInvite(ctx, invitee.ID, rawToken, true, nil)
	}()

	select {
	case <-reachedHook:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for AcceptInvite to reach the post-owner-check hook")
	}

	// AcceptInvite is now paused with its owner-status decision made but its transaction
	// still open. Try to run the simulated Paddle cancel concurrently.
	cancelDone := make(chan error, 1)
	cancelPIDReady := make(chan int32, 1)
	go func() {
		cancelTx, txErr := f.pool.Begin(ctx)
		if txErr != nil {
			cancelPIDReady <- 0
			cancelDone <- txErr
			return
		}
		var pid int32
		if scanErr := cancelTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); scanErr != nil {
			_ = cancelTx.Rollback(ctx)
			cancelPIDReady <- 0
			cancelDone <- scanErr
			return
		}
		cancelPIDReady <- pid
		if _, txErr = cancelTx.Exec(ctx, `UPDATE users SET subscription_status = 'cancelled' WHERE id = $1`, owner.ID); txErr != nil {
			_ = cancelTx.Rollback(ctx)
			cancelDone <- txErr
			return
		}
		cancelDone <- cancelTx.Commit(ctx)
	}()
	cancelPID := <-cancelPIDReady
	require.NotZero(t, cancelPID)

	// Rather than inferring "blocked" from elapsed wall-clock time (which a saturated test DB
	// could make unreliable in either direction), poll pg_stat_activity directly for the
	// cancel backend's own wait_event_type — this is the same technique proven in the GT-6
	// deadlock regression test's lock-gate helpers, applied here to confirm a GENUINE lock
	// wait rather than mere slowness.
	cancelBlockedOnLock := false
	deadline := time.Now().Add(3 * time.Second)
pollLoop:
	for time.Now().Before(deadline) {
		select {
		case cancelErr := <-cancelDone:
			require.NoError(t, cancelErr) // it did complete — that's the failure being asserted below
			t.Fatal("the simulated Paddle cancel completed while AcceptInvite's owner-status decision was still pending inside an open transaction — the owner's row is not locked, so the decision was made on a read a concurrent writer could invalidate at any moment")
		default:
		}

		var waitEventType *string
		require.NoError(t, f.pool.QueryRow(ctx,
			`SELECT wait_event_type FROM pg_stat_activity WHERE pid = $1`, cancelPID,
		).Scan(&waitEventType))
		if waitEventType != nil && *waitEventType == "Lock" {
			cancelBlockedOnLock = true
			break pollLoop
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.True(t, cancelBlockedOnLock,
		"timed out waiting for the simulated cancel to genuinely block on a lock — AcceptInvite may have finished without ever needing the owner-row lock")

	releaseHookFn()
	wg.Wait()

	cancelErr := <-cancelDone
	require.NoError(t, cancelErr, "the simulated Paddle cancel must succeed once AcceptInvite's transaction ends")
	require.NoError(t, acceptErr, "AcceptInvite legitimately held the owner-row lock first (owner was genuinely still active throughout), so it must succeed")
}

// TestHouseholdServiceIntegration_AcceptInvite_HiddenAccountIDs_CreatesConsistentHiddenRow
// is the #1368 (Phase 4) regression proving Decision A: the accept-transaction's
// hidden_account_ids opt-out (#1367 — Phase 3, household_service.go step (8)) is left
// UNCHANGED by Phase 4 and produces a row consistent with the corrected data model —
// visibility='hidden' with revoked_at STAYING NULL, not revoked. If a future change ever
// makes accept write revoked_at for a pre-hidden account, this catches it: such a row
// would be invisible to RevokeSharesForUser's `WHERE revoked_at IS NULL` filter, meaning
// member removal could never revoke it (see household_share_repository.go).
func TestHouseholdServiceIntegration_AcceptInvite_HiddenAccountIDs_CreatesConsistentHiddenRow(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	owner := insertIntegrationTestUser(t, f.profileRepo, "owner-"+t.Name()+"@example.com", "household_2", "active")
	invitee := insertIntegrationTestUser(t, f.profileRepo, "invitee-"+t.Name()+"@example.com", "premium", "trial")
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []string{owner.ID, invitee.ID}) //nolint:errcheck
	})

	household, err := f.svc.CreateHousehold(ctx, owner.ID, "Integration Test Household")
	require.NoError(t, err)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) //nolint:errcheck
	})

	var acctID string
	err = f.pool.QueryRow(ctx, `
		INSERT INTO user_accounts (user_id, institution_id, institution_id_blind_idx)
		VALUES ($1, 'enc_inst', 'blind_inst_'||gen_random_uuid()::text)
		RETURNING id::text
	`, invitee.ID).Scan(&acctID)
	require.NoError(t, err)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM user_accounts WHERE id = $1`, acctID) //nolint:errcheck
	})

	inviteResult, err := f.svc.CreateInvite(ctx, owner.ID, invitee.Email, "https://app.example.com")
	require.NoError(t, err)
	rawToken := inviteResult.InviteLink[len("https://app.example.com/households/invites/"):]

	// Invitee pre-hides this account before accepting (the GDPR consent-gate opt-out).
	_, err = f.svc.AcceptInvite(ctx, invitee.ID, rawToken, true, []string{acctID})
	require.NoError(t, err)

	var visibility string
	var revokedAt *time.Time
	err = f.pool.QueryRow(ctx, `SELECT visibility, revoked_at FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2`,
		household.ID, acctID).Scan(&visibility, &revokedAt)
	require.NoError(t, err)
	require.Equal(t, "hidden", visibility)
	require.Nil(t, revokedAt, "a pre-hidden account must stay an ACTIVE row (revoked_at NULL) — hidden and revoked are different states")

	// And it must still be reachable by member-removal's revocation path (proves the row
	// is genuinely active, not orphaned).
	shareRepo := repository.NewHouseholdShareRepository(f.pool)
	require.NoError(t, shareRepo.RevokeSharesForUser(ctx, household.ID, invitee.ID))
	err = f.pool.QueryRow(ctx, `SELECT revoked_at FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2`,
		household.ID, acctID).Scan(&revokedAt)
	require.NoError(t, err)
	require.NotNil(t, revokedAt, "RevokeSharesForUser must be able to find and revoke the hidden row")
}

// TestHouseholdServiceIntegration_SetAccountShareVisibility_RaceRemoveMember_NoActiveShareSurvives
// proves the #1368 (Phase 4) TOCTOU guard (Codex design-review High-4): a member's own
// PATCH .../household-share cannot race the owner's concurrent member-removal and leave a
// dangling active share behind for someone no longer in the household. Both
// SetAccountShareVisibility and RemoveMember lock the SAME household_members row (the
// former via LockActiveMembershipForUser FOR UPDATE OF hm, the latter via its UPDATE), so
// real Postgres row-level locking fully serializes every possible interleaving:
//   - PATCH commits first  -> RemoveMember's later RevokeSharesForUser revokes the row PATCH just wrote.
//   - RemoveMember commits first -> PATCH's lock attempt finds no active membership -> 404, no row written.
//
// Either way, once both calls complete, no active (revoked_at IS NULL) share may remain
// for the removed member's account.
func TestHouseholdServiceIntegration_SetAccountShareVisibility_RaceRemoveMember_NoActiveShareSurvives(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	owner := insertIntegrationTestUser(t, f.profileRepo, "owner-"+t.Name()+"@example.com", "household_2", "active")
	member := insertIntegrationTestUser(t, f.profileRepo, "member-"+t.Name()+"@example.com", "premium", "trial")
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []string{owner.ID, member.ID}) //nolint:errcheck
	})

	household, err := f.svc.CreateHousehold(ctx, owner.ID, "Integration Test Household")
	require.NoError(t, err)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) //nolint:errcheck
	})

	// Insert the member's active membership row directly — this test only cares about the
	// share/removal race, not the full invite/entitlement lifecycle already covered by
	// TestHouseholdServiceIntegration_AcceptInvite_FullHappyPath.
	_, err = f.pool.Exec(ctx, `
		INSERT INTO household_members (household_id, user_id, role, status) VALUES ($1, $2, 'member', 'active')
	`, household.ID, member.ID)
	require.NoError(t, err)

	var acctID string
	err = f.pool.QueryRow(ctx, `
		INSERT INTO user_accounts (user_id, institution_id, institution_id_blind_idx)
		VALUES ($1, 'enc_inst', 'blind_inst_'||gen_random_uuid()::text)
		RETURNING id::text
	`, member.ID).Scan(&acctID)
	require.NoError(t, err)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM user_accounts WHERE id = $1`, acctID) //nolint:errcheck
	})

	// Pre-seed a 'shared' row so there is something active for the race to contend over.
	shareRepo := repository.NewHouseholdShareRepository(f.pool)
	require.NoError(t, shareRepo.InsertShare(ctx, household.ID, acctID, nil, domain.ShareVisibilityShared))

	var wg sync.WaitGroup
	var patchErr, removeErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, patchErr = f.svc.SetAccountShareVisibility(ctx, member.ID, acctID, domain.ShareVisibilityHidden)
	}()
	go func() {
		defer wg.Done()
		removeErr = f.svc.RemoveMember(ctx, owner.ID, member.ID)
	}()
	wg.Wait()

	require.NoError(t, removeErr, "owner-initiated removal must always succeed regardless of the race outcome")
	// PATCH either won the race (succeeds, then gets revoked by RemoveMember) or lost it
	// (finds the membership already gone -> 404). Both are correct; anything else is a bug.
	if patchErr != nil {
		require.ErrorIs(t, patchErr, domain.ErrHouseholdNotFound)
	}

	var activeCount int
	err = f.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM household_account_shares
		WHERE household_id = $1 AND user_account_id = $2 AND revoked_at IS NULL
	`, household.ID, acctID).Scan(&activeCount)
	require.NoError(t, err)
	require.Equal(t, 0, activeCount, "no active share may survive for a removed member's account, regardless of interleaving")
}

// TestHouseholdServiceIntegration_SetAccountShareVisibility_CrossMemberIsolation closes a
// #1368 (Phase 4) test-coverage gap found on a deeper re-review: the ownership guard in
// SetAccountShareVisibility (household_account_share_service.go) is otherwise only proven via
// pgxmock with a stubbed fingerprint list, and ListMyAccountShares' housemate-exclusion is only
// proven with a single user in the household. This is the real-Postgres, two-real-members proof
// that one household member genuinely cannot see or toggle another member's account sharing —
// the IDOR-sensitive boundary a mock can't fully validate (see feedback_pgxmock_integration_coverage.md).
func TestHouseholdServiceIntegration_SetAccountShareVisibility_CrossMemberIsolation(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	owner := insertIntegrationTestUser(t, f.profileRepo, "owner-"+t.Name()+"@example.com", "household_2", "active")
	member := insertIntegrationTestUser(t, f.profileRepo, "member-"+t.Name()+"@example.com", "premium", "trial")
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []string{owner.ID, member.ID}) //nolint:errcheck
	})

	household, err := f.svc.CreateHousehold(ctx, owner.ID, "Integration Test Household")
	require.NoError(t, err)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) //nolint:errcheck
	})

	_, err = f.pool.Exec(ctx, `
		INSERT INTO household_members (household_id, user_id, role, status) VALUES ($1, $2, 'member', 'active')
	`, household.ID, member.ID)
	require.NoError(t, err)

	insertAccount := func(userID string) string {
		var acctID string
		err := f.pool.QueryRow(ctx, `
			INSERT INTO user_accounts (user_id, institution_id, institution_id_blind_idx)
			VALUES ($1, 'enc_inst', 'blind_inst_'||gen_random_uuid()::text)
			RETURNING id::text
		`, userID).Scan(&acctID)
		require.NoError(t, err)
		t.Cleanup(func() {
			f.pool.Exec(context.Background(), `DELETE FROM user_accounts WHERE id = $1`, acctID) //nolint:errcheck
		})
		return acctID
	}
	ownerAcctID := insertAccount(owner.ID)
	memberAcctID := insertAccount(member.ID)

	// Owner (an active member of the same household) must not be able to toggle the other
	// member's account — real SQL, not a stubbed fingerprint list.
	_, err = f.svc.SetAccountShareVisibility(ctx, owner.ID, memberAcctID, domain.ShareVisibilityHidden)
	require.ErrorIs(t, err, domain.ErrAccountNotOwned)

	// And the reverse direction.
	_, err = f.svc.SetAccountShareVisibility(ctx, member.ID, ownerAcctID, domain.ShareVisibilityHidden)
	require.ErrorIs(t, err, domain.ErrAccountNotOwned)

	// Neither call should have created a share row for the account it didn't own.
	var shareCount int
	err = f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM household_account_shares WHERE household_id = $1`, household.ID).Scan(&shareCount)
	require.NoError(t, err)
	require.Equal(t, 0, shareCount, "a denied cross-member PATCH must not leave any share row behind")

	// GET must return only the caller's own account, never the housemate's.
	ownerViews, err := f.svc.ListMyAccountShares(ctx, owner.ID)
	require.NoError(t, err)
	ownerIDs := make([]string, len(ownerViews))
	for i, v := range ownerViews {
		ownerIDs[i] = v.UserAccountID
	}
	require.Contains(t, ownerIDs, ownerAcctID)
	require.NotContains(t, ownerIDs, memberAcctID, "owner's account list must never leak a housemate's account")

	memberViews, err := f.svc.ListMyAccountShares(ctx, member.ID)
	require.NoError(t, err)
	memberIDs := make([]string, len(memberViews))
	for i, v := range memberViews {
		memberIDs[i] = v.UserAccountID
	}
	require.Contains(t, memberIDs, memberAcctID)
	require.NotContains(t, memberIDs, ownerAcctID, "member's account list must never leak the owner's account")
}

// ─── AcceptInvite — re-invite-within-window restore path (#1371 Task 4) ──────

// TestHouseholdServiceIntegration_AcceptInvite_RestoresRemovedMemberWithinWindow proves
// the core Task 4 behavior against real Postgres: a member who was removed less than the
// 30-day purge window ago, then re-invited, gets their EXACT SAME household_members row
// reactivated on accept — not a second, duplicate row — with a fresh household
// entitlement and their seat counted as active again.
func TestHouseholdServiceIntegration_AcceptInvite_RestoresRemovedMemberWithinWindow(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	owner := insertIntegrationTestUser(t, f.profileRepo, "owner-"+t.Name()+"@example.com", "household_2", "active")
	invitee := insertIntegrationTestUser(t, f.profileRepo, "invitee-"+t.Name()+"@example.com", "premium", "trial")
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []string{owner.ID, invitee.ID}) //nolint:errcheck
	})

	household, err := f.svc.CreateHousehold(ctx, owner.ID, "Integration Test Household (restore)")
	require.NoError(t, err)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) //nolint:errcheck
	})

	hhRepo := repository.NewHouseholdRepository(f.pool)

	// First join.
	inviteResult1, err := f.svc.CreateInvite(ctx, owner.ID, invitee.Email, "https://app.example.com")
	require.NoError(t, err)
	rawToken1 := inviteResult1.InviteLink[len("https://app.example.com/households/invites/"):]
	acceptResult1, err := f.svc.AcceptInvite(ctx, invitee.ID, rawToken1, true, nil)
	require.NoError(t, err)
	originalMemberID := acceptResult1.Member.ID

	// Owner removes them — soft-remove leaves a status='removed' historical row (this is
	// exactly the state Task 2's purge worker would later sweep after 30 days).
	require.NoError(t, f.svc.RemoveMember(ctx, owner.ID, invitee.ID))

	var removedAt *time.Time
	err = f.pool.QueryRow(ctx, `SELECT removed_at FROM household_members WHERE id = $1`, originalMemberID).Scan(&removedAt)
	require.NoError(t, err)
	require.NotNil(t, removedAt, "the original row must be soft-removed, not deleted")

	// Owner re-invites the same email within the 30-day window.
	inviteResult2, err := f.svc.CreateInvite(ctx, owner.ID, invitee.Email, "https://app.example.com")
	require.NoError(t, err)
	rawToken2 := inviteResult2.InviteLink[len("https://app.example.com/households/invites/"):]

	acceptResult2, err := f.svc.AcceptInvite(ctx, invitee.ID, rawToken2, true, nil)
	require.NoError(t, err)
	require.Equal(t, originalMemberID, acceptResult2.Member.ID, "the SAME row must be restored, not a new one inserted")
	require.Equal(t, domain.HouseholdMemberStatusActive, acceptResult2.Member.Status)
	require.Nil(t, acceptResult2.Member.RemovedAt)

	// Exactly ONE household_members row for this (household, user) pair — never two.
	var rowCount int
	err = f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM household_members WHERE household_id = $1 AND user_id = $2`,
		household.ID, invitee.ID).Scan(&rowCount)
	require.NoError(t, err)
	require.Equal(t, 1, rowCount, "restore must reuse the existing row, never leave a duplicate historical row behind")

	// The restored row itself is active with removed_at cleared.
	member, err := hhRepo.GetActiveMember(ctx, household.ID, invitee.ID)
	require.NoError(t, err)
	require.Equal(t, originalMemberID, member.ID)
	require.Nil(t, member.RemovedAt)

	// A fresh household entitlement exists for the restored member.
	entRepo := repository.NewEntitlementRepository(f.pool)
	entitlement, err := entRepo.GetActiveEntitlement(ctx, invitee.ID)
	require.NoError(t, err)
	require.Equal(t, domain.EntitlementSourceHousehold, entitlement.Source)
	require.Equal(t, owner.ID, *entitlement.PayerUserID)

	// Seat count reflects them as active again (owner + restored invitee = 2).
	activeCount, err := hhRepo.CountActiveMembers(ctx, household.ID)
	require.NoError(t, err)
	require.Equal(t, 2, activeCount)
}

// TestHouseholdServiceIntegration_AcceptInvite_SeatLimitBlocksRestore proves the restore
// path does NOT bypass the seat check: if the household is already at capacity by the
// time the re-invited member accepts, they are blocked with ErrHouseholdSeatsFull exactly
// like a fresh join would be, and their removed row is left untouched (not restored).
func TestHouseholdServiceIntegration_AcceptInvite_SeatLimitBlocksRestore(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	// Owner starts on household_3 (3 seats) so memberB's re-invite can be issued (and
	// remain pending) alongside memberC's, before the owner's plan is downgraded to
	// household_2 — mirrors the established downgrade-race technique used by
	// TestHouseholdServiceIntegration_AcceptInvite_ConcurrentDifferentTokens_SeatCapEnforced.
	owner := insertIntegrationTestUser(t, f.profileRepo, "owner-"+t.Name()+"@example.com", "household_3", "active")
	memberB := insertIntegrationTestUser(t, f.profileRepo, "member-b-"+t.Name()+"@example.com", "premium", "trial")
	memberC := insertIntegrationTestUser(t, f.profileRepo, "member-c-"+t.Name()+"@example.com", "premium", "trial")
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []string{owner.ID, memberB.ID, memberC.ID}) //nolint:errcheck
	})

	household, err := f.svc.CreateHousehold(ctx, owner.ID, "Integration Test Household (seat limit restore)")
	require.NoError(t, err)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) //nolint:errcheck
	})

	hhRepo := repository.NewHouseholdRepository(f.pool)

	// memberB joins, then is removed — leaving a removed row and freeing a seat.
	inviteB1, err := f.svc.CreateInvite(ctx, owner.ID, memberB.Email, "https://app.example.com")
	require.NoError(t, err)
	tokenB1 := inviteB1.InviteLink[len("https://app.example.com/households/invites/"):]
	acceptB1, err := f.svc.AcceptInvite(ctx, memberB.ID, tokenB1, true, nil)
	require.NoError(t, err)
	removedMemberID := acceptB1.Member.ID
	require.NoError(t, f.svc.RemoveMember(ctx, owner.ID, memberB.ID))

	// Owner re-invites memberB (this invite is still pending when memberB attempts to
	// accept it, below) BEFORE memberC takes the freed seat.
	inviteB2, err := f.svc.CreateInvite(ctx, owner.ID, memberB.Email, "https://app.example.com")
	require.NoError(t, err)
	tokenB2 := inviteB2.InviteLink[len("https://app.example.com/households/invites/"):]

	// memberC now fills the freed seat (owner + memberC = 2 active, still under the
	// household_3 cap of 3).
	inviteC, err := f.svc.CreateInvite(ctx, owner.ID, memberC.Email, "https://app.example.com")
	require.NoError(t, err)
	tokenC := inviteC.InviteLink[len("https://app.example.com/households/invites/"):]
	_, err = f.svc.AcceptInvite(ctx, memberC.ID, tokenC, true, nil)
	require.NoError(t, err)

	// Owner's plan is downgraded to household_2 (2 seats) — the household is now exactly
	// at capacity (owner + memberC), simulating a Paddle downgrade race.
	_, err = f.pool.Exec(ctx, `UPDATE users SET subscription_tier = 'household_2' WHERE id = $1`, owner.ID)
	require.NoError(t, err)

	// memberB — who still holds a valid pending invite AND has a removed row for this
	// household — tries to accept. The seat check must block this exactly like it would a
	// fresh join, even though a removed row exists to restore.
	_, err = f.svc.AcceptInvite(ctx, memberB.ID, tokenB2, true, nil)
	require.ErrorIs(t, err, domain.ErrHouseholdSeatsFull)

	// The removed row must be left completely untouched — still removed, not restored.
	member, err := hhRepo.GetRemovedMemberForUpdate(ctx, household.ID, memberB.ID)
	require.NoError(t, err)
	require.Equal(t, removedMemberID, member.ID)
	require.Equal(t, domain.HouseholdMemberStatusRemoved, member.Status)
	require.NotNil(t, member.RemovedAt)
}

// TestHouseholdServiceIntegration_AcceptInvite_RestoreConflict_UserActiveElsewhere proves
// the conflict case: a caller has a removed row for household A, but has SINCE joined a
// DIFFERENT household B as an active member. Re-accepting a stale invite to household A
// must surface RestoreMember's existing conflict mapping (ErrAlreadyInHousehold) rather
// than silently succeeding or leaving two active rows for the same user — the global
// uq_household_members_active_user partial unique index (migration 000107) is what
// RestoreMember's UPDATE collides against here.
func TestHouseholdServiceIntegration_AcceptInvite_RestoreConflict_UserActiveElsewhere(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	ownerA := insertIntegrationTestUser(t, f.profileRepo, "owner-a-"+t.Name()+"@example.com", "household_2", "active")
	ownerB := insertIntegrationTestUser(t, f.profileRepo, "owner-b-"+t.Name()+"@example.com", "household_2", "active")
	invitee := insertIntegrationTestUser(t, f.profileRepo, "invitee-"+t.Name()+"@example.com", "premium", "trial")
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []string{ownerA.ID, ownerB.ID, invitee.ID}) //nolint:errcheck
	})

	householdA, err := f.svc.CreateHousehold(ctx, ownerA.ID, "Household A (conflict test)")
	require.NoError(t, err)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, householdA.ID) //nolint:errcheck
	})
	householdB, err := f.svc.CreateHousehold(ctx, ownerB.ID, "Household B (conflict test)")
	require.NoError(t, err)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, householdB.ID) //nolint:errcheck
	})

	hhRepo := repository.NewHouseholdRepository(f.pool)

	// Invitee joins household A, then is removed — leaving a removed row in A.
	inviteA1, err := f.svc.CreateInvite(ctx, ownerA.ID, invitee.Email, "https://app.example.com")
	require.NoError(t, err)
	tokenA1 := inviteA1.InviteLink[len("https://app.example.com/households/invites/"):]
	acceptA1, err := f.svc.AcceptInvite(ctx, invitee.ID, tokenA1, true, nil)
	require.NoError(t, err)
	removedMemberID := acceptA1.Member.ID
	require.NoError(t, f.svc.RemoveMember(ctx, ownerA.ID, invitee.ID))

	// Invitee joins household B as an active member — a DIFFERENT household entirely.
	inviteB, err := f.svc.CreateInvite(ctx, ownerB.ID, invitee.Email, "https://app.example.com")
	require.NoError(t, err)
	tokenB := inviteB.InviteLink[len("https://app.example.com/households/invites/"):]
	_, err = f.svc.AcceptInvite(ctx, invitee.ID, tokenB, true, nil)
	require.NoError(t, err)

	// Owner A re-invites the same email — invitee still has a removed row in A.
	inviteA2, err := f.svc.CreateInvite(ctx, ownerA.ID, invitee.Email, "https://app.example.com")
	require.NoError(t, err)
	tokenA2 := inviteA2.InviteLink[len("https://app.example.com/households/invites/"):]

	_, err = f.svc.AcceptInvite(ctx, invitee.ID, tokenA2, true, nil)
	require.ErrorIs(t, err, domain.ErrAlreadyInHousehold)

	// The stale removed row in A must remain untouched (still removed).
	memberA, err := hhRepo.GetRemovedMemberForUpdate(ctx, householdA.ID, invitee.ID)
	require.NoError(t, err)
	require.Equal(t, removedMemberID, memberA.ID)
	require.Equal(t, domain.HouseholdMemberStatusRemoved, memberA.Status)

	// The active row in B must remain untouched (still active).
	memberB, err := hhRepo.GetActiveMember(ctx, householdB.ID, invitee.ID)
	require.NoError(t, err)
	require.Equal(t, domain.HouseholdMemberStatusActive, memberB.Status)
}
