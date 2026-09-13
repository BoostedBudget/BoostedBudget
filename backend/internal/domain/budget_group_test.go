package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBudgetCategoryGroupedValidation(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		valid      bool
	}{
		{"shared categories", `{"category_kind":"group","group_name":"Everyday","members":[{"category_kind":"pfc","pfc_primary":"FOOD_AND_DRINK"},{"category_kind":"uncategorized"}]}`, true},
		{"automatic remainder", `{"category_kind":"remainder"}`, true},
		{"missing name", `{"category_kind":"group","members":[{"category_kind":"uncategorized"}]}`, false},
		{"empty group", `{"category_kind":"group","group_name":"Empty"}`, false},
		{"remainder has members", `{"category_kind":"remainder","members":[{"category_kind":"uncategorized"}]}`, false},
		{"group cannot use tags", `{"category_kind":"group","group_name":"Named","members":[{"category_kind":"uncategorized"}],"tags":["x"]}`, false},
		{"member invalid", `{"category_kind":"group","group_name":"Named","members":[{"category_kind":"savings"}]}`, false},
		{"unknown PFC", `{"category_kind":"group","group_name":"Named","members":[{"category_kind":"pfc","pfc_primary":"BOGUS"}]}`, false},
		{"wrong detailed parent", `{"category_kind":"group","group_name":"Named","members":[{"category_kind":"pfc","pfc_primary":"FOOD_AND_DRINK","pfc_detailed":"INCOME_WAGES"}]}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var category BudgetCategory
			require.NoError(t, json.Unmarshal([]byte(tt.body), &category))
			category.BudgetID = "budget"
			if tt.valid {
				require.NoError(t, category.Validate())
			} else {
				require.Error(t, category.Validate())
			}
		})
	}
}

func TestBudgetGroupMemberCoverage(t *testing.T) {
	food, detail, custom, child := "FOOD_AND_DRINK", "FOOD_AND_DRINK_COFFEE", "parent", "child"
	root := BudgetGroupMember{CategoryKind: "pfc", PFCPrimary: &food}
	leaf := BudgetGroupMember{CategoryKind: "pfc", PFCPrimary: &food, PFCDetailed: &detail}
	parent := BudgetGroupMember{CategoryKind: "custom", CustomCategoryID: &custom, CustomParentPFCPrimary: &food}
	grandchild := BudgetGroupMember{CategoryKind: "custom", CustomCategoryID: &child, CustomParentID: &custom, CustomGrandparentPFCPrimary: &food}
	require.Len(t, NormalizeBudgetGroupMembers([]BudgetGroupMember{leaf, root, grandchild, parent, root}), 1)
	require.Len(t, NormalizeBudgetGroupMembers([]BudgetGroupMember{grandchild, parent}), 1)
	require.Error(t, ValidateBudgetGroupCoverage([]BudgetCategory{{CategoryKind: "group", Members: []BudgetGroupMember{root}}, {CategoryKind: "group", Members: []BudgetGroupMember{grandchild}}}))
	require.NoError(t, ValidateBudgetGroupCoverage([]BudgetCategory{{CategoryKind: "pfc", PFCPrimary: &food}, {CategoryKind: "group", Members: []BudgetGroupMember{root}}}))
	require.Error(t, ValidateBudgetGroupCoverage([]BudgetCategory{{CategoryKind: "remainder"}, {CategoryKind: "remainder"}}))
	require.NoError(t, ValidateBudgetGroupCoverage([]BudgetCategory{{CategoryKind: "group", Members: []BudgetGroupMember{leaf}}, {CategoryKind: "group", Members: []BudgetGroupMember{{CategoryKind: "uncategorized"}}}}))
}

func TestBudgetGroupEmptyRequiresExistingIdentity(t *testing.T) {
	name := "Retained"
	c := BudgetCategory{ID: "retained", BudgetID: "budget", CategoryKind: "group", GroupName: &name}
	require.NoError(t, c.Validate())
	require.ErrorIs(t, c.ValidateForCreate(), ErrInvalidBudget, "caller-supplied IDs must not bypass creation validation")
}
