/**
 * BudgetCategoryForm Tests
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * TDD RED Phase: Write failing tests first
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { BudgetCategoryForm } from './BudgetCategoryForm';
import type { BudgetCategory } from '../../types';
import type { UserTag } from '@/features/tags/types';

// Mock CategoryMatchConfig
vi.mock('./CategoryMatchConfig', () => ({
  CategoryMatchConfig: ({
    value,
    onChange,
    disabled,
  }: {
    value: string;
    onChange: (v: string) => void;
    disabled?: boolean;
  }) => (
    <div data-testid="category-match-config">
      <select
        data-testid="match-logic-select"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
      >
        <option value="category_only">Category Only</option>
        <option value="tag_only">Tag Only</option>
        <option value="and">Category AND Tag</option>
        <option value="or">Category OR Tag</option>
      </select>
    </div>
  ),
}));

describe('BudgetCategoryForm', () => {
  const mockOnClose = vi.fn();
  const mockOnSubmit = vi.fn();

  const mockAvailableCategories: string[][] = [
    ['Food and Drink', 'Restaurants'],
    ['Food and Drink', 'Groceries'],
    ['Transportation', 'Gas'],
    ['Shopping', 'Clothing'],
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
    tags: ['Essential'],
    match_logic: 'category_only',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    ...overrides,
  });

  beforeEach(() => {
    vi.clearAllMocks();
    mockOnSubmit.mockResolvedValue(undefined);
  });

  describe('Create Mode', () => {
    it('should render with empty fields in create mode', () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      // Amount field should be empty
      const amountInput = screen.getByRole('spinbutton', { name: /amount/i });
      expect(amountInput).toHaveValue(null);

      // Dialog title should indicate create mode
      expect(screen.getByText(/add.*category/i)).toBeInTheDocument();
    });

    it('should show category selector enabled in create mode', () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i });
      expect(categorySelect).not.toBeDisabled();
    });

    it('should require category selection before submit', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      // Fill amount but not category
      const amountInput = screen.getByRole('spinbutton', { name: /amount/i });
      await user.clear(amountInput);
      await user.type(amountInput, '100');

      // Try to submit
      const submitButton = screen.getByRole('button', { name: /save|add|create/i });
      await user.click(submitButton);

      // Should show validation error
      await waitFor(() => {
        expect(screen.getByText(/category.*required/i)).toBeInTheDocument();
      });

      expect(mockOnSubmit).not.toHaveBeenCalled();
    });

    it('should call onSubmit with CreateBudgetCategoryRequest', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      // Select category
      const categorySelect = screen.getByRole('combobox', { name: /^category$/i });
      await user.selectOptions(categorySelect, 'Food and Drink > Restaurants');

      // Fill amount
      const amountInput = screen.getByRole('spinbutton', { name: /amount/i });
      await user.clear(amountInput);
      await user.type(amountInput, '500');

      // Submit
      const submitButton = screen.getByRole('button', { name: /save|add|create/i });
      await user.click(submitButton);

      await waitFor(() => {
        expect(mockOnSubmit).toHaveBeenCalledTimes(1);
        expect(mockOnSubmit).toHaveBeenCalledWith(
          expect.objectContaining({
            category: ['Food and Drink', 'Restaurants'],
            allocated_amount: 500,
            match_logic: 'category_only',
          })
        );
      });
    });
  });

  describe('Edit Mode', () => {
    it('should pre-fill form with initialData in edit mode', () => {
      const category = createMockCategory();
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="edit"
          initialData={category}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      // Dialog title should indicate edit mode
      expect(screen.getByText(/edit.*category/i)).toBeInTheDocument();

      // Amount should be pre-filled
      const amountInput = screen.getByRole('spinbutton', { name: /amount/i });
      expect(amountInput).toHaveValue(500);
    });

    it('should disable category selector in edit mode', () => {
      const category = createMockCategory();
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="edit"
          initialData={category}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i });
      expect(categorySelect).toBeDisabled();
    });

    it('should call onSubmit with UpdateBudgetCategoryRequest', async () => {
      const user = userEvent.setup({ delay: null });
      const category = createMockCategory();
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="edit"
          initialData={category}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      // Update amount
      const amountInput = screen.getByRole('spinbutton', { name: /amount/i });
      await user.clear(amountInput);
      await user.type(amountInput, '600');

      // Submit
      const submitButton = screen.getByRole('button', { name: /save|update/i });
      await user.click(submitButton);

      await waitFor(() => {
        expect(mockOnSubmit).toHaveBeenCalledWith(
          expect.objectContaining({
            allocated_amount: 600,
          })
        );
      });
    });
  });

  describe('Validation', () => {
    it('should validate allocated_amount must be greater than 0', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      // Select category
      const categorySelect = screen.getByRole('combobox', { name: /^category$/i });
      await user.selectOptions(categorySelect, 'Food and Drink > Restaurants');

      // Enter invalid amount
      const amountInput = screen.getByRole('spinbutton', { name: /amount/i });
      await user.clear(amountInput);
      await user.type(amountInput, '0');

      // Submit
      const submitButton = screen.getByRole('button', { name: /save|add|create/i });
      await user.click(submitButton);

      await waitFor(() => {
        expect(screen.getByText(/amount.*greater.*0/i)).toBeInTheDocument();
      });

      expect(mockOnSubmit).not.toHaveBeenCalled();
    });

    it('should show validation error for empty required fields', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      // Try to submit with no data
      const submitButton = screen.getByRole('button', { name: /save|add|create/i });
      await user.click(submitButton);

      await waitFor(() => {
        // Should show category required error
        expect(screen.getByText(/category.*required/i)).toBeInTheDocument();
      });

      expect(mockOnSubmit).not.toHaveBeenCalled();
    });

    it('should validate at least one tag when match_logic is tag_only', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      // Select category
      const categorySelect = screen.getByRole('combobox', { name: /^category$/i });
      await user.selectOptions(categorySelect, 'Food and Drink > Restaurants');

      // Fill amount
      const amountInput = screen.getByRole('spinbutton', { name: /amount/i });
      await user.clear(amountInput);
      await user.type(amountInput, '100');

      // Change match logic to tag_only
      const matchLogicSelect = screen.getByTestId('match-logic-select');
      await user.selectOptions(matchLogicSelect, 'tag_only');

      // Try to submit without selecting any tags
      const submitButton = screen.getByRole('button', { name: /save|add|create/i });
      await user.click(submitButton);

      await waitFor(() => {
        expect(screen.getByText(/at least one tag.*required/i)).toBeInTheDocument();
      });

      expect(mockOnSubmit).not.toHaveBeenCalled();
    });
  });

  describe('Match Logic Integration', () => {
    it('should show CategoryMatchConfig with correct value', () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      expect(screen.getByTestId('category-match-config')).toBeInTheDocument();
    });

    it('should hide tag picker when match_logic is category_only', () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      // Tags section should not be visible in category_only mode (default)
      expect(screen.queryByText(/^tags$/i)).not.toBeInTheDocument();
    });

    it('should show tag picker when match_logic includes tags', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      // Change to tag_only
      const matchLogicSelect = screen.getByTestId('match-logic-select');
      await user.selectOptions(matchLogicSelect, 'tag_only');

      // Tags section should now be visible
      await waitFor(() => {
        expect(screen.getByText(/^tags$/i)).toBeInTheDocument();
      });
    });
  });

  describe('Submit Behavior', () => {
    it('should disable submit button while submitting', async () => {
      const user = userEvent.setup({ delay: null });
      mockOnSubmit.mockImplementation(() => new Promise((resolve) => setTimeout(resolve, 100)));

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      // Fill form
      const categorySelect = screen.getByRole('combobox', { name: /^category$/i });
      await user.selectOptions(categorySelect, 'Food and Drink > Restaurants');

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i });
      await user.clear(amountInput);
      await user.type(amountInput, '100');

      // Submit
      const submitButton = screen.getByRole('button', { name: /save|add|create/i });
      await user.click(submitButton);

      // Button should be disabled
      await waitFor(() => {
        expect(submitButton).toBeDisabled();
      });
    });

    it('should show error message on failed submit', async () => {
      const user = userEvent.setup({ delay: null });
      mockOnSubmit.mockRejectedValue(new Error('Network error'));

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      // Fill form
      const categorySelect = screen.getByRole('combobox', { name: /^category$/i });
      await user.selectOptions(categorySelect, 'Food and Drink > Restaurants');

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i });
      await user.clear(amountInput);
      await user.type(amountInput, '100');

      // Submit
      const submitButton = screen.getByRole('button', { name: /save|add|create/i });
      await user.click(submitButton);

      // Should show error
      await waitFor(() => {
        expect(screen.getByText(/failed|error/i)).toBeInTheDocument();
      });
    });
  });

  describe('Dialog Behavior', () => {
    it('should close dialog when cancel clicked', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      const cancelButton = screen.getByRole('button', { name: /cancel/i });
      await user.click(cancelButton);

      expect(mockOnClose).toHaveBeenCalledTimes(1);
    });

    it('should not render when isOpen is false', () => {
      render(
        <BudgetCategoryForm
          isOpen={false}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      );

      expect(screen.queryByText(/add.*category/i)).not.toBeInTheDocument();
    });
  });
});
