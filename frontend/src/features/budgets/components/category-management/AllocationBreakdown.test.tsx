/**
 * AllocationBreakdown Tests
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * TDD RED Phase: Write failing tests first
 */

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { AllocationBreakdown } from './AllocationBreakdown';
import type { BudgetCategory } from '../../types';

describe('AllocationBreakdown', () => {
  const createMockCategory = (overrides?: Partial<BudgetCategory>): BudgetCategory => ({
    id: 'cat-1',
    budget_id: 'budget-1',
    category: ['Food and Drink', 'Restaurants'],
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
        createMockCategory({ id: 'cat-1', category: ['Food'], allocated_amount: 1000 }),
        createMockCategory({ id: 'cat-2', category: ['Transport'], allocated_amount: 500 }),
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
          category: ['Food and Drink', 'Restaurants'],
          allocated_amount: 500,
        }),
        createMockCategory({
          id: 'cat-2',
          category: ['Transportation', 'Gas'],
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

      // Check legend items exist
      expect(screen.getByText(/food and drink.*restaurants/i)).toBeInTheDocument();
      expect(screen.getByText(/transportation.*gas/i)).toBeInTheDocument();
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

      expect(screen.getByText(/unallocated/i)).toBeInTheDocument();
      expect(screen.getByText('$200.00')).toBeInTheDocument();
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

      // Should show warning for over-allocation
      expect(screen.getByText(/over.*allocated/i)).toBeInTheDocument();
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
          category: ['Food'],
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

    it('should display percentage allocated', () => {
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

      // Should show 75% allocated
      expect(screen.getByText(/75%/)).toBeInTheDocument();
    });
  });
});
