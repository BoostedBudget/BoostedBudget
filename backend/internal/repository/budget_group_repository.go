package repository

import (
	"context"
	"fmt"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// LockBudgetConfiguration serializes hierarchy changes with budget creation for
// one user. The matching migration triggers use this same key for other writers.
// Acquire before reading the hierarchy or enumerating budgets, then retain until
// the validation and write commit together.
func LockBudgetConfiguration(ctx context.Context, q Querier, userID string) error {
	_, err := q.Exec(ctx, lockHierarchySQL, userID)
	return err
}

func (r *budgetRepository) prepareGroup(ctx context.Context, q Querier, c *domain.BudgetCategory) error {
	if c.CategoryKind != "group" && c.CategoryKind != "remainder" {
		return nil
	}
	if err := c.Validate(); err != nil {
		return err
	}
	for i := range c.Members {
		m := &c.Members[i]
		if m.CategoryKind != "custom" {
			continue
		}
		err := q.QueryRow(ctx, `SELECT cc.parent_id,cc.parent_pfc_primary,gp.parent_pfc_primary
   FROM custom_categories cc
   LEFT JOIN custom_categories gp ON gp.id=cc.parent_id AND gp.deleted_at IS NULL
   JOIN budgets b ON b.id=$2 AND b.user_id=cc.user_id AND b.deleted_at IS NULL
   WHERE cc.id=$1 AND cc.deleted_at IS NULL`, m.CustomCategoryID, c.BudgetID).Scan(&m.CustomParentID, &m.CustomParentPFCPrimary, &m.CustomGrandparentPFCPrimary)
		if err != nil {
			return fmt.Errorf("%w: group member must reference an owned live custom category", domain.ErrInvalidBudget)
		}
	}
	c.Members = domain.NormalizeBudgetGroupMembers(c.Members)
	return nil
}

func (r *budgetRepository) replaceGroupMembers(ctx context.Context, q Querier, c *domain.BudgetCategory) error {
	if _, err := q.Exec(ctx, `UPDATE budget_categories SET group_name=$2 WHERE id=$1 AND deleted_at IS NULL`, c.ID, c.GroupName); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `UPDATE budget_group_members SET deleted_at=now() WHERE budget_category_id=$1 AND deleted_at IS NULL`, c.ID); err != nil {
		return err
	}
	for _, m := range c.Members {
		if _, err := q.Exec(ctx, `INSERT INTO budget_group_members (budget_category_id,category_kind,pfc_primary,pfc_detailed,custom_category_id) VALUES ($1,$2,$3,$4,$5)`, c.ID, m.CategoryKind, m.PFCPrimary, m.PFCDetailed, m.CustomCategoryID); err != nil {
			return err
		}
	}
	return nil
}

// loadGroupMembers batches the supplemental fields without extra queries for
// budgets that contain no groups. Call after closing the category cursor: tx
// connections cannot run another query while their previous cursor is open.
func (r *budgetRepository) loadGroupMembers(ctx context.Context, q Querier, categories []domain.BudgetCategory) error {
	ids := []string{}
	byID := map[string]*domain.BudgetCategory{}
	for i := range categories {
		if categories[i].CategoryKind == "group" {
			ids = append(ids, categories[i].ID)
			byID[categories[i].ID] = &categories[i]
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := q.Query(ctx, `SELECT bc.id,bc.group_name,m.category_kind,m.pfc_primary,m.pfc_detailed,m.custom_category_id,
  cc.parent_id,cc.parent_pfc_primary,gp.parent_pfc_primary
  FROM budget_categories bc
  LEFT JOIN budget_group_members m ON m.budget_category_id=bc.id AND m.deleted_at IS NULL
  LEFT JOIN custom_categories cc ON cc.id=m.custom_category_id AND cc.deleted_at IS NULL
  LEFT JOIN custom_categories gp ON gp.id=cc.parent_id AND gp.deleted_at IS NULL
  WHERE bc.id=ANY($1) AND bc.deleted_at IS NULL ORDER BY bc.id,m.created_at,m.id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var name, kind *string
		var m domain.BudgetGroupMember
		if err := rows.Scan(&id, &name, &kind, &m.PFCPrimary, &m.PFCDetailed, &m.CustomCategoryID, &m.CustomParentID, &m.CustomParentPFCPrimary, &m.CustomGrandparentPFCPrimary); err != nil {
			return err
		}
		c := byID[id]
		c.GroupName = name
		if kind != nil {
			m.CategoryKind = *kind
			c.Members = append(c.Members, m)
		}
	}
	return rows.Err()
}
