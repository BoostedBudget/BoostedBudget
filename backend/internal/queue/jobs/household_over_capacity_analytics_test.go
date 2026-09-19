package jobs

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type householdCapacityCapture struct {
	subject string
	event   string
	props   map[string]interface{}
}

type householdCapacityTransport struct{ captures []householdCapacityCapture }

func (t *householdCapacityTransport) Capture(subject, event string, props map[string]interface{}) {
	t.captures = append(t.captures, householdCapacityCapture{subject: subject, event: event, props: props})
}
func (*householdCapacityTransport) Identify(string, map[string]interface{}) {}
func (*householdCapacityTransport) Close()                                  {}

type householdCapacityAuthority struct{ allowed bool }

func (a householdCapacityAuthority) WithConsentedSubject(_ context.Context, _ string, emit func(string)) (bool, error) {
	if a.allowed {
		emit("canonical-subject")
	}
	return a.allowed, nil
}

// householdCapacityForbiddenKeys are the identifiers/reasons the over-capacity event must never
// carry. household_id is included because the subscription-webhook fast-path emitter used to send
// it and the promoted contract now rejects it.
var householdCapacityForbiddenKeys = []string{
	"reason", "actor_id", "email", "amount", "account_id", "transaction_id",
	"item_id", "customer_id", "plaid_item_id", "user_id", "household_id", "member_id",
}

func newCapacityAnalyticsWorker(t *testing.T, authority householdCapacityAuthority) (
	*HouseholdOverCapacityCheckWorker,
	*mockHouseholdOverCapacityCheckRepository,
	*mockHouseholdOverCapacityUserRepository,
	*mockHouseholdOverCapacityEmailService,
	*householdCapacityTransport,
) {
	t.Helper()
	worker, repo, userRepo, _, emailSvc := setupOverCapacityCheckWorkerTest(t)
	transport := &householdCapacityTransport{}
	worker.SetAnalytics(phclient.NewGateway(transport, authority, slog.New(slog.NewTextHandler(io.Discard, nil))))
	return worker, repo, userRepo, emailSvc, transport
}

// expectOverCapacityEntry queues the SQL a not-yet-flagged over-capacity household issues: the
// owner is household_2 (2 seats) and 3 members are active, so the backstop flags it and sends the
// start notice in the same run.
func expectOverCapacityEntry(
	ctx context.Context,
	repo *mockHouseholdOverCapacityCheckRepository,
	userRepo *mockHouseholdOverCapacityUserRepository,
	emailSvc *mockHouseholdOverCapacityEmailService,
	hh *domain.Household,
	owner *domain.User,
) {
	userRepo.On("GetUserByID", ctx, owner.ID).Return(owner, nil)
	repo.On("CountActiveMembers", ctx, hh.ID).Return(3, nil)
	repo.On("GetHouseholdByID", ctx, hh.ID).Return(hh, nil)
	repo.On("SetOverCapacity", ctx, hh.ID, mock.AnythingOfType("time.Time")).Return(time.Now(), nil)
	emailSvc.On("SendHouseholdOverCapacityNotice", ctx, owner.Email, hh.Name, 3, 2, mock.AnythingOfType("time.Time")).Return(nil)
	repo.On("MarkOverCapacityStartNoticeSent", ctx, hh.ID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(nil)
}

// TestHouseholdOverCapacityEnteredThroughRealGateway drives the authoritative daily backstop's
// over-capacity entry through the REAL Gateway + AuthorityResolver. The event name is the existing
// household_over_capacity_entered, reused rather than duplicated: the backstop discovers the same
// product state transition the subscription-webhook fast path reports.
func TestHouseholdOverCapacityEnteredThroughRealGateway(t *testing.T) {
	ctx := context.Background()
	worker, repo, userRepo, emailSvc, transport := newCapacityAnalyticsWorker(t, householdCapacityAuthority{allowed: true})
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths"}
	expectOverCapacityEntry(ctx, repo, userRepo, emailSvc, hh, owner)

	require.NoError(t, worker.processHousehold(ctx, 1, hh))
	require.Len(t, transport.captures, 1)
	require.Equal(t, phclient.EventHouseholdOverCapacityEntered, transport.captures[0].event)
	require.Equal(t, map[string]interface{}{"active_count": int64(3), "seat_limit": int64(2)}, transport.captures[0].props)
	require.Equal(t, "canonical-subject", transport.captures[0].subject)
	for _, forbidden := range householdCapacityForbiddenKeys {
		require.NotContains(t, transport.captures[0].props, forbidden)
	}
}

// TestHouseholdOverCapacityEnteredNotReEmittedForAlreadyFlaggedHousehold is the failure QA:
// re-firing the same transition must not emit a duplicate. The first pass flags the household; the
// second pass sees the flag already set and must not re-emit.
func TestHouseholdOverCapacityEnteredNotReEmittedForAlreadyFlaggedHousehold(t *testing.T) {
	ctx := context.Background()
	worker, repo, userRepo, emailSvc, transport := newCapacityAnalyticsWorker(t, householdCapacityAuthority{allowed: true})
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths"}
	expectOverCapacityEntry(ctx, repo, userRepo, emailSvc, hh, owner)

	require.NoError(t, worker.processHousehold(ctx, 1, hh))
	require.Len(t, transport.captures, 1, "the first pass flags the household and emits once")
	require.NotNil(t, hh.OverCapacitySince, "the first pass must have set the grace anchor")

	require.NoError(t, worker.processHousehold(ctx, 1, hh))
	require.Len(t, transport.captures, 1, "re-firing the same transition must not emit a duplicate")
}

// TestHouseholdOverCapacityEnteredRespectsConsent pins the consent boundary: an opted-out owner
// produces no event even though the backstop still flags the household.
func TestHouseholdOverCapacityEnteredRespectsConsent(t *testing.T) {
	ctx := context.Background()
	worker, repo, userRepo, emailSvc, transport := newCapacityAnalyticsWorker(t, householdCapacityAuthority{allowed: false})
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths"}
	expectOverCapacityEntry(ctx, repo, userRepo, emailSvc, hh, owner)

	require.NoError(t, worker.processHousehold(ctx, 1, hh))
	require.Empty(t, transport.captures, "an opted-out owner must not produce a product event")
	require.NotNil(t, hh.OverCapacitySince, "the flag is still set regardless of analytics")
}
