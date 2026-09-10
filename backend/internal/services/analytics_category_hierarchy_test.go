package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestBuildCategoryTree pins the shape the API returns for #1626.
//
// The repository emits one row per LEAF, carrying the whole ancestor path. Folding those into a
// tree is where two things can go wrong, and both are silent:
//
//   - Double counting. A parent's Total is a ROLL-UP of everything beneath it. Summing parent rows
//     and child rows together counts the same money twice.
//   - Losing a parent's own spend. A node can have both children AND transactions of its own — a
//     PFC primary with rows carrying no detailed value, a custom parent used directly. That money
//     has nowhere to live unless the node records it separately, and a renderer cannot make the
//     flows into and out of the node balance without it.
func TestBuildCategoryTree(t *testing.T) {
	const eatingOut = "custom:11111111-1111-1111-1111-111111111111"
	const coffee = "custom:22222222-2222-2222-2222-222222222222"

	rows := []domain.CategoryHierarchyRow{
		// FOOD_AND_DRINK has direct spend, a PFC child, a custom child, and a grandchild.
		{L1Key: "FOOD_AND_DRINK", Total: 40, TransactionCount: 1},
		{L1Key: "FOOD_AND_DRINK", L2Key: "FOOD_AND_DRINK_FAST_FOOD", Total: 60, TransactionCount: 2},
		{L1Key: "FOOD_AND_DRINK", L2Key: eatingOut, L2Display: "Eating Out", Total: 100, TransactionCount: 3},
		{L1Key: "FOOD_AND_DRINK", L2Key: eatingOut, L2Display: "Eating Out", L3Key: coffee, L3Display: "Coffee Runs", Total: 25, TransactionCount: 4},
		{L1Key: "TRANSPORTATION", Total: 500, TransactionCount: 5},
	}

	levels, grandTotal := buildCategoryTree(rows)

	assert.InDelta(t, 725.0, grandTotal, 0.001)
	require.Len(t, levels, 2)

	// Ordering is total DESC, so the bigger primary leads even though it is alphabetically later.
	assert.Equal(t, "TRANSPORTATION", levels[0].Category)
	assert.InDelta(t, 500.0, levels[0].Total, 0.001)
	assert.InDelta(t, 500.0, levels[0].DirectTotal, 0.001,
		"a leaf's whole total is direct — it has nothing beneath it")
	assert.Empty(t, levels[0].Children)

	fd := levels[1]
	assert.Equal(t, "FOOD_AND_DRINK", fd.Category)
	assert.Equal(t, "Food And Drink", fd.Display, "PFC keys resolve through the registry")
	assert.InDelta(t, 225.0, fd.Total, 0.001, "a parent's total rolls up everything beneath it")
	assert.InDelta(t, 40.0, fd.DirectTotal, 0.001,
		"the primary's own uncategorised-at-detail spend must survive as direct_total")
	assert.Equal(t, 10, fd.TransactionCount, "counts roll up alongside amounts")
	require.Len(t, fd.Children, 2)

	// Children ordered by total DESC: Eating Out (125) before Fast Food (60).
	eo := fd.Children[0]
	assert.Equal(t, eatingOut, eo.Category)
	assert.Equal(t, "Eating Out", eo.Display, "a custom key uses its stored name")
	assert.InDelta(t, 125.0, eo.Total, 0.001)
	assert.InDelta(t, 100.0, eo.DirectTotal, 0.001)
	require.Len(t, eo.Children, 1)
	assert.Equal(t, coffee, eo.Children[0].Category)
	assert.Equal(t, "Coffee Runs", eo.Children[0].Display)
	assert.InDelta(t, 25.0, eo.Children[0].Total, 0.001)
	assert.InDelta(t, 25.0, eo.Children[0].DirectTotal, 0.001)

	ff := fd.Children[1]
	assert.Equal(t, "FOOD_AND_DRINK_FAST_FOOD", ff.Category)
	assert.Equal(t, "Fast Food", ff.Display, "a PFC detailed resolves to its short name")
}

// TestBuildCategoryTree_ConservesTotalsAtEveryNode is the property the renderer depends on.
// Enumerating expected numbers proves the cases we thought of; this proves the invariant.
func TestBuildCategoryTree_ConservesTotalsAtEveryNode(t *testing.T) {
	rows := []domain.CategoryHierarchyRow{
		{L1Key: "A", Total: 7, TransactionCount: 1},
		{L1Key: "A", L2Key: "A_X", Total: 11, TransactionCount: 1},
		{L1Key: "A", L2Key: "custom:p", L2Display: "P", Total: 13, TransactionCount: 1},
		{L1Key: "A", L2Key: "custom:p", L2Display: "P", L3Key: "custom:c", L3Display: "C", Total: 17, TransactionCount: 1},
		{L1Key: "B", L2Key: "B_Y", Total: 19, TransactionCount: 1},
	}

	levels, grandTotal := buildCategoryTree(rows)

	var walk func(n domain.CategoryHierarchyNode) float64
	walk = func(n domain.CategoryHierarchyNode) float64 {
		var kids float64
		for _, c := range n.Children {
			kids += walk(c)
		}
		assert.InDelta(t, n.Total, n.DirectTotal+kids, 0.001,
			"node %q must satisfy total == direct + sum(children.total)", n.Category)
		assert.GreaterOrEqual(t, n.DirectTotal, 0.0,
			"node %q has a negative direct_total, which means a child was counted twice", n.Category)
		return n.Total
	}

	var top float64
	for _, l := range levels {
		top += walk(l)
	}
	assert.InDelta(t, grandTotal, top, 0.001, "the roots must sum to the grand total")
	assert.InDelta(t, 67.0, grandTotal, 0.001)
}

// TestBuildCategoryTree_TwoKeysSharingOneDisplayStaySeparateNodes is the REACHABLE reason nodes are
// keyed by the stable key, and it replaces a test that pinned an unreachable one (#1626 Phase 2,
// review finding X6).
//
// WHAT THE OLD TEST GOT WRONG. It supplied ONE key carrying TWO displays and attributed the hazard
// to the asynchronous `effective_category_name` refresh. That refresh is real, but this read does
// not touch that column: categoryPathExprs resolves display from `cc.name` / `p.name`, joined LIVE,
// so every row in a single result set sees the same current name and the mid-rename split cannot
// occur. upsert's own comment says exactly that. So the test asserted production SQL cannot emit
// the shape it fed in, while the hazard that IS reachable went untested.
//
// THE REACHABLE HAZARD. Display is a formatted string chosen by a CASE arm, and two DISTINCT
// categories are free to share one: an empty name, a neutral placeholder, or plainly the same name
// used under two different parents ("Coffee" under Food and "Coffee" under Gifts). Merging by
// display would fuse them into one node — halving the apparent count of categories and summing two
// unrelated pots of money into a single figure the user cannot decompose.
//
// The fixture uses two keys with an IDENTICAL display and UNEQUAL totals, so a merge is visible
// both as one node instead of two AND as a total neither category has.
func TestBuildCategoryTree_TwoKeysSharingOneDisplayStaySeparateNodes(t *testing.T) {
	rows := []domain.CategoryHierarchyRow{
		{L1Key: "custom:aaa", L1Display: "Coffee", Total: 23, TransactionCount: 2},
		{L1Key: "custom:bbb", L1Display: "Coffee", Total: 41, TransactionCount: 3},
	}

	levels, grandTotal := buildCategoryTree(rows)

	require.Len(t, levels, 2, "two distinct keys are two nodes, however identical their displays")
	byKey := map[string]domain.CategoryHierarchyNode{}
	for _, l := range levels {
		byKey[l.Category] = l
	}
	require.Contains(t, byKey, "custom:aaa")
	require.Contains(t, byKey, "custom:bbb")
	assert.InDelta(t, 23.0, byKey["custom:aaa"].Total, 0.001,
		"merging by display would report 64 here and drop the other node entirely")
	assert.InDelta(t, 41.0, byKey["custom:bbb"].Total, 0.001)
	assert.Equal(t, 2, byKey["custom:aaa"].TransactionCount)
	assert.Equal(t, 3, byKey["custom:bbb"].TransactionCount)
	assert.Equal(t, "Coffee", byKey["custom:aaa"].Display)
	assert.Equal(t, "Coffee", byKey["custom:bbb"].Display)
	assert.InDelta(t, 64.0, grandTotal, 0.001, "the grand total still covers both")
}

// TestTreeNodeUpsert_FirstNonEmptyDisplayWins is a HELPER-LEVEL invariant, deliberately NOT a
// production regression test (#1626 Phase 2, review finding X6).
//
// It is the surviving half of the test described above. One key arriving with two different
// displays is a shape the category-path SQL cannot emit — it reads live joined names — so asserting
// it over buildCategoryTree would claim coverage of a production path that does not exist. What
// upsert's tie-break IS good for is the reachable case of a display arriving EMPTY on one row and
// populated on another (a NULL `cc.name`, or a level a CASE arm left blank), where blanking an
// established name would put "Custom category" on a node that has a real one. Asserted directly on
// the helper, at the level where the rule actually lives.
func TestTreeNodeUpsert_FirstNonEmptyDisplayWins(t *testing.T) {
	t.Run("an empty display must not blank an established name", func(t *testing.T) {
		root := newTreeNode("", "")
		root.upsert("custom:same", "Therapy")
		child := root.upsert("custom:same", "")
		assert.Equal(t, "Therapy", child.display)
		assert.Len(t, root.order, 1, "one key is one child")
	})

	t.Run("a later non-empty display fills in for an earlier empty one", func(t *testing.T) {
		root := newTreeNode("", "")
		root.upsert("custom:same", "")
		child := root.upsert("custom:same", "Therapy")
		assert.Equal(t, "Therapy", child.display)
	})

	t.Run("two keys are two children even with one display", func(t *testing.T) {
		root := newTreeNode("", "")
		root.upsert("custom:a", "Coffee")
		root.upsert("custom:b", "Coffee")
		assert.Len(t, root.order, 2)
	})
}

// TestBuildCategoryTree_Empty — no rows must produce an empty tree, not a nil-deref or a phantom
// root.
func TestBuildCategoryTree_Empty(t *testing.T) {
	levels, grandTotal := buildCategoryTree(nil)
	assert.Empty(t, levels)
	assert.Zero(t, grandTotal)
}

// TestCategoryHierarchyCacheKey_SeparatesDirections is the collision guard, and it exercises the
// REAL wrapper rather than re-deriving the key beside it — a test that rebuilds the key itself
// would pass even if the wrapper forgot to encode direction at all, which is the whole failure
// being guarded against.
//
// Direction is a METHOD ARGUMENT, not a SpendingFilters field, so cache.EncodeSpendingFilters
// cannot see it. Without an explicit encode, income and expense share one key and serve each
// other's data — a user asking where their money went would be shown where it came from, with no
// error anywhere.
//
// It stays an argument on purpose: TestSpendingFiltersEncoder_CoversEveryStructField asserts by
// reflection that the encoder covers every field of SpendingFilters, so moving direction into that
// struct would change every existing analytics cache key.
func TestCategoryHierarchyCacheKey_SeparatesDirections(t *testing.T) {
	ctx := context.Background()
	filters := domain.SpendingFilters{
		StartDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	// The inner service reports which direction it was asked for, so a cache hit under the wrong
	// key is visible as the WRONG ANSWER rather than merely a missing call.
	var seen []domain.CategoryDirection
	inner := &stubHierarchyService{
		fn: func(direction domain.CategoryDirection) *domain.CategoryHierarchyResponse {
			seen = append(seen, direction)
			return &domain.CategoryHierarchyResponse{Direction: string(direction)}
		},
	}

	svc := NewCachedAnalyticsService(inner, newInMemoryTestCache())

	expense, err := svc.GetCategoryHierarchy(ctx, "user-123", filters, domain.CategoryDirectionExpense)
	require.NoError(t, err)
	income, err := svc.GetCategoryHierarchy(ctx, "user-123", filters, domain.CategoryDirectionIncome)
	require.NoError(t, err)

	assert.Equal(t, "expense", expense.Direction)
	assert.Equal(t, "income", income.Direction,
		"the income request must NOT be served the cached expense answer")
	assert.Equal(t,
		[]domain.CategoryDirection{domain.CategoryDirectionExpense, domain.CategoryDirectionIncome},
		seen, "both directions must reach the inner service under distinct keys")

	// And the same direction twice must still hit the cache, or the key is unstable.
	seen = nil
	_, err = svc.GetCategoryHierarchy(ctx, "user-123", filters, domain.CategoryDirectionExpense)
	require.NoError(t, err)
	assert.Empty(t, seen, "a repeat request for the same direction must be served from cache")
}

// stubHierarchyService implements AnalyticsService but only answers GetCategoryHierarchy; every
// other method panics, so a test that accidentally exercises one is loud rather than silently
// returning zero values.
type stubHierarchyService struct {
	AnalyticsService
	fn func(domain.CategoryDirection) *domain.CategoryHierarchyResponse
}

func (s *stubHierarchyService) GetCategoryHierarchy(_ context.Context, _ string, _ domain.SpendingFilters, direction domain.CategoryDirection) (*domain.CategoryHierarchyResponse, error) {
	return s.fn(direction), nil
}

// inMemoryTestCache is the smallest thing that behaves like a cache: a map keyed by the SAME
// composite key the production code derives, so a key-collision bug shows up as a wrong answer.
// It is deliberately not a mock — asserting "GetOrLoad was called" would prove nothing about
// whether two different requests derived the same key.
type inMemoryTestCache struct {
	entries map[string][]byte
	gens    *cache.Generations
}

func newInMemoryTestCache() *inMemoryTestCache {
	return &inMemoryTestCache{entries: map[string][]byte{}}
}

func (c *inMemoryTestCache) GetOrLoad(ctx context.Context, k cache.Key, loader func(context.Context) ([]byte, error)) ([]byte, bool, error) {
	dk := k.DataKey(1, "test-gen")
	if v, ok := c.entries[dk]; ok {
		return v, true, nil
	}
	v, err := loader(ctx)
	if err != nil {
		return nil, false, err
	}
	c.entries[dk] = v
	return v, false, nil
}

func (c *inMemoryTestCache) Generations() *cache.Generations { return c.gens }
func (c *inMemoryTestCache) Enabled(cache.Namespace) bool    { return true }
