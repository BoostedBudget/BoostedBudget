/**
 * BudgetOverviewWidget Tests
 *
 * TDD RED: Tests for the budget overview dashboard widget.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { BudgetOverviewWidget } from './BudgetOverviewWidget';
import * as budgetsApi from '../../budgets/api/budgetsApi';
import type { Budget } from '../../budgets/types';

// Mock the API module
vi.mock('../../budgets/api/budgetsApi');

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
      vi.mocked(budgetsApi.fetchCurrentBudget).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      );

      render(<BudgetOverviewWidget {...defaultProps} />);

      // Check for loading indicator
      const loading = document.querySelector('.animate-spin');
      expect(loading).toBeInTheDocument();
    });
  });

  describe('Success State', () => {
    it('should render budget name', async () => {
      vi.mocked(budgetsApi.fetchCurrentBudget).mockResolvedValue(mockBudget);

      render(<BudgetOverviewWidget {...defaultProps} />);

      await waitFor(() => {
        expect(screen.getByText('Monthly Budget')).toBeInTheDocument();
      });
    });

    it('should render budget amount', async () => {
      vi.mocked(budgetsApi.fetchCurrentBudget).mockResolvedValue(mockBudget);

      render(<BudgetOverviewWidget {...defaultProps} />);

      await waitFor(() => {
        expect(screen.getByText('$2,000.00')).toBeInTheDocument();
      });
    });

    it('should render spent amount', async () => {
      vi.mocked(budgetsApi.fetchCurrentBudget).mockResolvedValue(mockBudget);

      render(<BudgetOverviewWidget {...defaultProps} />);

      await waitFor(() => {
        // Total spent: 250 + 100 = 350
        expect(screen.getByText('$350.00')).toBeInTheDocument();
      });
    });

    it('should render progress bar', async () => {
      vi.mocked(budgetsApi.fetchCurrentBudget).mockResolvedValue(mockBudget);

      render(<BudgetOverviewWidget {...defaultProps} />);

      await waitFor(() => {
        expect(screen.getByRole('progressbar')).toBeInTheDocument();
      });
    });

    it('should show percentage used', async () => {
      vi.mocked(budgetsApi.fetchCurrentBudget).mockResolvedValue(mockBudget);

      render(<BudgetOverviewWidget {...defaultProps} />);

      await waitFor(() => {
        // 350 / 2000 * 100 = 17.5% -> 18%
        expect(screen.getByText('18%')).toBeInTheDocument();
      });
    });
  });

  describe('No Budget State', () => {
    it('should show "no budget" message when 404', async () => {
      const error = { response: { status: 404 } };
      vi.mocked(budgetsApi.fetchCurrentBudget).mockRejectedValue(error);

      render(<BudgetOverviewWidget {...defaultProps} />);

      await waitFor(() => {
        expect(screen.getByText(/no active budget/i)).toBeInTheDocument();
      });
    });

    it('should show create budget prompt when no budget', async () => {
      const error = { response: { status: 404 } };
      vi.mocked(budgetsApi.fetchCurrentBudget).mockRejectedValue(error);

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
      const error = new Error('Network error');
      vi.mocked(budgetsApi.fetchCurrentBudget).mockRejectedValue(error);

      render(<BudgetOverviewWidget {...defaultProps} />);

      await waitFor(() => {
        expect(screen.getByText(/error/i)).toBeInTheDocument();
      });
    });
  });
});
