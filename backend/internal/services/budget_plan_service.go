package services

import (
	"context"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

type BudgetPlanReader interface {
	GetPlanVsActual(context.Context, string, string, time.Time) (*domain.BudgetPlanVsActualResponse, error)
}

type BudgetPlanService interface {
	GetPlanVsActual(context.Context, string, string) (*domain.BudgetPlanVsActualResponse, error)
}

type budgetPlanService struct{ reader BudgetPlanReader }

func NewBudgetPlanService(reader BudgetPlanReader) BudgetPlanService {
	return &budgetPlanService{reader: reader}
}

func (s *budgetPlanService) GetPlanVsActual(ctx context.Context, userID, budgetID string) (*domain.BudgetPlanVsActualResponse, error) {
	return s.reader.GetPlanVsActual(ctx, userID, budgetID, time.Now().UTC())
}
