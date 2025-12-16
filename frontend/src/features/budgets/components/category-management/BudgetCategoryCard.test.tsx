/**
 * BudgetCategoryCard Tests
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * TDD RED Phase: Write failing tests first
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { BudgetCategoryCard } from './BudgetCategoryCard';
import type { BudgetCategory } from '../../types';
import { MATCH_LOGIC_LABELS } from '@/shared/types/matching';

// Mock BudgetProgressBar component
vi.mock('../BudgetProgressBar', () => ({
  BudgetProgressBar: ({ spent, total }: { spent: number; total: number }) => (
    <div data-testid="budget-progress-bar" data-spent={spent} data-total={total}>
      Progress: {spent}/{total}
    </div>
  ),
}));

describe('BudgetCategoryCard', () => {
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

  const mockOnEdit = vi.fn();
  const mockOnDelete = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('Rendering', () => {
    it('should render category path as hierarchical string', () => {
      const category = createMockCategory();
      render(<BudgetCategoryCard category={category} />);

      expect(screen.getByText('Food and Drink > Restaurants')).toBeInTheDocument();
    });

    it('should render single-level category path correctly', () => {
      const category = createMockCategory({ category: ['Shopping'] });
      render(<BudgetCategoryCard category={category} />);

      expect(screen.getByText('Shopping')).toBeInTheDocument();
    });

    it('should render allocated amount formatted as currency', () => {
      const category = createMockCategory({ allocated_amount: 1234.56 });
      render(<BudgetCategoryCard category={category} />);

      expect(screen.getByText('$1,234.56')).toBeInTheDocument();
    });

    it('should render spent amount with progress bar', () => {
      const category = createMockCategory({ spent_amount: 250, allocated_amount: 500 });
      render(<BudgetCategoryCard category={category} />);

      const progressBar = screen.getByTestId('budget-progress-bar');
      expect(progressBar).toHaveAttribute('data-spent', '250');
      expect(progressBar).toHaveAttribute('data-total', '500');
    });

    it('should render match logic badge with correct label', () => {
      const category = createMockCategory({ match_logic: 'tag_only' });
      render(<BudgetCategoryCard category={category} />);

      expect(screen.getByText(MATCH_LOGIC_LABELS.tag_only)).toBeInTheDocument();
    });

    it('should render "and" match logic badge', () => {
      const category = createMockCategory({ match_logic: 'and' });
      render(<BudgetCategoryCard category={category} />);

      expect(screen.getByText(MATCH_LOGIC_LABELS.and)).toBeInTheDocument();
    });

    it('should render "or" match logic badge', () => {
      const category = createMockCategory({ match_logic: 'or' });
      render(<BudgetCategoryCard category={category} />);

      expect(screen.getByText(MATCH_LOGIC_LABELS.or)).toBeInTheDocument();
    });

    it('should render tags list when tags are present', () => {
      const category = createMockCategory({ tags: ['groceries', 'essential'] });
      render(<BudgetCategoryCard category={category} />);

      expect(screen.getByText('groceries')).toBeInTheDocument();
      expect(screen.getByText('essential')).toBeInTheDocument();
    });

    it('should not render tags section when tags array is empty', () => {
      const category = createMockCategory({ tags: [] });
      render(<BudgetCategoryCard category={category} />);

      expect(screen.queryByText('Tags:')).not.toBeInTheDocument();
    });

    it('should render spent amount text', () => {
      const category = createMockCategory({ spent_amount: 123.45 });
      render(<BudgetCategoryCard category={category} />);

      expect(screen.getByText(/\$123\.45/)).toBeInTheDocument();
    });
  });

  describe('Actions', () => {
    it('should render edit button when onEdit is provided', () => {
      const category = createMockCategory();
      render(<BudgetCategoryCard category={category} onEdit={mockOnEdit} />);

      expect(screen.getByRole('button', { name: /edit/i })).toBeInTheDocument();
    });

    it('should call onEdit callback when edit button is clicked', async () => {
      const user = userEvent.setup({ delay: null });
      const category = createMockCategory();
      render(<BudgetCategoryCard category={category} onEdit={mockOnEdit} />);

      await user.click(screen.getByRole('button', { name: /edit/i }));

      expect(mockOnEdit).toHaveBeenCalledTimes(1);
    });

    it('should render delete button when onDelete is provided', () => {
      const category = createMockCategory();
      render(<BudgetCategoryCard category={category} onDelete={mockOnDelete} />);

      expect(screen.getByRole('button', { name: /delete/i })).toBeInTheDocument();
    });

    it('should call onDelete callback when delete button is clicked', async () => {
      const user = userEvent.setup({ delay: null });
      const category = createMockCategory();
      render(<BudgetCategoryCard category={category} onDelete={mockOnDelete} />);

      await user.click(screen.getByRole('button', { name: /delete/i }));

      expect(mockOnDelete).toHaveBeenCalledTimes(1);
    });

    it('should hide edit button when onEdit is not provided', () => {
      const category = createMockCategory();
      render(<BudgetCategoryCard category={category} />);

      expect(screen.queryByRole('button', { name: /edit/i })).not.toBeInTheDocument();
    });

    it('should hide delete button when onDelete is not provided', () => {
      const category = createMockCategory();
      render(<BudgetCategoryCard category={category} />);

      expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument();
    });
  });

  describe('States', () => {
    it('should disable action buttons when disabled=true', () => {
      const category = createMockCategory();
      render(
        <BudgetCategoryCard
          category={category}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
          disabled={true}
        />
      );

      expect(screen.getByRole('button', { name: /edit/i })).toBeDisabled();
      expect(screen.getByRole('button', { name: /delete/i })).toBeDisabled();
    });

    it('should show over-budget warning when spent exceeds allocated', () => {
      const category = createMockCategory({ spent_amount: 600, allocated_amount: 500 });
      render(<BudgetCategoryCard category={category} />);

      // Should have a visual indicator of over-budget status
      const card = screen.getByRole('article');
      expect(card.className).toContain('over-budget');
    });

    it('should not show over-budget warning when under budget', () => {
      const category = createMockCategory({ spent_amount: 250, allocated_amount: 500 });
      render(<BudgetCategoryCard category={category} />);

      const card = screen.getByRole('article');
      expect(card.className).not.toContain('over-budget');
    });
  });

  describe('Accessibility', () => {
    it('should have role="article" for the card', () => {
      const category = createMockCategory();
      render(<BudgetCategoryCard category={category} />);

      expect(screen.getByRole('article')).toBeInTheDocument();
    });

    it('should have aria-labels on action buttons', () => {
      const category = createMockCategory();
      render(
        <BudgetCategoryCard
          category={category}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      );

      expect(screen.getByRole('button', { name: /edit/i })).toHaveAttribute('aria-label');
      expect(screen.getByRole('button', { name: /delete/i })).toHaveAttribute('aria-label');
    });

    it('should have semantic heading for category name', () => {
      const category = createMockCategory();
      render(<BudgetCategoryCard category={category} />);

      expect(screen.getByRole('heading')).toHaveTextContent('Food and Drink > Restaurants');
    });
  });
});
