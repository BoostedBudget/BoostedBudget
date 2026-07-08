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
	pool := setupHouseholdServiceIntegrationDB(t)
	ctx := context.Background()
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

	owner := insertIntegrationTestUser(t, profileRepo, "owner-"+t.Name()+"@example.com", "household_2", "active")
	invitee := insertIntegrationTestUser(t, profileRepo, "invitee-"+t.Name()+"@example.com", "premium", "trial")
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []string{owner.ID, invitee.ID}) //nolint:errcheck
	})

	household, err := svc.CreateHousehold(ctx, owner.ID, "Integration Test Household")
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) //nolint:errcheck
	})

	// Give the invitee one account so the default-share fan-out has something to do.
	var acctID string
	err = pool.QueryRow(ctx, `
		INSERT INTO user_accounts (user_id, institution_id, institution_id_blind_idx)
		VALUES ($1, 'enc_inst', 'blind_inst_'||gen_random_uuid()::text)
		RETURNING id::text
	`, invitee.ID).Scan(&acctID)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM user_accounts WHERE id = $1`, acctID) //nolint:errcheck
	})

	inviteResult, err := svc.CreateInvite(ctx, owner.ID, invitee.Email, "https://app.example.com")
	require.NoError(t, err)
	rawToken := inviteResult.InviteLink[len("https://app.example.com/households/invites/"):]

	acceptResult, err := svc.AcceptInvite(ctx, invitee.ID, rawToken, true, nil)
	require.NoError(t, err)
	require.Equal(t, household.ID, acceptResult.Household.ID)

	// Membership row exists and is active.
	member, err := householdRepo.GetActiveMember(ctx, household.ID, invitee.ID)
	require.NoError(t, err)
	require.Equal(t, domain.HouseholdRoleMember, member.Role)

	// Consent row was written with a non-empty snapshot.
	var snapshotRaw []byte
	err = pool.QueryRow(ctx, `SELECT snapshot FROM household_share_consents WHERE household_id = $1 AND user_id = $2`,
		household.ID, invitee.ID).Scan(&snapshotRaw)
	require.NoError(t, err)
	var snapshot map[string][]string
	require.NoError(t, json.Unmarshal(snapshotRaw, &snapshot))
	require.Contains(t, snapshot["shared"], acctID)

	// Account share was created as 'shared' (not hidden).
	var visibility string
	err = pool.QueryRow(ctx, `SELECT visibility FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2`,
		household.ID, acctID).Scan(&visibility)
	require.NoError(t, err)
	require.Equal(t, "shared", visibility)

	// Household entitlement was written for the invitee, sourced from the owner.
	entitlement, err := entRepo.GetActiveEntitlement(ctx, invitee.ID)
	require.NoError(t, err)
	require.Equal(t, domain.EntitlementSourceHousehold, entitlement.Source)
	require.Equal(t, owner.ID, *entitlement.PayerUserID)
	require.Equal(t, "household_2", entitlement.Tier)

	// Invite flipped to accepted.
	var inviteStatus string
	var acceptedAt *time.Time
	err = pool.QueryRow(ctx, `SELECT status, accepted_at FROM household_invites WHERE household_id = $1 AND inviter_user_id = $2`,
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
// AcceptInvite transaction racing on the SAME invite token. This proves the invite-row
// `SELECT ... FOR UPDATE` actually serializes two concurrent accepts of one token (e.g. a
// double-click) — the second call must see the flipped status, not double-accept.
func TestHouseholdServiceIntegration_AcceptInvite_ConcurrentSameToken_InviteRowLockEnforced(t *testing.T) {
	pool := setupHouseholdServiceIntegrationDB(t)
	ctx := context.Background()
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

	owner := insertIntegrationTestUser(t, profileRepo, "owner-"+t.Name()+"@example.com", "household_2", "active")
	invitee := insertIntegrationTestUser(t, profileRepo, "invitee-"+t.Name()+"@example.com", "premium", "trial")
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []string{owner.ID, invitee.ID}) //nolint:errcheck
	})

	household, err := svc.CreateHousehold(ctx, owner.ID, "Concurrency Test Household (same token)")
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) //nolint:errcheck
	})

	inviteResult, err := svc.CreateInvite(ctx, owner.ID, invitee.Email, "https://app.example.com")
	require.NoError(t, err)
	rawToken := inviteResult.InviteLink[len("https://app.example.com/households/invites/"):]

	// Gate: hold the invite row's lock BEFORE starting the two real accept attempts, so
	// both are guaranteed to actually contend for it — proving real lock contention
	// rather than hoping goroutine scheduling happens to overlap them.
	gateTx, err := pool.Begin(ctx)
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
			_, errs[i] = svc.AcceptInvite(ctx, invitee.ID, rawToken, true, nil)
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
	pool := setupHouseholdServiceIntegrationDB(t)
	ctx := context.Background()
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

	// Owner starts on household_3 (3 seats) so both invites can be created — the
	// invite-creation seat gate counts active+pending, so a 2-seat household could never
	// have had two simultaneously-pending invites in the first place.
	owner := insertIntegrationTestUser(t, profileRepo, "owner-"+t.Name()+"@example.com", "household_3", "active")
	inviteeA := insertIntegrationTestUser(t, profileRepo, "invitee-a-"+t.Name()+"@example.com", "premium", "trial")
	inviteeB := insertIntegrationTestUser(t, profileRepo, "invitee-b-"+t.Name()+"@example.com", "premium", "trial")
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []string{owner.ID, inviteeA.ID, inviteeB.ID}) //nolint:errcheck
	})

	household, err := svc.CreateHousehold(ctx, owner.ID, "Concurrency Test Household (seat race)")
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) //nolint:errcheck
	})

	inviteA, err := svc.CreateInvite(ctx, owner.ID, inviteeA.Email, "https://app.example.com")
	require.NoError(t, err)
	inviteB, err := svc.CreateInvite(ctx, owner.ID, inviteeB.Email, "https://app.example.com")
	require.NoError(t, err)
	tokenA := inviteA.InviteLink[len("https://app.example.com/households/invites/"):]
	tokenB := inviteB.InviteLink[len("https://app.example.com/households/invites/"):]

	// Simulate the owner downgrading from household_3 (3 seats) to household_2 (2 seats)
	// after both invites were issued but before either invitee accepts. Owner (1 active)
	// + one more accept = 2 = included; the second concurrent accept must be rejected.
	_, err = pool.Exec(ctx, `UPDATE users SET subscription_tier = 'household_2' WHERE id = $1`, owner.ID)
	require.NoError(t, err)

	// Gate: hold the household row's lock BEFORE starting the two real accept attempts.
	// Each invitee's own invite row is distinct (no contention there); the real
	// contention point this test proves is the shared household-row FOR UPDATE +
	// seat re-check, so the gate must target `households`, not `household_invites`.
	gateTx, err := pool.Begin(ctx)
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
		_, errs[0] = svc.AcceptInvite(ctx, inviteeA.ID, tokenA, true, nil)
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = svc.AcceptInvite(ctx, inviteeB.ID, tokenB, true, nil)
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
