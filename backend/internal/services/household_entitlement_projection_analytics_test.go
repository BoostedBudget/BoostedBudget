package services

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// householdProjectionForbiddenKeys are the identifiers/reasons the projection event must never
// carry. household_id and member_id are included because the event is household-scoped and the
// temptation to attach one is real.
var householdProjectionForbiddenKeys = []string{
	"reason", "actor_id", "email", "amount", "account_id", "transaction_id",
	"item_id", "customer_id", "plaid_item_id", "user_id", "household_id", "member_id",
}

// expectAcceptInviteSuccess queues the exact SQL the happy-path AcceptInvite issues, mirroring
// TestHouseholdService_AcceptInvite_Success. The owner is household_2 (2 seats) and the household
// has 1 active member before the join, so the projection is tier=household_2, seat_count=2,
// seats_included=2.
func expectAcceptInviteSuccess(pool pgxmock.PgxPoolIface) {
	mask := "mask-blind-1"
	pool.ExpectBegin()
	expectHouseholdIDLookupAndLock(pool, "hh-1", "owner-1")
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(acceptInviteRow("pending", time.Now().Add(48*time.Hour)))
	expectLockCallerRow(pool, "caller-1")
	expectLockCallerRow(pool, "owner-1")
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	expectNoRemovedMember(pool, "hh-1", "caller-1")
	pool.ExpectQuery("INSERT INTO household_members").
		WithArgs("hh-1", "caller-1", domain.HouseholdRoleMember, domain.HouseholdMemberStatusActive, pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id", "joined_at", "created_at", "updated_at"}).AddRow("mem-1", time.Now(), time.Now(), time.Now()))
	pool.ExpectQuery("user_accounts").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx", "identity_type", "identity_subtype"}).
			AddRow("acct-1", "inst-1", &mask, nil, nil).
			AddRow("acct-2", "inst-2", nil, nil, nil))
	pool.ExpectQuery("INSERT INTO household_share_consents").
		WithArgs("hh-1", "caller-1", domain.HouseholdShareConsentVersion, pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id", "consented_at", "created_at"}).AddRow("consent-1", time.Now(), time.Now()))
	pool.ExpectExec("INSERT INTO household_account_shares").
		WithArgs("hh-1", "acct-1", pgxmock.AnyArg(), domain.ShareVisibilityShared).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	pool.ExpectExec("INSERT INTO household_account_shares").
		WithArgs("hh-1", "acct-2", (*string)(nil), domain.ShareVisibilityShared).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	pool.ExpectExec("UPDATE subscription_entitlements SET effective_until").WithArgs("caller-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	pool.ExpectExec("INSERT INTO subscription_entitlements").
		WithArgs("caller-1", "household_2", "active", "owner-1", "hh-1").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	pool.ExpectExec("UPDATE household_invites SET status = 'accepted'").WithArgs("caller-1", "inv-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()
}

func newProjectionAnalyticsService(t *testing.T, authority *rcGatewayAuthority) (*householdService, pgxmock.PgxPoolIface, *rcGatewayTransport) {
	t.Helper()
	userRepo := &mockUserLoader{}
	caller := &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: true, SubscriptionTier: "premium", SubscriptionStatus: "cancelled"}
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	userRepo.On("GetUserByID", mock.Anything, "caller-1").Return(caller, nil)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)

	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	transport := &rcGatewayTransport{}
	svc.SetProductAnalytics(phclient.NewGateway(transport, authority, slog.New(slog.NewTextHandler(io.Discard, nil))))
	return svc, pool, transport
}

// TestHouseholdEntitlementProjectedThroughRealGateway drives a genuine AcceptInvite through the
// REAL Gateway + AuthorityResolver. A mock would bypass contract validation, so a wrong contract
// (an unbounded string, a property key containing an id, a value outside its enum) would silently
// drop in production while the test stayed green.
func TestHouseholdEntitlementProjectedThroughRealGateway(t *testing.T) {
	t.Parallel()
	svc, pool, transport := newProjectionAnalyticsService(t, &rcGatewayAuthority{allowed: true, subject: "canonical-subject"})
	defer pool.Close()
	expectAcceptInviteSuccess(pool)

	_, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, nil)
	require.NoError(t, err)
	require.Equal(t, []string{phclient.EventHouseholdEntitlementProjected}, rcCapturedNames(transport))
	require.Equal(t, map[string]interface{}{
		"tier":           "household_2",
		"seat_count":     int64(2),
		"seats_included": int64(2),
	}, transport.captures[0].props)
	require.Equal(t, "canonical-subject", transport.captures[0].subject)
	for _, forbidden := range householdProjectionForbiddenKeys {
		require.NotContains(t, transport.captures[0].props, forbidden)
	}
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestHouseholdEntitlementProjectedRespectsConsent pins the consent boundary: an opted-out owner
// produces no event even though the join itself succeeds.
func TestHouseholdEntitlementProjectedRespectsConsent(t *testing.T) {
	t.Parallel()
	svc, pool, transport := newProjectionAnalyticsService(t, &rcGatewayAuthority{allowed: false})
	defer pool.Close()
	expectAcceptInviteSuccess(pool)

	_, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, nil)
	require.NoError(t, err)
	require.Empty(t, transport.captures, "an opted-out owner must not produce a product event")
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestHouseholdEntitlementProjectedNotEmittedOnRolledBackJoin is the failure QA: a join that
// rolls back (expired invite) must emit nothing, because the capture runs only after commit.
func TestHouseholdEntitlementProjectedNotEmittedOnRolledBackJoin(t *testing.T) {
	t.Parallel()
	svc, pool, transport := newProjectionAnalyticsService(t, &rcGatewayAuthority{allowed: true, subject: "canonical-subject"})
	defer pool.Close()

	pool.ExpectBegin()
	expectHouseholdIDLookupAndLock(pool, "hh-1", "owner-1")
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(acceptInviteRow("pending", time.Now().Add(-1*time.Hour)))
	pool.ExpectRollback()

	_, err := svc.AcceptInvite(context.Background(), "caller-1", "rawtoken", true, nil)
	require.ErrorIs(t, err, domain.ErrInviteExpired)
	require.Empty(t, transport.captures, "a rolled-back join must not emit the projection event")
	require.NoError(t, pool.ExpectationsWereMet())
}
