package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Issue #1486 round-15 (Codex pass 5, Medium 2): the claim grants no exclusivity ──────────────
//
// ClaimItemForCleanup is a conditional UPDATE whose guarantee ends with its own statement (see its
// doc comment, linked_item_repository.go). Between it and the Plaid /item/remove sits a whole
// separate service method with its own read, so a connection repair or an active-account write can
// commit in the middle and the item is disconnected anyway. Work now hands RemoveItem the SAME
// eligibility question as a callback, and RemoveItem asks it at the point of use.
//
// These tests drive the worker; MockPlaidCleanupService.RemoveItem consults the callback exactly as
// the real service does, so what is being pinned here is the WORKER's half: that it supplies a real
// recheck, and that a decline at use time takes the same release-the-stale-schedule path as a
// decline at claim time (no invented second behaviour).

// TestItemCleanupWorker_Work_BecomesIneligibleAfterClaim_SkipsRemovalAndReleasesSchedule is the RED
// test: the claim is GRANTED, and the item then becomes ineligible before the removal call.
func TestItemCleanupWorker_Work_BecomesIneligibleAfterClaim_SkipsRemovalAndReleasesSchedule(t *testing.T) {
	ctx := context.Background()
	linkedItemID := "item-repaired-after-claim"

	mockCleanupSvc := new(MockPlaidCleanupService)
	mockRepo := new(MockLinkedItemRepoForCleanup)

	// Granted at claim time, declined at use time — the exact window the claim cannot cover.
	mockRepo.On("ClaimItemForCleanup", ctx, linkedItemID, defaultCleanupErrorDays, defaultCleanupStaleDays).
		Return(true, nil).Once()
	mockRepo.On("ClaimItemForCleanup", ctx, linkedItemID, defaultCleanupErrorDays, defaultCleanupStaleDays).
		Return(false, nil).Once()
	mockRepo.On("ClearCleanupSchedule", ctx, linkedItemID).Return(nil)

	worker := NewItemCleanupWorker(ItemCleanupWorkerDeps{
		PlaidCleanupSvc: mockCleanupSvc,
		LinkedItemRepo:  mockRepo,
		Logger:          slog.Default(),
	})

	err := worker.Work(ctx, newTestItemCleanupJob(ItemCleanupArgs{
		LinkedItemID: linkedItemID,
		UserID:       "user-456",
		Reason:       "long_term_error",
	}))

	// Same outcome as a decline at claim time: success with nothing to do, stale schedule released.
	require.NoError(t, err)
	mockRepo.AssertNotCalled(t, "Delete")
	mockRepo.AssertCalled(t, "ClearCleanupSchedule", ctx, linkedItemID)
	mockRepo.AssertNumberOfCalls(t, "ClaimItemForCleanup", 2)
	mockRepo.AssertExpectations(t)
}

// TestItemCleanupWorker_Work_BecomesIneligibleAfterClaim_ClearFailureIsRetried pins that the
// use-time decline reuses the claim-time decline's recovery behaviour exactly: a failed release is
// RETURNED, because returning nil would let River record success and never come back, leaving
// cleanup_scheduled_at set on an item no sweep can re-select.
func TestItemCleanupWorker_Work_BecomesIneligibleAfterClaim_ClearFailureIsRetried(t *testing.T) {
	ctx := context.Background()
	linkedItemID := "item-repaired-clear-fails"

	mockCleanupSvc := new(MockPlaidCleanupService)
	mockRepo := new(MockLinkedItemRepoForCleanup)

	mockRepo.On("ClaimItemForCleanup", ctx, linkedItemID, defaultCleanupErrorDays, defaultCleanupStaleDays).
		Return(true, nil).Once()
	mockRepo.On("ClaimItemForCleanup", ctx, linkedItemID, defaultCleanupErrorDays, defaultCleanupStaleDays).
		Return(false, nil).Once()
	mockRepo.On("ClearCleanupSchedule", ctx, linkedItemID).Return(errors.New("connection reset"))

	worker := NewItemCleanupWorker(ItemCleanupWorkerDeps{
		PlaidCleanupSvc: mockCleanupSvc,
		LinkedItemRepo:  mockRepo,
		Logger:          slog.Default(),
	})

	err := worker.Work(ctx, newTestItemCleanupJob(ItemCleanupArgs{
		LinkedItemID: linkedItemID,
		UserID:       "user-456",
		Reason:       "long_term_error",
	}))

	require.Error(t, err, "a failed schedule release must surface so River retries it")
	assert.Contains(t, err.Error(), "connection reset")
	mockRepo.AssertNotCalled(t, "Delete")
	mockRepo.AssertExpectations(t)
}

// TestItemCleanupWorker_Work_StillEligibleAtUseTime_RemovesItem is the MANDATORY sibling: an item
// that remains a legitimate cleanup target at BOTH check points is still removed and still deleted.
// Without it, the two tests above pass equally well if the use-time recheck became a blanket refusal
// — the both-false trap.
func TestItemCleanupWorker_Work_StillEligibleAtUseTime_RemovesItem(t *testing.T) {
	ctx := context.Background()
	linkedItemID := "item-still-a-candidate"

	mockCleanupSvc := new(MockPlaidCleanupService)
	mockRepo := new(MockLinkedItemRepoForCleanup)

	mockRepo.On("ClaimItemForCleanup", ctx, linkedItemID, defaultCleanupErrorDays, defaultCleanupStaleDays).
		Return(true, nil)
	mockCleanupSvc.On("RemoveItem", ctx, linkedItemID).Return(nil)
	mockRepo.On("ClearCleanupSchedule", ctx, linkedItemID).Return(nil)
	mockRepo.On("Delete", ctx, linkedItemID).Return(nil)

	worker := NewItemCleanupWorker(ItemCleanupWorkerDeps{
		PlaidCleanupSvc: mockCleanupSvc,
		LinkedItemRepo:  mockRepo,
		Logger:          slog.Default(),
	})

	err := worker.Work(ctx, newTestItemCleanupJob(ItemCleanupArgs{
		LinkedItemID: linkedItemID,
		UserID:       "user-456",
		Reason:       "long_term_error",
	}))

	require.NoError(t, err)
	mockCleanupSvc.AssertCalled(t, "RemoveItem", ctx, linkedItemID)
	mockRepo.AssertCalled(t, "Delete", ctx, linkedItemID)
	mockRepo.AssertExpectations(t)
	mockCleanupSvc.AssertExpectations(t)
}
