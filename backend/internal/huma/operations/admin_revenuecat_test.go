package operations

import (
	"context"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/queue/jobs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/require"
)

type adminRCGrants struct {
	op    domain.RevenueCatOperation
	calls int
}

type adminRCResourceOverrides struct {
	repository.SubscriptionOverrideRepository
}

func (adminRCResourceOverrides) GetActiveOverrides(context.Context, string, time.Time) ([]domain.UserSubscriptionOverride, error) {
	return []domain.UserSubscriptionOverride{{ID: 12}}, nil
}
func TestAdminRevenueCatResourceOverridesRemainVisibleDuringOutage(t *testing.T) {
	h := (&AdminHandler{subService: services.NewSubscriptionService(nil, adminRCResourceOverrides{})}).WithRevenueCat(RevenueCatAdminDeps{Profiles: &subscriptionRCProfiles{}, Summary: profileRevenueCatSummary{}})
	out, err := h.GetUserSubscription(context.Background(), &AdminSubscriptionDetailInput{UserID: "target"})
	require.NoError(t, err)
	require.Len(t, out.Body.ResourceOverrides, 1)
	require.Equal(t, int64(12), out.Body.ResourceOverrides[0].ID)
}

func TestAdminRevenueCatDetailUsesOneConfirmedTierForLimitsAndStatus(t *testing.T) {
	cfg := literalAdminSubscriptionsTierConfig()
	tier := cfg.Tiers[domain.TierHousehold3]
	tier.Limits.TransactionHistoryDays = 300
	cfg.Tiers[domain.TierHousehold3] = tier
	asOf := time.Now().Add(-time.Second)
	summary := services.RevenueCatSummary{AsOf: asOf, AccessResolved: true, Effective: &domain.EffectiveEntitlement{Tier: "household_3", Status: "active"}}
	h := (&AdminHandler{subService: services.NewSubscriptionService(cfg, nil)}).WithRevenueCat(RevenueCatAdminDeps{Profiles: &subscriptionRCProfiles{}, Summary: profileRevenueCatSummary{summary: summary}})
	out, err := h.GetUserSubscription(context.Background(), &AdminSubscriptionDetailInput{UserID: "target"})
	require.NoError(t, err)
	require.Equal(t, "active", out.Body.EffectiveStatus)
	require.Equal(t, "household_3", out.Body.EffectiveTier)
	require.Equal(t, 300, out.Body.Limits.TransactionHistoryDays)
	require.Equal(t, asOf, out.Body.AsOf)
}

func TestAdminRevenueCatLegacySubscriptionReadSurvivesAuthorityOutage(t *testing.T) {
	h := (&AdminHandler{}).WithRevenueCat(RevenueCatAdminDeps{Profiles: &subscriptionRCProfiles{}, Summary: profileRevenueCatSummary{}})
	out, err := h.GetUserSubscription(context.Background(), &AdminSubscriptionDetailInput{UserID: "target"})
	require.NoError(t, err)
	require.Equal(t, "unresolved", out.Body.EffectiveStatus)
	require.NotNil(t, out.Body.Authority)
	require.False(t, out.Body.Authority.AccessResolved)
}

func TestAdminRevenueCatRoutesRejectUnauthenticatedRequests(t *testing.T) {
	_, api := humatest.New(t)
	RegisterAdminRevenueCatRoutes(api, &AdminHandler{})
	require.Equal(t, 401, api.Get("/api/v1/admin/revenuecat/health").Code)
	require.Equal(t, 401, api.Post("/api/v1/admin/users/target/revenuecat/grants", `{"class":"comp","tier":"premium","reason":"test"}`).Code)
}

func TestAdminRevenueCatTrialManagementDoesNotCreateFreshEligibility(t *testing.T) {
	g := &adminRCGrants{}
	h := (&AdminHandler{}).WithRevenueCat(RevenueCatAdminDeps{Profiles: &subscriptionRCProfiles{}, Grants: g})
	in := &AdminRevenueCatGrantInput{AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "actor"}}, TargetUserID: "target"}
	in.Body.Class = "trial"
	in.Body.Tier = "premium"
	in.Body.Reason = "trial management"
	_, err := h.RequestRevenueCatGrant(context.Background(), in)
	var status huma.StatusError
	require.ErrorAs(t, err, &status)
	require.Equal(t, 400, status.GetStatus())
	end := time.Now().Add(time.Hour)
	in.Body.ExpiresAt = &end
	_, err = h.RequestRevenueCatGrant(context.Background(), in)
	require.ErrorAs(t, err, &status)
	require.Equal(t, 409, status.GetStatus())
	require.Zero(t, g.calls)
}

type adminRCQueue struct {
	queue.JobQueue
	jobs []jobs.RevenueCatSyncArgs
}

func (q *adminRCQueue) Enqueue(_ context.Context, job queue.Job, _ ...queue.EnqueueOption) (*queue.JobResult, error) {
	q.jobs = append(q.jobs, job.(jobs.RevenueCatSyncArgs))
	return &queue.JobResult{}, nil
}

type adminRCReceipts struct {
	repository.RevenueCatAdminRepository
	receipt domain.RevenueCatWebhookReceipt
}

type adminRCOperations struct {
	repository.RevenueCatRepository
	operations []domain.RevenueCatOperation
}

func (r adminRCOperations) Read(context.Context, string) (domain.RevenueCatState, error) {
	return domain.RevenueCatState{PendingOperations: 1}, nil
}
func (r adminRCReceipts) ListWebhooks(context.Context, int, int) ([]domain.RevenueCatWebhookReceipt, error) {
	return []domain.RevenueCatWebhookReceipt{r.receipt}, nil
}

func TestAdminRevenueCatDiagnosticsKeepsPendingWorkVisibleDuringAccessOutage(t *testing.T) {
	h := (&AdminHandler{}).WithRevenueCat(RevenueCatAdminDeps{Profiles: &subscriptionRCProfiles{}, Summary: profileRevenueCatSummary{}, States: adminRCOperations{operations: []domain.RevenueCatOperation{{ID: "operation", Stage: "pending"}}}})
	out, err := h.GetRevenueCatUser(context.Background(), &AdminRevenueCatUserInput{TargetUserID: "target"})
	require.NoError(t, err)
	require.False(t, out.Body.Summary.AccessResolved)
	require.Len(t, out.Body.Operations, 1)
	require.Equal(t, 1, out.Body.State.PendingOperations)
}

func TestAdminRevenueCatListsReceiptsAndQueuesCustomerRefresh(t *testing.T) {
	q := &adminRCQueue{}
	h := (&AdminHandler{}).WithRevenueCat(RevenueCatAdminDeps{Profiles: &subscriptionRCProfiles{}, Admin: adminRCReceipts{receipt: domain.RevenueCatWebhookReceipt{EventID: "event"}}}).WithJobQueue(q)
	out, err := h.ListRevenueCatWebhooks(context.Background(), &AdminRevenueCatWebhooksInput{Limit: 20})
	require.NoError(t, err)
	require.Len(t, out.Body, 1)
	_, err = h.RefreshRevenueCatUser(context.Background(), &AdminRevenueCatUserInput{AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "actor"}}, TargetUserID: "target"})
	require.NoError(t, err)
	require.Equal(t, []jobs.RevenueCatSyncArgs{{UserID: "target"}}, q.jobs)
}

func (r adminRCOperations) ListOperations(context.Context, string) ([]domain.RevenueCatOperation, error) {
	return r.operations, nil
}

func TestAdminRevenueCatRetryDoesNotReviveAppliedOrSupersededOperations(t *testing.T) {
	for _, stage := range []string{"pending", "applied", "superseded", "abandoned"} {
		q := &adminRCQueue{}
		h := (&AdminHandler{}).WithRevenueCat(RevenueCatAdminDeps{States: adminRCOperations{operations: []domain.RevenueCatOperation{{ID: "operation", UserID: "target", Stage: stage}}}}).WithJobQueue(q)
		in := &AdminRevenueCatOperationInput{AdminRevenueCatUserInput: AdminRevenueCatUserInput{AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "actor"}}, TargetUserID: "target"}, OperationID: "operation"}
		_, err := h.RetryRevenueCatOperation(context.Background(), in)
		if stage == "pending" {
			require.NoError(t, err)
			require.Equal(t, []jobs.RevenueCatSyncArgs{{UserID: "target", OperationID: "operation"}}, q.jobs)
		} else {
			var status huma.StatusError
			require.ErrorAs(t, err, &status)
			require.Equal(t, 409, status.GetStatus(),
				"only a pending operation may be retried; %q is terminal or superseded", stage)
			require.Empty(t, q.jobs)
		}
	}
}

func (r adminRCReceipts) GetWebhook(context.Context, string) (domain.RevenueCatWebhookReceipt, error) {
	return r.receipt, nil
}

func TestAdminRevenueCatUnavailableMetricsAreNotReportedAsZero(t *testing.T) {
	h := &AdminHandler{}
	out, err := h.GetRevenueCatHealth(context.Background(), &AdminRevenueCatHealthInput{})
	require.NoError(t, err)
	require.False(t, out.Body.Available)
	require.Nil(t, out.Body.Metrics)
}

func TestAdminRevenueCatReplayUsesExistingReceiptWithoutReingestion(t *testing.T) {
	for _, processed := range []bool{false, true} {
		q := &adminRCQueue{}
		receipt := domain.RevenueCatWebhookReceipt{EventID: "event", UserID: "target"}
		if processed {
			now := time.Now()
			receipt.ProcessedAt = &now
		}
		h := (&AdminHandler{}).WithRevenueCat(RevenueCatAdminDeps{Admin: adminRCReceipts{receipt: receipt}}).WithJobQueue(q)
		in := &AdminRevenueCatReplayInput{AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "actor"}}, EventID: "event"}
		out, err := h.ReplayRevenueCatWebhook(context.Background(), in)
		require.NoError(t, err)
		require.Equal(t, "queued", out.Body.Status)
		require.Len(t, q.jobs, 1)
		require.Equal(t, "target", q.jobs[0].UserID)
		if processed {
			require.Empty(t, q.jobs[0].EventID)
		} else {
			require.Equal(t, "event", q.jobs[0].EventID)
		}
	}
}

func (g *adminRCGrants) RequestGrant(_ context.Context, target, class, tier string, end *time.Time, revoke bool, actor, reason string) (domain.RevenueCatOperation, error) {
	g.calls++
	g.op = domain.RevenueCatOperation{ID: "operation", UserID: target, Class: class, Key: class + "_" + tier, ExpiresAt: end, Revoke: revoke, ActorID: &actor, Reason: reason, Stage: "pending"}
	return g.op, nil
}

func TestAdminRevenueCatGrantRecordsActorAndAcknowledgesPending(t *testing.T) {
	g := &adminRCGrants{}
	h := (&AdminHandler{}).WithRevenueCat(RevenueCatAdminDeps{Grants: g, Profiles: &subscriptionRCProfiles{}})
	in := &AdminRevenueCatGrantInput{AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "actor"}}, TargetUserID: "target"}
	in.Body.Class = "comp"
	in.Body.Tier = "household_3"
	in.Body.Reason = "Support grant"
	out, err := h.RequestRevenueCatGrant(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, "pending", out.Body.Stage)
	require.Equal(t, "actor", *g.op.ActorID)
	require.Equal(t, "target", g.op.UserID)
}

func TestAdminRevenueCatGrantRejectsPaidAndUnattributedWrites(t *testing.T) {
	for _, tc := range []struct{ class, actor string }{{"paid", "actor"}, {"comp", ""}} {
		g := &adminRCGrants{}
		h := (&AdminHandler{}).WithRevenueCat(RevenueCatAdminDeps{Grants: g, Profiles: &subscriptionRCProfiles{}})
		in := &AdminRevenueCatGrantInput{AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: tc.actor}}, TargetUserID: "target"}
		in.Body.Class = tc.class
		in.Body.Tier = "premium"
		in.Body.Reason = "reason"
		_, err := h.RequestRevenueCatGrant(context.Background(), in)
		var status huma.StatusError
		require.ErrorAs(t, err, &status)
		require.Zero(t, g.calls)
	}
}

func TestAdminRevenueCatLegacyTierEndpointCannotWriteLocalOverride(t *testing.T) {
	g := &adminRCGrants{}
	h := (&AdminHandler{}).WithRevenueCat(RevenueCatAdminDeps{Grants: g, Profiles: &subscriptionRCProfiles{}})
	in := &AdminSubscriptionTierInput{AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "actor"}}, UserID: "target"}
	in.Body.Tier = "premium"
	in.Body.Reason = "support"
	out, err := h.UpdateUserTier(context.Background(), in)
	require.NoError(t, err)
	require.Contains(t, out.Body.Message, "queued")
	require.Equal(t, "pending", out.Body.Stage)
	require.Equal(t, 1, g.calls)
	require.Equal(t, "comp", g.op.Class)
}

func TestAdminRevenueCatLegacyClearQueuesCompOnlyRevocation(t *testing.T) {
	g := &adminRCGrants{}
	h := (&AdminHandler{}).WithRevenueCat(RevenueCatAdminDeps{Grants: g, Profiles: &subscriptionRCProfiles{}})
	in := &AdminRemoveTierOverrideInput{AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "actor"}}, UserID: "target"}
	in.Body.Reason = "End courtesy access"
	out, err := h.RemoveUserTierOverride(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, "pending", out.Body.Stage)
	require.False(t, out.Body.Removed, "pending is not confirmed removal")
	require.True(t, g.op.Revoke)
	require.Equal(t, "comp", g.op.Class)
}
