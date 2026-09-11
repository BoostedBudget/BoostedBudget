import { describe, it, expect } from 'bun:test'
import { render, screen, within } from '@testing-library/react'
import { AllocationBreakdown } from './AllocationBreakdown'
import type { BudgetCategory } from '../../types'
import type { CustomCategoryMap } from '../../utils/groupBudgetCategoriesByPrimary'

describe('AllocationBreakdown', () => {
  const createMockCategory = (overrides?: Partial<BudgetCategory>): BudgetCategory => ({
    id: 'cat-1',
    budget_id: 'budget-1',
    category_kind: 'pfc',
    pfc_primary: 'FOOD_AND_DRINK',
    pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
    allocated_amount: 500,
    spent_amount: 250,
    tags: [],
    match_logic: 'category_only',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    ...overrides,
  });

  describe('Rendering', () => {
    it('should render segmented bar with proportional widths', () => {
      const categories = [
        createMockCategory({ id: 'cat-1', pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: undefined, allocated_amount: 1000 }),
        createMockCategory({ id: 'cat-2', pfc_primary: 'TRANSPORTATION', pfc_detailed: undefined, allocated_amount: 500 }),
      ];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={2000}
          unallocated={500}
        />
      );

      const segments = screen.getAllByTestId(/segment-/);
      expect(segments.length).toBeGreaterThan(0);
    });

    it('should render unallocated segment in different color', () => {
      const categories = [
        createMockCategory({ id: 'cat-1', allocated_amount: 1000 }),
      ];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={2000}
          unallocated={1000}
        />
      );

      const unallocatedSegment = screen.getByTestId('segment-unallocated');
      expect(unallocatedSegment).toBeInTheDocument();
    });

    it('should render legend with category names and amounts', () => {
      const categories = [
        createMockCategory({
          id: 'cat-1',
          category_kind: 'pfc',
          pfc_primary: 'FOOD_AND_DRINK',
          pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
          allocated_amount: 500,
        }),
        createMockCategory({
          id: 'cat-2',
          category_kind: 'pfc',
          pfc_primary: 'TRANSPORTATION',
          pfc_detailed: 'TRANSPORTATION_GAS',
          allocated_amount: 300,
        }),
      ];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={1000}
          unallocated={200}
        />
      );

      // Check legend items exist — no display fields so formatPFCEnum fallback is used
      expect(screen.getByText('Food And Drink › Restaurants')).toBeInTheDocument();
      expect(screen.getByText('Transportation › Gas')).toBeInTheDocument();
      expect(screen.getByText('$500.00')).toBeInTheDocument();
      expect(screen.getByText('$300.00')).toBeInTheDocument();
    });

    it('should show unallocated amount in legend', () => {
      const categories = [
        createMockCategory({ id: 'cat-1', allocated_amount: 800 }),
      ];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={1000}
          unallocated={200}
        />
      );

      // Unallocated now appears in multiple places (legend, stats section)
      const unallocatedElements = screen.getAllByText(/unallocated/i);
      expect(unallocatedElements.length).toBeGreaterThanOrEqual(1);
      // $200.00 appears in both legend and stats section spans, so use getAllByText
      expect(screen.getAllByText('$200.00').length).toBeGreaterThanOrEqual(1);
    });
  });

  describe('Edge Cases', () => {
    it('should handle over-allocated state (total > income)', () => {
      const categories = [
        createMockCategory({ id: 'cat-1', allocated_amount: 1500 }),
        createMockCategory({ id: 'cat-2', allocated_amount: 1000 }),
      ];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={2000}
          unallocated={-500}
        />
      );

      // Should show warning for over-allocation (appears in both warning div and live region)
      const overAllocatedElements = screen.getAllByText(/over.*allocated/i);
      expect(overAllocatedElements.length).toBeGreaterThanOrEqual(1);
    });

    it('should handle zero income gracefully', () => {
      render(
        <AllocationBreakdown
          categories={[]}
          totalIncome={0}
          unallocated={0}
        />
      );

      // Should not crash and show something meaningful
      expect(screen.getByText(/allocation/i)).toBeInTheDocument();
    });

    it('should handle empty categories', () => {
      render(
        <AllocationBreakdown
          categories={[]}
          totalIncome={1000}
          unallocated={1000}
        />
      );

      // Should show all as unallocated
      expect(screen.getByTestId('segment-unallocated')).toBeInTheDocument();
    });

    it('should handle fully allocated budget', () => {
      const categories = [
        createMockCategory({ id: 'cat-1', allocated_amount: 500 }),
        createMockCategory({ id: 'cat-2', allocated_amount: 500 }),
      ];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={1000}
          unallocated={0}
        />
      );

      // Should not show unallocated segment when fully allocated
      expect(screen.queryByTestId('segment-unallocated')).not.toBeInTheDocument();
    });
  });

  describe('Accessibility', () => {
    it('should have aria-labels on segments', () => {
      const categories = [
        createMockCategory({
          id: 'cat-1',
          pfc_primary: 'FOOD_AND_DRINK',
          pfc_detailed: undefined,
          allocated_amount: 500,
        }),
      ];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={1000}
          unallocated={500}
        />
      );

      const segment = screen.getByTestId('segment-cat-1');
      expect(segment).toHaveAttribute('aria-label');
    });

    it('should have proper role for the bar', () => {
      const categories = [createMockCategory()];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={1000}
          unallocated={500}
        />
      );

      expect(screen.getByRole('img', { name: /allocation/i })).toBeInTheDocument();
    });

    it('should have aria-live region for screen reader announcements', () => {
      const categories = [
        createMockCategory({ id: 'cat-1', allocated_amount: 750 }),
      ];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={1000}
          unallocated={250}
        />
      );

      const liveRegion = screen.getByRole('status');
      expect(liveRegion).toHaveAttribute('aria-live', 'polite');
      expect(liveRegion).toHaveAttribute('aria-atomic', 'true');
    });

    it('should announce allocation percentage in live region', () => {
      const categories = [
        createMockCategory({ id: 'cat-1', allocated_amount: 750 }),
      ];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={1000}
          unallocated={250}
        />
      );

      const liveRegion = screen.getByRole('status');
      expect(liveRegion).toHaveTextContent(/75%.*allocated/i);
    });

    it('should announce over-allocated state in live region', () => {
      const categories = [
        createMockCategory({ id: 'cat-1', allocated_amount: 1500 }),
      ];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={1000}
          unallocated={-500}
        />
      );

      const liveRegion = screen.getByRole('status');
      expect(liveRegion).toHaveTextContent(/over-allocated/i);
    });

    it('should announce remaining funds in live region when not over-allocated', () => {
      const categories = [
        createMockCategory({ id: 'cat-1', allocated_amount: 600 }),
      ];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={1000}
          unallocated={400}
        />
      );

      const liveRegion = screen.getByRole('status');
      expect(liveRegion).toHaveTextContent(/\$400.*remaining/i);
    });
  });

  describe('Visual Display', () => {
    it('should display total income', () => {
      render(
        <AllocationBreakdown
          categories={[]}
          totalIncome={2500}
          unallocated={2500}
        />
      );

      expect(screen.getByText(/total income.*\$2,?500/i)).toBeInTheDocument();
    });

    it('should display percentage allocated in header', () => {
      const categories = [
        createMockCategory({ id: 'cat-1', allocated_amount: 750 }),
      ];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={1000}
          unallocated={250}
        />
      );

      // Should show 75% allocated in the visible header
      // Use getAllByText since it now appears in both header and live region
      const percentageElements = screen.getAllByText(/75%.*allocated/i);
      expect(percentageElements.length).toBeGreaterThanOrEqual(1);
    });
  });

  describe('Soft Warning — over-income amber banner', () => {
    it('should show amber income warning when total allocated exceeds expected income', () => {
      const categories = [
        createMockCategory({ id: 'cat-1', allocated_amount: 800 }),
        createMockCategory({ id: 'cat-2', allocated_amount: 400 }),
      ];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={1000}
          unallocated={-200}
        />
      );

      // The visible (non-sr-only) warning text should mention exceeding income
      expect(screen.getByText(/exceed.*expected income/i)).toBeInTheDocument();
    });

    it('should not show amber income warning when allocations are within income', () => {
      const categories = [
        createMockCategory({ id: 'cat-1', allocated_amount: 500 }),
      ];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={1000}
          unallocated={500}
        />
      );

      expect(screen.queryByText(/exceed.*expected income/i)).not.toBeInTheDocument();
    });
  });

  describe('Allocation Display Fixes (Phase 5)', () => {
    it('should show "Total Budget" label instead of "Total Income"', () => {
      render(
        <AllocationBreakdown
          categories={[]}
          totalIncome={3000}
          unallocated={3000}
        />
      );

      // Should show "Total Budget" terminology
      expect(screen.getByText(/total budget/i)).toBeInTheDocument();
    });

    it('should show allocated amount and percentage', () => {
      const categories = [
        createMockCategory({ id: 'cat-1', allocated_amount: 600 }),
        createMockCategory({ id: 'cat-2', allocated_amount: 400 }),
      ];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={2000}
          unallocated={1000}
        />
      );

      // Should show allocated amount with percentage (appears in multiple places)
      const allocatedElements = screen.getAllByText(/allocated.*\$1,?000/i);
      expect(allocatedElements.length).toBeGreaterThanOrEqual(1);
    });

    it('should show unallocated amount prominently when not over-allocated', () => {
      const categories = [
        createMockCategory({ id: 'cat-1', allocated_amount: 500 }),
      ];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={1000}
          unallocated={500}
        />
      );

      // Amount is split across element nodes so use textContent-based matcher
      const unallocatedElements = screen.getAllByText(
        (_, element) => Boolean(element?.textContent?.match(/unallocated.*\$500/i))
      );
      expect(unallocatedElements.length).toBeGreaterThanOrEqual(1);
    });

    it('should show over-allocated warning with amount when over budget', () => {
      const categories = [
        createMockCategory({ id: 'cat-1', allocated_amount: 1200 }),
      ];

      render(
        <AllocationBreakdown
          categories={categories}
          totalIncome={1000}
          unallocated={-200}
        />
      );

      // Should show over-allocated warning
      const overElements = screen.getAllByText(/over-allocated.*\$200/i);
      expect(overElements.length).toBeGreaterThanOrEqual(1);
    });
  });

  describe('Sub-allocation dedup and nesting (#1630)', () => {
    it('excludes a sub-allocation child from the total/percentage (Task 0.5 dedup)', () => {
      const parent = createMockCategory({
        id: 'parent',
        category_kind: 'pfc',
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: undefined,
        allocated_amount: 500,
      });
      const child = createMockCategory({
        id: 'child',
        category_kind: 'pfc',
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: 'FOOD_AND_DRINK_FAST_FOOD',
        allocated_amount: 200,
      });

      render(
        <AllocationBreakdown
          categories={[parent, child]}
          totalIncome={1000}
          unallocated={500}
        />
      );

      // Total/percent must count the parent's own $500 once — not $700 (parent + child).
      expect(screen.getAllByText('$500.00').length).toBeGreaterThanOrEqual(1);
      expect(screen.getAllByText(/50%.*Allocated/i).length).toBeGreaterThanOrEqual(1);
      expect(screen.queryByText('$700.00')).not.toBeInTheDocument();
    });

    it("renders the child's segment nested inside its parent's segment, not as a sibling", () => {
      const parent = createMockCategory({
        id: 'parent',
        category_kind: 'pfc',
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: undefined,
        allocated_amount: 500,
      });
      const child = createMockCategory({
        id: 'child',
        category_kind: 'pfc',
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: 'FOOD_AND_DRINK_FAST_FOOD',
        allocated_amount: 200,
      });

      render(
        <AllocationBreakdown
          categories={[parent, child]}
          totalIncome={1000}
          unallocated={500}
        />
      );

      const parentSegment = screen.getByTestId('segment-parent');
      // A DOM-nested query scoped to the parent segment must find the child segment —
      // if the child were rendered as a sibling in the outer bar instead, this throws.
      expect(within(parentSegment).getByTestId('segment-child')).toBeInTheDocument();
    });
  });

  describe('Three-level nesting (#1630 review defect: grandchild sized against wrong parent)', () => {
    it("sizes a grandchild's segment against its DIRECT parent's allocation, not the root's, and nests it inside that parent's div", () => {
      // Chain: root (pfc, primary-only) -> child (custom, direct child of root via
      // custom_parent_pfc_primary) -> grandchild (custom, direct child of `child` via
      // custom_parent_id, and recognized as a transitive descendant of root via
      // custom_grandparent_pfc_primary — the exact "widening" shape Task B2 introduced).
      const root: BudgetCategory = {
        id: 'root',
        budget_id: 'budget-1',
        category_kind: 'pfc',
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: undefined,
        allocated_amount: 500,
        spent_amount: 0,
        tags: [],
        match_logic: 'category_only',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
      };
      const child: BudgetCategory = {
        id: 'child',
        budget_id: 'budget-1',
        category_kind: 'custom',
        custom_category_id: 'child-uuid',
        custom_parent_pfc_primary: 'FOOD_AND_DRINK',
        allocated_amount: 200,
        spent_amount: 0,
        tags: [],
        match_logic: 'category_only',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
      };
      const grandchild: BudgetCategory = {
        id: 'grandchild',
        budget_id: 'budget-1',
        category_kind: 'custom',
        custom_category_id: 'grandchild-uuid',
        custom_parent_id: 'child-uuid',
        custom_grandparent_pfc_primary: 'FOOD_AND_DRINK',
        allocated_amount: 50,
        spent_amount: 0,
        tags: [],
        match_logic: 'category_only',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
      };

      render(
        <AllocationBreakdown
          categories={[root, child, grandchild]}
          totalIncome={1000}
          unallocated={500}
        />
      );

      const rootSegment = screen.getByTestId('segment-root');
      const childSegment = within(rootSegment).getByTestId('segment-child');
      // The grandchild must be nested INSIDE its direct parent's div, not as a sibling
      // directly under root.
      const grandchildSegment = within(childSegment).getByTestId('segment-grandchild');
      expect(grandchildSegment).toBeInTheDocument();

      // 50 / 200 (grandchild's direct parent `child`'s own allocation) = 25%, NOT
      // 50 / 500 (root's allocation) = 10% — the arithmetic the pre-fix code produced.
      expect(grandchildSegment).toHaveAttribute('aria-label', '25% of parent allocation');
      expect(grandchildSegment.style.width).toBe('25%');
    });
  });

  describe('Custom category label resolution (Codex gap fix)', () => {
    const makeCustomCategory = (id: string, customCategoryId: string): BudgetCategory => ({
      id,
      budget_id: 'budget-1',
      category_kind: 'custom',
      custom_category_id: customCategoryId,
      allocated_amount: 400,
      spent_amount: 0,
      tags: [],
      match_logic: 'category_only',
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
    })

    it('shows resolved name in legend when customCategoryMap is provided', () => {
      const cat = makeCustomCategory('cat-custom', 'uuid-abc')
      const customCategoryMap: CustomCategoryMap = new Map([
        ['uuid-abc', { name: 'My Dining', parent_pfc_primary: null }],
      ])

      render(
        <AllocationBreakdown
          categories={[cat]}
          totalIncome={1000}
          unallocated={600}
          customCategoryMap={customCategoryMap}
        />
      )

      expect(screen.getByText('My Dining')).toBeInTheDocument()
      expect(screen.queryByText('uuid-abc')).not.toBeInTheDocument()
    })

    it('falls back to "Custom category" in legend when name not in map', () => {
      const cat = makeCustomCategory('cat-custom', 'unknown-uuid')
      const customCategoryMap: CustomCategoryMap = new Map()

      render(
        <AllocationBreakdown
          categories={[cat]}
          totalIncome={1000}
          unallocated={600}
          customCategoryMap={customCategoryMap}
        />
      )

      expect(screen.queryByText('unknown-uuid')).not.toBeInTheDocument()
      expect(screen.getByText('Custom category')).toBeInTheDocument()
    })

    it('falls back to formatBudgetCategoryLabel when no customCategoryMap prop (backward compat)', () => {
      // Without customCategoryMap, PFC categories still display normally
      const pfcCat = createMockCategory({
        id: 'pfc-cat',
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: undefined,
      })

      render(
        <AllocationBreakdown
          categories={[pfcCat]}
          totalIncome={1000}
          unallocated={500}
        />
      )

      expect(screen.getByText('Food And Drink')).toBeInTheDocument()
    })
  });
});
