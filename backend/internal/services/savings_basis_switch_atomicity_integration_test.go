//go:build integration

package services

// Issue #1625 Task 17 — live-Postgres proof that a savings-goal progress_basis switch is ATOMIC
// and SERIALISABLE, and that a contribution cannot land under a goal that has left the ledger.
//
// WHY NONE OF THIS CAN BE A pgxmock TEST. Every defect here is a property of the DATABASE, not of
// the Go control flow:
//
//   - Defect 1 (a failed seed zeroing the goal) turns on whether the basis UPDATE and the seed
//     INSERT share a transaction. pgxmock would happily replay both against a fake that has no
//     notion of rollback.
//   - Defects 2 and 3 are RACES. They need two goroutines on two real connections whose statements
//     genuinely interleave, and a real `SELECT … FOR UPDATE` to serialise them. A sequential mock
//     call passes against the broken code and proves nothing at all.
//   - Defect 4 is a WHERE clause. Only a real row can decline to match it.
//
// Run:
//
//	DATABASE_URL="postgres://..." go test -tags integration -p 1 ./internal/services/ \
//	    -run TestSavingsBasisSwitch_Integration -v

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// =============================================================================
// Fixture
// =============================================================================

// basisSwitchFixture is one user, one account and one savings goal, plus the real service wired
// over real repositories. Nothing here is mocked: the point is the SQL.
type basisSwitchFixture struct {
	pool        *pgxpool.Pool
	userID      string
	accountID   string
	goalID      string
	goalRepo    repository.SavingsGoalRepository
	contribRepo repository.SavingsContributionRepository
}

func setupBasisSwitchFixture(t *testing.T, basis string, currentAmount float64) *basisSwitchFixture {
	t.Helper()
	pool := setupHouseholdServiceIntegrationDB(t)
	ctx := context.Background()

	userID := uuid.New().String()
	// ⚠ Registered immediately, BEFORE any child row exists — the ordering
	// setupSavingsChainFixtureWithBasis documents at length. A require.* failure part-way through
	// this function calls t.FailNow(), and a cleanup registered after it would never run, leaking
	// rows into a database every other package in this repo shares.
	t.Cleanup(func() {
		// context.Background(), not t.Context(): a cancelled context leaves fixture rows behind.
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM savings_goal_contributions WHERE savings_goal_id IN
			(SELECT id FROM savings_goals WHERE user_id = $1)`, userID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM savings_goals WHERE user_id = $1`, userID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM transactions WHERE user_id = $1`, userID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM accounts WHERE user_id = $1`, userID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM linked_items WHERE user_id = $1`, userID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, userID)
	})

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "clerk_"+userID, "basis_"+userID+"@example.com")
	require.NoError(t, err)

	linkedItemID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "item_"+linkedItemID)
	require.NoError(t, err)

	accountID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, subtype,
			current_balance, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'Basis Test Account', 'depository', 'savings', 1000.0, now(), now())
	`, accountID, linkedItemID, userID, "plaid_"+accountID)
	require.NoError(t, err)

	goalID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO savings_goals (id, user_id, account_id, name, target_amount, current_amount,
			progress_basis, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 100000.0, $5, $6, now(), now())
	`, goalID, userID, accountID, "Basis Goal "+goalID, currentAmount, basis)
	require.NoError(t, err)

	return &basisSwitchFixture{
		pool:        pool,
		userID:      userID,
		accountID:   accountID,
		goalID:      goalID,
		goalRepo:    repository.NewSavingsGoalRepository(pool),
		contribRepo: repository.NewSavingsContributionRepository(pool),
	}
}

// newService wires the real savings goal service over this fixture's repositories. goalRepo and
// ledger are injectable so a test can wrap one in a decorator that blocks at a chosen point — the
// only way to force a specific interleaving from outside.
func (f *basisSwitchFixture) newService(goalRepo repository.SavingsGoalRepository, ledger GoalBasisSwitchContribRepo) SavingsGoalService {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	progress := NewSavingsProgressService(f.goalRepo, f.contribRepo, nil, nil, logger)
	svc := NewSavingsGoalService(goalRepo, nil, logger)
	svc.SetProgressService(progress)
	svc.SetBasisSwitchContribRepo(ledger)
	return svc
}

// liveConfirmedTotal is the sum every product surface reads: confirmed, not tombstoned, not
// archived into a closed period.
func (f *basisSwitchFixture) liveConfirmedTotal(t *testing.T) float64 {
	t.Helper()
	var total float64
	require.NoError(t, f.pool.QueryRow(context.Background(), `
		SELECT COALESCE(SUM(amount), 0) FROM savings_goal_contributions
		WHERE savings_goal_id = $1 AND status = 'confirmed'
		  AND deleted_at IS NULL AND archived_at IS NULL
	`, f.goalID).Scan(&total))
	return total
}

func (f *basisSwitchFixture) goalRow(t *testing.T) (basis string, currentAmount float64) {
	t.Helper()
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT progress_basis, current_amount FROM savings_goals WHERE id = $1`,
		f.goalID).Scan(&basis, &currentAmount))
	return basis, currentAmount
}

// =============================================================================
// Choreography primitives
// =============================================================================

// meetPoint is a rendezvous with a deadline. Both halves of a race have to reach the same point
// before either continues, or the interleaving under test never happens and the test passes
// vacuously.
//
// It TIMES OUT rather than blocking forever, and that is load-bearing in both directions. Against
// the BROKEN code both parties arrive and the meet is instant. Against the FIXED code one party is
// refused before it ever gets there — the fix is precisely that it does not reach the second
// archive — so a strict barrier would deadlock the test that proves the fix works. Timing out lets
// the same test express "overlap if you can" and then assert the invariant either way.
type meetPoint struct {
	arrived atomic.Int32
	done    atomic.Bool
	want    int32
	timeout time.Duration
}

func newMeetPoint(want int, timeout time.Duration) *meetPoint {
	return &meetPoint{want: int32(want), timeout: timeout}
}

// arrive announces this goroutine and waits (up to timeout) for the others.
//
// It becomes a NO-OP once the meet has resolved, and that is not tidiness — it is required. The
// hooks below hang off repository methods that UpdateGoal calls more than once per request
// (reloadAfterRecompute re-reads the goal through GetByID), so a strict one-shot barrier
// deadlocked the test on the third arrival. The meet has to express "the first N callers
// rendezvous, everyone after that walks through".
func (m *meetPoint) arrive() {
	if m.done.Load() {
		return
	}
	if m.arrived.Add(1) >= m.want {
		m.done.Store(true)
		return
	}
	deadline := time.Now().Add(m.timeout)
	for !m.done.Load() && m.arrived.Load() < m.want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	m.done.Store(true)
}

// goalRepoAfterGetByID wraps the real goal repository and runs a hook once GetByID has returned.
// UpdateGoal reads the goal through GetByID and plans the switch from what it sees, so this is the
// exact point at which two concurrent requests can both come to believe the goal is still manual.
type goalRepoAfterGetByID struct {
	repository.SavingsGoalRepository
	hook func()
}

func (g *goalRepoAfterGetByID) GetByID(ctx context.Context, id string) (*domain.SavingsGoal, error) {
	goal, err := g.SavingsGoalRepository.GetByID(ctx, id)
	if err == nil && g.hook != nil {
		g.hook()
	}
	return goal, err
}

// ledgerAfterArchive wraps the contribution repository and runs a hook between the archive and the
// seed — the window in which two switches that both archived can both go on to seed.
type ledgerAfterArchive struct {
	repository.SavingsContributionRepository
	afterArchive func()
	insertErr    error
}

func (l *ledgerAfterArchive) ArchiveByGoalInTx(ctx context.Context, tx repository.Transaction, goalID string) error {
	if err := l.SavingsContributionRepository.ArchiveByGoalInTx(ctx, tx, goalID); err != nil {
		return err
	}
	if l.afterArchive != nil {
		l.afterArchive()
	}
	return nil
}

func (l *ledgerAfterArchive) InsertInTx(ctx context.Context, tx repository.Transaction, userID string, contrib *domain.SavingsGoalContribution) error {
	if l.insertErr != nil {
		return l.insertErr
	}
	return l.SavingsContributionRepository.InsertInTx(ctx, tx, userID, contrib)
}

// =============================================================================
// Defect 1 — a failed seed used to zero the goal permanently
// =============================================================================

// TestSavingsBasisSwitch_Integration_SeedFailureLeavesTheGoalIntact is the DATA-LOSS regression.
//
// Before #1625 Task 17: the new basis committed, the prior period was archived, the seed insert
// failed and was LOGGED, UpdateGoal returned success, and RecomputeGoal then summed a live ledger
// that no longer had anything in it — persisting $0 over a balance the user had typed by hand,
// with no restore path anywhere on this branch.
//
// The assertion is not merely "an error is returned". It is that the DATABASE is unchanged: same
// basis, same amount, and the prior period still LIVE (not archived), because rolling a switch
// back must never be a deletion or a retirement of retained history.
func TestSavingsBasisSwitch_Integration_SeedFailureLeavesTheGoalIntact(t *testing.T) {
	f := setupBasisSwitchFixture(t, domain.ProgressBasisManual, 100.0)
	ctx := context.Background()

	// A prior contributions period, still live. The pre-seed archive would close it; the rollback
	// must un-close it.
	priorID := uuid.New().String()
	_, err := f.pool.Exec(ctx, `
		INSERT INTO savings_goal_contributions
			(id, savings_goal_id, transaction_id, amount, source, status,
			 managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at)
		VALUES ($1, $2, NULL, 40.0, 'manual', 'confirmed', false, true, CURRENT_DATE, now(), now())
	`, priorID, f.goalID)
	require.NoError(t, err)

	boom := errors.New("db: connection reset by peer")
	ledger := &ledgerAfterArchive{SavingsContributionRepository: f.contribRepo, insertErr: boom}
	svc := f.newService(f.goalRepo, ledger)

	newBasis := domain.ProgressBasisContributions
	_, err = svc.UpdateGoal(ctx, f.userID, f.goalID, &domain.UpdateSavingsGoalRequest{ProgressBasis: &newBasis})

	require.Error(t, err, "a failed seed must fail the switch — this returned 200 and persisted $0")
	assert.ErrorIs(t, err, boom)

	basis, amount := f.goalRow(t)
	assert.Equal(t, domain.ProgressBasisManual, basis, "the basis must not have moved")
	assert.Equal(t, 100.0, amount, "the hand-entered balance must be exactly where it was")

	var archivedAt *time.Time
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT archived_at FROM savings_goal_contributions WHERE id = $1`, priorID).Scan(&archivedAt))
	assert.Nil(t, archivedAt, "the rollback must un-close the period the aborted switch archived")

	var seeds int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT count(*) FROM savings_goal_contributions WHERE savings_goal_id = $1 AND id <> $2`,
		f.goalID, priorID).Scan(&seeds))
	assert.Equal(t, 0, seeds, "no partial seed may survive")
}

// =============================================================================
// Defect 2 — two concurrent switches doubled the goal
// =============================================================================

// TestSavingsBasisSwitch_Integration_ConcurrentSwitchesSeedExactlyOnce forces the interleaving
// that produced $200 from $100.
//
// HOW THE OVERLAP IS FORCED — this is the part a sequential test cannot substitute for. Two
// goroutines run UpdateGoal on two connections from the same pool, and two rendezvous points pin
// the order:
//
//	readMeet    — after each goroutine's GetByID. Neither may proceed until BOTH have read the
//	              goal, so both plan a switch FROM the manual basis. Without this the second
//	              request simply reads "contributions", finds no basis change, and does nothing.
//	archiveMeet — between the archive and the seed. Both seeds then go in after both archives, so
//	              neither archive can retire the other's seed. That is the ordering that doubles.
//
// Against the broken code both meets complete instantly and two live seeds exist. Against the
// fixed code the loser blocks on the winner's `SELECT … FOR UPDATE`, then finds the row on a basis
// it did not plan from and is refused — so it never reaches archiveMeet, which is why that meet
// has a deadline instead of being a strict barrier.
func TestSavingsBasisSwitch_Integration_ConcurrentSwitchesSeedExactlyOnce(t *testing.T) {
	f := setupBasisSwitchFixture(t, domain.ProgressBasisManual, 100.0)
	ctx := context.Background()

	readMeet := newMeetPoint(2, 5*time.Second)
	archiveMeet := newMeetPoint(2, 3*time.Second)

	goalRepo := &goalRepoAfterGetByID{SavingsGoalRepository: f.goalRepo, hook: readMeet.arrive}
	ledger := &ledgerAfterArchive{SavingsContributionRepository: f.contribRepo, afterArchive: archiveMeet.arrive}
	svc := f.newService(goalRepo, ledger)

	newBasis := domain.ProgressBasisContributions
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.UpdateGoal(ctx, f.userID, f.goalID,
				&domain.UpdateSavingsGoalRequest{ProgressBasis: &newBasis})
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 100.0, f.liveConfirmedTotal(t),
		"two concurrent switches must seed the carry-over ONCE; this read 200 before the fix")

	var live int
	require.NoError(t, f.pool.QueryRow(ctx, `
		SELECT count(*) FROM savings_goal_contributions
		WHERE savings_goal_id = $1 AND deleted_at IS NULL AND archived_at IS NULL
	`, f.goalID).Scan(&live))
	assert.Equal(t, 1, live, "exactly one live synthetic seed")

	// Exactly one request wins. The loser is refused with a PRECONDITION error, which the HTTP
	// layer maps to 409 — never a 500, and never a silent success.
	var won, lost int
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, domain.ErrSavingsGoalBasisConflict):
			lost++
		default:
			t.Fatalf("unexpected error from a concurrent switch: %v", err)
		}
	}
	assert.Equal(t, 1, won, "exactly one switch may succeed")
	assert.Equal(t, 1, lost, "the other must be refused, not silently allowed to seed again")

	basis, _ := f.goalRow(t)
	assert.Equal(t, domain.ProgressBasisContributions, basis)
}

// =============================================================================
// Defect 3 — the basis gate was a TOCTOU
// =============================================================================

// TestSavingsBasisSwitch_Integration_ConfirmCannotLandAfterASwitchToManual reproduces the
// check-then-act window in ContributionHandler.assertLedgerIsLive.
//
// HOW THE OVERLAP IS FORCED. Two goroutines on two connections, ordered by two channels so the
// window is not merely likely but certain:
//
//  1. the confirmer reads the goal exactly as the handler's gate does, sees "contributions", and
//     passes — then signals `gatePassed` and waits;
//  2. the switcher runs a full PUT to the manual basis and COMMITS, then signals `switchDone`;
//  3. only then does the confirmer call ConfirmContribution.
//
// That is the real production sequence: the gate's answer is already stale by the time the write
// runs. Before the fix the confirm succeeded, flipping a row to 'confirmed' and LIVE under a goal
// whose current_amount no longer reads the ledger — money recorded where nothing counts it. The
// repository now re-asserts the basis while HOLDING the goal row's lock, so the write is refused.
func TestSavingsBasisSwitch_Integration_ConfirmCannotLandAfterASwitchToManual(t *testing.T) {
	f := setupBasisSwitchFixture(t, domain.ProgressBasisContributions, 0.0)
	ctx := context.Background()

	proposedID := uuid.New().String()
	_, err := f.pool.Exec(ctx, `
		INSERT INTO savings_goal_contributions
			(id, savings_goal_id, transaction_id, amount, source, status,
			 managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at)
		VALUES ($1, $2, NULL, 75.0, 'confirmed_transfer', 'proposed', true, false, CURRENT_DATE, now(), now())
	`, proposedID, f.goalID)
	require.NoError(t, err)

	gatePassed := make(chan struct{})
	switchDone := make(chan struct{})
	var switchErr, confirmErr error

	var wg sync.WaitGroup
	wg.Add(2)

	// The confirmer: clears the handler's gate, then races.
	go func() {
		defer wg.Done()
		goal, err := f.goalRepo.GetByIDForUser(ctx, f.goalID, f.userID)
		if err != nil || goal.ProgressBasis != domain.ProgressBasisContributions {
			confirmErr = fmt.Errorf("gate precondition failed: %v / %v", err, goal)
			close(gatePassed)
			return
		}
		close(gatePassed) // the gate said yes — from here the answer can go stale
		<-switchDone
		_, confirmErr = f.contribRepo.ConfirmContribution(ctx, f.userID, f.goalID, proposedID)
	}()

	// The switcher: takes the goal off the contributions basis while the confirmer is in flight.
	go func() {
		defer wg.Done()
		<-gatePassed
		manual := domain.ProgressBasisManual
		svc := f.newService(f.goalRepo, f.contribRepo)
		_, switchErr = svc.UpdateGoal(ctx, f.userID, f.goalID,
			&domain.UpdateSavingsGoalRequest{ProgressBasis: &manual})
		close(switchDone)
	}()
	wg.Wait()

	require.NoError(t, switchErr)
	require.Error(t, confirmErr, "a confirm that lost the race must be refused, not applied")
	assert.ErrorIs(t, confirmErr, domain.ErrContributionBasisMismatch)

	var status string
	var archivedAt *time.Time
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT status, archived_at FROM savings_goal_contributions WHERE id = $1`,
		proposedID).Scan(&status, &archivedAt))
	assert.Equal(t, "proposed", status, "the row must not have been flipped to confirmed")
	assert.NotNil(t, archivedAt, "and it must stay in the period the switch closed")
}

// TestSavingsBasisSwitch_Integration_LedgerMutationsRefuseAManualGoal is the sequential half of
// defect 3: the predicate itself, on all three mutation paths, with no race required. A race test
// alone would leave the everyday case unpinned.
func TestSavingsBasisSwitch_Integration_LedgerMutationsRefuseAManualGoal(t *testing.T) {
	f := setupBasisSwitchFixture(t, domain.ProgressBasisManual, 0.0)
	ctx := context.Background()

	// Written straight to SQL: the repository paths under test are the ones that now refuse it.
	rowID := uuid.New().String()
	_, err := f.pool.Exec(ctx, `
		INSERT INTO savings_goal_contributions
			(id, savings_goal_id, transaction_id, amount, source, status,
			 managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at)
		VALUES ($1, $2, NULL, 20.0, 'confirmed_transfer', 'proposed', true, false, CURRENT_DATE, now(), now())
	`, rowID, f.goalID)
	require.NoError(t, err)

	t.Run("Insert", func(t *testing.T) {
		err := f.contribRepo.Insert(ctx, f.userID, &domain.SavingsGoalContribution{
			SavingsGoalID: f.goalID, Amount: 10, Source: "manual", Status: "confirmed",
			ConfirmedByUser: true, OccurredOn: time.Now().Format("2006-01-02"),
		})
		assert.ErrorIs(t, err, domain.ErrContributionBasisMismatch)
	})

	t.Run("Confirm", func(t *testing.T) {
		_, err := f.contribRepo.ConfirmContribution(ctx, f.userID, f.goalID, rowID)
		assert.ErrorIs(t, err, domain.ErrContributionBasisMismatch)
	})

	t.Run("Reject", func(t *testing.T) {
		_, err := f.contribRepo.RejectContribution(ctx, f.userID, f.goalID, rowID)
		assert.ErrorIs(t, err, domain.ErrContributionBasisMismatch)
	})

	var count int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT count(*) FROM savings_goal_contributions WHERE savings_goal_id = $1`,
		f.goalID).Scan(&count))
	assert.Equal(t, 1, count, "no refused mutation may have written anything")
}

// =============================================================================
// Defect 4 — a stale recompute overwrote a newer basis's value
// =============================================================================

// TestSavingsBasisSwitch_Integration_StaleRecomputeCannotOverwriteANewerBasis pins the predicate
// UpdateCurrentAmount gained.
//
// The sequence: request A sums a contributions ledger and gets 100. Before it writes, request B
// switches the goal to the manual basis and sets 500. A's write then lands — its figure is
// arithmetically correct and semantically wrong, nothing recomputes it back, and the user's 500
// is gone. Binding the basis A computed for into the WHERE makes the stale write match no row.
func TestSavingsBasisSwitch_Integration_StaleRecomputeCannotOverwriteANewerBasis(t *testing.T) {
	f := setupBasisSwitchFixture(t, domain.ProgressBasisContributions, 100.0)
	ctx := context.Background()

	// Request B wins: the row is now manual, holding 500.
	_, err := f.pool.Exec(ctx,
		`UPDATE savings_goals SET progress_basis = $2, current_amount = 500.0 WHERE id = $1`,
		f.goalID, domain.ProgressBasisManual)
	require.NoError(t, err)

	// Request A, paused since before the switch, finally writes what it computed.
	err = f.goalRepo.UpdateCurrentAmount(ctx, f.goalID, domain.ProgressBasisContributions, 100.0, time.Now(), nil)

	require.Error(t, err, "a recompute computed for a basis the row has left must not land")
	assert.ErrorIs(t, err, domain.ErrSavingsGoalBasisConflict)

	basis, amount := f.goalRow(t)
	assert.Equal(t, domain.ProgressBasisManual, basis)
	assert.Equal(t, 500.0, amount, "the winner's value must survive")

	// The same call for the basis the row actually has still works — the predicate must not have
	// turned UpdateCurrentAmount into a write nobody can perform.
	require.NoError(t, f.goalRepo.UpdateCurrentAmount(ctx, f.goalID, domain.ProgressBasisManual, 650.0, time.Now(), nil))
	_, amount = f.goalRow(t)
	assert.Equal(t, 650.0, amount)

	// A goal that does not exist is still reported as not-found, not as a basis conflict: the two
	// are different answers and only one of them is worth retrying.
	err = f.goalRepo.UpdateCurrentAmount(ctx, uuid.New().String(), domain.ProgressBasisManual, 1.0, time.Now(), nil)
	assert.ErrorIs(t, err, domain.ErrSavingsGoalNotFound)
}

// =============================================================================
// The bypass — a basis change must be unrepresentable outside the guarded path
// =============================================================================

// TestSavingsBasisSwitch_Integration_PlainUpdateCannotChangeTheBasis is the live-Postgres proof
// that the last route around the guarded switch is closed (#1625 Task 17, fix round 1).
//
// Everything this task built hangs off the service choosing UpdateWithBasisGuardInTx — and the
// service chose it only when an OPTIONAL, setter-injected ledger repo was non-nil. A construction
// site that forgot the setter (two existed) went through the plain Update instead: no row lock, no
// version predicate, no archive, no seed. All the tests above would still have passed.
//
// The repository now refuses it, so no caller can perform the unguarded switch whatever it forgot
// to wire. Asserted here rather than only against pgxmock because the refusal reads the stored
// basis under a real `SELECT … FOR UPDATE`, and pgxmock replays canned rows: it would pass a query
// that never locked anything.
func TestSavingsBasisSwitch_Integration_PlainUpdateCannotChangeTheBasis(t *testing.T) {
	f := setupBasisSwitchFixture(t, domain.ProgressBasisManual, 250.0)
	ctx := context.Background()

	goal, err := f.goalRepo.GetByIDForUser(ctx, f.goalID, f.userID)
	require.NoError(t, err)

	// The rename alone is fine — the guard is about the BASIS, not about updates.
	goal.Name = "Renamed but still manual"
	require.NoError(t, f.goalRepo.Update(ctx, goal))

	// The same call carrying a different basis is refused outright.
	goal.ProgressBasis = domain.ProgressBasisContributions
	err = f.goalRepo.Update(ctx, goal)
	require.Error(t, err, "the unguarded path must not be able to move a progress_basis")
	assert.ErrorIs(t, err, repository.ErrUnguardedBasisChange)

	basis, amount := f.goalRow(t)
	assert.Equal(t, domain.ProgressBasisManual, basis, "the row must not have moved")
	assert.Equal(t, 250.0, amount)

	var contributions int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT count(*) FROM savings_goal_contributions WHERE savings_goal_id = $1`,
		f.goalID).Scan(&contributions))
	assert.Equal(t, 0, contributions, "and nothing may have been archived or seeded behind it")

	// The refusal must not have swallowed the earlier legitimate write.
	var name string
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT name FROM savings_goals WHERE id = $1`, f.goalID).Scan(&name))
	assert.Equal(t, "Renamed but still manual", name)
}

// TestSavingsBasisSwitch_Integration_UnwiredServiceRefusesTheSwitch is the same property one layer
// up, end to end: a service constructed WITHOUT the basis-switch ledger — the exact shape two
// production call sites had — refuses a basis change instead of performing an unguarded one.
func TestSavingsBasisSwitch_Integration_UnwiredServiceRefusesTheSwitch(t *testing.T) {
	f := setupBasisSwitchFixture(t, domain.ProgressBasisManual, 250.0)
	ctx := context.Background()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
	unwired := NewSavingsGoalService(f.goalRepo, nil, logger)
	unwired.SetProgressService(NewSavingsProgressService(f.goalRepo, f.contribRepo, nil, nil, logger))
	// Deliberately no SetBasisSwitchContribRepo.

	newBasis := domain.ProgressBasisContributions
	_, err := unwired.UpdateGoal(ctx, f.userID, f.goalID,
		&domain.UpdateSavingsGoalRequest{ProgressBasis: &newBasis})

	require.Error(t, err)
	assert.ErrorIs(t, err, repository.ErrUnguardedBasisChange)

	basis, amount := f.goalRow(t)
	assert.Equal(t, domain.ProgressBasisManual, basis)
	assert.Equal(t, 250.0, amount, "an unwired switch must not touch the balance either")
}

// =============================================================================
// Task 22 — the same TOCTOU class on the three UPSERT paths
// =============================================================================

// seedTransaction inserts one posted transaction on the fixture's account and returns its id.
// The three upsert paths under test are all transaction-linked, so they need a real row to lock.
func (f *basisSwitchFixture) seedTransaction(t *testing.T, amount float64) string {
	t.Helper()
	txnID := uuid.New().String()
	var linkedItemID string
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT linked_item_id FROM accounts WHERE id = $1`, f.accountID).Scan(&linkedItemID))
	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO transactions (
			id, user_id, account_id, linked_item_id, source,
			external_transaction_id, external_transaction_id_blind_idx,
			amount, date, name, pfc_primary, pfc_detailed,
			pending, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, 'plaid', $5, $6,
			$7, CURRENT_DATE, 'Basis Test Transfer',
			'TRANSFER_IN', 'TRANSFER_IN_SAVINGS',
			false, now(), now()
		)
	`, txnID, f.userID, f.accountID, linkedItemID, "ext-"+txnID, "blind-"+txnID, amount)
	require.NoError(t, err)
	return txnID
}

func (f *basisSwitchFixture) contributionCount(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM savings_goal_contributions WHERE savings_goal_id = $1`,
		f.goalID).Scan(&n))
	return n
}

// TestSavingsBasisSwitch_Integration_UpsertPathsRefuseAManualGoal is the sequential half of Task
// 22: the predicate itself, on the three transaction-linked upsert paths, with no race required.
//
// These three were out of Task 17's scope and kept the original shape — an UNLOCKED ownership
// probe (`SELECT 1 FROM savings_goals … `, no FOR UPDATE) and no basis assertion at all. Both
// automatic callers do filter on the basis (transactionSyncService.detectAndUpsertProposed-
// Contributions and transferCorrelationService.createBoostMatchContribution each skip a goal whose
// ProgressBasis is not "contributions"), but that read is taken from a goal list loaded before a
// whole loop of further round-trips — it is the definition of check-then-act, not a reason the
// condition is unreachable.
func TestSavingsBasisSwitch_Integration_UpsertPathsRefuseAManualGoal(t *testing.T) {
	f := setupBasisSwitchFixture(t, domain.ProgressBasisManual, 0.0)
	ctx := context.Background()

	t.Run("UpsertFromTransaction", func(t *testing.T) {
		txnID := f.seedTransaction(t, -120.0)
		err := f.contribRepo.UpsertFromTransaction(ctx, f.userID, f.goalID, txnID, 50.0, "boost_match")
		assert.ErrorIs(t, err, domain.ErrContributionBasisMismatch)
	})

	t.Run("UpsertProposedFromTransaction", func(t *testing.T) {
		txnID := f.seedTransaction(t, -130.0)
		err := f.contribRepo.UpsertProposedFromTransaction(ctx, f.userID, f.goalID, txnID, 60.0)
		assert.ErrorIs(t, err, domain.ErrContributionBasisMismatch)
	})

	t.Run("InsertManualFromTransaction", func(t *testing.T) {
		txnID := f.seedTransaction(t, -140.0)
		_, err := f.contribRepo.InsertManualFromTransaction(ctx, f.userID, f.goalID, txnID, 70.0,
			time.Now().Format("2006-01-02"))
		assert.ErrorIs(t, err, domain.ErrContributionBasisMismatch)
	})

	assert.Equal(t, 0, f.contributionCount(t), "no refused upsert may have written anything")
}

// TestSavingsBasisSwitch_Integration_UpsertPathsStillAcceptALiveLedger is the other direction: the
// guard must refuse a goal that has left the ledger and NOTHING else. Without it, a guard that
// simply returned an error unconditionally would pass the test above.
func TestSavingsBasisSwitch_Integration_UpsertPathsStillAcceptALiveLedger(t *testing.T) {
	f := setupBasisSwitchFixture(t, domain.ProgressBasisContributions, 0.0)
	ctx := context.Background()

	proposedTxn := f.seedTransaction(t, -200.0)
	require.NoError(t, f.contribRepo.UpsertProposedFromTransaction(ctx, f.userID, f.goalID, proposedTxn, 200.0))

	confirmedTxn := f.seedTransaction(t, -300.0)
	require.NoError(t, f.contribRepo.UpsertFromTransaction(ctx, f.userID, f.goalID, confirmedTxn, 300.0, "boost_match"))

	manualTxn := f.seedTransaction(t, -400.0)
	got, err := f.contribRepo.InsertManualFromTransaction(ctx, f.userID, f.goalID, manualTxn, 400.0,
		time.Now().Format("2006-01-02"))
	require.NoError(t, err)
	assert.Equal(t, 400.0, got.Amount)

	assert.Equal(t, 3, f.contributionCount(t), "all three live-ledger writes must have landed")
}

// TestSavingsBasisSwitch_Integration_UpsertPathsCannotDeadlockAgainstConfirm is the whole reason
// Task 22 is a task of its own, and it is worth more than the two tests above put together.
//
// THE TRAP. The established lock order on this branch is goal → contribution → transaction.
// ConfirmContribution takes all three in that order. The upsert paths already lock the
// TRANSACTION, so bolting the goal assertion on AFTER that lock — the obvious place, right next to
// the capacity check it protects — gives the upsert the order transaction → goal and creates a
// textbook ABBA cycle. It would deadlock intermittently in production and be almost impossible to
// attribute.
//
// THE MECHANISM, which is deterministic rather than a hope that two goroutines happen to
// interleave. A third connection holds the GOAL row (and only the goal row) locked, so both racers
// are parked at a known point:
//
//	blocker : BEGIN; SELECT … FROM savings_goals … FOR UPDATE          (holds G)
//	confirm : ConfirmContribution → wants G first                      → queued behind blocker
//	upsert  : UpsertFromTransaction → wants G first (fixed)            → queued behind confirm
//	                                → takes T first (broken)           → holds T, queued for G
//	blocker : COMMIT                                                   → releases G
//
// With the fix, the goal lock is the first thing both take, the wait queue is FIFO, and they
// simply serialise. With the assertion after the transaction lock, `upsert` is sitting on T while
// `confirm` — which now owns G — reaches for T, and Postgres kills one of them with
// SQLSTATE 40P01. Asserting "neither error is a deadlock, and both returned" therefore fails RED
// on the wrong placement and passes GREEN on the right one.
func TestSavingsBasisSwitch_Integration_UpsertPathsCannotDeadlockAgainstConfirm(t *testing.T) {
	f := setupBasisSwitchFixture(t, domain.ProgressBasisContributions, 0.0)
	ctx := context.Background()

	txnID := f.seedTransaction(t, -500.0)
	proposedID := uuid.New().String()
	_, err := f.pool.Exec(ctx, `
		INSERT INTO savings_goal_contributions
			(id, savings_goal_id, transaction_id, amount, source, status,
			 managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at)
		VALUES ($1, $2, $3, 100.0, 'confirmed_transfer', 'proposed', true, false, CURRENT_DATE, now(), now())
	`, proposedID, f.goalID, txnID)
	require.NoError(t, err)

	// The blocker: holds the goal row, and nothing else, until both racers are parked.
	blocker, err := f.pool.Begin(ctx)
	require.NoError(t, err)
	var heldBasis string
	require.NoError(t, blocker.QueryRow(ctx,
		`SELECT progress_basis FROM savings_goals WHERE id = $1 FOR UPDATE`, f.goalID).Scan(&heldBasis))

	var confirmErr, upsertErr error
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, confirmErr = f.contribRepo.ConfirmContribution(ctx, f.userID, f.goalID, proposedID)
	}()
	// Let the confirmer reach the goal lock first, so it is ahead of the upsert in the wait queue.
	time.Sleep(500 * time.Millisecond)

	go func() {
		defer wg.Done()
		upsertErr = f.contribRepo.UpsertFromTransaction(ctx, f.userID, f.goalID, txnID, 100.0, "boost_match")
	}()
	// Let the upsert reach ITS first lock — the goal when the order is right, the transaction when
	// it is not. This sleep is what makes the broken ordering deadlock deterministically instead
	// of occasionally.
	time.Sleep(500 * time.Millisecond)

	require.NoError(t, blocker.Commit(ctx))

	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("confirm and upsert did not both complete: they are blocked on each other")
	}

	for name, err := range map[string]error{"confirm": confirmErr, "upsert": upsertErr} {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			assert.NotEqual(t, "40P01", pgErr.Code,
				"%s was killed as a deadlock victim — the upsert paths take their locks out of order", name)
		}
	}
	// Neither is entitled to a basis mismatch here: the goal never left the contributions basis.
	assert.NotErrorIs(t, confirmErr, domain.ErrContributionBasisMismatch)
	assert.NotErrorIs(t, upsertErr, domain.ErrContributionBasisMismatch)
}
