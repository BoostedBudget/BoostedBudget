package errors_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoggingMapError_Nil(t *testing.T) {
	result := humaerrors.LoggingMapError(context.Background(), nil)
	assert.Nil(t, result)
}

func TestLoggingMapError_404(t *testing.T) {
	result := humaerrors.LoggingMapError(context.Background(), domain.ErrBudgetNotFound)
	require.NotNil(t, result)

	statusErr, ok := result.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusNotFound, statusErr.GetStatus())
}

func TestLoggingMapError_500(t *testing.T) {
	unknownErr := errors.New("unexpected internal failure")
	result := humaerrors.LoggingMapError(context.Background(), unknownErr)
	require.NotNil(t, result)

	statusErr, ok := result.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusInternalServerError, statusErr.GetStatus())
}

// TestLoggingMapError_409_AccountAlreadyDisconnected: Issue #1569, Task 2 — a repeat disconnect
// must map to 409 Conflict, the same shape as PurgeAccount's ErrAccountNotDisconnected, not a 500
// or a silently-swallowed success.
func TestLoggingMapError_409_AccountAlreadyDisconnected(t *testing.T) {
	result := humaerrors.LoggingMapError(context.Background(), repository.ErrAccountAlreadyDisconnected)
	require.NotNil(t, result)

	statusErr, ok := result.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusConflict, statusErr.GetStatus())
}

// Issue #1270: GAP-L — rule validation errors must return 400, not 500.
func TestLoggingMapError_400_InvalidRule(t *testing.T) {
	wrapped := fmt.Errorf("%w: condition field 'budget_category.bad_field' is not allowed for event 'budget_period_end'", domain.ErrInvalidBoost)
	result := humaerrors.LoggingMapError(context.Background(), wrapped)
	require.NotNil(t, result)

	statusErr, ok := result.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
}

// Issue #1270: GAP-L — foreign category_id in condition must return 403, not 500.
func TestLoggingMapError_403_RuleConditionForbidden(t *testing.T) {
	wrapped := fmt.Errorf("%w: condition category_id does not belong to this user", domain.ErrBoostConditionForbidden)
	result := humaerrors.LoggingMapError(context.Background(), wrapped)
	require.NotNil(t, result)

	statusErr, ok := result.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusForbidden, statusErr.GetStatus())
}

// Issue #1367 (Codex review, Medium finding 5): the paying-invitee 409 surfaces the
// caller's eligibility date when known, and falls back to the generic message when not.
func TestLoggingMapError_InviteeHasActivePaidSubscription_SurfacesDateWhenKnown(t *testing.T) {
	endsAt := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	err := &domain.InviteeHasActivePaidSubscriptionError{EligibleAfter: &endsAt}

	result := humaerrors.LoggingMapError(context.Background(), err)
	require.NotNil(t, result)
	statusErr, ok := result.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusConflict, statusErr.GetStatus())
	assert.Contains(t, statusErr.Error(), "March 15, 2026")
}

func TestLoggingMapError_InviteeHasActivePaidSubscription_NoDate_GenericMessage(t *testing.T) {
	err := &domain.InviteeHasActivePaidSubscriptionError{EligibleAfter: nil}

	result := humaerrors.LoggingMapError(context.Background(), err)
	require.NotNil(t, result)
	statusErr, ok := result.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusConflict, statusErr.GetStatus())
	assert.Contains(t, statusErr.Error(), "you can join a household once it ends")
}

// Issue #1367: household sentinel error → HTTP status mapping.
func TestLoggingMapError_Household(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"household not found", domain.ErrHouseholdNotFound, http.StatusNotFound},
		{"invite not found", domain.ErrInviteNotFound, http.StatusNotFound},
		{"member not found", domain.ErrMemberNotFound, http.StatusNotFound},
		{"already owns household", domain.ErrAlreadyOwnsHousehold, http.StatusConflict},
		{"already in household", domain.ErrAlreadyInHousehold, http.StatusConflict},
		{"household has active members", domain.ErrHouseholdHasActiveMembers, http.StatusConflict},
		{"household seats full", domain.ErrHouseholdSeatsFull, http.StatusConflict},
		{"invite pending exists", domain.ErrInvitePendingExists, http.StatusConflict},
		{"owner plan inactive", domain.ErrOwnerPlanInactive, http.StatusConflict},
		{"invitee has active paid subscription", domain.ErrInviteeHasActivePaidSubscription, http.StatusConflict},
		{"already covered by household", domain.ErrAlreadyCoveredByHousehold, http.StatusConflict},
		{"invite not pending", domain.ErrInviteNotPending, http.StatusUnauthorized},
		{"invite expired", domain.ErrInviteExpired, http.StatusUnauthorized},
		{"not household owner", domain.ErrNotHouseholdOwner, http.StatusForbidden},
		{"invite email mismatch", domain.ErrInviteEmailMismatch, http.StatusForbidden},
		{"invalid consent", domain.ErrInvalidConsent, http.StatusBadRequest},
		{"cannot invite self", domain.ErrCannotInviteSelf, http.StatusBadRequest},
		{"cannot remove owner", domain.ErrCannotRemoveOwner, http.StatusBadRequest},
		{"account not owned", domain.ErrAccountNotOwned, http.StatusForbidden},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result := humaerrors.LoggingMapError(context.Background(), tc.err)
			require.NotNil(t, result)
			statusErr, ok := result.(huma.StatusError)
			require.True(t, ok, "expected huma.StatusError")
			assert.Equal(t, tc.status, statusErr.GetStatus())
		})
	}
}

// TestMapError_TenancyMismatchSentinelsAreDistinguishable pins that all FOUR batch-tenancy sentinels
// map to distinct messages a reader can act on, rather than falling through to the anonymous
// "Internal server error" default.
//
// FOUR, not three, and not all cross-tenant. The header here said "three cross-tenant sentinels"
// while the table below iterated four, one of which (ErrReplaceAccountMismatch) is a SAME-tenant
// wrong-account fault. The count and the adjective are both fixed.
//
// Codex adversarial review (#1537 round 4) flagged ErrReplaceTenantMismatch as unhandled by
// MapError. Checking the class rather than the reported instance found all three of the
// then-existing sentinels unmapped: ErrOverrideTenantMismatch and ErrAccountBatchTenantMismatch were
// added earlier in the same branch and had the identical hole.
//
// WHY 500 AND NOT 4xx. The reason recorded here was FALSE and is corrected: it claimed "none of
// these can reach a handler today", and that "every one of these is raised by a pre-flight assertion
// against a batch the SERVER assembled from a linked item it owns". Two of the four are
// handler-reachable, and one of those two takes a CLIENT-supplied batch —
// ErrOverrideTenantMismatch via MerchantService.BulkSetOverrides → BulkApply
// (services/merchant_service.go:311) behind the secured bulkSetMerchantOverrides operation, and
// ErrAccountBatchTenantMismatch via accountService.LinkAccounts → CreateBatch
// (services/account_service.go:576) behind exchangePublicToken. Only the two ReplaceForAccount
// sentinels are queue-only.
//
// 500 is still correct, on the argument that survives: each sentinel reports a violated INTERNAL
// INVARIANT, not a rejected input. Every row's tenancy is set by the server from the authenticated
// actor before the repository sees it, so a mismatch means server-side assembly is broken — no input
// the caller could send produces it and no change they make fixes it. A 4xx would tell them to retry
// something that is not theirs to correct. This follows the existing convention for internal faults
// in mapper.go, which map to Error500InternalServerError with a specific message
// (ErrInvalidPaddleEnv, ErrEncryptionKeyRequired, ErrInvalidKeySize).
// allTenancySentinels is every batch-tenancy sentinel MapError distinguishes. Adding one here
// without mapping it, or mapping two to the same string, fails the pairwise check below.
var allTenancySentinels = []error{
	repository.ErrOverrideTenantMismatch,
	repository.ErrAccountBatchTenantMismatch,
	repository.ErrReplaceTenantMismatch,
	repository.ErrReplaceAccountMismatch,
}

func TestMapError_TenancyMismatchSentinelsAreDistinguishable(t *testing.T) {
	generic := "Internal server error"

	for _, tc := range []struct {
		name string
		err  error
	}{
		{"merchant override batch (cross-tenant, client-supplied)", repository.ErrOverrideTenantMismatch},
		{"account batch (cross-tenant, server-assembled)", repository.ErrAccountBatchTenantMismatch},
		{"replace-for-account batch (cross-tenant, queue-only)", repository.ErrReplaceTenantMismatch},
		{"replace-for-account wrong account (SAME tenant, queue-only)", repository.ErrReplaceAccountMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := humaerrors.MapError(tc.err)
			require.NotNil(t, result)

			statusErr, ok := result.(huma.StatusError)
			require.True(t, ok, "expected huma.StatusError")
			assert.Equal(t, http.StatusInternalServerError, statusErr.GetStatus(),
				"a server-assembled batch crossing tenants is an internal invariant violation, not a "+
					"client error — 500 is intended")
			assert.NotEqual(t, generic, statusErr.Error(),
				"the sentinel falls through to the anonymous default, so an operator seeing this in "+
					"Sentry cannot tell a cross-tenant batch from any other 500. Map it explicitly.")

			// PAIRWISE distinct across all four, not just the two replace-batch ones. The check
			// used to compare only ErrReplaceAccountMismatch against ErrReplaceTenantMismatch, so
			// collapsing the merchant-override message into the account-batch message — two
			// different batches, two different subsystems — passed. Every sentinel names a
			// different assembly path, and an operator reading one line in Sentry has nothing else
			// to go on.
			for _, other := range allTenancySentinels {
				if other == tc.err {
					continue
				}
				otherMapped, ok := humaerrors.MapError(other).(huma.StatusError)
				require.True(t, ok)
				assert.NotEqual(t, otherMapped.Error(), statusErr.Error(),
					"%v and %v map to the SAME message — an operator cannot tell which batch "+
						"assembly path failed", tc.err, other)
			}

			// Wrapped, as a repository would return it through a service.
			wrapped := humaerrors.MapError(fmt.Errorf("syncing holdings: %w", tc.err))
			wrappedStatus, ok := wrapped.(huma.StatusError)
			require.True(t, ok)
			assert.NotEqual(t, generic, wrappedStatus.Error(),
				"errors.Is must see through wrapping — repositories return these wrapped by callers")
		})
	}
}

// TestMapError_RepairNonceInvalidIs403AndSaysNothingAboutWhy pins Issue #1620's mapping.
//
// Two properties, not one. The status must be 403 — the caller is authenticated and the item is
// theirs and is repairable, so 404 ("not found") and 409 ("wrong state") both describe something
// that is not true, and both would send the frontend down a different recovery path. And the
// message must not leak WHICH of the burn's predicates failed: "already used", "expired" and
// "never minted" are equally useless to a legitimate user (the recovery is identical) and
// individually useful to someone probing item_ids.
func TestMapError_RepairNonceInvalidIs403AndSaysNothingAboutWhy(t *testing.T) {
	result := humaerrors.MapError(services.ErrRepairNonceInvalid)
	require.NotNil(t, result)

	statusErr, ok := result.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusForbidden, statusErr.GetStatus())

	msg := strings.ToLower(result.Error())
	for _, leak := range []string{"expired", "already used", "already spent", "not minted", "nonce"} {
		assert.NotContains(t, msg, leak,
			"the user-facing message must not disclose which burn predicate failed")
	}
	assert.Contains(t, msg, "reconnect bank",
		"the message must name the one recovery a user can actually perform")
}

// Issue #1626 Phase 2 (review finding X2): a `custom:` category token whose suffix is not a UUID
// must be 400, not 500. The analytics read casts that suffix with `::uuid`, so before this mapping
// a client's own malformed query string was reported as a server failure — and would have paged
// somebody. The wrapped form is used deliberately: the service returns
// `fmt.Errorf("%w: %q", ErrInvalidCategoryToken, suffix)`, so an errors.Is-based case is the only
// one that can see it.
func TestLoggingMapError_400_InvalidCategoryToken(t *testing.T) {
	wrapped := fmt.Errorf("%w: %q", services.ErrInvalidCategoryToken, "not-a-uuid")
	result := humaerrors.LoggingMapError(context.Background(), wrapped)
	require.NotNil(t, result)

	statusErr, ok := result.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
	assert.NotContains(t, statusErr.Error(), "not-a-uuid",
		"the response must not echo attacker-supplied text back to the client")
}
