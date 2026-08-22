// Package repository_test — household_accept_concurrency_integration_test.go proves
// the accept-transaction's serializable seat re-check (#1367 — Phase 3) actually
// prevents seat overflow under real concurrent contention. This is the
// repository-layer analog of HouseholdService.AcceptInvite steps (4)-(5):
// LockHouseholdForUpdate → CountActiveMembers → InsertMember, run by N goroutines
// racing for the same household's last seat.
//
// Per project rule: pgxmock never catches constraint/type errors or exercises real row
// locking — only a real Postgres proves FOR UPDATE actually serializes concurrent
// transactions (see memory feedback_pgxmock_integration_coverage.md).
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/repository/ -run TestHouseholdAcceptConcurrency -v
package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// errSeatsFull is the local sentinel returned by attemptJoin when the seat cap is
// already met — analogous to domain.ErrHouseholdSeatsFull, kept local to avoid this
// test depending on the household_service package's decision logic.
var errSeatsFull = errors.New("seats full")

// attemptJoin replicates the accept-tx's seat-gated join sequence in isolation:
// BEGIN; LockHouseholdForUpdate; CountActiveMembers; if under seatCap INSERT member; COMMIT.
// Returns nil on a successful join, repository is untouched by rollback on any failure
// (including the seats-full case, which returns a sentinel-wrapped error).
func attemptJoin(t *testing.T, pool *pgxpool.Pool, householdID, userID string, seatCap int) error {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op if already committed

	hhRepo := repository.NewHouseholdRepository(tx)

	if _, err := hhRepo.LockHouseholdForUpdate(ctx, householdID); err != nil {
		return err
	}
	active, err := hhRepo.CountActiveMembers(ctx, householdID)
	if err != nil {
		return err
	}
	if active >= seatCap {
		return errSeatsFull
	}
	member := &domain.HouseholdMember{
		HouseholdID: householdID,
		UserID:      userID,
		Role:        domain.HouseholdRoleMember,
		Status:      domain.HouseholdMemberStatusActive,
	}
	if err := hhRepo.InsertMember(ctx, member); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func TestHouseholdAcceptConcurrency_SeatCapEnforced(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	ownerID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestHouseholdMember(t, pool, householdID, ownerID, "owner") // seat 1 of 2 (household_2 seatCap)

	const seatCap = 2 // household_2 tier: UsersIncluded=2
	const contenders = 5
	contenderIDs := make([]string, contenders)
	for i := range contenderIDs {
		contenderIDs[i] = insertTestUser(t, pool)
	}

	var wg sync.WaitGroup
	errs := make([]error, contenders)
	for i, uid := range contenderIDs {
		wg.Add(1)
		go func(idx int, userID string) {
			defer wg.Done()
			errs[idx] = attemptJoin(t, pool, householdID, userID, seatCap)
		}(i, uid)
	}
	wg.Wait()

	successCount, seatsFullCount, otherErrCount := 0, 0, 0
	for _, err := range errs {
		switch err {
		case nil:
			successCount++
		case errSeatsFull:
			seatsFullCount++
		default:
			otherErrCount++
			t.Logf("unexpected error: %v", err)
		}
	}

	if successCount != 1 {
		t.Errorf("expected exactly 1 successful join (only 1 seat remaining under a seatCap of %d with owner already seated), got %d", seatCap, successCount)
	}
	if seatsFullCount != contenders-1 {
		t.Errorf("expected %d seats-full rejections, got %d", contenders-1, seatsFullCount)
	}
	if otherErrCount != 0 {
		t.Errorf("expected 0 unexpected errors, got %d", otherErrCount)
	}

	finalCount, err := repository.NewHouseholdRepository(pool).CountActiveMembers(ctx, householdID)
	if err != nil {
		t.Fatalf("CountActiveMembers: %v", err)
	}
	if finalCount != seatCap {
		t.Errorf("expected final active member count to equal seatCap %d (no overflow), got %d", seatCap, finalCount)
	}

	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM household_members WHERE household_id = $1`, householdID) //nolint:errcheck
	})
}
