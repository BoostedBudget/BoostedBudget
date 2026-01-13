import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { IncomeSourcePicker } from './IncomeSourcePicker'
import type { IncomeSource } from '../../types'
import type { UserTag } from '@/features/tags/types'

describe('IncomeSourcePicker', () => {
  const mockOnAdd = mock(() => {})

  const mockCategories = ['INCOME', 'TRANSFER_IN', 'LOAN_PAYMENTS']
  const mockTags: UserTag[] = [
    { id: '1', user_id: 'user-1', name: 'Salary', color: '#22c55e', usage_count: 5, created_at: '', updated_at: '' },
    { id: '2', user_id: 'user-1', name: 'Freelance', color: '#3b82f6', usage_count: 3, created_at: '', updated_at: '' },
    { id: '3', user_id: 'user-1', name: 'Bonus', color: '#f59e0b', usage_count: 1, created_at: '', updated_at: '' },
  ]

  beforeEach(() => {
    mockOnAdd.mockClear()
  })

  describe('Type Selection', () => {
    it('renders type selector with three options', () => {
      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={[]}
          onAdd={mockOnAdd}
        />
      );

      const typeSelector = screen.getByRole('combobox', { name: /source type/i }) as HTMLSelectElement;
      expect(typeSelector).toBeInTheDocument();

      // Check all options exist in the type selector
      const options = Array.from(typeSelector.options).map(opt => opt.text.toLowerCase());
      expect(options).toContain('category');
      expect(options).toContain('tag');
      expect(options).toContain('manual');
    });

    it('defaults to plaid_category type', () => {
      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={[]}
          onAdd={mockOnAdd}
        />
      );

      const typeSelector = screen.getByRole('combobox', { name: /source type/i }) as HTMLSelectElement;
      expect(typeSelector.value).toBe('plaid_category');
    });

    it('shows category dropdown when plaid_category selected', () => {
      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={[]}
          onAdd={mockOnAdd}
        />
      );

      // Category dropdown should be visible by default
      expect(screen.getByRole('combobox', { name: /select category/i })).toBeInTheDocument();
    });

    it('shows tag dropdown when tag selected', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={[]}
          onAdd={mockOnAdd}
        />
      );

      // Select tag type
      const typeSelector = screen.getByRole('combobox', { name: /source type/i });
      await user.selectOptions(typeSelector, 'tag');

      // Tag dropdown should be visible
      await waitFor(() => {
        expect(screen.getByRole('combobox', { name: /select tag/i })).toBeInTheDocument();
      });
    });

    it('shows amount input when manual selected', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={[]}
          onAdd={mockOnAdd}
        />
      );

      // Select manual type
      const typeSelector = screen.getByRole('combobox', { name: /source type/i });
      await user.selectOptions(typeSelector, 'manual');

      // Amount input should be visible
      await waitFor(() => {
        expect(screen.getByLabelText(/amount/i)).toBeInTheDocument();
      });
    });
  });

  // ==========================================================================
  // Plaid Category Selection Tests
  // ==========================================================================

  describe('Plaid Category Selection', () => {
    it('renders available categories in dropdown', () => {
      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={[]}
          onAdd={mockOnAdd}
        />
      );

      const categorySelect = screen.getByRole('combobox', { name: /select category/i });
      expect(categorySelect).toBeInTheDocument();

      // Check options exist (formatted)
      expect(screen.getByRole('option', { name: /income/i })).toBeInTheDocument();
      expect(screen.getByRole('option', { name: /transfer in/i })).toBeInTheDocument();
    });

    it('filters out already selected categories', () => {
      const existingSources: IncomeSource[] = [
        { type: 'plaid_category', value: 'INCOME' },
      ];

      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={existingSources}
          onAdd={mockOnAdd}
        />
      );

      // INCOME should not be in the dropdown
      expect(screen.queryByRole('option', { name: /^income$/i })).not.toBeInTheDocument();
      // Others should still be there
      expect(screen.getByRole('option', { name: /transfer in/i })).toBeInTheDocument();
    });

    it('enables add button when category selected', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={[]}
          onAdd={mockOnAdd}
        />
      );

      // Add button should be disabled initially
      const addButton = screen.getByRole('button', { name: /add/i });
      expect(addButton).toBeDisabled();

      // Select a category
      const categorySelect = screen.getByRole('combobox', { name: /select category/i });
      await user.selectOptions(categorySelect, 'INCOME');

      // Add button should be enabled
      await waitFor(() => {
        expect(addButton).not.toBeDisabled();
      });
    });
  });

  // ==========================================================================
  // Tag Selection Tests
  // ==========================================================================

  describe('Tag Selection', () => {
    it('renders available tags in dropdown', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={[]}
          onAdd={mockOnAdd}
        />
      );

      // Switch to tag type
      const typeSelector = screen.getByRole('combobox', { name: /source type/i });
      await user.selectOptions(typeSelector, 'tag');

      await waitFor(() => {
        const tagSelect = screen.getByRole('combobox', { name: /select tag/i });
        expect(tagSelect).toBeInTheDocument();
      });

      // Check tags are available
      expect(screen.getByRole('option', { name: /salary/i })).toBeInTheDocument();
      expect(screen.getByRole('option', { name: /freelance/i })).toBeInTheDocument();
    });

    it('filters out already selected tags', async () => {
      const user = userEvent.setup({ delay: null });
      const existingSources: IncomeSource[] = [
        { type: 'tag', value: 'Salary' },
      ];

      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={existingSources}
          onAdd={mockOnAdd}
        />
      );

      // Switch to tag type
      const typeSelector = screen.getByRole('combobox', { name: /source type/i });
      await user.selectOptions(typeSelector, 'tag');

      await waitFor(() => {
        expect(screen.getByRole('combobox', { name: /select tag/i })).toBeInTheDocument();
      });

      // Salary should not be in the dropdown
      expect(screen.queryByRole('option', { name: /^salary$/i })).not.toBeInTheDocument();
      // Others should still be there
      expect(screen.getByRole('option', { name: /freelance/i })).toBeInTheDocument();
    });
  });

  // ==========================================================================
  // Manual Entry Tests
  // ==========================================================================

  describe('Manual Entry', () => {
    it('validates amount is a positive number', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={[]}
          onAdd={mockOnAdd}
        />
      );

      // Switch to manual type
      const typeSelector = screen.getByRole('combobox', { name: /source type/i });
      await user.selectOptions(typeSelector, 'manual');

      await waitFor(() => {
        expect(screen.getByLabelText(/amount/i)).toBeInTheDocument();
      });

      const amountInput = screen.getByLabelText(/amount/i);
      const addButton = screen.getByRole('button', { name: /add/i });

      // Enter 0 - should keep button disabled
      await user.type(amountInput, '0');
      expect(addButton).toBeDisabled();

      // Clear and enter valid amount
      await user.clear(amountInput);
      await user.type(amountInput, '1000');
      expect(addButton).not.toBeDisabled();
    });

    it('allows optional description input', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={[]}
          onAdd={mockOnAdd}
        />
      );

      // Switch to manual type
      const typeSelector = screen.getByRole('combobox', { name: /source type/i });
      await user.selectOptions(typeSelector, 'manual');

      await waitFor(() => {
        expect(screen.getByLabelText(/description/i)).toBeInTheDocument();
      });

      const descriptionInput = screen.getByLabelText(/description/i);
      expect(descriptionInput).toBeInTheDocument();
    });

    it('enables add button when valid amount entered', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={[]}
          onAdd={mockOnAdd}
        />
      );

      // Switch to manual type
      const typeSelector = screen.getByRole('combobox', { name: /source type/i });
      await user.selectOptions(typeSelector, 'manual');

      await waitFor(() => {
        expect(screen.getByLabelText(/amount/i)).toBeInTheDocument();
      });

      const amountInput = screen.getByLabelText(/amount/i);
      await user.type(amountInput, '500.50');

      const addButton = screen.getByRole('button', { name: /add/i });
      expect(addButton).not.toBeDisabled();
    });
  });

  // ==========================================================================
  // Add Action Tests
  // ==========================================================================

  describe('Add Action', () => {
    it('calls onAdd with correct IncomeSource for category', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={[]}
          onAdd={mockOnAdd}
        />
      );

      // Select a category
      const categorySelect = screen.getByRole('combobox', { name: /select category/i });
      await user.selectOptions(categorySelect, 'INCOME');

      // Click add
      const addButton = screen.getByRole('button', { name: /add/i });
      await user.click(addButton);

      expect(mockOnAdd).toHaveBeenCalledWith({
        type: 'plaid_category',
        value: 'INCOME',
      });
    });

    it('calls onAdd with correct IncomeSource for tag', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={[]}
          onAdd={mockOnAdd}
        />
      );

      // Switch to tag type
      const typeSelector = screen.getByRole('combobox', { name: /source type/i });
      await user.selectOptions(typeSelector, 'tag');

      await waitFor(() => {
        expect(screen.getByRole('combobox', { name: /select tag/i })).toBeInTheDocument();
      });

      // Select a tag
      const tagSelect = screen.getByRole('combobox', { name: /select tag/i });
      await user.selectOptions(tagSelect, 'Freelance');

      // Click add
      const addButton = screen.getByRole('button', { name: /add/i });
      await user.click(addButton);

      expect(mockOnAdd).toHaveBeenCalledWith({
        type: 'tag',
        value: 'Freelance',
      });
    });

    it('calls onAdd with correct IncomeSource for manual', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={[]}
          onAdd={mockOnAdd}
        />
      );

      // Switch to manual type
      const typeSelector = screen.getByRole('combobox', { name: /source type/i });
      await user.selectOptions(typeSelector, 'manual');

      await waitFor(() => {
        expect(screen.getByLabelText(/amount/i)).toBeInTheDocument();
      });

      // Fill in amount and description
      const amountInput = screen.getByLabelText(/amount/i);
      await user.type(amountInput, '2500');

      const descriptionInput = screen.getByLabelText(/description/i);
      await user.type(descriptionInput, 'Monthly salary');

      // Click add
      const addButton = screen.getByRole('button', { name: /add/i });
      await user.click(addButton);

      expect(mockOnAdd).toHaveBeenCalledWith({
        type: 'manual',
        value: '2500',
        description: 'Monthly salary',
      });
    });

    it('clears selection after adding', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={[]}
          onAdd={mockOnAdd}
        />
      );

      // Select a category
      const categorySelect = screen.getByRole('combobox', { name: /select category/i }) as HTMLSelectElement;
      await user.selectOptions(categorySelect, 'INCOME');

      // Click add
      const addButton = screen.getByRole('button', { name: /add/i });
      await user.click(addButton);

      // Selection should be cleared
      await waitFor(() => {
        expect(categorySelect.value).toBe('');
      });

      // Add button should be disabled again
      expect(addButton).toBeDisabled();
    });
  });

  // ==========================================================================
  // Disabled State Tests
  // ==========================================================================

  describe('Disabled State', () => {
    it('disables all inputs when disabled prop is true', () => {
      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={[]}
          onAdd={mockOnAdd}
          disabled
        />
      );

      const typeSelector = screen.getByRole('combobox', { name: /source type/i });
      expect(typeSelector).toBeDisabled();

      const categorySelect = screen.getByRole('combobox', { name: /select category/i });
      expect(categorySelect).toBeDisabled();

      const addButton = screen.getByRole('button', { name: /add/i });
      expect(addButton).toBeDisabled();
    });
  });

  // ==========================================================================
  // Accessibility Tests
  // ==========================================================================

  describe('Accessibility', () => {
    it('has accessible labels for all inputs', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <IncomeSourcePicker
          availableCategories={mockCategories}
          availableTags={mockTags}
          existingSources={[]}
          onAdd={mockOnAdd}
        />
      );

      // Type selector
      expect(screen.getByRole('combobox', { name: /source type/i })).toBeInTheDocument();

      // Category selector (default)
      expect(screen.getByRole('combobox', { name: /select category/i })).toBeInTheDocument();

      // Switch to manual and check labels
      const typeSelector = screen.getByRole('combobox', { name: /source type/i });
      await user.selectOptions(typeSelector, 'manual');

      await waitFor(() => {
        expect(screen.getByLabelText(/amount/i)).toBeInTheDocument();
        expect(screen.getByLabelText(/description/i)).toBeInTheDocument();
      });
    });
  });
});
