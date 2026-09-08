package jobs

import (
	"context"
	"log/slog"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ItemErrorNotifier is the narrow slice of services.ItemErrorService the sync workers need to
// route a permanent Plaid error into the same in-app notification the webhook path produces
// (Issue #1609 P3-T2). A local interface — rather than depending on services.ItemErrorService
// directly — mirrors the pattern webhook_processing.go's own narrow ItemErrorService interface
// already uses in this package for the same reason: keep each worker's dependency surface to
// exactly what it calls. services.ItemErrorService satisfies this interface structurally; no
// adapter is needed.
type ItemErrorNotifier interface {
	HandleLoginRequired(ctx context.Context, linkedItem *domain.LinkedItem, errorCode string, errorMessage string) error
	HandleError(ctx context.Context, linkedItem *domain.LinkedItem, errorCode string, errorMessage string) error
}

// notifyPermanentPlaidError routes a permanent Plaid error discovered by a sync job through the
// SAME ItemErrorService handlers the ITEM webhook path uses (Issue #1609 P3-T2), so a broken bank
// connection discovered by a sync — not just a webhook — surfaces an in-app notification and a
// linked_items status change instead of leaving the user with no signal at all.
//
// Deliberately shared by all three sync workers (transaction, investment holdings, investment
// transactions): all three are enqueued together for a given item, and a broken access token or
// login-required item fails all three identically — the gap this closes is identical across them.
//
// Deliberately does NOT notify for PRODUCTS_NOT_SUPPORTED: the connection itself is fine there —
// only one product is unavailable for this institution — and there is no matching notification
// type (that is issue #1609's T4). Callers must not even reach here for that code in a way that
// fires analytics as a "failure"; this function itself no-ops so a caller that does not special-
// case it first still behaves correctly.
//
// Non-blocking by design (Issue #1609 P3-T2 constraint): a sync must not fail because a
// notification failed. Errors from the ItemErrorService call are logged, not returned — matching
// item_error_service.go's own log-and-continue idiom for its notification/email steps.
func notifyPermanentPlaidError(
	ctx context.Context,
	itemErrorSvc ItemErrorNotifier,
	logger *slog.Logger,
	linkedItem *domain.LinkedItem,
	plaidErr *domain.PlaidSyncError,
) {
	if itemErrorSvc == nil {
		return
	}
	if plaidErr.ErrorCode == "PRODUCTS_NOT_SUPPORTED" {
		return
	}

	errorMessage := plaidErr.Error()

	// INVALID_ACCESS_TOKEN and ITEM_LOGIN_REQUIRED are the only two permanent codes
	// domain.ClassifyPlaidError currently produces besides PRODUCTS_NOT_SUPPORTED (excluded
	// above), and both mean the SAME thing: only the user can fix this by re-authenticating. Route
	// both through the same handler the ITEM_LOGIN_REQUIRED webhook uses. Any other/future
	// permanent code falls back to the general connection-error handler rather than silently
	// notifying nothing.
	var err error
	switch plaidErr.ErrorCode {
	case "INVALID_ACCESS_TOKEN", "ITEM_LOGIN_REQUIRED":
		err = itemErrorSvc.HandleLoginRequired(ctx, linkedItem, plaidErr.ErrorCode, errorMessage)
	default:
		err = itemErrorSvc.HandleError(ctx, linkedItem, plaidErr.ErrorCode, errorMessage)
	}
	if err != nil {
		logger.Warn("failed to notify user about permanent plaid error",
			"item_id", linkedItem.ItemID,
			"error_code", plaidErr.ErrorCode,
			"error", err,
		)
	}
}
