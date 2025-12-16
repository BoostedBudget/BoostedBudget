/**
 * BudgetCategoryManager Tests
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * TDD RED Phase: Write failing tests first
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { BudgetCategoryManager } from './BudgetCategoryManager';
import type { BudgetCategory, CreateBudgetCategoryRequest, UpdateBudgetCategoryRequest } from '../../types';
import type { UserTag } from '@/features/tags/types';

// Mock BudgetCategoryCard
vi.mock('./BudgetCategoryCard', () => ({
  BudgetCategoryCard: ({
    category,
    onEdit,
    onDelete,
    disabled,
  }: {
    category: BudgetCategory;
    onEdit?: () => void;
    onDelete?: () => void;
    disabled?: boolean;
  }) => (
    <div data-testid={`category-card-${category.id}`}>
      <span data-testid="category-name">{category.category.join(' > ')}</span>
      <span data-testid="allocated-amount">{category.allocated_amount}</span>
      <button onClick={onEdit} disabled={disabled} data-testid="edit-btn">
        Edit
      </button>
      <button onClick={onDelete} disabled={disabled} data-testid="delete-btn">
        Delete
      </button>
    </div>
  ),
}));

// Mock BudgetCategoryForm
vi.mock('./BudgetCategoryForm', () => ({
  BudgetCategoryForm: ({
    isOpen,
    onClose,
    onSubmit,
    mode,
    initialData,
    availableCategories,
  }: {
    isOpen: boolean;
    onClose: () => void;
    onSubmit: (data: CreateBudgetCategoryRequest | UpdateBudgetCategoryRequest) => Promise<void>;
    mode: 'create' | 'edit';
    initialData?: BudgetCategory;
    availableCategories: string[][];
  }) =>
    isOpen ? (
      <div data-testid="category-form-dialog">
        <span data-testid="form-mode">{mode}</span>
        <span data-testid="form-initial-id">{initialData?.id || 'none'}</span>
        <span data-testid="available-categories-count">{availableCategories.length}</span>
        <button
          data-testid="form-submit"
          onClick={() =>
            onSubmit({
              category: ['Test', 'Category'],
              allocated_amount: 100,
              match_logic: 'category_only',
            })
          }
        >
          Submit
        </button>
        <button data-testid="form-cancel" onClick={onClose}>
          Cancel
        </button>
      </div>
    ) : null,
}));

// Mock AllocationBreakdown
vi.mock('./AllocationBreakdown', () => ({
  AllocationBreakdown: ({
    categories,
    totalIncome,
    unallocated,
  }: {
    categories: BudgetCategory[];
    totalIncome: number;
    unallocated: number;
  }) => (
    <div data-testid="allocation-breakdown">
      <span data-testid="breakdown-total-income">{totalIncome}</span>
      <span data-testid="breakdown-unallocated">{unallocated}</span>
      <span data-testid="breakdown-category-count">{categories.length}</span>
    </div>
  ),
}));

describe('BudgetCategoryManager', () => {
  const mockOnAddCategory = vi.fn();
  const mockOnUpdateCategory = vi.fn();
  const mockOnDeleteCategory = vi.fn();

  const mockAvailableCategories: string[][] = [
    ['Food and Drink', 'Restaurants'],
    ['Food and Drink', 'Groceries'],
    ['Transportation', 'Gas'],
    ['Shopping', 'Clothing'],
    ['Entertainment', 'Movies'],
  ];

  const mockAvailableTags: UserTag[] = [
    {
      id: '1',
      user_id: 'user-1',
      name: 'Essential',
      color: '#22c55e',
      usage_count: 5,
      created_at: '',
      updated_at: '',
    },
    {
      id: '2',
      user_id: 'user-1',
      name: 'Discretionary',
      color: '#f59e0b',
      usage_count: 3,
      created_at: '',
      updated_at: '',
    },
  ];

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

  beforeEach(() => {
    vi.clearAllMocks();
    mockOnAddCategory.mockResolvedValue(createMockCategory());
    mockOnUpdateCategory.mockResolvedValue(createMockCategory());
    mockOnDeleteCategory.mockResolvedValue(undefined);
  });

  describe('Rendering', () => {
    it('should render empty state when no categories', () => {
      render(
        <BudgetCategoryManager
          budgetId="budget-1"
          categories={[]}
          totalIncome={3000}
          onAddCategory={mockOnAddCategory}
          onUpdateCategory={mockOnUpdateCategory}
          onDeleteCategory={mockOnDeleteCategory}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      expect(screen.getByText(/no.*categories/i)).toBeInTheDocument();
    });

    it('should render BudgetCategoryCard for each category', () => {
      const categories = [
        createMockCategory({ id: 'cat-1', category: ['Food and Drink', 'Restaurants'] }),
        createMockCategory({ id: 'cat-2', category: ['Transportation', 'Gas'] }),
      ];

      render(
        <BudgetCategoryManager
          budgetId="budget-1"
          categories={categories}
          totalIncome={3000}
          onAddCategory={mockOnAddCategory}
          onUpdateCategory={mockOnUpdateCategory}
          onDeleteCategory={mockOnDeleteCategory}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      expect(screen.getByTestId('category-card-cat-1')).toBeInTheDocument();
      expect(screen.getByTestId('category-card-cat-2')).toBeInTheDocument();
    });

    it('should render add category button', () => {
      render(
        <BudgetCategoryManager
          budgetId="budget-1"
          categories={[]}
          totalIncome={3000}
          onAddCategory={mockOnAddCategory}
          onUpdateCategory={mockOnUpdateCategory}
          onDeleteCategory={mockOnDeleteCategory}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      expect(screen.getByRole('button', { name: /add.*category/i })).toBeInTheDocument();
    });

    it('should render AllocationBreakdown when categories exist', () => {
      const categories = [createMockCategory()];

      render(
        <BudgetCategoryManager
          budgetId="budget-1"
          categories={categories}
          totalIncome={3000}
          onAddCategory={mockOnAddCategory}
          onUpdateCategory={mockOnUpdateCategory}
          onDeleteCategory={mockOnDeleteCategory}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      expect(screen.getByTestId('allocation-breakdown')).toBeInTheDocument();
    });

    it('should not render AllocationBreakdown when no categories', () => {
      render(
        <BudgetCategoryManager
          budgetId="budget-1"
          categories={[]}
          totalIncome={3000}
          onAddCategory={mockOnAddCategory}
          onUpdateCategory={mockOnUpdateCategory}
          onDeleteCategory={mockOnDeleteCategory}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      expect(screen.queryByTestId('allocation-breakdown')).not.toBeInTheDocument();
    });
  });

  describe('Add Category', () => {
    it('should open form in create mode when add button clicked', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <BudgetCategoryManager
          budgetId="budget-1"
          categories={[]}
          totalIncome={3000}
          onAddCategory={mockOnAddCategory}
          onUpdateCategory={mockOnUpdateCategory}
          onDeleteCategory={mockOnDeleteCategory}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      await user.click(screen.getByRole('button', { name: /add.*category/i }));

      await waitFor(() => {
        expect(screen.getByTestId('category-form-dialog')).toBeInTheDocument();
        expect(screen.getByTestId('form-mode')).toHaveTextContent('create');
      });
    });

    it('should call onAddCategory when form is submitted in create mode', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <BudgetCategoryManager
          budgetId="budget-1"
          categories={[]}
          totalIncome={3000}
          onAddCategory={mockOnAddCategory}
          onUpdateCategory={mockOnUpdateCategory}
          onDeleteCategory={mockOnDeleteCategory}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      // Open form
      await user.click(screen.getByRole('button', { name: /add.*category/i }));

      // Submit form
      await user.click(screen.getByTestId('form-submit'));

      await waitFor(() => {
        expect(mockOnAddCategory).toHaveBeenCalledTimes(1);
      });
    });

    it('should filter used categories from form dropdown', async () => {
      const user = userEvent.setup({ delay: null });
      const existingCategory = createMockCategory({
        id: 'cat-1',
        category: ['Food and Drink', 'Restaurants'],
      });

      render(
        <BudgetCategoryManager
          budgetId="budget-1"
          categories={[existingCategory]}
          totalIncome={3000}
          onAddCategory={mockOnAddCategory}
          onUpdateCategory={mockOnUpdateCategory}
          onDeleteCategory={mockOnDeleteCategory}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      // Open form
      await user.click(screen.getByRole('button', { name: /add.*category/i }));

      await waitFor(() => {
        // Available categories should be filtered (5 total - 1 used = 4)
        expect(screen.getByTestId('available-categories-count')).toHaveTextContent('4');
      });
    });
  });

  describe('Edit Category', () => {
    it('should open form in edit mode when edit button clicked', async () => {
      const user = userEvent.setup({ delay: null });
      const categories = [createMockCategory({ id: 'cat-1' })];

      render(
        <BudgetCategoryManager
          budgetId="budget-1"
          categories={categories}
          totalIncome={3000}
          onAddCategory={mockOnAddCategory}
          onUpdateCategory={mockOnUpdateCategory}
          onDeleteCategory={mockOnDeleteCategory}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      const card = screen.getByTestId('category-card-cat-1');
      await user.click(within(card).getByTestId('edit-btn'));

      await waitFor(() => {
        expect(screen.getByTestId('category-form-dialog')).toBeInTheDocument();
        expect(screen.getByTestId('form-mode')).toHaveTextContent('edit');
        expect(screen.getByTestId('form-initial-id')).toHaveTextContent('cat-1');
      });
    });

    it('should call onUpdateCategory when form is submitted in edit mode', async () => {
      const user = userEvent.setup({ delay: null });
      const categories = [createMockCategory({ id: 'cat-1' })];

      render(
        <BudgetCategoryManager
          budgetId="budget-1"
          categories={categories}
          totalIncome={3000}
          onAddCategory={mockOnAddCategory}
          onUpdateCategory={mockOnUpdateCategory}
          onDeleteCategory={mockOnDeleteCategory}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      // Click edit
      const card = screen.getByTestId('category-card-cat-1');
      await user.click(within(card).getByTestId('edit-btn'));

      // Submit form
      await user.click(screen.getByTestId('form-submit'));

      await waitFor(() => {
        expect(mockOnUpdateCategory).toHaveBeenCalledWith('cat-1', expect.any(Object));
      });
    });
  });

  describe('Delete Category', () => {
    it('should show confirmation dialog when delete button clicked', async () => {
      const user = userEvent.setup({ delay: null });
      const categories = [createMockCategory({ id: 'cat-1' })];

      render(
        <BudgetCategoryManager
          budgetId="budget-1"
          categories={categories}
          totalIncome={3000}
          onAddCategory={mockOnAddCategory}
          onUpdateCategory={mockOnUpdateCategory}
          onDeleteCategory={mockOnDeleteCategory}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      const card = screen.getByTestId('category-card-cat-1');
      await user.click(within(card).getByTestId('delete-btn'));

      await waitFor(() => {
        expect(screen.getByText(/confirm.*delete/i)).toBeInTheDocument();
      });
    });

    it('should call onDeleteCategory after confirmation', async () => {
      const user = userEvent.setup({ delay: null });
      const categories = [createMockCategory({ id: 'cat-1' })];

      render(
        <BudgetCategoryManager
          budgetId="budget-1"
          categories={categories}
          totalIncome={3000}
          onAddCategory={mockOnAddCategory}
          onUpdateCategory={mockOnUpdateCategory}
          onDeleteCategory={mockOnDeleteCategory}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      // Click delete
      const card = screen.getByTestId('category-card-cat-1');
      await user.click(within(card).getByTestId('delete-btn'));

      // Confirm deletion
      await user.click(screen.getByRole('button', { name: /^delete$/i }));

      await waitFor(() => {
        expect(mockOnDeleteCategory).toHaveBeenCalledWith('cat-1');
      });
    });

    it('should close confirmation dialog when cancelled', async () => {
      const user = userEvent.setup({ delay: null });
      const categories = [createMockCategory({ id: 'cat-1' })];

      render(
        <BudgetCategoryManager
          budgetId="budget-1"
          categories={categories}
          totalIncome={3000}
          onAddCategory={mockOnAddCategory}
          onUpdateCategory={mockOnUpdateCategory}
          onDeleteCategory={mockOnDeleteCategory}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      // Click delete
      const card = screen.getByTestId('category-card-cat-1');
      await user.click(within(card).getByTestId('delete-btn'));

      // Cancel
      await user.click(screen.getByRole('button', { name: /cancel/i }));

      await waitFor(() => {
        expect(screen.queryByText(/confirm.*delete/i)).not.toBeInTheDocument();
      });

      expect(mockOnDeleteCategory).not.toHaveBeenCalled();
    });
  });

  describe('Disabled State', () => {
    it('should disable add button when disabled=true', () => {
      render(
        <BudgetCategoryManager
          budgetId="budget-1"
          categories={[]}
          totalIncome={3000}
          onAddCategory={mockOnAddCategory}
          onUpdateCategory={mockOnUpdateCategory}
          onDeleteCategory={mockOnDeleteCategory}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          disabled={true}
        />
      );

      expect(screen.getByRole('button', { name: /add.*category/i })).toBeDisabled();
    });

    it('should pass disabled prop to category cards', () => {
      const categories = [createMockCategory({ id: 'cat-1' })];

      render(
        <BudgetCategoryManager
          budgetId="budget-1"
          categories={categories}
          totalIncome={3000}
          onAddCategory={mockOnAddCategory}
          onUpdateCategory={mockOnUpdateCategory}
          onDeleteCategory={mockOnDeleteCategory}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          disabled={true}
        />
      );

      const card = screen.getByTestId('category-card-cat-1');
      expect(within(card).getByTestId('edit-btn')).toBeDisabled();
      expect(within(card).getByTestId('delete-btn')).toBeDisabled();
    });
  });

  describe('AllocationBreakdown Integration', () => {
    it('should pass correct props to AllocationBreakdown', () => {
      const categories = [
        createMockCategory({ id: 'cat-1', allocated_amount: 500 }),
        createMockCategory({ id: 'cat-2', allocated_amount: 1000 }),
      ];

      render(
        <BudgetCategoryManager
          budgetId="budget-1"
          categories={categories}
          totalIncome={3000}
          onAddCategory={mockOnAddCategory}
          onUpdateCategory={mockOnUpdateCategory}
          onDeleteCategory={mockOnDeleteCategory}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      const breakdown = screen.getByTestId('allocation-breakdown');
      expect(within(breakdown).getByTestId('breakdown-total-income')).toHaveTextContent('3000');
      // Unallocated = 3000 - 500 - 1000 = 1500
      expect(within(breakdown).getByTestId('breakdown-unallocated')).toHaveTextContent('1500');
      expect(within(breakdown).getByTestId('breakdown-category-count')).toHaveTextContent('2');
    });
  });
});
