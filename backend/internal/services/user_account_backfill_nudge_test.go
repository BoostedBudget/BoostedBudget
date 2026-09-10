package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// #1631 (M4) — the backfill NUDGE.
//
// ⚠ The nudge is not the obligation, and these tests are written to keep that distinction
// enforceable. The obligation is user_accounts.investment_backfill_requested_at, written by
// Postgres inside the same UPDATE that clears the flag (see
// investment_backfill_obligation_integration_test.go). This enqueue only decides whether the sync
// that honours it runs in seconds or whenever Plaid next has something to say about the item —
// which, for a brokerage with no activity, can be months. So every failure here must be a no-op,
// never an error, and the test that proves it is the one that matters most.
// ============================================================================

type backfillNudgeHarness struct {
	svc         UserAccountService
	repo        *MockUserAccountRepository
	accountRepo *MockAccountRepository
	jobQueue    *MockJobQueue
	chainID     string
	userID      string
	itemID      string
}

// newBackfillNudgeHarness builds a service whose chain is currently EXCLUDED, so that a PATCH
// clearing the flag is the true -> false transition the nudge fires on.
func newBackfillNudgeHarness(t *testing.T, wire bool) *backfillNudgeHarness {
	t.Helper()
	h := &backfillNudgeHarness{
		repo:        new(MockUserAccountRepository),
		accountRepo: new(MockAccountRepository),
		jobQueue:    new(MockJobQueue),
		chainID:     uuid.New().String(),
		userID:      uuid.New().String(),
		itemID:      uuid.New().String(),
	}
	h.svc = NewUserAccountService(h.repo)
	if wire {
		h.svc.SetInvestmentBackfillEnqueuer(h.jobQueue, h.accountRepo, nil)
	}

	h.repo.On("GetByIDAndUser", mock.Anything, h.chainID, h.userID).Return(&domain.UserAccount{
		ID:                    h.chainID,
		UserID:                h.userID,
		InstitutionID:         "ins_brokerage",
		ExcludeInvestmentSync: true,
		PrimaryType:           strPtrIfNotEmpty("investment"),
	}, nil)
	h.repo.On("Update", mock.Anything, mock.Anything).Return(nil)
	return h
}

func (h *backfillNudgeHarness) patchExclude(t *testing.T, ctx context.Context, exclude bool) {
	t.Helper()
	_, err := h.svc.UpdateCustomization(ctx, h.chainID, h.userID, &domain.UpdateUserAccountRequest{
		ExcludeInvestmentSync: &exclude,
	})
	require.NoError(t, err)
}

// TestBackfillNudge_UnTogglingEnqueuesExactlyOneSyncForTheLiveItem — one job, for the item that
// currently carries the chain. "Exactly one" is asserted with Times(1) rather than a loose
// AssertCalled: a chain spans re-links and has MANY accounts rows, so a naive implementation that
// iterated them would enqueue a duplicate sync per historical member.
func TestBackfillNudge_UnTogglingEnqueuesExactlyOneSyncForTheLiveItem(t *testing.T) {
	h := newBackfillNudgeHarness(t, true)
	ctx := context.Background()

	h.accountRepo.On("GetByUserAccountIDAndUser", ctx, h.chainID, h.userID).Return(&domain.Account{
		ID:            uuid.New().String(),
		LinkedItemID:  h.itemID,
		UserID:        h.userID,
		Status:        domain.AccountStatusActive,
		UserAccountID: &h.chainID,
	}, nil)

	h.jobQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		args, ok := job.(investmentTransactionsSyncJobArgs)
		return ok && args.ItemID == h.itemID && args.UserID == h.userID
	}), mock.Anything).Return(&queue.JobResult{}, nil).Times(1)

	h.patchExclude(t, ctx, false)

	h.jobQueue.AssertExpectations(t)
}

// TestBackfillNudge_AFailedEnqueueDoesNotFailTheRequest is the recoverability guarantee, and the
// reason the obligation was put on the row instead of in the queue. The customization SUCCEEDED and
// the obligation is committed; a queue that is down must not turn that into a 500 the user retries,
// because the retry would be false -> false and would record no new obligation.
func TestBackfillNudge_AFailedEnqueueDoesNotFailTheRequest(t *testing.T) {
	h := newBackfillNudgeHarness(t, true)
	ctx := context.Background()

	h.accountRepo.On("GetByUserAccountIDAndUser", ctx, h.chainID, h.userID).Return(&domain.Account{
		ID:           uuid.New().String(),
		LinkedItemID: h.itemID,
		UserID:       h.userID,
		Status:       domain.AccountStatusActive,
	}, nil)
	h.jobQueue.On("Enqueue", ctx, mock.Anything, mock.Anything).
		Return(nil, errors.New("river is unreachable"))

	updated, err := h.svc.UpdateCustomization(ctx, h.chainID, h.userID,
		&domain.UpdateUserAccountRequest{ExcludeInvestmentSync: boolPtr(false)})

	require.NoError(t, err,
		"a failed nudge must not fail the customization: the flag write and the durable backfill "+
			"obligation are already committed, and a user-visible error would invite a retry that "+
			"records nothing (false -> false is not a transition)")
	require.NotNil(t, updated)
	assert.False(t, updated.ExcludeInvestmentSync)
	h.repo.AssertCalled(t, "Update", mock.Anything, mock.Anything)
}

// TestBackfillNudge_NoLiveAccountIsNotAnError — the chain's every account may be terminal
// (disconnected, replaced), so there is no item to sync through. The obligation stays on the row
// for whenever one exists again.
func TestBackfillNudge_NoLiveAccountIsNotAnError(t *testing.T) {
	h := newBackfillNudgeHarness(t, true)
	ctx := context.Background()

	h.accountRepo.On("GetByUserAccountIDAndUser", ctx, h.chainID, h.userID).
		Return(nil, errors.New("account not found"))

	h.patchExclude(t, ctx, false)

	h.jobQueue.AssertNotCalled(t, "Enqueue", mock.Anything, mock.Anything, mock.Anything)
}

// TestBackfillNudge_SettingTheFlagNudgesNothing — the nudge fires on re-INCLUSION only. Firing on
// exclusion would order a sync whose only job is to decline to store anything.
func TestBackfillNudge_SettingTheFlagNudgesNothing(t *testing.T) {
	h := newBackfillNudgeHarness(t, true)
	ctx := context.Background()

	h.patchExclude(t, ctx, true)

	h.jobQueue.AssertNotCalled(t, "Enqueue", mock.Anything, mock.Anything, mock.Anything)
	h.accountRepo.AssertNotCalled(t, "GetByUserAccountIDAndUser", mock.Anything, mock.Anything, mock.Anything)
}

// TestBackfillNudge_AnUnrelatedEditNudgesNothing — a nickname change routes through the same
// UpdateCustomization and the same UPDATE statement. Nudging on it would enqueue a sync for every
// cosmetic edit any user makes to any excluded chain.
func TestBackfillNudge_AnUnrelatedEditNudgesNothing(t *testing.T) {
	h := newBackfillNudgeHarness(t, true)
	ctx := context.Background()

	nickname := "Brokerage Cash"
	_, err := h.svc.UpdateCustomization(ctx, h.chainID, h.userID,
		&domain.UpdateUserAccountRequest{Nickname: &nickname})
	require.NoError(t, err)

	h.jobQueue.AssertNotCalled(t, "Enqueue", mock.Anything, mock.Anything, mock.Anything)
}

// TestBackfillNudge_UnwiredEnqueuerIsASilentNoOp — a deployment with no River client (and every
// test that does not wire one) must still be able to clear the flag. The obligation is durable
// without the nudge, so its absence is a latency cost and nothing more.
func TestBackfillNudge_UnwiredEnqueuerIsASilentNoOp(t *testing.T) {
	h := newBackfillNudgeHarness(t, false)
	ctx := context.Background()

	h.patchExclude(t, ctx, false)

	h.jobQueue.AssertNotCalled(t, "Enqueue", mock.Anything, mock.Anything, mock.Anything)
	h.accountRepo.AssertNotCalled(t, "GetByUserAccountIDAndUser", mock.Anything, mock.Anything, mock.Anything)
}
