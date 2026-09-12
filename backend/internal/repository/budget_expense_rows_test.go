package repository

import (
	"fmt"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"time"
)

func expectBudgetExpenseDrilldown(mock pgxmock.PgxPoolIface, c *domain.BudgetCategory, user string, start, end time.Time, total float64, count int) *pgxmock.Rows {
	mock.ExpectBegin()
	mock.ExpectExec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY").WillReturnResult(pgxmock.NewResult("SET", 0))
	facts := budgetExpenseTestRows(c, total, count)
	mock.ExpectQuery(`WITH contribution_weights`).WithArgs(user, start, end).WillReturnRows(facts)
	rows := pgxmock.NewRows(transactionSelectColumns)
	mock.ExpectQuery(`SELECT (.+) FROM transactions WHERE user_id=\$1 AND id=ANY`).WithArgs(user, pgxmock.AnyArg()).WillReturnRows(rows)
	mock.ExpectCommit()
	return rows
}

func budgetExpenseTestRows(c *domain.BudgetCategory, total float64, count int) *pgxmock.Rows {
	rows := pgxmock.NewRows([]string{"id", "amount", "primary", "detailed", "custom", "parent", "custom_pfc", "grandparent_pfc", "currency", "tags", "savings", "reason", "name"})
	if count == 0 || total == 0 {
		return rows
	}
	value := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	for i := 0; i < count; i++ {
		rows.AddRow(fmt.Sprintf("tx-%d", i+1), total/float64(count), value(c.PFCPrimary), value(c.PFCDetailed), value(c.CustomCategoryID), "", "", "", "USD", c.Tags, 0.0, "", "")
	}
	return rows
}
