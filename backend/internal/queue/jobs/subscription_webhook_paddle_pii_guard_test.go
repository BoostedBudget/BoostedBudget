package jobs

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Issue #1652 Todo 13 (ii) — PII guard for the four Paddle events emitted through
// `subscription_webhook.go`.
//
// The consolidated guard in `internal/integration/posthog/revenuecat_delivery_guard_test.go` drives
// the Gateway with a COPY of these property maps. This test is the stronger half the plan asks for:
// it runs the REAL emitter (`SubscriptionWebhookWorker.Work`) with the REAL `Gateway` +
// `AuthorityResolver` and captures what the transport boundary actually receives. It therefore
// proves the *live* call site — not a copy — cannot leak `amount` / `transaction_id` /
// `subscription_id` through the gateway.
//
// All four Paddle contracts are `legacy`/`legacy_properties_unreviewed`, so the Gateway's
// `validateProperties` returns an empty map before it ever forwards a key: the assertion is that
// the event still DELIVERS (the funnel must not go dark) but arrives with no properties at all.

type paddleGuardCapture struct {
	subject string
	event   string
	props   map[string]interface{}
}

type paddleGuardTransport struct{ captures []paddleGuardCapture }

func (t *paddleGuardTransport) Capture(subject, event string, props map[string]interface{}) {
	t.captures = append(t.captures, paddleGuardCapture{subject: subject, event: event, props: props})
}
func (*paddleGuardTransport) Identify(string, map[string]interface{}) {}
func (*paddleGuardTransport) Close()                                  {}

type paddleGuardAuthority struct {
	subject string
	granted bool
}

func (a paddleGuardAuthority) WithConsentedSubject(_ context.Context, _ string, emit func(string)) (bool, error) {
	if !a.granted {
		return false, nil
	}
	emit(a.subject)
	return true, nil
}

// forbiddenPaddleKeys mirrors the plan's forbidden set. It is duplicated here (rather than imported
// from the posthog package) because that package's copy is unexported; both lists are asserted
// against the same transport-boundary maps, and the duplication is the point — a change to one
// without the other is a defect, not a refactor.
var forbiddenPaddleKeys = []string{
	"reason", "actor_id", "email", "amount", "account_id", "transaction_id",
	"item_id", "customer_id", "plaid_item_id", "household_id", "user_id",
}

func newPaddleGuardGateway(transport *paddleGuardTransport) *phclient.Gateway {
	return phclient.NewGateway(transport, paddleGuardAuthority{subject: "canonical-paddle-subject", granted: true},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func assertPaddleCaptureIsPIIFree(t *testing.T, transport *paddleGuardTransport, event string) {
	t.Helper()
	require.Len(t, transport.captures, 1, "the Paddle event must still deliver through the gateway")
	captured := transport.captures[0]
	assert.Equal(t, "canonical-paddle-subject", captured.subject)
	assert.Equal(t, event, captured.event)
	for _, forbidden := range forbiddenPaddleKeys {
		assert.NotContains(t, captured.props, forbidden, "forbidden key reached the transport")
	}
	assert.Empty(t, captured.props, "a legacy/unreviewed Paddle contract must strip every property")
}

// TestSubscriptionCreatedThroughGatewayCarriesNoIdentifierProperties drives the real
// subscription.created emitter. The emitter passes {tier, status, subscription_id}
// (subscription_webhook.go:754); the transport must see none of them.
func TestSubscriptionCreatedThroughGatewayCarriesNoIdentifierProperties(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	transport := &paddleGuardTransport{}
	worker.SetAnalytics(newPaddleGuardGateway(transport))

	webhook := loadWebhookFixture(t, "subscription_created.json")
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: webhook.EventID, EventType: "subscription.created"}
	job := createRiverJob(args)

	user := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "user_2abc123def456",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
	}
	expectedSubID := "sub_01h1234567890abcdef"

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdatePaddleCustomerID", ctx, "user_2abc123def456", "ctm_01h1234567890abcdef").Return(nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "active", mock.AnythingOfType("*time.Time"), mock.AnythingOfType("*time.Time"), &expectedSubID).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "user_2abc123def456", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearRetentionDates", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	require.NoError(t, worker.Work(ctx, job))
	assertPaddleCaptureIsPIIFree(t, transport, phclient.EventSubscriptionCreated)
}

// TestSubscriptionCancelledThroughGatewayCarriesNoIdentifierProperties drives the real
// subscription.canceled emitter (subscription_webhook.go:967). It passes nil properties, so the
// guard is that the event still delivers and carries no key.
func TestSubscriptionCancelledThroughGatewayCarriesNoIdentifierProperties(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	transport := &paddleGuardTransport{}
	worker.SetAnalytics(newPaddleGuardGateway(transport))

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: webhook.EventID, EventType: "subscription.canceled"}
	job := createRiverJob(args)

	canceledUser := &domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk_user_123", SubscriptionTier: "premium"}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(canceledUser, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	require.NoError(t, worker.Work(ctx, job))
	assertPaddleCaptureIsPIIFree(t, transport, phclient.EventSubscriptionCancelled)
}

// TestPaymentSucceededThroughGatewayCarriesNoAmountOrTransactionID drives the real
// transaction.completed emitter. The emitter passes {amount, currency, transaction_id}
// (subscription_webhook.go:1415); the transport must see none of them.
func TestPaymentSucceededThroughGatewayCarriesNoAmountOrTransactionID(t *testing.T) {
	worker, mockWebhookRepo, _, _, _, mockTxnRepo := setupWorkerTest(t)
	ctx := context.Background()

	transport := &paddleGuardTransport{}
	worker.SetAnalytics(newPaddleGuardGateway(transport))

	webhook := loadWebhookFixture(t, "transaction_completed.json")
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: webhook.EventID, EventType: "transaction.completed"}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockTxnRepo.On("Create", ctx, mock.MatchedBy(func(txn *domain.PaddleTransaction) bool {
		return txn.Status == "completed" && txn.UserID == "user_2abc123def456"
	})).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	require.NoError(t, worker.Work(ctx, job))
	assertPaddleCaptureIsPIIFree(t, transport, phclient.EventPaymentSucceeded)
}

// TestPaymentFailedThroughGatewayCarriesNoTransactionID drives the real
// transaction.payment_failed emitter. The emitter passes {transaction_id}
// (subscription_webhook.go:1458); the transport must see none of it.
func TestPaymentFailedThroughGatewayCarriesNoTransactionID(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, mockTxnRepo := setupWorkerTest(t)
	ctx := context.Background()

	transport := &paddleGuardTransport{}
	worker.SetAnalytics(newPaddleGuardGateway(transport))

	webhook := loadWebhookFixture(t, "transaction_payment_failed.json")
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: webhook.EventID, EventType: "transaction.payment_failed"}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk_user_123", SubscriptionTier: "premium", SubscriptionStatus: "past_due"}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockTxnRepo.On("Create", ctx, mock.MatchedBy(func(txn *domain.PaddleTransaction) bool {
		return txn.Status == "failed" && txn.UserID == "user_2abc123def456"
	})).Return(nil)
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "payment_failed" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	require.NoError(t, worker.Work(ctx, job))
	assertPaddleCaptureIsPIIFree(t, transport, phclient.EventPaymentFailed)
}

// TestPaddleEmittersPassSensitiveKeysBeforeTheGatewayStripsThem pins the premise of the guard above:
// the emitters really do pass amount/transaction_id/subscription_id. If that stops being true the
// stripping assertions above become vacuous, so this test must be updated alongside them.
func TestPaddleEmittersPassSensitiveKeysBeforeTheGatewayStripsThem(t *testing.T) {
	source, err := os.ReadFile("subscription_webhook.go")
	require.NoError(t, err)
	for _, key := range []string{`"amount"`, `"transaction_id"`, `"subscription_id"`} {
		assert.Contains(t, string(source), key)
	}
}
