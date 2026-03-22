package operations

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
)

// GetPaymentFailuresInput represents the query parameters for listing failed payment transactions.
type GetPaymentFailuresInput struct {
	inputs.AdminAuthenticatedInput
	Limit  int `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"Number of transactions to return"`
	Offset int `query:"offset" default:"0" minimum:"0" doc:"Number of transactions to skip"`
}

// GetPaymentFailuresOutput represents the response for listing failed payment transactions.
type GetPaymentFailuresOutput struct {
	Body struct {
		Transactions []domain.PaddleTransaction `json:"transactions"`
		Total        int                        `json:"total"`
		Limit        int                        `json:"limit"`
		Offset       int                        `json:"offset"`
	}
}

// GetPaymentFailures returns failed/past_due/canceled transactions for admin review.
//
// GET /api/v1/admin/payments/failures
func (h *AdminHandler) GetPaymentFailures(ctx context.Context, input *GetPaymentFailuresInput) (*GetPaymentFailuresOutput, error) {
	if h.paddleTransactionRepo == nil {
		return nil, huma.Error503ServiceUnavailable("payment transaction repository not available")
	}

	txns, total, err := h.paddleTransactionRepo.GetFailedTransactions(ctx, input.Limit, input.Offset)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get payment failures: %w", err))
	}

	// Ensure non-nil slice for JSON response
	if txns == nil {
		txns = []domain.PaddleTransaction{}
	}

	return &GetPaymentFailuresOutput{
		Body: struct {
			Transactions []domain.PaddleTransaction `json:"transactions"`
			Total        int                        `json:"total"`
			Limit        int                        `json:"limit"`
			Offset       int                        `json:"offset"`
		}{
			Transactions: txns,
			Total:        total,
			Limit:        input.Limit,
			Offset:       input.Offset,
		},
	}, nil
}

// RegisterAdminPaymentFailuresOperations registers the payment failures admin endpoint.
func RegisterAdminPaymentFailuresOperations(api huma.API, handler *AdminHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "getAdminPaymentFailures",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/payments/failures",
		Summary:     "List failed payment transactions (admin)",
		Description: "Returns failed, past_due, and canceled Paddle transactions for admin review. Requires admin role.",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
		Middlewares: huma.Middlewares{
			humamiddleware.NewAdminMiddleware(handler.config, handler.adminService),
		},
	}, handler.GetPaymentFailures)
}
