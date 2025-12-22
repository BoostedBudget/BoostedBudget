// Package errors provides error mapping utilities for converting domain errors to Huma errors.
package errors

import (
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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
	case errors.Is(err, domain.ErrBudgetNotFound):
		return huma.Error404NotFound("Budget not found")
	case errors.Is(err, domain.ErrBudgetCategoryNotFound):
		return huma.Error404NotFound("Budget category not found")
	case errors.Is(err, domain.ErrTagNotFound):
		return huma.Error404NotFound("Tag not found")
	case errors.Is(err, domain.ErrCategoryNotFound):
		return huma.Error404NotFound("Category not found")
	case errors.Is(err, domain.ErrRuleNotFound):
		return huma.Error404NotFound("Categorization rule not found")
	case errors.Is(err, domain.ErrSavingsGoalNotFound):
		return huma.Error404NotFound("Savings goal not found")

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

	// ================================
	// 403 Forbidden - Tier limits exceeded
	// ================================

	case errors.Is(err, domain.ErrTagLimitExceeded):
		return huma.Error403Forbidden("Tag limit exceeded for your subscription tier. Upgrade to add more tags.")
	case errors.Is(err, domain.ErrCategoryLimitExceeded):
		return huma.Error403Forbidden("Category limit exceeded for your subscription tier. Upgrade to add more categories.")
	case errors.Is(err, domain.ErrRuleLimitExceeded):
		return huma.Error403Forbidden("Categorization rule limit exceeded for your subscription tier. Upgrade to add more rules.")
	case errors.Is(err, services.ErrAccountLimitExceeded):
		return huma.Error403Forbidden("Account limit exceeded for your subscription tier. Upgrade to link more accounts.")
	case errors.Is(err, services.ErrForbiddenUpdate):
		return huma.Error403Forbidden("You are not allowed to update this field")
	case errors.Is(err, repository.ErrNotificationAccessDenied):
		return huma.Error403Forbidden("Access denied to this notification")

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
		return huma.Error400BadRequest("Invalid primary region. Valid regions: iad, lhr, syd")
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

	// ================================
	// 503 Service Unavailable - External service failures
	// ================================

	case errors.Is(err, services.ErrPlaidAPIFailure):
		return huma.Error503ServiceUnavailable("Bank connection service temporarily unavailable. Please try again later.")
	case errors.Is(err, services.ErrPlaidNotConfigured):
		return huma.Error503ServiceUnavailable("Bank connection service not configured")

	// ================================
	// 500 Internal Server Error - Default
	// ================================

	default:
		return huma.Error500InternalServerError("Internal server error")
	}
}
