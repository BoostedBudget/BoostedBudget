package operations

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	rc "github.com/maxnussbaum/BoostedBudget/internal/integration/revenuecat"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/queue/jobs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

type revenueCatInbox interface {
	AcceptWebhook(context.Context, string, string, string, func(context.Context, pgx.Tx, string) error) (bool, error)
}
type revenueCatIdentityLookup interface {
	GetUserByClerkID(context.Context, string) (*domain.User, error)
}
type revenueCatTransactionalQueue interface {
	EnqueueTx(context.Context, pgx.Tx, queue.Job, ...queue.EnqueueOption) (*queue.JobResult, error)
}
type RevenueCatWebhookDeps struct {
	Inbox                        revenueCatInbox
	Profiles                     revenueCatIdentityLookup
	Queue                        revenueCatTransactionalQueue
	InvalidAttempts              repository.InvalidWebhookAttemptRepository
	Analytics                    phclient.ProductAnalytics
	Authorization, SigningSecret string
}
type RevenueCatWebhookHandler struct{ deps RevenueCatWebhookDeps }

func NewRevenueCatWebhookHandler(deps RevenueCatWebhookDeps) *RevenueCatWebhookHandler {
	return &RevenueCatWebhookHandler{deps: deps}
}

type RevenueCatWebhookInput struct {
	Authorization string `header:"Authorization"`
	Signature     string `header:"X-RevenueCat-Webhook-Signature"`
	RawBody       []byte
}
type RevenueCatWebhookOutput struct {
	Body struct {
		Status string `json:"status" doc:"Receipt status, not confirmation of access"`
	}
}

func (h *RevenueCatWebhookHandler) Handle(ctx context.Context, input *RevenueCatWebhookInput) (*RevenueCatWebhookOutput, error) {
	start := time.Now()
	if err := rc.VerifyWebhook(input.RawBody, input.Authorization, input.Signature, h.deps.Authorization, h.deps.SigningSecret, time.Now()); err != nil {
		h.reject("", observability.RevenueCatWebhookReasonInvalidSignature, "webhook authentication failed")
		recordInvalidWebhookAttempt(ctx, h.deps.InvalidAttempts, "revenuecat", input.RawBody, nil)
		return nil, huma.Error401Unauthorized("Webhook authentication failed.")
	}
	var payload struct {
		Event struct {
			ID        string `json:"id"`
			Type      string `json:"type"`
			AppUserID string `json:"app_user_id"`
		} `json:"event"`
	}
	if json.Unmarshal(input.RawBody, &payload) != nil || payload.Event.ID == "" || len(payload.Event.ID) > 200 || payload.Event.Type == "" || len(payload.Event.Type) > 100 {
		h.reject("", observability.RevenueCatWebhookReasonMalformedPayload, "webhook event fields are missing or malformed")
		return nil, huma.Error400BadRequest("Webhook event fields are missing or malformed.")
	}
	out := &RevenueCatWebhookOutput{}
	if payload.Event.Type == "TEST" {
		out.Body.Status = "test_received"
		observability.ObserveRevenueCatWebhookProcessingDuration(payload.Event.Type, time.Since(start).Seconds())
		return out, nil
	}
	if payload.Event.AppUserID == "" || len(payload.Event.AppUserID) > 255 {
		h.reject(payload.Event.Type, observability.RevenueCatWebhookReasonMalformedPayload, "webhook customer identity is missing or malformed")
		return nil, huma.Error400BadRequest("Webhook customer identity is missing or malformed.")
	}
	if h.deps.Inbox == nil || h.deps.Queue == nil || h.deps.Profiles == nil {
		h.reject(payload.Event.Type, observability.RevenueCatWebhookReasonUnavailable, "subscription synchronization is unavailable")
		return nil, huma.Error503ServiceUnavailable("Subscription synchronization is unavailable. Retry this delivery.")
	}
	// Exact Clerk identity only. Email, anonymous IDs and client-supplied internal
	// UUIDs cannot identify an account for subscription access.
	user, err := h.deps.Profiles.GetUserByClerkID(ctx, payload.Event.AppUserID)
	if err != nil || user == nil || user.IsDeleted {
		h.reject(payload.Event.Type, observability.RevenueCatWebhookReasonUnresolvedCustomer, "webhook customer could not be resolved")
		return nil, huma.Error503ServiceUnavailable("Webhook customer could not be resolved. Retry this delivery.")
	}
	accepted, err := h.deps.Inbox.AcceptWebhook(ctx, payload.Event.ID, user.ID, payload.Event.Type, func(ctx context.Context, tx pgx.Tx, eventID string) error {
		_, err := h.deps.Queue.EnqueueTx(ctx, tx, jobs.RevenueCatSyncArgs{UserID: user.ID, EventID: eventID}, jobs.RevenueCatSyncEnqueueOptions(true)...)
		return err
	})
	if err != nil {
		h.reject(payload.Event.Type, observability.RevenueCatWebhookReasonQueueError, "webhook processing could not be queued")
		return nil, huma.Error503ServiceUnavailable("Webhook processing could not be queued. Retry this delivery.")
	}
	out.Body.Status = "duplicate"
	if accepted {
		out.Body.Status = "queued"
		// Only a NEW accepted row is throughput; a deduped redelivery is provider retry pressure
		// and is deliberately not counted here.
		observability.RecordRevenueCatWebhookReceived(payload.Event.Type)
		h.captureAccessChange(ctx, user.ID, payload.Event.Type)
	}
	observability.ObserveRevenueCatWebhookProcessingDuration(payload.Event.Type, time.Since(start).Seconds())
	return out, nil
}

// reject records an ingestion rejection on both operational surfaces: the fixed-enum Prometheus
// counter (event type normalized, reason class derived from WHERE the rejection happened) and a
// structured log line. It deliberately carries neither the raw body nor any customer identity.
func (h *RevenueCatWebhookHandler) reject(eventType, reasonClass, message string) {
	observability.RecordRevenueCatWebhookFailed(eventType, reasonClass)
	slog.Default().Warn("revenuecat webhook rejected",
		"reason_class", reasonClass,
		"event_type", observability.NormalizeRevenueCatEventType(eventType),
		"message", message,
	)
}

// captureAccessChange emits the product event for a delivery that changed access. It is a pure
// side effect of an accepted delivery: the response, the inbox write and the queued refresh are
// identical whether or not analytics is configured. Only the bounded RevenueCat event type and the
// grant/revoke direction are carried — never the webhook body, a customer id or an email. Duplicate
// deliveries never reach here (they changed nothing) and a TEST delivery returns earlier.
func (h *RevenueCatWebhookHandler) captureAccessChange(ctx context.Context, internalUserID, eventType string) {
	if h.deps.Analytics == nil {
		return
	}
	change, ok := rc.ClassifyAccessChange(eventType)
	if !ok {
		return
	}
	h.deps.Analytics.CaptureEvent(ctx, internalUserID, phclient.RevenueCatWebhookAccessChangedPayload{
		EventType: phclient.RevenueCatWebhookAccessChangedEventType(change.EventType),
		Revoked:   change.Revoked,
	})
}

func RegisterRevenueCatWebhookRoutes(api huma.API, handler *RevenueCatWebhookHandler) {
	huma.Register(api, huma.Operation{OperationID: "revenuecat-webhook", Method: "POST", Path: "/webhooks/revenuecat", Summary: "Receive RevenueCat webhook notifications", Tags: []string{"Webhooks", "RevenueCat"}}, handler.Handle)
}
