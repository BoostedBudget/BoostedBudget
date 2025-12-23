/**
 * BudgetOverviewWidget Tests
 *
 * TDD RED: Tests for the budget overview dashboard widget.
 * Issue #564: Migrated to mock useCurrentBudget hook instead of API
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { BudgetOverviewWidget } from './BudgetOverviewWidget';
import type { Budget, BudgetWithMetrics } from '../../budgets/types';

// Mock the useCurrentBudget hook
vi.mock('../../budgets/hooks/useCurrentBudget');

// Import the mocked hook
import { useCurrentBudget } from '../../budgets/hooks/useCurrentBudget';

describe('BudgetOverviewWidget', () => {
  const mockBudget: Budget = {
    id: 'budget-1',
    user_id: 'user-123',
    name: 'Monthly Budget',
    description: 'My monthly budget',
    period: 'monthly',
    start_date: '2025-01-01T00:00:00Z',
    end_date: '2025-01-31T23:59:59Z',
    amount: 2000,
    currency_code: 'USD',
    is_active: true,
    categories: [
      {
        id: 'cat-1',
        budget_id: 'budget-1',
        category: ['Food and Drink'],
        allocated_amount: 500,
        spent_amount: 250,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
        // Issue #495: Envelope budgeting fields
        tags: [],
        match_logic: 'category_only',
      },
      {
        id: 'cat-2',
        budget_id: 'budget-1',
        category: ['Transportation'],
        allocated_amount: 300,
        spent_amount: 100,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
        // Issue #495: Envelope budgeting fields
        tags: [],
        match_logic: 'category_only',
      },
    ],
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    // Issue #495: Envelope budgeting fields
    income_amount: 0,
    income_sources: [],
  };

  // Computed metrics for the mock budget (extends Budget with metrics)
  const mockMetrics: BudgetWithMetrics = {
    ...mockBudget,
    total_allocated: 800, // 500 + 300
    total_spent: 350, // 250 + 100
    remaining_amount: 1650, // 2000 - 350
    percent_used: 18, // (350 / 2000) * 100 = 17.5% -> 18%
    is_over_budget: false,
    unallocated_funds: 1200, // 2000 - 800
    is_fully_allocated: false,
    categories: mockBudget.categories!.map((cat) => ({
      ...cat,
      percent_used: cat.spent_amount / cat.allocated_amount * 100,
      remaining_amount: cat.allocated_amount - cat.spent_amount,
    })),
  };

  const defaultProps = {
    widgetId: 'widget-1',
    onRemove: vi.fn(),
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('Loading State', () => {
    it('should show loading spinner initially', () => {
      vi.mocked(useCurrentBudget).mockReturnValue({
        budget: null,
        metrics: null,
        loading: true,
        error: null,
        hasNoBudget: false,
        refetch: vi.fn(),
      });

      render(<BudgetOverviewWidget {...defaultProps} />);

      // Check for loading indicator
      const loading = document.querySelector('.animate-spin');
      expect(loading).toBeInTheDocument();
    });
  });

  describe('Success State', () => {
    it('should render budget name', async () => {
      vi.mocked(useCurrentBudget).mockReturnValue({
        budget: mockBudget,
        metrics: mockMetrics,
        loading: false,
        error: null,
        hasNoBudget: false,
        refetch: vi.fn(),
      });

      render(<BudgetOverviewWidget {...defaultProps} />);

      await waitFor(() => {
        expect(screen.getByText('Monthly Budget')).toBeInTheDocument();
      });
    });

    it('should render budget amount', async () => {
      vi.mocked(useCurrentBudget).mockReturnValue({
        budget: mockBudget,
        metrics: mockMetrics,
        loading: false,
        error: null,
        hasNoBudget: false,
        refetch: vi.fn(),
      });

      render(<BudgetOverviewWidget {...defaultProps} />);

      await waitFor(() => {
        expect(screen.getByText('$2,000.00')).toBeInTheDocument();
      });
    });

    it('should render spent amount', async () => {
      vi.mocked(useCurrentBudget).mockReturnValue({
        budget: mockBudget,
        metrics: mockMetrics,
        loading: false,
        error: null,
        hasNoBudget: false,
        refetch: vi.fn(),
      });

      render(<BudgetOverviewWidget {...defaultProps} />);

      await waitFor(() => {
        // Total spent: 250 + 100 = 350
        expect(screen.getByText('$350.00')).toBeInTheDocument();
      });
    });

    it('should render progress bar', async () => {
      vi.mocked(useCurrentBudget).mockReturnValue({
        budget: mockBudget,
        metrics: mockMetrics,
        loading: false,
        error: null,
        hasNoBudget: false,
        refetch: vi.fn(),
      });

      render(<BudgetOverviewWidget {...defaultProps} />);

      await waitFor(() => {
        expect(screen.getByRole('progressbar')).toBeInTheDocument();
      });
    });

    it('should show percentage used', async () => {
      vi.mocked(useCurrentBudget).mockReturnValue({
        budget: mockBudget,
        metrics: mockMetrics,
        loading: false,
        error: null,
        hasNoBudget: false,
        refetch: vi.fn(),
      });

      render(<BudgetOverviewWidget {...defaultProps} />);

      await waitFor(() => {
        // 350 / 2000 * 100 = 17.5% -> 18%
        expect(screen.getByText('18%')).toBeInTheDocument();
      });
    });
  });

  describe('No Budget State', () => {
    it('should show "no budget" message when hasNoBudget is true', async () => {
      vi.mocked(useCurrentBudget).mockReturnValue({
        budget: null,
        metrics: null,
        loading: false,
        error: null,
        hasNoBudget: true,
        refetch: vi.fn(),
      });

      render(<BudgetOverviewWidget {...defaultProps} />);

      await waitFor(() => {
        expect(screen.getByText(/no active budget/i)).toBeInTheDocument();
      });
    });

    it('should show create budget prompt when no budget', async () => {
      vi.mocked(useCurrentBudget).mockReturnValue({
        budget: null,
        metrics: null,
        loading: false,
        error: null,
        hasNoBudget: true,
        refetch: vi.fn(),
      });

      render(<BudgetOverviewWidget {...defaultProps} />);

      await waitFor(() => {
        // Check for the Create Budget button specifically
        const createBtn = screen.getByRole('link', { name: /create budget/i });
        expect(createBtn).toBeInTheDocument();
        expect(createBtn).toHaveAttribute('href', '/budgets');
      });
    });
  });

  describe('Error State', () => {
    it('should show error message on failure', async () => {
      vi.mocked(useCurrentBudget).mockReturnValue({
        budget: null,
        metrics: null,
        loading: false,
        error: new Error('Network error'),
        hasNoBudget: false,
        refetch: vi.fn(),
      });

      render(<BudgetOverviewWidget {...defaultProps} />);

      await waitFor(() => {
        expect(screen.getByText(/error/i)).toBeInTheDocument();
      });
    });
  });
});
