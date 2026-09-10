package services

// plaid_item_cleanup_service_side_effect_claim_integration_test.go — real-Postgres proof that
// repository.ActionPlaidDisconnectUser genuinely excludes two concurrent flows from calling the
// Plaid collaborator for the same user at once (Issue #1413 H1). Mirrors
// household_service_integration_test.go's convention: no build tag, skips at runtime if
// DATABASE_URL is unset — pgxmock can only match query text and cannot prove genuine concurrent
// exclusivity against the ON CONFLICT ... WHERE semantics work_claims depends on (see
// internal/repository/work_claims_repository_integration_test.go for the generic mechanism proof;
// this test proves plaidItemCleanupService actually wires that mechanism into a real "the
// collaborator was never called" outcome, not just that the SQL primitive works in isolation).
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/ -run TestPlaidCleanupService_Integration -v

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLinkedItemRepoForRace is a minimal linkedItemRepoForCleanup stub returning one canned item
// per call. This test is about work_claims exclusivity against a REAL Postgres backend, not
// linked_items persistence (covered separately in
// linked_item_repository_mark_disconnected_integration_test.go), so the linked-item side stays a
// fake.
type fakeLinkedItemRepoForRace struct {
	item domain.LinkedItem
}

func (f *fakeLinkedItemRepoForRace) GetByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	return []domain.LinkedItem{f.item}, nil
}
func (f *fakeLinkedItemRepoForRace) GetByID(ctx context.Context, id string) (*domain.LinkedItem, error) {
	return &f.item, nil
}
func (f *fakeLinkedItemRepoForRace) GetActiveItemsByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	return []domain.LinkedItem{f.item}, nil
}
func (f *fakeLinkedItemRepoForRace) MarkAllUserItemsDisconnected(ctx context.Context, userID string) error {
	return nil
}
func (f *fakeLinkedItemRepoForRace) MarkItemsDisconnected(ctx context.Context, ids []string) error {
	return nil
}
func (f *fakeLinkedItemRepoForRace) MarkPlaidRemoved(ctx context.Context, ids []string) error {
	return nil
}

// MarkPlaidRemovalFailed — Issue #1494. This fixture's Plaid stub never fails, so no terminal
// verdict is ever recorded; present to satisfy the interface.
func (f *fakeLinkedItemRepoForRace) MarkPlaidRemovalFailed(ctx context.Context, ids []string, errorCode string) error {
	return nil
}

// blockingPlaidCollaborator is a plaidServiceForCleanup stub whose RemoveItem call signals
// `entered` (once) and then blocks on `release` — used to hold the side-effect claim open long
// enough for a concurrent second attempt to observe it as busy, without relying on sleep-based
// timing.
type blockingPlaidCollaborator struct {
	mu          sync.Mutex
	calls       int
	release     chan struct{}
	entered     chan struct{}
	enteredOnce sync.Once
}

func (b *blockingPlaidCollaborator) RemoveItem(ctx context.Context, accessToken string) error {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	b.enteredOnce.Do(func() { close(b.entered) })
	<-b.release
	return nil
}

func (b *blockingPlaidCollaborator) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// countingPlaidCollaborator is a non-blocking plaidServiceForCleanup stub that just counts calls.
type countingPlaidCollaborator struct {
	mu    sync.Mutex
	calls int
}

func (c *countingPlaidCollaborator) RemoveItem(ctx context.Context, accessToken string) error {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return nil
}

func (c *countingPlaidCollaborator) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestPlaidCleanupService_Integration_SideEffectClaim_TwoFlowsRacing_OnlyOneCallsPlaid is the
// falsifiability-critical H1 test: two INDEPENDENT plaidItemCleanupService instances (standing in
// for two different flows — e.g. a past-due grace-cancel cron and a subscription-cancellation
// webhook — sharing only the real work_claims table, exactly as production does via one shared
// service instance) both try to disconnect the SAME user's Plaid items at once. Exactly one must
// actually call Plaid; the other must back off with ErrSideEffectClaimBusy rather than silently
// proceeding as though the disconnect had already happened.
func TestPlaidCleanupService_Integration_SideEffectClaim_TwoFlowsRacing_OnlyOneCallsPlaid(t *testing.T) {
	pool := setupHouseholdServiceIntegrationDB(t)
	ctx := context.Background()
	claimsRepo := repository.NewWorkClaimsRepository(pool)

	userID := uuid.New().String()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM work_claims WHERE resource_type = $1 AND resource_id = $2`,
			repository.ResourceTypeUser, userID)
	})

	item := domain.LinkedItem{ID: "item-race", UserID: userID, AccessToken: "tok"}

	holder := &blockingPlaidCollaborator{release: make(chan struct{}), entered: make(chan struct{})}
	holderSvc := NewPlaidItemCleanupService(&fakeLinkedItemRepoForRace{item: item}, holder)
	holderSvc.SetWorkClaimsRepo(claimsRepo)
	// Issue #1569 Task 20: a no-op account-repository stand-in — see the note in
	// plaid_item_cleanup_service_null_status_integration_test.go.
	holderSvc.SetAccountRepo(permissiveAccountRepoForCleanup{})

	racer := &countingPlaidCollaborator{}
	racerSvc := NewPlaidItemCleanupService(&fakeLinkedItemRepoForRace{item: item}, racer)
	racerSvc.SetWorkClaimsRepo(claimsRepo)
	// Issue #1569 Task 20: a no-op account-repository stand-in — see the note in
	// plaid_item_cleanup_service_null_status_integration_test.go.
	racerSvc.SetAccountRepo(permissiveAccountRepoForCleanup{})

	holderErrCh := make(chan error, 1)
	go func() {
		holderErrCh <- holderSvc.RemoveAllUserItems(ctx, userID, alwaysEligible)
	}()

	<-holder.entered // block until the holder is genuinely inside the Plaid call, claim held

	racerErr := racerSvc.RemoveAllUserItems(ctx, userID, alwaysEligible)
	assert.ErrorIs(t, racerErr, ErrSideEffectClaimBusy,
		"the racer must be told the side effect is busy, not proceed as though the disconnect already happened")
	assert.Equal(t, 0, racer.callCount(), "the racer's Plaid collaborator must never be called while the holder is mid-flight")

	close(holder.release)
	require.NoError(t, <-holderErrCh, "the holder must complete successfully once unblocked")
	assert.Equal(t, 1, holder.callCount(), "the holder's Plaid collaborator must have run exactly once")

	// After the holder releases the claim, the operation is legitimately reclaimable again (a
	// Plaid disconnect is not a once-ever-per-user event — see claimSideEffectAndRun's doc
	// comment) — proving Release, not Complete, was used.
	racerErr = racerSvc.RemoveAllUserItems(ctx, userID, alwaysEligible)
	assert.NoError(t, racerErr, "once the holder releases, the side-effect claim must be immediately reclaimable — Plaid disconnect can legitimately recur")
	assert.Equal(t, 1, racer.callCount(), "the racer's retry must now succeed and call Plaid")
}

// #1626 Phase 3, Defect 3: the periodic balance-capture dispatcher's global item enumeration.
// Nothing in this test file's subject reaches it; the method exists so the mock still satisfies
// repository.LinkedItemRepository.
func (f *fakeLinkedItemRepoForRace) GetItemsForBalanceCapturePage(context.Context, *repository.BalanceCaptureSweepCursor, int) ([]domain.BalanceCaptureTarget, error) {
	return nil, nil
}
