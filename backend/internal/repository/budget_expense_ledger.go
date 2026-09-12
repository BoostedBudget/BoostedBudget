package repository

import (
	"context"
	"fmt"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"sort"
	"time"
)

// BudgetEffectiveTransferExpr requires transactions t plus historical custom
// category cc and parent gp joins. Both custom ancestry levels classify transfers.
const BudgetEffectiveTransferExpr = "COALESCE(t.effective_pfc_primary,cc.parent_pfc_primary,gp.parent_pfc_primary,'')"

func budgetScalarCurrency(currencies []string, expected string) (string, error) {
	for _, currency := range currencies {
		if currency == "" {
			return "", fmt.Errorf("budget actuals incomplete: unknown_currency")
		}
		if expected == "" {
			expected = currency
		}
		if currency != expected {
			return "", fmt.Errorf("budget actuals incomplete: mixed_currency")
		}
	}
	return expected, nil
}

func budgetExpensePreview(categories []domain.BudgetCategory, category domain.BudgetCategory, facts []domain.BudgetExpenseFact) (*domain.CategoryPreviewResponse, error) {
	ledger := domain.CalculateBudgetExpenseLedger(categories, facts)
	line := ledger.Lines[category.ID]
	currency, err := budgetScalarCurrency(line.CurrencyCodes, "")
	if err != nil {
		return nil, err
	}
	if line.Amount == nil {
		return nil, fmt.Errorf("budget actuals incomplete: %s", line.Reason)
	}
	resp := &domain.CategoryPreviewResponse{CurrencyCode: currency, TransactionCount: len(line.TransactionIDs), TotalAmount: *line.Amount}
	selected := map[string]bool{}
	for _, id := range line.TransactionIDs {
		selected[id] = true
	}
	categoryOnly := category
	categoryOnly.MatchLogic = domain.MatchLogicCategoryOnly
	categoryOnly.Tags = nil
	inCategory := domain.CalculateBudgetExpenseLedger([]domain.BudgetCategory{categoryOnly}, facts).Lines[category.ID]
	categoryIDs := map[string]bool{}
	for _, id := range inCategory.TransactionIDs {
		categoryIDs[id] = true
	}
	children := map[string]domain.CategoryPreviewBreakdownItem{}
	for _, f := range facts {
		if !selected[f.ID] {
			continue
		}
		amount := f.Amount - f.SavingsAmount
		if !categoryIDs[f.ID] {
			resp.TagOnlyTransactionCount++
			resp.TagOnlyAmount += amount
			continue
		}
		nested := f.CustomCategoryID != "" && (category.CategoryKind == "pfc" || (category.CategoryKind == "custom" && category.CustomCategoryID != nil && *category.CustomCategoryID != f.CustomCategoryID))
		if nested {
			child := children[f.CustomCategoryID]
			child.CategoryID = f.CustomCategoryID
			child.CategoryName = f.CustomCategoryName
			child.TransactionCount++
			child.TotalAmount += amount
			children[f.CustomCategoryID] = child
		} else {
			resp.DirectTransactionCount++
			resp.DirectAmount += amount
		}
	}
	for _, child := range children {
		resp.Children = append(resp.Children, child)
	}
	sort.Slice(resp.Children, func(i, j int) bool {
		if resp.Children[i].CategoryName == resp.Children[j].CategoryName {
			return resp.Children[i].CategoryID < resp.Children[j].CategoryID
		}
		return resp.Children[i].CategoryName < resp.Children[j].CategoryName
	})
	return resp, nil
}

// PreviewBudgetCategory evaluates the draft with its complete sibling context.
// BudgetID is owner-scoped; caller-supplied ancestry is never trusted.
func (r *transactionRepository) PreviewBudgetCategory(ctx context.Context, userID string, req *domain.CategoryPreviewRequest) (*domain.CategoryPreviewResponse, error) {
	categories := append([]domain.BudgetCategory(nil), req.DraftCategories...)
	var expectedCurrency string
	if req.BudgetID != "" {
		var owner string
		if err := r.db.QueryRow(ctx, `SELECT user_id,currency_code FROM budgets WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL`, req.BudgetID, userID).Scan(&owner, &expectedCurrency); err != nil {
			return nil, err
		}
		var err error
		categories, err = (&budgetRepository{db: r.db}).getCategoriesByBudgetIDQuerier(ctx, r.db, req.BudgetID)
		if err != nil {
			return nil, err
		}
	}
	candidate := domain.BudgetCategory{ID: "preview", BudgetID: req.BudgetID, CategoryKind: req.CategoryKind, GroupName: req.GroupName, Members: req.Members, PFCPrimary: req.PFCPrimary, PFCDetailed: req.PFCDetailed, CustomCategoryID: req.CustomCategoryID, Tags: req.Tags, MatchLogic: req.MatchLogic}
	if candidate.CategoryKind == "group" || candidate.CategoryKind == "remainder" {
		candidate.BudgetID = "preview"
		if err := candidate.ValidateForCreate(); err != nil {
			return nil, err
		}
		candidate.BudgetID = req.BudgetID
	}
	next := []domain.BudgetCategory{}
	found := req.ReplaceCategoryID == ""
	for _, c := range categories {
		if req.ReplaceCategoryID != "" && c.ID == req.ReplaceCategoryID {
			found = true
			continue
		}
		// Draft IDs are client supplied. Use distinct internal keys so none can
		// alias the candidate or another sibling in the ledger's result map.
		c.ID = fmt.Sprintf("sibling-%d", len(next))
		next = append(next, c)
	}
	if !found {
		return nil, domain.ErrBudgetCategoryNotFound
	}
	categories = append(next, candidate)
	for i := range categories {
		c := &categories[i]
		for j := range c.Members {
			m := &c.Members[j]
			if err := m.Validate(); err != nil {
				return nil, err
			}
			m.CustomParentID = nil
			m.CustomParentPFCPrimary = nil
			m.CustomGrandparentPFCPrimary = nil
			if m.CategoryKind == "custom" {
				if err := r.db.QueryRow(ctx, `SELECT cc.parent_id,cc.parent_pfc_primary,gp.parent_pfc_primary FROM custom_categories cc LEFT JOIN custom_categories gp ON gp.id=cc.parent_id AND gp.user_id=cc.user_id AND gp.deleted_at IS NULL WHERE cc.id=$1 AND cc.user_id=$2 AND cc.deleted_at IS NULL`, m.CustomCategoryID, userID).Scan(&m.CustomParentID, &m.CustomParentPFCPrimary, &m.CustomGrandparentPFCPrimary); err != nil {
					return nil, domain.ErrInvalidBudget
				}
			}
		}
		c.Members = domain.NormalizeBudgetGroupMembers(c.Members)
	}
	if err := domain.ValidateBudgetGroupCoverage(categories); err != nil {
		return nil, err
	}
	facts, err := readBudgetExpenseFacts(ctx, r.db, userID, req.StartDate.Time(), req.EndDate.EndOfDay())
	if err != nil {
		return nil, err
	}
	resp, err := budgetExpensePreview(categories, candidate, facts)
	if err != nil {
		return nil, err
	}
	if resp.CurrencyCode != "" {
		if _, err := budgetScalarCurrency([]string{resp.CurrencyCode}, expectedCurrency); err != nil {
			return nil, err
		}
	} else {
		resp.CurrencyCode = expectedCurrency
	}
	return resp, nil
}

func (r *transactionRepository) budgetCategoryLedger(ctx context.Context, userID string, category *domain.BudgetCategory, start, end time.Time) (domain.BudgetExpenseLedger, error) {
	if category.MatchLogic != "" && !category.MatchLogic.IsValid() {
		return domain.BudgetExpenseLedger{}, fmt.Errorf("unsupported match logic: %s", category.MatchLogic)
	}
	categories := []domain.BudgetCategory{*category}
	var expectedCurrency string
	if category.BudgetID != "" {
		var owner string
		if err := r.db.QueryRow(ctx, `SELECT user_id,currency_code FROM budgets WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL`, category.BudgetID, userID).Scan(&owner, &expectedCurrency); err != nil {
			return domain.BudgetExpenseLedger{}, err
		}
		var err error
		categories, err = (&budgetRepository{db: r.db}).getCategoriesByBudgetIDQuerier(ctx, r.db, category.BudgetID)
		if err != nil {
			return domain.BudgetExpenseLedger{}, err
		}
		found := false
		for i := range categories {
			if categories[i].ID == category.ID {
				found = true
			}
		}
		if !found && category.CategoryKind != "unbudgeted" {
			return domain.BudgetExpenseLedger{}, domain.ErrBudgetCategoryNotFound
		}
	}
	ledger, err := ReadBudgetExpenseLedger(ctx, r.db, userID, categories, start, end)
	if err == nil && category.CategoryKind == "unbudgeted" {
		ledger.Lines[category.ID] = domain.BudgetExpenseLineActual{Amount: ledger.UnbudgetedAmount, TransactionIDs: ledger.UnbudgetedTransactionIDs, Reason: ledger.Reason, CurrencyCodes: ledger.UnbudgetedCurrencyCodes}
	}
	if err == nil {
		_, err = budgetScalarCurrency(ledger.Lines[category.ID].CurrencyCodes, expectedCurrency)
	}
	return ledger, err
}

// ReadBudgetExpenseLedger reads one complete expense universe in the caller's
// snapshot. Contributions are considered across all goals and dates: archiving a
// generation releases uniqueness, so a date-local join could count the same bank
// transaction as both spending and multiple generations of savings.
func ReadBudgetExpenseLedger(ctx context.Context, q Querier, userID string, categories []domain.BudgetCategory, start, end time.Time) (domain.BudgetExpenseLedger, error) {
	facts, err := readBudgetExpenseFacts(ctx, q, userID, start, end)
	if err != nil {
		return domain.BudgetExpenseLedger{}, err
	}
	return domain.CalculateBudgetExpenseLedger(categories, facts), nil
}

const budgetExpenseFactsQuery = `WITH contribution_weights AS (
 SELECT c.transaction_id,
 COALESCE(SUM(c.amount) FILTER (WHERE c.flow_kind='contribution'),0) AS savings_amount,
 ARRAY_AGG(DISTINCT g.currency_code) AS currencies,
 CASE WHEN BOOL_OR(c.flow_kind='legacy_unknown') THEN 'legacy_savings_origin'
 WHEN BOOL_OR(c.archived_at IS NOT NULL) AND COUNT(*)>1 THEN 'ambiguous_savings_generation'
 ELSE '' END AS reason
 FROM savings_goal_contributions c JOIN savings_goals g ON g.id=c.savings_goal_id
 WHERE g.user_id=$1 AND g.deleted_at IS NULL AND c.deleted_at IS NULL
 AND c.status='confirmed' AND c.transaction_id IS NOT NULL AND c.flow_kind<>'opening_balance'
 GROUP BY c.transaction_id
 )
 SELECT t.id,t.amount,COALESCE(t.effective_pfc_primary,''),COALESCE(t.effective_pfc_detailed,''),
 COALESCE(t.custom_category_override_id::text,''),COALESCE(cc.parent_id::text,''),
 COALESCE(cc.parent_pfc_primary,''),COALESCE(gp.parent_pfc_primary,''),
 COALESCE(a.currency_code,''),COALESCE(t.tags,'[]'::jsonb),COALESCE(w.savings_amount,0),
 CASE WHEN COALESCE(w.reason,'')<>'' THEN w.reason
 WHEN EXISTS (SELECT 1 FROM unnest(w.currencies) currency WHERE currency IS NULL OR currency='') THEN 'unknown_currency'
 WHEN EXISTS (SELECT 1 FROM unnest(w.currencies) currency WHERE currency<>a.currency_code) THEN 'mixed_currency'
 ELSE '' END,COALESCE(cc.name,'')
 FROM transactions t
 JOIN accounts a ON a.id=t.account_id AND a.user_id=t.user_id
 LEFT JOIN custom_categories cc ON cc.id=t.custom_category_override_id AND cc.user_id=t.user_id
 LEFT JOIN custom_categories gp ON gp.id=cc.parent_id AND gp.user_id=t.user_id
 -- tombstone-exempt(custom_categories cc, custom_categories gp): HISTORY FILTER. Retained ancestry classifies historical spending and transfers even after category retirement.
 LEFT JOIN contribution_weights w ON w.transaction_id=t.id
 WHERE t.user_id=$1 AND t.deleted_at IS NULL AND t.amount>0 AND t.pending=false
 AND t.date BETWEEN $2 AND $3
 AND ` + BudgetEffectiveTransferExpr + ` NOT LIKE 'TRANSFER%'
 ORDER BY t.date DESC,t.created_at DESC,t.id`

func readBudgetExpenseFacts(ctx context.Context, q Querier, userID string, start, end time.Time) ([]domain.BudgetExpenseFact, error) {
	rows, err := q.Query(ctx, budgetExpenseFactsQuery, userID, start, end)
	if err != nil {
		return nil, fmt.Errorf("read budget expense facts: %w", err)
	}
	defer rows.Close()
	facts := []domain.BudgetExpenseFact{}
	for rows.Next() {
		var f domain.BudgetExpenseFact
		if err := rows.Scan(&f.ID, &f.Amount, &f.PFCPrimary, &f.PFCDetailed, &f.CustomCategoryID, &f.CustomParentID, &f.CustomPFCPrimary, &f.CustomGrandparentPFCPrimary, &f.CurrencyCode, &f.Tags, &f.SavingsAmount, &f.Reason, &f.CustomCategoryName); err != nil {
			return nil, err
		}
		facts = append(facts, f)
	}
	return facts, rows.Err()
}
