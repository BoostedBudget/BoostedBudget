# Paddle Deletion Cleanup — Design Spec

**Issue**: #1006  
**Date**: 2026-04-20  
**Status**: Approved for implementation

---

## Context

`DeletionService.ExecuteDeletion()` already handles Plaid (removes items before DB deletion for cost control) and Clerk (deletes remote user post-cascade), but performs no Paddle-side cleanup. When a user's account is deleted, their Paddle subscription is left active and their Paddle customer record retains the original email PII indefinitely.

Two deletion paths exist:
- **User-initiated GDPR deletion** (30-day grace period): User requests deletion → waits 30 days → `ExecuteDeletion` fires. Subscription may still be active if the user never cancelled via the billing portal during the grace period.
- **Retention-based auto-deletion** (6-month inactive): Subscription was already cancelled when the retention clock started. By the time auto-deletion fires, the subscription is typically already `cancelled` locally — no active billing to stop.

The primary risk is the user-initiated path where an active subscription is never cancelled in Paddle, resulting in orphaned billing state and a customer record holding PII after the user's local data has been anonymized.

---

## Chosen Approach: Narrowed `PaddleDeletionCleaner` interface + setter injection in `DeletionService`

Rather than injecting the full `PaddleService`, a narrowed two-method interface `PaddleDeletionCleaner` is defined in `deletion_service.go`. This expresses the minimal dependency explicitly, simplifies mocking (only 2 methods), and mirrors the precedent set by `PlaidItemCleanupService`. The `paddleService` struct satisfies it automatically via duck typing.

---

## Paddle SDK Capabilities Confirmed (v4.2.0)

| Operation | SDK primitive |
|---|---|
| Cancel immediately | `CancelSubscriptionRequest{EffectiveFrom: &paddle.EffectiveFromImmediately}` |
| Archive customer | `UpdateCustomerRequest{Status: paddle.NewPatchField(paddle.StatusArchived)}` |
| Anonymize customer email | `UpdateCustomerRequest{Email: paddle.NewPatchField(anonymizedEmail)}` |
| Archive + anonymize in one call | Combine `Status` and `Email` in a single `UpdateCustomerRequest` PATCH |

---

## Subscription Status Values

The canonical status constants are in `backend/internal/domain/subscription.go`. There are **six** local statuses — the comment in `user.go:23` is stale and omits `trialing`:

| Status | Meaning | Cancel on deletion? |
|---|---|---|
| `active` | Active paid subscription | Yes |
| `trial` | BoostedBudget internal trial | Yes (if subscription ID set) |
| `trialing` | Paid subscription during Paddle trial period | Yes |
| `past_due` | Payment overdue | Yes |
| `cancelled` | Already cancelled via webhook | No (already handled) |
| `inactive` | Frozen account (expired subscription) | No |

`IsActiveSubscription()` in `subscription.go` already captures the correct set: `active`, `trial`, `trialing`. Use this helper in the implementation rather than a hardcoded string comparison.

---

## Design

### 1. New `PaddleDeletionCleaner` interface (in `deletion_service.go`)

```go
// PaddleDeletionCleaner is the narrowed Paddle interface used by DeletionService.
// Expressed as a minimal dependency — paddleService satisfies it automatically.
type PaddleDeletionCleaner interface {
    // CancelSubscriptionImmediately cancels a subscription effective immediately.
    // Must treat already-cancelled and terminal responses as success (idempotent).
    CancelSubscriptionImmediately(ctx context.Context, subscriptionID string) error

    // ArchiveAndAnonymizeCustomer archives the Paddle customer and replaces their
    // email with an anonymized value in a single PATCH call (GDPR PII erasure).
    // Must treat already-archived responses as success (idempotent).
    ArchiveAndAnonymizeCustomer(ctx context.Context, customerID, anonymizedEmail string) error
}
```

---

### 2. New methods on `PaddleService` (in `paddle_service.go`)

These are added to both the `PaddleService` interface and the `paddleService` implementation.

**`CancelSubscriptionImmediately`** — calls `CancelSubscription` with `EffectiveFrom: &paddle.EffectiveFromImmediately`. Paddle returns the updated subscription; if its status is already `canceled`, treat as success (idempotent). Any other SDK error is propagated.

**`ArchiveAndAnonymizeCustomer`** — calls `UpdateCustomer` with both `Email` and `Status` in one PATCH:
```go
s.client.UpdateCustomer(ctx, &paddle.UpdateCustomerRequest{
    CustomerID: customerID,
    Email:      paddle.NewPatchField(anonymizedEmail),
    Status:     paddle.NewPatchField(paddle.StatusArchived),
})
```
If Paddle returns an error indicating the customer is already archived, treat as success (idempotent). Any other error is propagated.

---

### 3. `DeletionService` — setter + `ExecuteDeletion` cleanup block

**Addition to `DeletionService` interface:**
```go
// SetPaddleCleanupService sets the Paddle cleanup service for billing reconciliation
// during deletion. Setter allows wiring after DeletionService creation since Paddle
// may not be configured. Mirrors SetPlaidCleanupService pattern.
SetPaddleCleanupService(svc PaddleDeletionCleaner)
```

**New struct field:**
```go
paddleCleanupSvc PaddleDeletionCleaner // nil if Paddle not configured
```

**Cleanup logic in `ExecuteDeletion`** — runs **after already-deleted check, before Plaid, before DB cascade** (Paddle IDs must be readable):

```
anonymizedEmail = fmt.Sprintf("deleted_%s@deleted.local", userID)

if paddleCleanupSvc != nil:
    if paddle_subscription_id set:
        if user.SubscriptionStatus.IsActiveSubscription():
            err = CancelSubscriptionImmediately(subscriptionID)
            if err: slog.Default().Warn(...); capture PostHog EventPaddleCleanupFailed
        # cancelled/inactive: skip — webhook already handled the cancel

    if paddle_customer_id set:
        err = ArchiveAndAnonymizeCustomer(customerID, anonymizedEmail)
        if err: slog.Default().Warn(...); capture PostHog EventPaddleCleanupFailed
```

**Subscription status decision (use `IsActiveSubscription()` helper):**

| Local status | `paddle_subscription_id` set? | Subscription action |
|---|---|---|
| `active` | Yes | `CancelSubscriptionImmediately` |
| `trial` | Maybe | `CancelSubscriptionImmediately` if set |
| `trialing` | Yes | `CancelSubscriptionImmediately` |
| `past_due` | Yes | `CancelSubscriptionImmediately` |
| `cancelled` | Maybe | Skip |
| `inactive` | No | Skip |

**Customer action**: `ArchiveAndAnonymizeCustomer` always fires when `paddle_customer_id` is non-nil, regardless of subscription status. A user may have a customer record without an active subscription (e.g., they signed up but never completed checkout).

**Error handling**: All Paddle operations are non-critical — they log and capture a PostHog event but do not abort deletion. Local data integrity takes precedence. This matches the established pattern for Plaid and Clerk cleanup steps.

**Known limitation**: `DeleteUserCascade` clears `paddle_customer_id` and `paddle_subscription_id`. If Paddle cleanup fails and deletion completes, a retry cannot recover the IDs to reattempt Paddle cleanup. The PostHog event provides observability for these cases; a durable retry mechanism is out of scope for this issue.

**Execution order in `ExecuteDeletion`:**
1. Get user details *(existing)*
2. Already-deleted check *(existing)*
3. **Paddle cleanup** *(new)* — cancel subscription + archive/anonymize customer
4. Plaid cleanup *(existing)* — remove items to stop Plaid billing
5. DB cascade deletion *(existing)* — atomically deletes and anonymizes local data
6. Clerk user deletion *(existing)* — non-critical
7. Audit log + analytics *(existing)* — non-critical

---

### 4. PostHog event (in `backend/internal/integration/posthog/events.go`)

Add `EventPaddleCleanupFailed` for observability when Paddle cleanup fails during deletion. This makes silent failures visible in PostHog dashboards.

---

### 5. Wiring in `main.go`

After the Paddle init block (around line 541, already labelled "after Paddle init"):

```go
// Issue #1006: Wire PaddleService into DeletionService for billing cleanup on deletion
if paddleService != nil && deletionService != nil {
    deletionService.SetPaddleCleanupService(paddleService)
}
```

`paddleService` satisfies `PaddleDeletionCleaner` automatically.

---

## File Changes

| File | Change |
|---|---|
| `backend/internal/services/deletion_service.go` | Add `PaddleDeletionCleaner` interface, `paddleCleanupSvc` field, `SetPaddleCleanupService` setter, Paddle cleanup in `ExecuteDeletion` |
| `backend/internal/services/paddle_service.go` | Add `CancelSubscriptionImmediately`, `ArchiveAndAnonymizeCustomer` to `PaddleService` interface + `paddleService` impl |
| `backend/internal/integration/posthog/events.go` | Add `EventPaddleCleanupFailed` constant |
| `backend/cmd/server/main.go` | Wire `deletionService.SetPaddleCleanupService(paddleService)` after Paddle init |
| `backend/internal/services/deletion_service_test.go` | Add `MockPaddleDeletionCleaner` (2-method mock), test cases |
| `backend/internal/services/paddle_service_test.go` | Add tests for `CancelSubscriptionImmediately` and `ArchiveAndAnonymizeCustomer` |

---

## Testing Strategy

### TDD sequence (Red → Green → Refactor)

Write tests first, then implement.

**New mock in `deletion_service_test.go`:**
```go
// MockPaddleDeletionCleaner implements PaddleDeletionCleaner — only 2 methods needed
type MockPaddleDeletionCleaner struct { mock.Mock }
func (m *MockPaddleDeletionCleaner) CancelSubscriptionImmediately(ctx context.Context, subscriptionID string) error
func (m *MockPaddleDeletionCleaner) ArchiveAndAnonymizeCustomer(ctx context.Context, customerID, anonymizedEmail string) error
```

**Test cases for `ExecuteDeletion` — Paddle behavior:**

| Test | Assertion |
|---|---|
| Active subscription → cancel called | `CancelSubscriptionImmediately` called with correct ID |
| Trial subscription + sub ID → cancel called | Same |
| `trialing` subscription → cancel called | Same |
| Past-due subscription → cancel called | Same |
| Cancelled subscription → cancel NOT called | `CancelSubscriptionImmediately` not called; archive still called |
| Inactive subscription (no sub ID) → cancel NOT called | Same |
| Customer ID set → archive+anonymize called | `ArchiveAndAnonymizeCustomer` called with `deleted_{userID}@deleted.local` |
| No customer ID → archive NOT called | `ArchiveAndAnonymizeCustomer` not called |
| Paddle cancel fails → deletion still completes | `DeleteUserCascade` still called; `ExecuteDeletion` returns nil |
| Paddle archive fails → deletion still completes | Same |
| `SetPaddleCleanupService` setter wires correctly | After setter, mock is invoked |
| Paddle cleanup runs before Plaid before DB | Call-order tracking: `[CancelSubscription, ArchiveCustomer, RemoveAllUserItems, DeleteUserCascade]` |

**Test cases for `paddle_service_test.go`:**

| Test | Assertion |
|---|---|
| `CancelSubscriptionImmediately` — success | SDK called with `EffectiveFromImmediately` |
| `CancelSubscriptionImmediately` — already cancelled | Treated as success (idempotent) |
| `CancelSubscriptionImmediately` — SDK error | Error propagated |
| `ArchiveAndAnonymizeCustomer` — success | SDK called with `Status: archived` + correct email |
| `ArchiveAndAnonymizeCustomer` — already archived | Treated as success (idempotent) |
| `ArchiveAndAnonymizeCustomer` — SDK error | Error propagated |

---

## GDPR Notes

- **Paddle is a data processor.** The original customer email in Paddle is PII subject to GDPR right-to-erasure.
- `ArchiveAndAnonymizeCustomer` replaces the email with `deleted_{userID}@deleted.local` — no identifying information. This matches the anonymized value stored locally after `DeleteUserCascade`.
- Archiving prevents the customer record from being used in future checkouts.
- Audit logs in BoostedBudget are anonymized (not deleted) per existing cascade — consistent with GDPR's allowance for legal-basis record retention.
- **Gap**: If Paddle cleanup fails silently, the original email persists in Paddle. The PostHog `EventPaddleCleanupFailed` event makes this monitorable. A durable retry mechanism is a future concern outside this issue's scope.
