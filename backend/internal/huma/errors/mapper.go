// Package errors provides error mapping utilities for converting domain errors to Huma errors.
package errors

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getsentry/sentry-go"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// MapError converts domain/service errors to Huma errors with RFC 9457 format.
// Returns nil if the input error is nil.
//
// The mapping follows these conventions:
//   - 400 Bad Request: Validation errors, invalid input
//   - 401 Unauthorized: Authentication failures
//   - 403 Forbidden: Subscription tier limit exceeded
//   - 404 Not Found: Resource not found
//   - 409 Conflict: Resource already exists
//   - 503 Service Unavailable: External service failures
//   - 500 Internal Server Error: All other errors
func MapError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	// ================================
	// 404 Not Found - Resource not found
	// ================================

	// Domain errors
	case errors.Is(err, domain.ErrRolloverHistoryNotFound):
		return huma.Error404NotFound("Rollover history not found")
	case errors.Is(err, domain.ErrBoostExecutionNotFound):
		return huma.Error404NotFound("Boost execution not found")
	case errors.Is(err, domain.ErrBudgetNotFound):
		return huma.Error404NotFound("Budget not found")
	case errors.Is(err, domain.ErrBudgetCategoryNotFound):
		return huma.Error404NotFound("Budget category not found")
	case errors.Is(err, domain.ErrTagNotFound):
		return huma.Error404NotFound("Tag not found")
	case errors.Is(err, domain.ErrCategoryNotFound):
		return huma.Error404NotFound("Category not found")
	case errors.Is(err, domain.ErrCategoryHasChildren):
		return huma.Error409Conflict("Category has subcategories; remove or reparent them first")
	case errors.Is(err, domain.ErrCategoryParentHasParent):
		return huma.Error400BadRequest("Parent category must be top-level (categories support only two levels)")
	case errors.Is(err, domain.ErrCategoryInvalidParent):
		return huma.Error400BadRequest("Invalid parent category")
	case errors.Is(err, domain.ErrRuleNotFound):
		return huma.Error404NotFound("Categorization rule not found")
	case errors.Is(err, domain.ErrSavingsGoalNotFound):
		return huma.Error404NotFound("Savings goal not found")
	case errors.Is(err, domain.ErrBoostNotFound):
		return huma.Error404NotFound("Boost not found")
	case errors.Is(err, domain.ErrTransferSuggestionNotFound):
		return huma.Error404NotFound("Transfer suggestion not found")
	case errors.Is(err, domain.ErrTransferSuggestionAlreadyActioned):
		return huma.Error409Conflict("Transfer suggestion has already been accepted or rejected")

	// Household errors (#1367 — Phase 3)
	case errors.Is(err, domain.ErrHouseholdNotFound):
		return huma.Error404NotFound("Household not found")
	case errors.Is(err, domain.ErrInviteNotFound):
		return huma.Error404NotFound("Invite not found")
	case errors.Is(err, domain.ErrMemberNotFound):
		return huma.Error404NotFound("Member not found")

	// Repository errors
	case errors.Is(err, repository.ErrTransactionNotFound):
		return huma.Error404NotFound("Transaction not found")
	case errors.Is(err, repository.ErrAccountNotFound):
		return huma.Error404NotFound("Account not found")
	case errors.Is(err, repository.ErrUserNotFound):
		return huma.Error404NotFound("User not found")
	case errors.Is(err, repository.ErrLinkedItemNotFound):
		return huma.Error404NotFound("Linked item not found")
	case errors.Is(err, repository.ErrNotificationNotFound):
		return huma.Error404NotFound("Notification not found")
	case errors.Is(err, repository.ErrPrivacySettingsNotFound):
		return huma.Error404NotFound("Privacy settings not found")
	case errors.Is(err, repository.ErrIdentityNotFound):
		return huma.Error404NotFound("User identity not found")
	case errors.Is(err, repository.ErrSnapshotNotFound):
		return huma.Error404NotFound("Balance snapshot not found")
	case errors.Is(err, repository.ErrUserAccountNotFound):
		return huma.Error404NotFound("User account not found")
	case errors.Is(err, repository.ErrDashboardPreferencesNotFound):
		return huma.Error404NotFound("Dashboard preferences not found")
	case errors.Is(err, repository.ErrWebhookNotFound):
		return huma.Error404NotFound("Webhook not found")
	case errors.Is(err, repository.ErrPaddleWebhookNotFound):
		return huma.Error404NotFound("Paddle webhook not found")

	// Service errors
	case errors.Is(err, services.ErrCustomerNotFound):
		return huma.Error404NotFound("Paddle customer not found")
	case errors.Is(err, services.ErrSubscriptionNotFound):
		return huma.Error404NotFound("Paddle subscription not found")
	case errors.Is(err, services.ErrClerkUserNotFound):
		return huma.Error404NotFound("Clerk user not found")

	// ================================
	// 409 Conflict - Resource already exists
	// ================================

	case errors.Is(err, domain.ErrBudgetAlreadyExists):
		return huma.Error409Conflict("A budget with this name already exists")
	case errors.Is(err, domain.ErrTagAlreadyExists):
		return huma.Error409Conflict("A tag with this name already exists")
	case errors.Is(err, domain.ErrCategoryAlreadyExists):
		return huma.Error409Conflict("A category with this name already exists")
	case errors.Is(err, domain.ErrRuleAlreadyExists):
		return huma.Error409Conflict("A categorization rule with this name already exists")
	case errors.Is(err, domain.ErrSavingsGoalAlreadyExists):
		return huma.Error409Conflict("A savings goal with this name already exists")
	case errors.Is(err, repository.ErrDuplicateEmail):
		return huma.Error409Conflict("Email address already in use")
	case errors.Is(err, repository.ErrDuplicateAccount):
		return huma.Error409Conflict("Account already exists")
	case errors.Is(err, repository.ErrDuplicateLinkedItem):
		return huma.Error409Conflict("Institution already linked")
	case errors.Is(err, repository.ErrDuplicateTransaction):
		return huma.Error409Conflict("Transaction already exists")
	case errors.Is(err, repository.ErrDuplicateIdentity):
		return huma.Error409Conflict("User identity already exists")
	case errors.Is(err, repository.ErrDuplicateUserAccount):
		return huma.Error409Conflict("User account already exists")
	case errors.Is(err, repository.ErrPaddleWebhookDuplicate):
		return huma.Error409Conflict("Paddle webhook event already processed")
	case errors.Is(err, repository.ErrAccountOwnedByAnotherUser):
		return huma.Error409Conflict("This bank account is already linked by another user. Each physical account may only be connected to one BoostedBudget account at a time.")
	// GAP AW: idempotency key reused for a different operation — RFC 7231 / Stripe semantics.
	case errors.Is(err, repository.ErrIdempotencyKeyConflict):
		return huma.Error409Conflict("This idempotency key was already used for a different operation. Use a new key to perform a different request.")

	// Household errors (#1367 — Phase 3)
	case errors.Is(err, domain.ErrAlreadyOwnsHousehold):
		return huma.Error409Conflict("You already own a household")
	case errors.Is(err, domain.ErrAlreadyInHousehold):
		return huma.Error409Conflict("You already belong to a household")
	case errors.Is(err, domain.ErrHouseholdHasActiveMembers):
		return huma.Error409Conflict("Remove members before deleting the household")
	case errors.Is(err, domain.ErrHouseholdSeatsFull):
		return huma.Error409Conflict("No seats available on this plan")
	case errors.Is(err, domain.ErrInvitePendingExists):
		return huma.Error409Conflict("A pending invite already exists for this email")
	case errors.Is(err, domain.ErrOwnerPlanInactive):
		return huma.Error409Conflict("The household owner's plan is not currently active")
	// #1367 decision #6: block immediate free coverage for an invitee with a live Paddle
	// billing relationship — the refund-liability guard. Scheduled join is Phase 7 (#1371).
	// Surfaces the advisory eligibility date when known (Codex review, Medium finding 5).
	case errors.Is(err, domain.ErrInviteeHasActivePaidSubscription):
		var withDate *domain.InviteeHasActivePaidSubscriptionError
		if errors.As(err, &withDate) && withDate.EligibleAfter != nil {
			return huma.Error409Conflict(fmt.Sprintf(
				"You have an active plan until %s; you can join a household once it ends",
				withDate.EligibleAfter.Format("January 2, 2006"),
			))
		}
		return huma.Error409Conflict("You have an active plan; you can join a household once it ends")
	// #1367 decision #9: a covered member cannot start their own Paddle checkout.
	case errors.Is(err, domain.ErrAlreadyCoveredByHousehold):
		return huma.Error409Conflict("You're covered under a household plan; leave it before subscribing")

	// ================================
	// 403 Forbidden - Tier limits exceeded
	// ================================

	case errors.Is(err, domain.ErrTagLimitExceeded):
		return huma.Error403Forbidden("Tag limit exceeded for your subscription tier. Upgrade to add more tags.")
	case errors.Is(err, domain.ErrCategoryLimitExceeded):
		return huma.Error403Forbidden("Category limit exceeded for your subscription tier. Upgrade to add more categories.")
	case errors.Is(err, domain.ErrRuleLimitExceeded):
		return huma.Error403Forbidden("Categorization rule limit exceeded for your subscription tier. Upgrade to add more rules.")
	case errors.Is(err, services.ErrForbiddenUpdate):
		return huma.Error403Forbidden("You are not allowed to update this field")
	case errors.Is(err, repository.ErrNotificationAccessDenied):
		return huma.Error403Forbidden("Access denied to this notification")
	// Issue #1270: GAP-L — foreign category_id in a condition belongs to another user.
	case errors.Is(err, domain.ErrBoostConditionForbidden):
		return huma.Error403Forbidden("Access denied: condition category belongs to another user")
	// Household errors (#1367 — Phase 3)
	case errors.Is(err, domain.ErrNotHouseholdOwner):
		return huma.Error403Forbidden("Only the household owner can perform this action")
	case errors.Is(err, domain.ErrInviteEmailMismatch):
		return huma.Error403Forbidden("This invite was sent to a different email address")
	// Household account-sharing errors (#1368 — Phase 4)
	case errors.Is(err, domain.ErrAccountNotOwned):
		return huma.Error403Forbidden("You do not own this account")

	// ================================
	// 400 Bad Request - Validation errors
	// ================================

	case errors.Is(err, domain.ErrInvalidTagName):
		return huma.Error400BadRequest("Invalid tag name")
	case errors.Is(err, domain.ErrInvalidTagColor):
		return huma.Error400BadRequest("Invalid tag color. Use hex format (e.g., #FF5733)")
	case errors.Is(err, services.ErrInvalidEmail):
		return huma.Error400BadRequest("Invalid email address")
	case errors.Is(err, services.ErrInvalidRegion):
		validCodes := domain.GetValidRegions()
		msg := fmt.Sprintf("Invalid primary region. Valid regions: %s", strings.Join(validCodes, ", "))
		return huma.Error400BadRequest(msg)
	case errors.Is(err, services.ErrInvalidLimit):
		return huma.Error400BadRequest("Limit must be between 1 and 100")
	case errors.Is(err, services.ErrInvalidOffset):
		return huma.Error400BadRequest("Offset must be non-negative")
	case errors.Is(err, services.ErrInvalidDateRange):
		return huma.Error400BadRequest("Start date must be before or equal to end date")
	case errors.Is(err, services.ErrInvalidAmountRange):
		return huma.Error400BadRequest("min_amount must be less than or equal to max_amount")
	case errors.Is(err, services.ErrInvalidPublicToken):
		return huma.Error400BadRequest("Invalid or expired public token from Plaid Link")
	case errors.Is(err, services.ErrInvalidPlaidEnv):
		return huma.Error400BadRequest("Invalid Plaid environment configuration")
	case errors.Is(err, services.ErrNoAccountsAvailable):
		return huma.Error400BadRequest("No accounts available from institution")
	case errors.Is(err, services.ErrInvalidSignatureFormat):
		return huma.Error400BadRequest("Invalid webhook signature format")
	case errors.Is(err, services.ErrInvalidPageSizeNumber):
		return huma.Error400BadRequest("page_size must be between 1 and 100")
	// Issue #1270: GAP-L — rule validation failed (unknown event, unsupported operator, bad field).
	// Strip the "invalid automation rule: " prefix to expose only the user-facing detail.
	case errors.Is(err, domain.ErrInvalidBoost):
		detail := strings.TrimPrefix(err.Error(), "invalid automation rule: ")
		return huma.Error400BadRequest(detail)
	// Household errors (#1367 — Phase 3)
	case errors.Is(err, domain.ErrInvalidConsent):
		return huma.Error400BadRequest("Consent is required to join a household")
	case errors.Is(err, domain.ErrCannotInviteSelf):
		return huma.Error400BadRequest("You cannot invite yourself")
	case errors.Is(err, domain.ErrCannotRemoveOwner):
		return huma.Error400BadRequest("The household owner cannot be removed; disband the household instead")

	// ================================
	// 503 Service Unavailable - External service failures
	// ================================

	case errors.Is(err, services.ErrPlaidAPIFailure):
		return huma.Error503ServiceUnavailable("Bank connection service temporarily unavailable. Please try again later.")
	case errors.Is(err, services.ErrPlaidNotConfigured):
		return huma.Error503ServiceUnavailable("Bank connection service not configured")
	case errors.Is(err, services.ErrPaddleNotConfigured):
		return huma.Error503ServiceUnavailable("Payment service not configured")
	case errors.Is(err, services.ErrWebhookSecretNotConfigured):
		return huma.Error503ServiceUnavailable("Webhook secret not configured")

	// ================================
	// 401 Unauthorized - Authentication failures
	// ================================

	case errors.Is(err, services.ErrSignatureVerificationFailed):
		return huma.Error401Unauthorized("Webhook signature verification failed")
	// Household errors (#1367 — Phase 3): the issue specifies 401 for a non-pending or
	// expired invite at accept/decline time (not 409/404), matching a stale-credential
	// semantic rather than a state conflict.
	case errors.Is(err, domain.ErrInviteNotPending):
		return huma.Error401Unauthorized("This invite is no longer valid")
	case errors.Is(err, domain.ErrInviteExpired):
		return huma.Error401Unauthorized("This invite has expired")

	// ================================
	// 500 Internal Server Error - Default
	// ================================

	case errors.Is(err, services.ErrInvalidPaddleEnv):
		return huma.Error500InternalServerError("Invalid payment environment configuration")
	case errors.Is(err, services.ErrEncryptionKeyRequired):
		return huma.Error500InternalServerError("Encryption key not configured")
	case errors.Is(err, services.ErrInvalidKeySize):
		return huma.Error500InternalServerError("Invalid encryption key configuration")

	// Cross-tenant batch assertions (#1537/#1566). Still 500s, but CORRECTED as to why: the sentence
	// that stood here said every one is "raised by a pre-flight check against a batch the SERVER
	// assembled from a linked item it owns", and two of the four are not.
	//
	//   * ErrOverrideTenantMismatch IS handler-reachable, over a CLIENT-supplied batch:
	//     MerchantService.BulkSetOverrides passes the request's own items to
	//     MerchantOverrideRepo.BulkApply (services/merchant_service.go:311), behind the secured
	//     bulkSetMerchantOverrides operation.
	//   * ErrAccountBatchTenantMismatch IS handler-reachable: accountService.LinkAccounts calls
	//     AccountRepository.CreateBatch (services/account_service.go:576), behind the secured
	//     exchangePublicToken operation.
	//   * ErrReplaceTenantMismatch and ErrReplaceAccountMismatch are the two that really are
	//     queue-only: ReplaceForAccount's callers are investment_sync_service.go and
	//     liability_sync_service.go, both driven by River workers.
	//
	// The 500 is nonetheless right for all four, on the argument that actually holds: each is a
	// violated INTERNAL INVARIANT, not a rejected input. The tenancy of every row in these batches is
	// set by the server from the authenticated actor before the repository sees it, so a mismatch
	// means server-side assembly is broken — nothing the caller sent could produce it and nothing the
	// caller can change will fix it. A 4xx would tell them to retry something that is not theirs to
	// correct. The point of mapping them at all is a message an operator can act on: falling through
	// to the anonymous default made a cross-tenant batch indistinguishable from any other 500 in
	// Sentry. Codex flagged ErrReplaceTenantMismatch; checking the class found all three of the
	// then-existing sentinels unmapped.
	case errors.Is(err, repository.ErrOverrideTenantMismatch):
		return huma.Error500InternalServerError("Merchant override batch spans more than one user")
	case errors.Is(err, repository.ErrAccountBatchTenantMismatch):
		return huma.Error500InternalServerError("Account batch spans more than one user")
	case errors.Is(err, repository.ErrReplaceTenantMismatch):
		return huma.Error500InternalServerError("Replace batch spans more than one user")
	// Same shape, different fault: SAME tenant, wrong account. Kept distinct from the line above on
	// purpose — reusing the cross-tenant message would send an operator hunting a data leak that did
	// not happen, when the actual fault is one of the user's own accounts overwriting another.
	case errors.Is(err, repository.ErrReplaceAccountMismatch):
		return huma.Error500InternalServerError("Replace batch targets a different account")

	default:
		return huma.Error500InternalServerError("Internal server error")
	}
}

// LoggingMapError converts a domain error to HTTP error, logging and capturing 5xx errors.
// For 5xx errors: logs the original error and sends to Sentry if configured.
func LoggingMapError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	mapped := MapError(err)
	if mapped == nil {
		return nil
	}

	if statusErr, ok := mapped.(huma.StatusError); ok && statusErr.GetStatus() >= 500 {
		logger := observability.LoggerFromContext(ctx)
		logger.ErrorContext(ctx, "internal server error",
			"error", err,
			"mapped_status", statusErr.GetStatus(),
		)
		sentry.CaptureException(err)
		observability.AppErrorsTotal.WithLabelValues(
			fmt.Sprintf("%T", err),
			fmt.Sprintf("%d", statusErr.GetStatus()),
		).Inc()
	}

	return mapped
}
