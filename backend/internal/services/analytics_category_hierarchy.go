package services

import (
	"context"
	"fmt"
	"sort"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ─── Folding the category path into a tree (#1626) ───────────────────────────────────────────────
//
// The repository emits one row per LEAF, carrying its whole ancestor path. Folding those into a
// tree has two silent failure modes, and the output shape is chosen to make both testable:
//
//   - DOUBLE COUNTING. A node's Total is a ROLL-UP of everything beneath it, so parent rows and
//     child rows must never be summed together.
//   - A PARENT'S OWN SPEND. A node can have children AND transactions of its own: a PFC primary
//     with rows carrying no detailed value, or a custom parent used directly. DirectTotal keeps
//     that money addressable. Without it a renderer cannot make the flows into and out of the node
//     balance, and the difference silently disappears.
//
// The invariant every node satisfies, which the tests assert by walking the tree:
//
//	Total == DirectTotal + sum(child.Total)

// treeNode is the mutable accumulator behind a domain.CategoryHierarchyNode.
type treeNode struct {
	key         string
	display     string // stored custom name, or "" for a PFC level
	total       float64
	directTotal float64
	count       int
	children    map[string]*treeNode
	order       []string // first-seen order, so output is deterministic before sorting
}

func newTreeNode(key, display string) *treeNode {
	return &treeNode{key: key, display: display, children: map[string]*treeNode{}}
}

// upsert returns the child for key, creating it on first sight. Nodes are keyed by the STABLE key,
// never by display: effective_category_name is refreshed asynchronously after a rename, so two rows
// for the same category can briefly disagree on its name, and merging by display would split one
// category into two nodes.
func (n *treeNode) upsert(key, display string) *treeNode {
	child, ok := n.children[key]
	if !ok {
		child = newTreeNode(key, display)
		n.children[key] = child
		n.order = append(n.order, key)
		return child
	}
	// First non-empty display wins; a later empty one must not blank an established name.
	if child.display == "" && display != "" {
		child.display = display
	}
	return child
}

// buildCategoryTree folds path rows into the nested tree the API returns, and reports the grand
// total. Each row's amount is added to every ancestor's roll-up, and to its own leaf's direct
// total, exactly once.
func buildCategoryTree(rows []domain.CategoryHierarchyRow) ([]domain.CategoryHierarchyNode, float64) {
	root := newTreeNode("", "")
	var grandTotal float64

	for _, r := range rows {
		if r.L1Key == "" {
			continue // a row with no path at all describes nothing
		}
		grandTotal += r.Total

		node := root.upsert(r.L1Key, r.L1Display)
		node.total += r.Total
		node.count += r.TransactionCount

		if r.L2Key != "" {
			node = node.upsert(r.L2Key, r.L2Display)
			node.total += r.Total
			node.count += r.TransactionCount
		}
		if r.L3Key != "" {
			node = node.upsert(r.L3Key, r.L3Display)
			node.total += r.Total
			node.count += r.TransactionCount
		}
		// node is now the row's LEAF — the only node whose direct total this row contributes to.
		node.directTotal += r.Total
	}

	return materialize(root, grandTotal), grandTotal
}

// materialize converts the accumulator tree into the response shape, resolving display names and
// sorting siblings.
func materialize(n *treeNode, grandTotal float64) []domain.CategoryHierarchyNode {
	if len(n.order) == 0 {
		return nil
	}
	out := make([]domain.CategoryHierarchyNode, 0, len(n.order))
	for _, key := range n.order {
		c := n.children[key]
		out = append(out, domain.CategoryHierarchyNode{
			Category:         c.key,
			Display:          resolveCategoryDisplay(c.key, c.display),
			Total:            c.total,
			DirectTotal:      c.directTotal,
			TransactionCount: c.count,
			Percentage:       calculatePercentage(c.total, grandTotal),
			Children:         materialize(c, grandTotal),
		})
	}
	sortCategoryHierarchyNodes(out)
	return out
}

// sortCategoryHierarchyNodes orders siblings through the same comparator every category-shaped
// response uses: total DESC, resolved display ASC, stable key ASC.
func sortCategoryHierarchyNodes(nodes []domain.CategoryHierarchyNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		return lessCategoryDisplay(
			categoryDisplayRow{Key: a.Category, Display: a.Display, Total: a.Total},
			categoryDisplayRow{Key: b.Category, Display: b.Display, Total: b.Total},
		)
	})
}

// GetCategoryHierarchy returns the category tree for one flow direction.
func (s *analyticsService) GetCategoryHierarchy(ctx context.Context, userID string, filters domain.SpendingFilters, direction domain.CategoryDirection) (*domain.CategoryHierarchyResponse, error) {
	if !direction.IsValid() {
		direction = domain.CategoryDirectionExpense
	}

	rows, err := s.analyticsRepo.GetCategoryHierarchy(ctx, userID, filters, direction)
	if err != nil {
		return nil, fmt.Errorf("failed to get category hierarchy: %w", err)
	}

	levels, total := buildCategoryTree(rows)

	return &domain.CategoryHierarchyResponse{
		Levels:    levels,
		Total:     total,
		Direction: string(direction),
		StartDate: filters.StartDate.Format("2006-01-02"),
		EndDate:   filters.EndDate.Format("2006-01-02"),
	}, nil
}
