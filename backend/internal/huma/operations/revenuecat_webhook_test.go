package operations

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/queue/jobs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
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
