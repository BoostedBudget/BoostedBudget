package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
)

func TestContributionInsertPersistsFlowProvenance(t *testing.T) {
	for _, kind := range []string{"", "opening_balance"} {
		t.Run(kind, func(t *testing.T) {
			mock, repo := setupContributionMockDB(t)
			defer mock.Close()
			userID, goalID := uuid.NewString(), uuid.NewString()
			want := kind
			if want == "" {
				want = "contribution"
			}
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT progress_basis").WithArgs(goalID, userID).
				WillReturnRows(pgxmock.NewRows([]string{"progress_basis"}).AddRow("contributions"))
			mock.ExpectExec("INSERT INTO savings_goal_contributions .*flow_kind.*VALUES.*'"+want+"'").
				WithArgs(pgxmock.AnyArg(), goalID, (*string)(nil), 50.0, "manual", "confirmed", false, false, "2026-09-12", pgxmock.AnyArg(), pgxmock.AnyArg()).
				WillReturnResult(pgxmock.NewResult("INSERT", 1))
			mock.ExpectCommit()
			err := repo.Insert(context.Background(), userID, &domain.SavingsGoalContribution{
				SavingsGoalID: goalID, Amount: 50, Source: "manual", Status: "confirmed",
				OccurredOn: "2026-09-12", FlowKind: kind,
			})
			require.NoError(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
