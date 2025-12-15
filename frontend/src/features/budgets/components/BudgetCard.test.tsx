/**
 * BudgetCard Component Tests
 *
 * TDD RED: These tests are written first.
 */

import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { BudgetCard } from './BudgetCard';
import type { Budget } from '../types';

describe('BudgetCard', () => {
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
        category: ['Food and Drink', 'Restaurants'],
        allocated_amount: 500,
        spent_amount: 150,
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

  describe('Rendering', () => {
    it('should render budget name', () => {
      render(<BudgetCard budget={mockBudget} />);

      expect(screen.getByText('Monthly Budget')).toBeInTheDocument();
    });

    it('should render budget description', () => {
      render(<BudgetCard budget={mockBudget} />);

      expect(screen.getByText('My monthly budget')).toBeInTheDocument();
    });

    it('should render budget period', () => {
      render(<BudgetCard budget={mockBudget} />);

      expect(screen.getByText('Monthly')).toBeInTheDocument();
    });

    it('should render budget amount', () => {
      render(<BudgetCard budget={mockBudget} />);

      expect(screen.getByText('$2,000.00')).toBeInTheDocument();
    });

    it('should show active badge when budget is active', () => {
      render(<BudgetCard budget={mockBudget} />);

      expect(screen.getByText('Active')).toBeInTheDocument();
    });

    it('should not show active badge when budget is inactive', () => {
      const inactiveBudget = { ...mockBudget, is_active: false };
      render(<BudgetCard budget={inactiveBudget} />);

      expect(screen.queryByText('Active')).not.toBeInTheDocument();
    });
  });

  describe('Progress Display', () => {
    it('should show total spent amount', () => {
      render(<BudgetCard budget={mockBudget} />);

      // Total spent: 150 + 100 = 250
      expect(screen.getByText('$250.00')).toBeInTheDocument();
    });

    it('should show progress percentage', () => {
      render(<BudgetCard budget={mockBudget} />);

      // 250 / 2000 * 100 = 12.5%, rounded to 13%
      expect(screen.getByText('13%')).toBeInTheDocument();
    });

    it('should show remaining amount', () => {
      render(<BudgetCard budget={mockBudget} />);

      // 2000 - 250 = 1750
      expect(screen.getByText(/\$1,750\.00.*remaining/i)).toBeInTheDocument();
    });
  });

  describe('Categories', () => {
    it('should show category count', () => {
      render(<BudgetCard budget={mockBudget} />);

      expect(screen.getByText(/2 categories/i)).toBeInTheDocument();
    });

    it('should handle budget with no categories', () => {
      const budgetNoCategories = { ...mockBudget, categories: [] };
      render(<BudgetCard budget={budgetNoCategories} />);

      expect(screen.getByText(/0 categories/i)).toBeInTheDocument();
    });
  });

  describe('Date Display', () => {
    it('should show date range', () => {
      render(<BudgetCard budget={mockBudget} />);

      // Check that dates element exists (exact format depends on timezone)
      const datesElement = document.querySelector('.budget-card__dates');
      expect(datesElement).toBeInTheDocument();
      expect(datesElement?.textContent).toMatch(/\w{3}\s+\d+.*\w{3}\s+\d+/);
    });
  });

  describe('Interactions', () => {
    it('should call onEdit when edit button is clicked', () => {
      const onEdit = vi.fn();
      render(<BudgetCard budget={mockBudget} onEdit={onEdit} />);

      const editButton = screen.getByRole('button', { name: /edit/i });
      fireEvent.click(editButton);

      expect(onEdit).toHaveBeenCalledWith(mockBudget);
    });

    it('should call onDelete when delete button is clicked', () => {
      const onDelete = vi.fn();
      render(<BudgetCard budget={mockBudget} onDelete={onDelete} />);

      const deleteButton = screen.getByRole('button', { name: /delete/i });
      fireEvent.click(deleteButton);

      expect(onDelete).toHaveBeenCalledWith('budget-1');
    });

    it('should not show action buttons when callbacks not provided', () => {
      render(<BudgetCard budget={mockBudget} />);

      expect(screen.queryByRole('button', { name: /edit/i })).not.toBeInTheDocument();
      expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument();
    });
  });
});
