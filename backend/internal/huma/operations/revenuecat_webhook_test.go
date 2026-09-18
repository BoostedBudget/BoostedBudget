package operations

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/queue/jobs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rcWebhookProfiles struct{}

func (rcWebhookProfiles) GetUserByClerkID(_ context.Context, id string) (*domain.User, error) {
	if id == "user_clerk" {
		return &domain.User{ID: "internal-user", ClerkUserID: id}, nil
	}
	return nil, repository.ErrUserNotFound
}

type rcWebhookInbox struct {
	seen   map[string]bool
	writes int
}

func (r *rcWebhookInbox) AcceptWebhook(ctx context.Context, id, userID, eventType string, enqueue func(context.Context, pgx.Tx, string) error) (bool, error) {
	if r.seen[id] {
		return false, nil
	}
	if err := enqueue(ctx, nil, id); err != nil {
		return false, err
	}
	r.seen[id] = true
	r.writes++
	return true, nil
}

type rcWebhookQueue struct {
	args []jobs.RevenueCatSyncArgs
	err  error
}

func (q *rcWebhookQueue) EnqueueTx(_ context.Context, _ pgx.Tx, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	if q.err != nil {
		return nil, q.err
	}
	q.args = append(q.args, job.(jobs.RevenueCatSyncArgs))
	return &queue.JobResult{}, nil
}
func rcSignedWebhook(body string) *RevenueCatWebhookInput {
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte("signing-test"))
	_, _ = mac.Write([]byte(stamp + "." + body))
	return &RevenueCatWebhookInput{RawBody: []byte(body), Authorization: "Bearer webhook-test", Signature: "t=" + stamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))}
}
func newRCWebhookTestHandler() (*RevenueCatWebhookHandler, *rcWebhookInbox, *rcWebhookQueue) {
	inbox := &rcWebhookInbox{seen: map[string]bool{}}
	q := &rcWebhookQueue{}
	h := NewRevenueCatWebhookHandler(RevenueCatWebhookDeps{Inbox: inbox, Profiles: rcWebhookProfiles{}, Queue: q, Authorization: "Bearer webhook-test", SigningSecret: "signing-test"})
	return h, inbox, q
}

func TestRevenueCatWebhookVerifiesBeforeInboxAndDeduplicates(t *testing.T) {
	h, inbox, q := newRCWebhookTestHandler()
	body := `{"event":{"id":"event-1","type":"NON_RENEWING_PURCHASE","app_user_id":"user_clerk"}}`
	input := rcSignedWebhook(body)
	input.Authorization = "wrong"
	_, err := h.Handle(context.Background(), input)
	require.Error(t, err)
	require.Zero(t, inbox.writes)
	got, err := h.Handle(context.Background(), rcSignedWebhook(body))
	require.NoError(t, err)
	require.Equal(t, "queued", got.Body.Status)
	require.Equal(t, []jobs.RevenueCatSyncArgs{{UserID: "internal-user", EventID: "event-1"}}, q.args)
	got, err = h.Handle(context.Background(), rcSignedWebhook(body))
	require.NoError(t, err)
	require.Equal(t, "duplicate", got.Body.Status)
	require.Equal(t, 1, inbox.writes)
	require.Len(t, q.args, 1)
}

// TestRevenueCatWebhookAcceptsRefundCancellationAndQueuesRefresh is the issue #1652 AC6 proof
// (webhook half). RevenueCat reports a refund as a CANCELLATION event with cancel_reason
// "CUSTOMER_SUPPORT" (docs: integrations/webhooks/sample-events). The handler is deliberately
// event-type agnostic: it authenticates, resolves the exact Clerk identity, records the receipt
// and enqueues an authoritative refresh — it never applies the event payload. This pins that a
// refund event is accepted and reaches that refresh path, so a refund cannot be silently dropped
// by a type allowlist.
func TestRevenueCatWebhookAcceptsRefundCancellationAndQueuesRefresh(t *testing.T) {
	h, inbox, q := newRCWebhookTestHandler()
	body := `{"event":{"id":"refund-event","type":"CANCELLATION","cancel_reason":"CUSTOMER_SUPPORT","store":"APP_STORE","price":-9.99,"app_user_id":"user_clerk"}}`
	got, err := h.Handle(context.Background(), rcSignedWebhook(body))
	require.NoError(t, err)
	require.Equal(t, "queued", got.Body.Status)
	require.Equal(t, 1, inbox.writes)
	require.Equal(t, []jobs.RevenueCatSyncArgs{{UserID: "internal-user", EventID: "refund-event"}}, q.args)
}

func TestRevenueCatWebhookTestDeliveryDoesNotQueueAccess(t *testing.T) {
	h, inbox, q := newRCWebhookTestHandler()
	out, err := h.Handle(context.Background(), rcSignedWebhook(`{"event":{"id":"test-event","type":"TEST"}}`))
	require.NoError(t, err)
	require.Equal(t, "test_received", out.Body.Status)
	require.Zero(t, inbox.writes)
	require.Empty(t, q.args)
}

func TestRevenueCatWebhookRejectsUnresolvedIdentities(t *testing.T) {
	for _, id := range []string{"unknown", "internal-user", "someone@example.com"} {
		t.Run(id, func(t *testing.T) {
			h, inbox, q := newRCWebhookTestHandler()
			_, err := h.Handle(context.Background(), rcSignedWebhook(`{"event":{"id":"event","type":"EXPIRATION","app_user_id":"`+id+`"}}`))
			var status huma.StatusError
			require.ErrorAs(t, err, &status)
			require.Equal(t, 503, status.GetStatus())
			require.Zero(t, inbox.writes)
			require.Empty(t, q.args)
		})
	}
}

func TestRevenueCatWebhookQueueFailureDoesNotAcknowledge(t *testing.T) {
	h, inbox, q := newRCWebhookTestHandler()
	q.err = errors.New("queue unavailable")
	_, err := h.Handle(context.Background(), rcSignedWebhook(`{"event":{"id":"event-2","type":"EXPIRATION","app_user_id":"user_clerk"}}`))
	require.Error(t, err)
	var status huma.StatusError
	require.ErrorAs(t, err, &status)
	require.Equal(t, 503, status.GetStatus())
	require.Zero(t, inbox.writes)
}

func TestRevenueCatWebhookRejectsMalformedTrustedPayload(t *testing.T) {
	h, inbox, _ := newRCWebhookTestHandler()
	for _, body := range []string{`{`, `{"event":{"type":"EXPIRATION","app_user_id":"user_clerk"}}`, `{"event":{"id":"event","type":"EXPIRATION"}}`} {
		_, err := h.Handle(context.Background(), rcSignedWebhook(body))
		require.Error(t, err)
		var status huma.StatusError
		require.ErrorAs(t, err, &status)
		require.Equal(t, 400, status.GetStatus())
	}
	require.Zero(t, inbox.writes)
}

func TestRevenueCatWebhookRoutePreservesSignedRawBody(t *testing.T) {
	h, inbox, _ := newRCWebhookTestHandler()
	_, api := humatest.New(t)
	RegisterRevenueCatWebhookRoutes(api, h)
	input := rcSignedWebhook("{ \"event\": {\"id\":\"route-event\",\"type\":\"INITIAL_PURCHASE\",\"app_user_id\":\"user_clerk\"}}")
	response := api.Post("/webhooks/revenuecat", bytes.NewReader(input.RawBody), "Content-Type: application/json", "Authorization: "+input.Authorization, "X-RevenueCat-Webhook-Signature: "+input.Signature)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Equal(t, 1, inbox.writes)
}

// rcWebhookCapture records what actually reached the transport boundary, after the real Gateway
// resolved identity, validated the generated contract and normalized every property.
type rcWebhookCapture struct {
	subject string
	event   string
	props   map[string]interface{}
}

type rcWebhookTransport struct{ captures []rcWebhookCapture }

func (t *rcWebhookTransport) Capture(subject, event string, props map[string]interface{}) {
	t.captures = append(t.captures, rcWebhookCapture{subject: subject, event: event, props: props})
}
func (*rcWebhookTransport) Identify(string, map[string]interface{}) {}
func (*rcWebhookTransport) Close()                                  {}

type rcWebhookAuthority struct {
	granted bool
	subject string
	err     error
}

func (a rcWebhookAuthority) WithConsentedSubject(_ context.Context, _ string, emit func(string)) (bool, error) {
	if a.err != nil {
		return false, a.err
	}
	if a.granted {
		emit(a.subject)
	}
	return a.granted, nil
}

// newRCWebhookGatewayHandler wires the handler to the real Gateway so a wrong contract cannot pass
// behind a mock: the gateway drops any event whose contract is unknown, operational, inactive or
// whose properties fail validation.
func newRCWebhookGatewayHandler(authority rcWebhookAuthority) (*RevenueCatWebhookHandler, *rcWebhookInbox, *rcWebhookQueue, *rcWebhookTransport) {
	inbox := &rcWebhookInbox{seen: map[string]bool{}}
	q := &rcWebhookQueue{}
	transport := &rcWebhookTransport{}
	gateway := phclient.NewGateway(transport, authority, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewRevenueCatWebhookHandler(RevenueCatWebhookDeps{
		Inbox: inbox, Profiles: rcWebhookProfiles{}, Queue: q, Analytics: gateway,
		Authorization: "Bearer webhook-test", SigningSecret: "signing-test",
	})
	return h, inbox, q, transport
}

func TestRevenueCatWebhookEmitsAccessChangeThroughRealGateway(t *testing.T) {
	for _, tc := range []struct {
		eventType string
		revoked   bool
	}{
		{eventType: "INITIAL_PURCHASE"},
		{eventType: "RENEWAL"},
		{eventType: "NON_RENEWING_PURCHASE"},
		{eventType: "PRODUCT_CHANGE"},
		{eventType: "SUBSCRIPTION_EXTENDED"},
		{eventType: "TEMPORARY_ENTITLEMENT_GRANT"},
		{eventType: "EXPIRATION", revoked: true},
		{eventType: "SUBSCRIPTION_PAUSED", revoked: true},
	} {
		t.Run(tc.eventType, func(t *testing.T) {
			h, _, q, transport := newRCWebhookGatewayHandler(rcWebhookAuthority{granted: true, subject: "canonical-clerk-subject"})
			body := `{"event":{"id":"event-` + tc.eventType + `","type":"` + tc.eventType + `","app_user_id":"user_clerk"}}`
			got, err := h.Handle(context.Background(), rcSignedWebhook(body))
			require.NoError(t, err)
			require.Equal(t, "queued", got.Body.Status)
			require.Equal(t, []jobs.RevenueCatSyncArgs{{UserID: "internal-user", EventID: "event-" + tc.eventType}}, q.args)
			require.Len(t, transport.captures, 1)
			capture := transport.captures[0]
			require.Equal(t, "canonical-clerk-subject", capture.subject)
			require.Equal(t, "revenuecat_webhook_access_changed", capture.event)
			require.Equal(t, map[string]interface{}{"event_type": tc.eventType, "revoked": tc.revoked}, capture.props)
			for _, forbidden := range []string{"reason", "actor_id", "email", "amount", "account_id", "transaction_id", "item_id", "customer_id"} {
				require.NotContains(t, capture.props, forbidden)
			}
		})
	}
}

func TestRevenueCatWebhookEmitsNothingForDuplicatesTestAndNonAccessDeliveries(t *testing.T) {
	h, inbox, _, transport := newRCWebhookGatewayHandler(rcWebhookAuthority{granted: true, subject: "canonical-clerk-subject"})
	for _, body := range []string{
		`{"event":{"id":"cancel-event","type":"CANCELLATION","cancel_reason":"CUSTOMER_SUPPORT","app_user_id":"user_clerk"}}`,
		`{"event":{"id":"billing-event","type":"BILLING_ISSUE","app_user_id":"user_clerk"}}`,
		`{"event":{"id":"transfer-event","type":"TRANSFER","app_user_id":"user_clerk"}}`,
		`{"event":{"id":"unknown-event","type":"SOMETHING_NEW","app_user_id":"user_clerk"}}`,
		`{"event":{"id":"test-event","type":"TEST"}}`,
	} {
		_, err := h.Handle(context.Background(), rcSignedWebhook(body))
		require.NoError(t, err)
	}
	delivery := `{"event":{"id":"dup-event","type":"INITIAL_PURCHASE","app_user_id":"user_clerk"}}`
	writesBefore := inbox.writes
	_, err := h.Handle(context.Background(), rcSignedWebhook(delivery))
	require.NoError(t, err)
	_, err = h.Handle(context.Background(), rcSignedWebhook(delivery))
	require.NoError(t, err)
	require.Equal(t, writesBefore+1, inbox.writes, "the duplicate must not be re-accepted")
	require.Len(t, transport.captures, 1, "only the first access-changing delivery changed access")
}

func TestRevenueCatWebhookEmitsNothingWhenConsentDenied(t *testing.T) {
	for _, tc := range []struct {
		name      string
		authority rcWebhookAuthority
	}{
		{name: "opted out", authority: rcWebhookAuthority{granted: false}},
		{name: "authority unavailable", authority: rcWebhookAuthority{granted: true, err: errors.New("database unavailable")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, _, transport := newRCWebhookGatewayHandler(tc.authority)
			got, err := h.Handle(context.Background(), rcSignedWebhook(`{"event":{"id":"consent-event","type":"INITIAL_PURCHASE","app_user_id":"user_clerk"}}`))
			require.NoError(t, err)
			require.Equal(t, "queued", got.Body.Status, "the business outcome is unchanged by consent")
			require.Empty(t, transport.captures)
		})
	}
}

func TestRevenueCatWebhookAnalyticsUnconfiguredIsNoOp(t *testing.T) {
	h, inbox, q := newRCWebhookTestHandler()
	got, err := h.Handle(context.Background(), rcSignedWebhook(`{"event":{"id":"noop-event","type":"INITIAL_PURCHASE","app_user_id":"user_clerk"}}`))
	require.NoError(t, err)
	require.Equal(t, "queued", got.Body.Status)
	require.Equal(t, 1, inbox.writes)
	require.Len(t, q.args, 1)
}

// TestRevenueCatWebhookRecordsBoundedIngestionMetrics drives the real handler and asserts the
// operational counters move with the right bounded labels: a NEW accepted receipt counts once (a
// deduped redelivery does not), and a rejection lands in the fixed reason class for where it
// happened — never a raw error string or the request path.
func TestRevenueCatWebhookRecordsBoundedIngestionMetrics(t *testing.T) {
	receivedPair := observability.RevenueCatWebhookReceivedTotal.WithLabelValues("INITIAL_PURCHASE")
	beforeReceived := testutil.ToFloat64(receivedPair)

	h, _, _ := newRCWebhookTestHandler()
	body := `{"event":{"id":"metrics-event","type":"INITIAL_PURCHASE","app_user_id":"user_clerk"}}`
	_, err := h.Handle(context.Background(), rcSignedWebhook(body))
	require.NoError(t, err)
	assert.Equal(t, beforeReceived+1, testutil.ToFloat64(receivedPair))

	_, err = h.Handle(context.Background(), rcSignedWebhook(body))
	require.NoError(t, err)
	assert.Equal(t, beforeReceived+1, testutil.ToFloat64(receivedPair),
		"a deduped redelivery is provider retry pressure, not throughput, and must not count")

	failPair := observability.RevenueCatWebhookFailedTotal.WithLabelValues(
		observability.RevenueCatLabelOther, observability.RevenueCatWebhookReasonInvalidSignature)
	beforeFail := testutil.ToFloat64(failPair)
	bad := rcSignedWebhook(body)
	bad.Authorization = "wrong"
	_, err = h.Handle(context.Background(), bad)
	require.Error(t, err)
	assert.Equal(t, beforeFail+1, testutil.ToFloat64(failPair))

	unresolvedPair := observability.RevenueCatWebhookFailedTotal.WithLabelValues(
		"EXPIRATION", observability.RevenueCatWebhookReasonUnresolvedCustomer)
	beforeUnresolved := testutil.ToFloat64(unresolvedPair)
	_, err = h.Handle(context.Background(), rcSignedWebhook(`{"event":{"id":"metrics-unresolved","type":"EXPIRATION","app_user_id":"unknown"}}`))
	require.Error(t, err)
	assert.Equal(t, beforeUnresolved+1, testutil.ToFloat64(unresolvedPair))

	assert.Greater(t, testutil.CollectAndCount(observability.RevenueCatWebhookProcessingDurationSeconds), 0,
		"the ingestion duration must be observed for an accepted delivery")
}
