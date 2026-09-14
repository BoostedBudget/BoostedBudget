package operations

import (
	"context"
	"encoding/json"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	rc "github.com/maxnussbaum/BoostedBudget/internal/integration/revenuecat"
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
	if err := rc.VerifyWebhook(input.RawBody, input.Authorization, input.Signature, h.deps.Authorization, h.deps.SigningSecret, time.Now()); err != nil {
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
		return nil, huma.Error400BadRequest("Webhook event fields are missing or malformed.")
	}
	out := &RevenueCatWebhookOutput{}
	if payload.Event.Type == "TEST" {
		out.Body.Status = "test_received"
		return out, nil
	}
	if payload.Event.AppUserID == "" || len(payload.Event.AppUserID) > 255 {
		return nil, huma.Error400BadRequest("Webhook customer identity is missing or malformed.")
	}
	if h.deps.Inbox == nil || h.deps.Queue == nil || h.deps.Profiles == nil {
		return nil, huma.Error503ServiceUnavailable("Subscription synchronization is unavailable. Retry this delivery.")
	}
	// Exact Clerk identity only. Email, anonymous IDs and client-supplied internal
	// UUIDs cannot identify an account for subscription access.
	user, err := h.deps.Profiles.GetUserByClerkID(ctx, payload.Event.AppUserID)
	if err != nil || user == nil || user.IsDeleted {
		return nil, huma.Error503ServiceUnavailable("Webhook customer could not be resolved. Retry this delivery.")
	}
	accepted, err := h.deps.Inbox.AcceptWebhook(ctx, payload.Event.ID, user.ID, payload.Event.Type, func(ctx context.Context, tx pgx.Tx, eventID string) error {
		_, err := h.deps.Queue.EnqueueTx(ctx, tx, jobs.RevenueCatSyncArgs{UserID: user.ID, EventID: eventID}, jobs.RevenueCatSyncEnqueueOptions(true)...)
		return err
	})
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("Webhook processing could not be queued. Retry this delivery.")
	}
	out.Body.Status = "duplicate"
	if accepted {
		out.Body.Status = "queued"
	}
	return out, nil
}

func RegisterRevenueCatWebhookRoutes(api huma.API, handler *RevenueCatWebhookHandler) {
	huma.Register(api, huma.Operation{OperationID: "revenuecat-webhook", Method: "POST", Path: "/webhooks/revenuecat", Summary: "Receive RevenueCat webhook notifications", Tags: []string{"Webhooks", "RevenueCat"}}, handler.Handle)
}
