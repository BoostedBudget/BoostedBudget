# Paddle Deletion Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cancel active Paddle subscriptions immediately and archive/anonymize the Paddle customer record during account deletion, closing a billing orphan and GDPR gap.

**Architecture:** Add two new methods to `PaddleService` (`CancelSubscriptionImmediately`, `ArchiveAndAnonymizeCustomer`), define a narrowed `PaddleDeletionCleaner` interface in `deletion_service.go`, inject it via setter (mirroring the Plaid pattern), and run Paddle cleanup before Plaid and DB cascade in `ExecuteDeletion`. Failures are non-critical — logged and PostHog-tracked but do not abort deletion.

**Tech Stack:** Go 1.25+, `github.com/PaddleHQ/paddle-go-sdk/v4` v4.2.0, `github.com/stretchr/testify`, `log/slog`, PostHog Go SDK

**Spec:** `docs/superpowers/specs/2026-04-20-paddle-deletion-cleanup-design.md`

---

## File Map

| File | Change |
|---|---|
| `backend/internal/integration/posthog/events.go` | Add `EventPaddleCleanupFailed` constant |
| `backend/internal/services/paddle_service.go` | Add `CancelSubscriptionImmediately` + `ArchiveAndAnonymizeCustomer` to interface + impl |
| `backend/internal/services/paddle_service_test.go` | Tests for both new methods |
| `backend/internal/services/deletion_service.go` | Add `PaddleDeletionCleaner` interface, `paddleCleanupSvc` field, `SetPaddleCleanupService` setter, Paddle cleanup block in `ExecuteDeletion` |
| `backend/internal/services/deletion_service_test.go` | Add `MockPaddleDeletionCleaner`, all Paddle cleanup test cases |
| `backend/cmd/server/main.go` | Wire `deletionService.SetPaddleCleanupService(paddleService)` after Paddle init |

---

### Task 1: Add `EventPaddleCleanupFailed` PostHog constant

**Files:**
- Modify: `backend/internal/integration/posthog/events.go`

- [ ] **Step 1: Add the constant**

In `backend/internal/integration/posthog/events.go`, add to the Paddle payment events block (after `EventPaymentFailed`):

```go
// Paddle payment events
EventSubscriptionCreated   = "subscription_created"
EventSubscriptionCancelled = "subscription_cancelled"
EventPaymentSucceeded      = "payment_succeeded"
EventPaymentFailed         = "payment_failed"
// Issue #1006: Paddle cleanup failure during account deletion
EventPaddleCleanupFailed   = "paddle_cleanup_failed"
```

- [ ] **Step 2: Verify it compiles**

```bash
cd backend && go build ./internal/integration/posthog/...
```

Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
cd backend && git add internal/integration/posthog/events.go
git commit -m "#1006: Add EventPaddleCleanupFailed PostHog constant"
```

---

### Task 2: Add `CancelSubscriptionImmediately` to PaddleService (TDD)

**Files:**
- Modify: `backend/internal/services/paddle_service.go`
- Modify: `backend/internal/services/paddle_service_test.go`

- [ ] **Step 1: Write failing tests**

In `backend/internal/services/paddle_service_test.go`, add after the existing test functions:

```go
// TestCancelSubscriptionImmediately_Success verifies the subscription is cancelled
// with EffectiveFromImmediately (not next billing period).
func TestCancelSubscriptionImmediately_Success(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	subscriptionID := "sub_01abc123"

	cancelledSub := &paddle.Subscription{
		ID:     subscriptionID,
		Status: paddle.SubscriptionStatusCanceled,
	}
	mockSDK.On("CancelSubscription", ctx, mock.MatchedBy(func(req *paddle.CancelSubscriptionRequest) bool {
		return req.SubscriptionID == subscriptionID &&
			req.EffectiveFrom != nil &&
			*req.EffectiveFrom == paddle.EffectiveFromImmediately
	})).Return(cancelledSub, nil)

	err := svc.CancelSubscriptionImmediately(ctx, subscriptionID)

	assert.NoError(t, err)
	mockSDK.AssertExpectations(t)
}

// TestCancelSubscriptionImmediately_SDKError verifies errors are propagated.
func TestCancelSubscriptionImmediately_SDKError(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	subscriptionID := "sub_01abc123"

	mockSDK.On("CancelSubscription", ctx, mock.MatchedBy(func(req *paddle.CancelSubscriptionRequest) bool {
		return req.SubscriptionID == subscriptionID
	})).Return(nil, errors.New("paddle API error"))

	err := svc.CancelSubscriptionImmediately(ctx, subscriptionID)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to cancel Paddle subscription immediately")
	mockSDK.AssertExpectations(t)
}

// TestCancelSubscriptionImmediately_AlreadyCancelled verifies already-cancelled
// subscriptions are treated as success (Paddle returns the sub with no error).
func TestCancelSubscriptionImmediately_AlreadyCancelled(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	subscriptionID := "sub_01abc123"

	// Paddle returns the already-cancelled subscription without error
	alreadyCancelledSub := &paddle.Subscription{
		ID:     subscriptionID,
		Status: paddle.SubscriptionStatusCanceled,
	}
	mockSDK.On("CancelSubscription", ctx, mock.MatchedBy(func(req *paddle.CancelSubscriptionRequest) bool {
		return req.SubscriptionID == subscriptionID
	})).Return(alreadyCancelledSub, nil)

	err := svc.CancelSubscriptionImmediately(ctx, subscriptionID)

	assert.NoError(t, err)
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd backend && go test ./internal/services/... -run TestCancelSubscriptionImmediately -v
```

Expected: `FAIL` — `svc.CancelSubscriptionImmediately undefined`

- [ ] **Step 3: Add to `PaddleService` interface**

In `backend/internal/services/paddle_service.go`, add to the `PaddleService` interface after `CancelSubscription`:

```go
// CancelSubscription cancels a subscription at the end of current billing period.
CancelSubscription(ctx context.Context, subscriptionID string) error

// CancelSubscriptionImmediately cancels a subscription effective immediately.
// Used during account deletion — no end-of-period grace.
// Idempotent: already-cancelled subscriptions are treated as success.
// Issue #1006: Paddle cleanup during account deletion
CancelSubscriptionImmediately(ctx context.Context, subscriptionID string) error
```

- [ ] **Step 4: Add implementation to `paddleService`**

In `backend/internal/services/paddle_service.go`, add after the `CancelSubscription` method:

```go
// CancelSubscriptionImmediately cancels a subscription effective immediately.
// Issue #1006: Used during account deletion to stop billing with no grace period.
func (s *paddleService) CancelSubscriptionImmediately(ctx context.Context, subscriptionID string) error {
	s.logger.Info("canceling Paddle subscription immediately",
		"subscription_id", subscriptionID,
	)

	effectiveFrom := paddle.EffectiveFromImmediately
	req := &paddle.CancelSubscriptionRequest{
		SubscriptionID: subscriptionID,
		EffectiveFrom:  &effectiveFrom,
	}

	canceledSub, err := s.client.CancelSubscription(ctx, req)
	if err != nil {
		s.logger.Error("failed to cancel Paddle subscription immediately",
			"subscription_id", subscriptionID,
			"error", err,
		)
		return fmt.Errorf("failed to cancel Paddle subscription immediately: %w", err)
	}

	s.logger.Info("Paddle subscription cancelled immediately",
		"subscription_id", subscriptionID,
		"status", canceledSub.Status,
	)

	return nil
}
```

- [ ] **Step 5: Run tests to confirm they pass**

```bash
cd backend && go test ./internal/services/... -run TestCancelSubscriptionImmediately -v
```

Expected: all 3 tests `PASS`

- [ ] **Step 6: Commit**

```bash
cd backend && git add internal/services/paddle_service.go internal/services/paddle_service_test.go
git commit -m "#1006: Add CancelSubscriptionImmediately to PaddleService"
```

---

### Task 3: Add `ArchiveAndAnonymizeCustomer` to PaddleService (TDD)

**Files:**
- Modify: `backend/internal/services/paddle_service.go`
- Modify: `backend/internal/services/paddle_service_test.go`

- [ ] **Step 1: Write failing tests**

In `backend/internal/services/paddle_service_test.go`, add after the `TestCancelSubscriptionImmediately_*` tests:

```go
// TestArchiveAndAnonymizeCustomer_Success verifies the customer is archived and
// their email anonymized in a single UpdateCustomer PATCH call.
func TestArchiveAndAnonymizeCustomer_Success(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	customerID := "ctm_01abc123"
	anonymizedEmail := "deleted_user-123@deleted.local"

	var capturedReq *paddle.UpdateCustomerRequest
	mockSDK.On("UpdateCustomer", ctx, mock.MatchedBy(func(req *paddle.UpdateCustomerRequest) bool {
		capturedReq = req
		return req.CustomerID == customerID
	})).Return(&paddle.Customer{ID: customerID, Status: paddle.StatusArchived}, nil)

	err := svc.ArchiveAndAnonymizeCustomer(ctx, customerID, anonymizedEmail)

	assert.NoError(t, err)
	require.NotNil(t, capturedReq)
	assert.NotNil(t, capturedReq.Email, "Email field must be set in request")
	assert.NotNil(t, capturedReq.Status, "Status field must be set in request")
	mockSDK.AssertExpectations(t)
}

// TestArchiveAndAnonymizeCustomer_SDKError verifies errors are propagated.
func TestArchiveAndAnonymizeCustomer_SDKError(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	customerID := "ctm_01abc123"
	anonymizedEmail := "deleted_user-123@deleted.local"

	mockSDK.On("UpdateCustomer", ctx, mock.MatchedBy(func(req *paddle.UpdateCustomerRequest) bool {
		return req.CustomerID == customerID
	})).Return(nil, errors.New("paddle API error"))

	err := svc.ArchiveAndAnonymizeCustomer(ctx, customerID, anonymizedEmail)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to archive and anonymize Paddle customer")
	mockSDK.AssertExpectations(t)
}

// TestArchiveAndAnonymizeCustomer_AlreadyArchived verifies already-archived customers
// are treated as success (Paddle returns the archived customer without error).
func TestArchiveAndAnonymizeCustomer_AlreadyArchived(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	customerID := "ctm_01abc123"
	anonymizedEmail := "deleted_user-123@deleted.local"

	mockSDK.On("UpdateCustomer", ctx, mock.MatchedBy(func(req *paddle.UpdateCustomerRequest) bool {
		return req.CustomerID == customerID
	})).Return(&paddle.Customer{ID: customerID, Status: paddle.StatusArchived}, nil)

	err := svc.ArchiveAndAnonymizeCustomer(ctx, customerID, anonymizedEmail)

	assert.NoError(t, err)
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd backend && go test ./internal/services/... -run TestArchiveAndAnonymizeCustomer -v
```

Expected: `FAIL` — `svc.ArchiveAndAnonymizeCustomer undefined`

- [ ] **Step 3: Add to `PaddleService` interface**

In `backend/internal/services/paddle_service.go`, add to the `PaddleService` interface after `CancelSubscriptionImmediately`:

```go
// ArchiveAndAnonymizeCustomer archives the Paddle customer and replaces their
// email with an anonymized value in a single PATCH call (GDPR PII erasure).
// anonymizedEmail must be in the form "deleted_{userID}@deleted.local".
// Idempotent: already-archived customers are treated as success.
// Issue #1006: Paddle cleanup during account deletion
ArchiveAndAnonymizeCustomer(ctx context.Context, customerID, anonymizedEmail string) error
```

- [ ] **Step 4: Add implementation to `paddleService`**

In `backend/internal/services/paddle_service.go`, add after `CancelSubscriptionImmediately`:

```go
// ArchiveAndAnonymizeCustomer archives the Paddle customer record and anonymizes
// their email in a single PATCH call.
// Issue #1006: GDPR PII erasure — Paddle is a data processor holding the original email.
func (s *paddleService) ArchiveAndAnonymizeCustomer(ctx context.Context, customerID, anonymizedEmail string) error {
	s.logger.Info("archiving and anonymizing Paddle customer",
		"customer_id", customerID,
	)

	_, err := s.client.UpdateCustomer(ctx, &paddle.UpdateCustomerRequest{
		CustomerID: customerID,
		Email:      paddle.NewPatchField(anonymizedEmail),
		Status:     paddle.NewPatchField(paddle.StatusArchived),
	})
	if err != nil {
		s.logger.Error("failed to archive and anonymize Paddle customer",
			"customer_id", customerID,
			"error", err,
		)
		return fmt.Errorf("failed to archive and anonymize Paddle customer: %w", err)
	}

	s.logger.Info("Paddle customer archived and anonymized",
		"customer_id", customerID,
	)

	return nil
}
```

- [ ] **Step 5: Run tests to confirm they pass**

```bash
cd backend && go test ./internal/services/... -run TestArchiveAndAnonymizeCustomer -v
```

Expected: all 3 tests `PASS`

- [ ] **Step 6: Commit**

```bash
cd backend && git add internal/services/paddle_service.go internal/services/paddle_service_test.go
git commit -m "#1006: Add ArchiveAndAnonymizeCustomer to PaddleService"
```

---

### Task 4: Write all Paddle cleanup tests for DeletionService (TDD Red)

**Files:**
- Modify: `backend/internal/services/deletion_service_test.go`

- [ ] **Step 1: Add `MockPaddleDeletionCleaner`**

In `backend/internal/services/deletion_service_test.go`, add after the `MockPlaidItemCleanupService` block (around line 556):

```go
// MockPaddleDeletionCleaner implements PaddleDeletionCleaner for testing.
// Issue #1006: Narrowed Paddle interface used by DeletionService.
type MockPaddleDeletionCleaner struct {
	mock.Mock
}

func (m *MockPaddleDeletionCleaner) CancelSubscriptionImmediately(ctx context.Context, subscriptionID string) error {
	args := m.Called(ctx, subscriptionID)
	return args.Error(0)
}

func (m *MockPaddleDeletionCleaner) ArchiveAndAnonymizeCustomer(ctx context.Context, customerID, anonymizedEmail string) error {
	args := m.Called(ctx, customerID, anonymizedEmail)
	return args.Error(0)
}
```

- [ ] **Step 2: Add test helpers and test cases**

Add after the `MockPaddleDeletionCleaner` block:

```go
// buildUserWithPaddle returns a test user with Paddle subscription and customer IDs set.
func buildUserWithPaddle(subscriptionStatus string) (*domain.User, string, string) {
	paddleSubID := "sub_01abc123"
	paddleCustID := "ctm_01abc123"
	user := &domain.User{
		ID:                   "user-123",
		ClerkUserID:          "user_123",
		Email:                "test@example.com",
		IsDeleted:            false,
		SubscriptionStatus:   subscriptionStatus,
		PaddleSubscriptionID: &paddleSubID,
		PaddleCustomerID:     &paddleCustID,
	}
	return user, paddleSubID, paddleCustID
}

// expectedAnonymizedEmail is the Paddle anonymized email for user-123.
const expectedAnonymizedEmail = "deleted_user-123@deleted.local"

// TestExecuteDeletion_ActiveSubscription_CancelsImmediately verifies that an active
// subscription is cancelled immediately before local data is deleted.
func TestExecuteDeletion_ActiveSubscription_CancelsImmediately(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, subID, custID := buildUserWithPaddle("active")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockPaddle.On("CancelSubscriptionImmediately", ctx, subID).Return(nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, custID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	mockPaddle.AssertCalled(t, "CancelSubscriptionImmediately", ctx, subID)
	mockPaddle.AssertCalled(t, "ArchiveAndAnonymizeCustomer", ctx, custID, expectedAnonymizedEmail)
}

// TestExecuteDeletion_TrialSubscription_CancelsImmediately verifies trial subscriptions
// are cancelled (SubscriptionStatusTrial = "trial").
func TestExecuteDeletion_TrialSubscription_CancelsImmediately(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, subID, custID := buildUserWithPaddle("trial")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockPaddle.On("CancelSubscriptionImmediately", ctx, subID).Return(nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, custID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	mockPaddle.AssertCalled(t, "CancelSubscriptionImmediately", ctx, subID)
}

// TestExecuteDeletion_TrialingSubscription_CancelsImmediately verifies paid-trial
// subscriptions (SubscriptionStatusTrialing = "trialing") are cancelled.
func TestExecuteDeletion_TrialingSubscription_CancelsImmediately(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, subID, custID := buildUserWithPaddle("trialing")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockPaddle.On("CancelSubscriptionImmediately", ctx, subID).Return(nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, custID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	mockPaddle.AssertCalled(t, "CancelSubscriptionImmediately", ctx, subID)
}

// TestExecuteDeletion_PastDueSubscription_CancelsImmediately verifies past-due
// subscriptions are cancelled during deletion.
func TestExecuteDeletion_PastDueSubscription_CancelsImmediately(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, subID, custID := buildUserWithPaddle("past_due")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockPaddle.On("CancelSubscriptionImmediately", ctx, subID).Return(nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, custID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	mockPaddle.AssertCalled(t, "CancelSubscriptionImmediately", ctx, subID)
}

// TestExecuteDeletion_CancelledSubscription_DoesNotCancelAgain verifies that
// already-cancelled subscriptions are not cancelled again (webhook already handled it),
// but the customer is still archived.
func TestExecuteDeletion_CancelledSubscription_DoesNotCancelAgain(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, _, custID := buildUserWithPaddle("cancelled")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, custID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	mockPaddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	mockPaddle.AssertCalled(t, "ArchiveAndAnonymizeCustomer", ctx, custID, expectedAnonymizedEmail)
}

// TestExecuteDeletion_InactiveSubscription_DoesNotCancel verifies that inactive
// accounts (no active subscription) do not trigger a cancel call.
func TestExecuteDeletion_InactiveSubscription_DoesNotCancel(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	_, _, custID := buildUserWithPaddle("inactive")
	// Inactive users typically have no subscription ID
	paddleCustID := custID
	user := &domain.User{
		ID:                   "user-123",
		ClerkUserID:          "user_123",
		Email:                "test@example.com",
		IsDeleted:            false,
		SubscriptionStatus:   "inactive",
		PaddleSubscriptionID: nil, // no subscription
		PaddleCustomerID:     &paddleCustID,
	}
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, custID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	mockPaddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
}

// TestExecuteDeletion_NoPaddleCustomerID_SkipsArchive verifies that users with no
// Paddle customer record (never completed checkout) are not archived.
func TestExecuteDeletion_NoPaddleCustomerID_SkipsArchive(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user := &domain.User{
		ID:                   "user-123",
		ClerkUserID:          "user_123",
		Email:                "test@example.com",
		IsDeleted:            false,
		SubscriptionStatus:   "trial",
		PaddleSubscriptionID: nil,
		PaddleCustomerID:     nil, // never completed checkout
	}
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	mockPaddle.AssertNotCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
	mockPaddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
}

// TestExecuteDeletion_PaddleCancelFails_DeletionContinues verifies that a Paddle
// cancel API failure does not abort the deletion (log and continue).
func TestExecuteDeletion_PaddleCancelFails_DeletionContinues(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, subID, custID := buildUserWithPaddle("active")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockPaddle.On("CancelSubscriptionImmediately", ctx, subID).Return(errors.New("paddle API unavailable"))
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, custID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	// Deletion must succeed even if Paddle cancel fails
	assert.NoError(t, err)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID)
}

// TestExecuteDeletion_PaddleArchiveFails_DeletionContinues verifies that a Paddle
// archive API failure does not abort the deletion (log and continue).
func TestExecuteDeletion_PaddleArchiveFails_DeletionContinues(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, subID, custID := buildUserWithPaddle("active")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockPaddle.On("CancelSubscriptionImmediately", ctx, subID).Return(nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, custID, expectedAnonymizedEmail).Return(errors.New("paddle archive failed"))
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID)
}

// TestSetPaddleCleanupService_WiresCorrectly verifies the setter injects the service.
func TestSetPaddleCleanupService_WiresCorrectly(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	// Create without Paddle
	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	// Wire via setter
	service.SetPaddleCleanupService(mockPaddle)

	user, subID, custID := buildUserWithPaddle("active")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockPaddle.On("CancelSubscriptionImmediately", ctx, subID).Return(nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, custID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	// Proves the setter wired up the service (mock was called)
	mockPaddle.AssertCalled(t, "CancelSubscriptionImmediately", ctx, subID)
	mockPaddle.AssertCalled(t, "ArchiveAndAnonymizeCustomer", ctx, custID, expectedAnonymizedEmail)
}

// TestExecuteDeletion_PaddleRunsBeforePlaidAndDB verifies execution order:
// Paddle cleanup → Plaid cleanup → DB cascade.
func TestExecuteDeletion_PaddleRunsBeforePlaidAndDB(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)
	mockPlaid := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaid, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, subID, custID := buildUserWithPaddle("active")
	callOrder := []string{}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockPaddle.On("CancelSubscriptionImmediately", ctx, subID).Return(nil).Run(func(mock.Arguments) {
		callOrder = append(callOrder, "CancelSubscription")
	})
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, custID, expectedAnonymizedEmail).Return(nil).Run(func(mock.Arguments) {
		callOrder = append(callOrder, "ArchiveCustomer")
	})
	mockPlaid.On("RemoveAllUserItems", ctx, user.ID).Return(nil).Run(func(mock.Arguments) {
		callOrder = append(callOrder, "RemoveAllUserItems")
	})
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil).Run(func(mock.Arguments) {
		callOrder = append(callOrder, "DeleteUserCascade")
	})
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	assert.Equal(t,
		[]string{"CancelSubscription", "ArchiveCustomer", "RemoveAllUserItems", "DeleteUserCascade"},
		callOrder,
		"Paddle cleanup must run before Plaid and DB cascade",
	)
}
```

- [ ] **Step 3: Run tests to confirm they all fail**

```bash
cd backend && go test ./internal/services/... -run "TestExecuteDeletion_.*Subscription|TestExecuteDeletion_NoPaddle|TestExecuteDeletion_Paddle|TestSetPaddleCleanupService" -v 2>&1 | tail -20
```

Expected: compilation errors (`SetPaddleCleanupService undefined`, `MockPaddleDeletionCleaner undefined`)

- [ ] **Step 4: Commit the failing tests**

```bash
cd backend && git add internal/services/deletion_service_test.go
git commit -m "#1006: Add failing tests for Paddle cleanup in DeletionService [RED]"
```

---

### Task 5: Implement Paddle cleanup in DeletionService (TDD Green)

**Files:**
- Modify: `backend/internal/services/deletion_service.go`

- [ ] **Step 1: Add `PaddleDeletionCleaner` interface and new field/setter**

In `backend/internal/services/deletion_service.go`, add after the `AuditLogger` interface (around line 49):

```go
// PaddleDeletionCleaner is the narrowed Paddle interface used by DeletionService.
// paddleService satisfies this interface automatically via duck typing.
// Issue #1006: Paddle cleanup during account deletion.
type PaddleDeletionCleaner interface {
	// CancelSubscriptionImmediately cancels a subscription effective immediately.
	CancelSubscriptionImmediately(ctx context.Context, subscriptionID string) error

	// ArchiveAndAnonymizeCustomer archives the customer and anonymizes their email.
	ArchiveAndAnonymizeCustomer(ctx context.Context, customerID, anonymizedEmail string) error
}
```

- [ ] **Step 2: Add field to `deletionService` struct**

In `backend/internal/services/deletion_service.go`, update the `deletionService` struct:

```go
type deletionService struct {
	profileRepo      repository.ProfileRepository
	emailService     EmailService
	clerkService     ClerkService
	auditLogger      AuditLogger
	plaidCleanupSvc  PlaidItemCleanupService // Issue #668
	paddleCleanupSvc PaddleDeletionCleaner   // Issue #1006: nil if Paddle not configured
	analytics        phclient.Analytics
	gracePeriodDays  int
	tokenSecret      string
}
```

- [ ] **Step 3: Add `SetPaddleCleanupService` to the `DeletionService` interface**

In `backend/internal/services/deletion_service.go`, update the `DeletionService` interface to add after `SetPlaidCleanupService`:

```go
// SetPaddleCleanupService sets the Paddle cleanup service for billing reconciliation
// during deletion. Setter allows wiring after DeletionService creation since Paddle
// may not be configured (no API key). Mirrors SetPlaidCleanupService pattern.
// Issue #1006: Paddle cleanup during account deletion
SetPaddleCleanupService(svc PaddleDeletionCleaner)
```

- [ ] **Step 4: Add setter implementation**

In `backend/internal/services/deletion_service.go`, add after `SetPlaidCleanupService`:

```go
// SetPaddleCleanupService sets the Paddle cleanup service.
// Issue #1006: Paddle cleanup during account deletion
func (s *deletionService) SetPaddleCleanupService(svc PaddleDeletionCleaner) {
	s.paddleCleanupSvc = svc
}
```

- [ ] **Step 5: Add Paddle cleanup block to `ExecuteDeletion`**

In `backend/internal/services/deletion_service.go`, update `ExecuteDeletion`. Add the Paddle cleanup block after the already-deleted check and before the Plaid cleanup. The full updated function:

```go
// ExecuteDeletion executes the full deletion process for a user
func (s *deletionService) ExecuteDeletion(ctx context.Context, userID string) error {
	// Get user details before deletion
	user, err := s.profileRepo.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	// Check if already deleted
	if user.IsDeleted {
		return nil
	}

	// Issue #1006: Paddle cleanup BEFORE Plaid and DB deletion.
	// paddle_subscription_id and paddle_customer_id must be readable — DB cascade clears them.
	if s.paddleCleanupSvc != nil {
		anonymizedEmail := fmt.Sprintf("deleted_%s@deleted.local", userID)

		// Cancel active subscriptions immediately (stop billing)
		if user.PaddleSubscriptionID != nil {
			status := domain.SubscriptionStatus(user.SubscriptionStatus)
			shouldCancel := status.IsActiveSubscription() || status == domain.SubscriptionStatusPastDue
			if shouldCancel {
				if err := s.paddleCleanupSvc.CancelSubscriptionImmediately(ctx, *user.PaddleSubscriptionID); err != nil {
					slog.Default().Warn("failed to cancel Paddle subscription during deletion",
						"user_id", userID,
						"subscription_id", *user.PaddleSubscriptionID,
						"error", err,
					)
					if s.analytics != nil {
						s.analytics.Capture(userID, phclient.EventPaddleCleanupFailed, map[string]interface{}{
							"step": "cancel_subscription",
						})
					}
				}
			}
		}

		// Archive and anonymize customer (GDPR PII erasure from Paddle's records)
		if user.PaddleCustomerID != nil {
			if err := s.paddleCleanupSvc.ArchiveAndAnonymizeCustomer(ctx, *user.PaddleCustomerID, anonymizedEmail); err != nil {
				slog.Default().Warn("failed to archive Paddle customer during deletion",
					"user_id", userID,
					"customer_id", *user.PaddleCustomerID,
					"error", err,
				)
				if s.analytics != nil {
					s.analytics.Capture(userID, phclient.EventPaddleCleanupFailed, map[string]interface{}{
						"step": "archive_customer",
					})
				}
			}
		}
	}

	// Issue #668: Remove Plaid items BEFORE database deletion to stop billing
	if s.plaidCleanupSvc != nil {
		_ = s.plaidCleanupSvc.RemoveAllUserItems(ctx, userID)
	}

	// Execute cascade deletion in database
	if err := s.profileRepo.DeleteUserCascade(ctx, userID); err != nil {
		return fmt.Errorf("failed to cascade delete user data: %w", err)
	}

	// Delete user from Clerk (non-critical)
	_ = s.clerkService.DeleteUser(ctx, user.ClerkUserID)

	// Create audit log (non-critical)
	_ = s.auditLogger.Log(ctx, "user.deleted", "user", userID)

	// Capture analytics event for account deletion
	if s.analytics != nil {
		s.analytics.Capture(userID, phclient.EventUserDeleted, nil)
	}

	return nil
}
```

- [ ] **Step 6: Add missing import for `domain` package**

In `backend/internal/services/deletion_service.go`, verify the imports block includes the domain package. The full imports should be:

```go
import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)
```

- [ ] **Step 7: Run the new tests to confirm they pass**

```bash
cd backend && go test ./internal/services/... -run "TestExecuteDeletion_.*Subscription|TestExecuteDeletion_NoPaddle|TestExecuteDeletion_Paddle|TestSetPaddleCleanupService" -v 2>&1 | tail -30
```

Expected: all new tests `PASS`

- [ ] **Step 8: Run the full service test suite to confirm nothing regressed**

```bash
cd backend && go test ./internal/services/... -v 2>&1 | grep -E "^(=== RUN|--- (PASS|FAIL)|FAIL|ok)" | tail -40
```

Expected: all existing tests still pass

- [ ] **Step 9: Commit**

```bash
cd backend && git add internal/services/deletion_service.go
git commit -m "#1006: Implement Paddle cleanup in DeletionService [GREEN]"
```

---

### Task 6: Wire in `main.go` and verify full test suite

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add wiring after Paddle init block**

In `backend/cmd/server/main.go`, find the comment block that begins:

```go
// Issue #1015: Create non-Plaid workers unconditionally (decoupled from Plaid config).
```

Add the wiring immediately before it (inside the outer block, after the closing `} else { appLogger.Warn("Paddle services disabled...") }`):

```go
// Issue #1006: Wire PaddleService into DeletionService for billing cleanup on deletion.
// Must run after both paddleService and deletionService are initialized.
// deletionService may be nil if DeletionTokenSecret is not configured.
if paddleService != nil && deletionService != nil {
    deletionService.SetPaddleCleanupService(paddleService)
    appLogger.Info("Paddle cleanup service wired to deletion service")
}
```

- [ ] **Step 2: Build to confirm no compilation errors**

```bash
cd backend && go build ./cmd/server/...
```

Expected: no output (success)

- [ ] **Step 3: Run the full backend test suite**

```bash
cd backend && go test ./... -v 2>&1 | grep -E "^(=== RUN|--- (PASS|FAIL)|FAIL|ok)" | tail -50
```

Expected: all tests pass, no failures

- [ ] **Step 4: Run lint**

```bash
make -C /Users/runner/coding/BoostedBudget/backend lint
```

Expected: no lint errors

- [ ] **Step 5: Commit**

```bash
cd backend && git add cmd/server/main.go
git commit -m "#1006: Wire PaddleService into DeletionService for deletion cleanup"
```

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Task |
|---|---|
| Cancel active/trial/trialing/past_due subscriptions immediately | Tasks 2, 5 |
| Skip already-cancelled/inactive subscriptions | Tasks 4, 5 |
| Archive + anonymize customer email in one PATCH | Tasks 3, 5 |
| Use `IsActiveSubscription()` helper | Task 5 |
| Narrowed `PaddleDeletionCleaner` interface | Task 5 |
| `SetPaddleCleanupService` setter | Tasks 4, 5 |
| Paddle cleanup before Plaid + DB | Tasks 4, 5 |
| Failures non-critical: log + PostHog event | Tasks 1, 5 |
| Wire in `main.go` | Task 6 |
| `trialing` status handled | Tasks 4, 5 |
| Idempotency (already-cancelled/archived → success) | Tasks 2, 3 |
| Full backend tests pass | Task 6 |

All spec requirements covered. ✓
