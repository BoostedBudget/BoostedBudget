package jobs

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ─── Issue #1500: the subscription-cancellation webhook's per-item recheck ──────────────────────
//
// processSubscriptionCanceled re-verifies ONE predicate immediately before its Plaid disconnect —
// "no comp has been granted since this webhook was claimed" (reVerified.HasActiveManualOverride) —
// and then makes one Plaid /item/remove call PER LINKED ITEM. A comp granted after the first item
// was removed used to be invisible: the remaining items were removed anyway, and an admin who had
// just comped the user watched their bank connections disappear one by one.
//
// THE PREDICATE IS THE COMP CHECK, NOT past-due. This flow evaluates nothing about past_due before
// its loop, so re-asking a past-due question here would be inventing an authorisation rule rather
// than re-asking the one this caller was already selected under. The past-due flows' own fix uses
// reReadStillPastDue for exactly the same reason, one caller over
// (services/past_due_window_recheck_test.go).

// fakePerItemWebhookCleanup simulates cleanupItems's per-item loop, which is the only way a unit
// test can see the difference the recheck makes: a testify mock treats "disconnect this user" as one
// atomic call, and the defect lives strictly BETWEEN items. Its contract mirrors the real
// cleanupItems: the recheck runs before EVERY item including the first, the first (false, nil) stops
// the loop without attempting anything further, and a loop that stopped early reports
// services.ErrCleanupAborted rather than nil.
type fakePerItemWebhookCleanup struct {
	itemIDs []string
	// removed records, in order, the items actually sent to Plaid /item/remove.
	removed []string
	// ifEligibleCalls records that production called the disconnect at all. There is no nil-recheck
	// entry point to count alongside it — Issue #1500 deleted it from the interface.
	ifEligibleCalls int
	// onRemoved, if set, runs immediately after each item is removed — how a test changes the
	// world partway through the loop.
	onRemoved func(itemID string)
}

func (f *fakePerItemWebhookCleanup) DisconnectAllUserItemsIfEligible(ctx context.Context, userID string, recheck func(ctx context.Context) (bool, error)) error {
	f.ifEligibleCalls++
	return f.loop(ctx, recheck)
}

func (f *fakePerItemWebhookCleanup) loop(ctx context.Context, recheck func(ctx context.Context) (bool, error)) error {
	if recheck == nil {
		// Mirrors the real service, which refuses a nil recheck before touching the repository
		// (Issue #1500) rather than treating it as "no recheck wanted".
		return fmt.Errorf("DisconnectAllUserItemsIfEligible: an eligibility recheck is required")
	}
	attempted := 0
	for _, id := range f.itemIDs {
		eligible, err := recheck(ctx)
		if err != nil {
			return fmt.Errorf("eligibility recheck failed before item %s: %w", id, err)
		}
		if !eligible {
			break
		}
		f.removed = append(f.removed, id)
		attempted++
		if f.onRemoved != nil {
			f.onRemoved(id)
		}
	}
	if attempted < len(f.itemIDs) {
		return fmt.Errorf("%w for the fake's user (%d of %d item(s) not attempted)",
			services.ErrCleanupAborted, len(f.itemIDs)-attempted, len(f.itemIDs))
	}
	return nil
}

// compAfter returns an onRemoved hook that grants the user a live comp — mutating the row the mocked
// repository hands back — the moment `afterItemID` has been removed. Mutating the shared row rather
// than programming a read count keeps the test about WHEN the comp landed relative to the loop, not
// about how many times the flow re-reads.
func compAfter(row *domain.User, afterItemID string) func(string) {
	return func(itemID string) {
		if itemID != afterItemID {
			return
		}
		tier := "premium"
		endsAt := time.Now().Add(30 * 24 * time.Hour)
		row.ManualTierOverride = &tier
		row.ManualTierEndsAt = &endsAt
	}
}

// TestWorker_SubscriptionCanceled_CompMidLoop_StopsRemainingRemovals is the RED test for #1500's
// webhook call site. An admin comps the user after the FIRST item has been removed; the remaining
// two must stay connected, and the retention clock — the destructive write this flow gates on the
// very same comp predicate before the loop — must not be started either.
func TestWorker_SubscriptionCanceled_CompMidLoop_StopsRemainingRemovals(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	plaid := &fakePerItemWebhookCleanup{itemIDs: []string{"item-1", "item-2", "item-3"}}
	worker.SetPlaidCleanupService(plaid)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	job := createRiverJob(SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	})

	row := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}
	// The row carries the CANCELLED status the handler's own UpdateSubscriptionFromPaddle write has
	// just committed — the mocked repository does not apply writes to the row it hands back, so the
	// fixture has to model the post-write world the re-reads actually see in production.
	row.SubscriptionStatus = string(domain.SubscriptionStatusCancelled)
	plaid.onRemoved = compAfter(row, "item-1")

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(row, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)
	// Permissive on purpose: this test's point is that the retention clock must NOT be started, and
	// a missing expectation would surface as a testify panic instead of the assertion that carries
	// the meaning.
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything).Return(nil).Maybe()

	err := worker.Work(ctx, job)

	require.NoError(t, err, "a comp granted mid-loop is a legitimate stop, not a webhook failure")
	assert.Equal(t, []string{"item-1"}, plaid.removed,
		"only the item removed before the comp may be gone; the remaining two must stay connected")
	assert.Equal(t, 1, plaid.ifEligibleCalls,
		"the cancellation webhook must use the recheck-carrying entry point")
	mockProfileRepo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything)
}

// TestWorker_SubscriptionCanceled_NoComp_RemovesEveryItem is the MANDATORY sibling assertion:
// without it the test above passes just as well if the disconnect broke outright. A user who is
// never comped must lose EVERY item and must get the retention clock started.
func TestWorker_SubscriptionCanceled_NoComp_RemovesEveryItem(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	plaid := &fakePerItemWebhookCleanup{itemIDs: []string{"item-1", "item-2", "item-3"}}
	worker.SetPlaidCleanupService(plaid)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	job := createRiverJob(SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	})

	row := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
		// Still cancelled for the whole loop, and never comped: BOTH conjuncts of the per-item
		// predicate hold at every item, so nothing may be skipped.
		SubscriptionStatus: string(domain.SubscriptionStatusCancelled),
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(row, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	assert.Equal(t, []string{"item-1", "item-2", "item-3"}, plaid.removed,
		"a user who is never comped must have every item removed — the recheck must not become a blanket refusal")
	assert.Equal(t, 1, plaid.ifEligibleCalls)
	mockProfileRepo.AssertCalled(t, "SetRetentionDates", ctx, "user_2abc123def456", mock.Anything)
}

// reactivationAfter returns an onRemoved hook that flips the row back to an ACTIVE subscription —
// what a subscription.activated / subscription.updated webhook writes — the moment `afterItemID`
// has been removed. Webhook jobs deliberately carry no River uniqueness options
// (unique_opts_by_kind.go), so a newer activation genuinely can land while an older cancellation
// job is still walking this user's items; mutating the shared row is how a unit test expresses
// "the world changed between item 1 and item 2".
func reactivationAfter(row *domain.User, afterItemID string) func(string) {
	return func(itemID string) {
		if itemID != afterItemID {
			return
		}
		row.SubscriptionStatus = string(domain.SubscriptionStatusActive)
	}
}

// TestWorker_SubscriptionCanceled_ReactivationMidLoop_StopsRemainingRemovals is the RED test for the
// SECOND conjunct of the per-item predicate. The comp check alone answers "has an admin granted a
// compensating override?" — it never re-asks whether the user is still CANCELLED, which is this
// flow's actual authorising condition. A user who resubscribes while an older cancellation job is
// mid-loop is active and paying; the remaining items must stay connected, and the retention clock —
// a six-month countdown to hard deletion — must never be started on them.
func TestWorker_SubscriptionCanceled_ReactivationMidLoop_StopsRemainingRemovals(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	plaid := &fakePerItemWebhookCleanup{itemIDs: []string{"item-1", "item-2", "item-3"}}
	worker.SetPlaidCleanupService(plaid)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	job := createRiverJob(SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	})

	row := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		SubscriptionStatus: string(domain.SubscriptionStatusCancelled),
	}
	// No comp is ever granted here — the ONLY thing that changes is the subscription status. That
	// isolation is the point: it is what makes this test fail while the predicate asks the comp
	// question alone.
	plaid.onRemoved = reactivationAfter(row, "item-1")

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(row, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)
	// Permissive on purpose, exactly as in the comp test above: the assertion that carries the
	// meaning is AssertNotCalled, and a missing expectation would surface as a testify panic instead.
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything).Return(nil).Maybe()

	err := worker.Work(ctx, job)

	require.NoError(t, err, "a reactivation mid-loop is a legitimate stop, not a webhook failure")
	assert.Equal(t, []string{"item-1"}, plaid.removed,
		"once the subscription is active again the remaining items must stay connected")
	assert.Equal(t, 1, plaid.ifEligibleCalls)
	mockProfileRepo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything)
}
