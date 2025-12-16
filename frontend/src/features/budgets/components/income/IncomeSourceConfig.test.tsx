/**
 * IncomeSourceConfig Tests
 * Issue #496: Income Source Configuration UI for Envelope Budgeting
 *
 * TDD RED Phase: Tests for the container component managing income sources.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { IncomeSourceConfig } from './IncomeSourceConfig';
import type { IncomeSource } from '../../types';
import type { UserTag } from '@/features/tags/types';

describe('IncomeSourceConfig', () => {
  const mockOnChange = vi.fn();

  const mockCategories = ['INCOME', 'TRANSFER_IN', 'LOAN_PAYMENTS'];
  const mockTags: UserTag[] = [
    { id: '1', user_id: 'user-1', name: 'Salary', color: '#22c55e', usage_count: 5, created_at: '', updated_at: '' },
    { id: '2', user_id: 'user-1', name: 'Freelance', color: '#3b82f6', usage_count: 3, created_at: '', updated_at: '' },
  ];

  const mockSources: IncomeSource[] = [
    { type: 'plaid_category', value: 'INCOME' },
    { type: 'tag', value: 'Freelance' },
    { type: 'manual', value: '500', description: 'Side gig' },
  ];

  beforeEach(() => {
    vi.clearAllMocks();
  });

  // ==========================================================================
  // Rendering Tests
  // ==========================================================================

  describe('Rendering', () => {
    it('renders empty state when no sources', () => {
      render(
        <IncomeSourceConfig
          sources={[]}
          onChange={mockOnChange}
          availableCategories={mockCategories}
          availableTags={mockTags}
        />
      );

      expect(screen.getByText(/no income sources configured/i)).toBeInTheDocument();
    });

    it('renders IncomeSourceItem for each source', () => {
      render(
        <IncomeSourceConfig
          sources={mockSources}
          onChange={mockOnChange}
          availableCategories={mockCategories}
          availableTags={mockTags}
        />
      );

      // Check all sources are rendered
      expect(screen.getByText('Income')).toBeInTheDocument();
      expect(screen.getByText('Freelance')).toBeInTheDocument();
      expect(screen.getByText('$500.00')).toBeInTheDocument();
      expect(screen.getByText('Side gig')).toBeInTheDocument();
    });

    it('renders IncomeSourcePicker', () => {
      render(
        <IncomeSourceConfig
          sources={[]}
          onChange={mockOnChange}
          availableCategories={mockCategories}
          availableTags={mockTags}
        />
      );

      expect(screen.getByRole('combobox', { name: /source type/i })).toBeInTheDocument();
    });
  });

  // ==========================================================================
  // Add Source Tests
  // ==========================================================================

  describe('Add Source', () => {
    it('adds source to list when picker adds one', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <IncomeSourceConfig
          sources={[]}
          onChange={mockOnChange}
          availableCategories={mockCategories}
          availableTags={mockTags}
        />
      );

      // Select a category and add
      const categorySelect = screen.getByRole('combobox', { name: /select category/i });
      await user.selectOptions(categorySelect, 'INCOME');

      const addButton = screen.getByRole('button', { name: /add/i });
      await user.click(addButton);

      // Should call onChange with the new source
      expect(mockOnChange).toHaveBeenCalledWith([
        { type: 'plaid_category', value: 'INCOME' },
      ]);
    });

    it('calls onChange with updated sources array', async () => {
      const user = userEvent.setup({ delay: null });
      const existingSources: IncomeSource[] = [
        { type: 'tag', value: 'Salary' },
      ];

      render(
        <IncomeSourceConfig
          sources={existingSources}
          onChange={mockOnChange}
          availableCategories={mockCategories}
          availableTags={mockTags}
        />
      );

      // Add another source
      const categorySelect = screen.getByRole('combobox', { name: /select category/i });
      await user.selectOptions(categorySelect, 'INCOME');

      const addButton = screen.getByRole('button', { name: /add/i });
      await user.click(addButton);

      // Should append to existing sources
      expect(mockOnChange).toHaveBeenCalledWith([
        { type: 'tag', value: 'Salary' },
        { type: 'plaid_category', value: 'INCOME' },
      ]);
    });
  });

  // ==========================================================================
  // Remove Source Tests
  // ==========================================================================

  describe('Remove Source', () => {
    it('removes source from list when item remove clicked', async () => {
      const user = userEvent.setup({ delay: null });
      const sources: IncomeSource[] = [
        { type: 'plaid_category', value: 'INCOME' },
        { type: 'tag', value: 'Freelance' },
      ];

      render(
        <IncomeSourceConfig
          sources={sources}
          onChange={mockOnChange}
          availableCategories={mockCategories}
          availableTags={mockTags}
        />
      );

      // Find and click the first remove button (for INCOME)
      const removeButtons = screen.getAllByRole('button', { name: /remove/i });
      await user.click(removeButtons[0]);

      // Should call onChange without the removed source
      expect(mockOnChange).toHaveBeenCalledWith([
        { type: 'tag', value: 'Freelance' },
      ]);
    });

    it('calls onChange with filtered sources array', async () => {
      const user = userEvent.setup({ delay: null });
      const sources: IncomeSource[] = [
        { type: 'manual', value: '1000', description: 'First' },
        { type: 'manual', value: '2000', description: 'Second' },
        { type: 'manual', value: '3000', description: 'Third' },
      ];

      render(
        <IncomeSourceConfig
          sources={sources}
          onChange={mockOnChange}
          availableCategories={mockCategories}
          availableTags={mockTags}
        />
      );

      // Remove the middle one (Second)
      const removeButtons = screen.getAllByRole('button', { name: /remove/i });
      await user.click(removeButtons[1]);

      // Should have first and third
      expect(mockOnChange).toHaveBeenCalledWith([
        { type: 'manual', value: '1000', description: 'First' },
        { type: 'manual', value: '3000', description: 'Third' },
      ]);
    });
  });

  // ==========================================================================
  // Expected Total Tests
  // ==========================================================================

  describe('Expected Total', () => {
    it('shows expected total when showTotal is true', () => {
      const sources: IncomeSource[] = [
        { type: 'manual', value: '1000' },
        { type: 'manual', value: '500' },
      ];

      render(
        <IncomeSourceConfig
          sources={sources}
          onChange={mockOnChange}
          availableCategories={mockCategories}
          availableTags={mockTags}
          showTotal
        />
      );

      expect(screen.getByText(/expected income/i)).toBeInTheDocument();
    });

    it('calculates total from manual sources only', () => {
      const sources: IncomeSource[] = [
        { type: 'plaid_category', value: 'INCOME' },
        { type: 'tag', value: 'Freelance' },
        { type: 'manual', value: '1000' },
        { type: 'manual', value: '500.50' },
      ];

      render(
        <IncomeSourceConfig
          sources={sources}
          onChange={mockOnChange}
          availableCategories={mockCategories}
          availableTags={mockTags}
          showTotal
        />
      );

      // Should only sum manual sources: 1000 + 500.50 = 1500.50
      expect(screen.getByText('$1,500.50')).toBeInTheDocument();
    });

    it('formats total as currency', () => {
      const sources: IncomeSource[] = [
        { type: 'manual', value: '12345.67' },
      ];

      render(
        <IncomeSourceConfig
          sources={sources}
          onChange={mockOnChange}
          availableCategories={mockCategories}
          availableTags={mockTags}
          showTotal
        />
      );

      // Find the total value specifically (it has role="status")
      const totalValue = screen.getByRole('status');
      expect(totalValue).toHaveTextContent('$12,345.67');
    });

    it('shows $0.00 when no manual sources', () => {
      const sources: IncomeSource[] = [
        { type: 'plaid_category', value: 'INCOME' },
        { type: 'tag', value: 'Freelance' },
      ];

      render(
        <IncomeSourceConfig
          sources={sources}
          onChange={mockOnChange}
          availableCategories={mockCategories}
          availableTags={mockTags}
          showTotal
        />
      );

      expect(screen.getByText('$0.00')).toBeInTheDocument();
    });

    it('does not show total when showTotal is false', () => {
      const sources: IncomeSource[] = [
        { type: 'manual', value: '1000' },
      ];

      render(
        <IncomeSourceConfig
          sources={sources}
          onChange={mockOnChange}
          availableCategories={mockCategories}
          availableTags={mockTags}
          showTotal={false}
        />
      );

      expect(screen.queryByText(/expected income/i)).not.toBeInTheDocument();
    });
  });

  // ==========================================================================
  // Disabled State Tests
  // ==========================================================================

  describe('Disabled State', () => {
    it('disables all interactions when disabled', () => {
      render(
        <IncomeSourceConfig
          sources={mockSources}
          onChange={mockOnChange}
          availableCategories={mockCategories}
          availableTags={mockTags}
          disabled
        />
      );

      // All remove buttons should be disabled
      const removeButtons = screen.getAllByRole('button', { name: /remove/i });
      removeButtons.forEach((button) => {
        expect(button).toBeDisabled();
      });

      // Picker should be disabled
      expect(screen.getByRole('combobox', { name: /source type/i })).toBeDisabled();
    });
  });

  // ==========================================================================
  // Accessibility Tests
  // ==========================================================================

  describe('Accessibility', () => {
    it('has accessible list structure', () => {
      render(
        <IncomeSourceConfig
          sources={mockSources}
          onChange={mockOnChange}
          availableCategories={mockCategories}
          availableTags={mockTags}
        />
      );

      // Should have a list element
      expect(screen.getByRole('list', { name: /income sources/i })).toBeInTheDocument();
    });

    it('each source item is a list item', () => {
      render(
        <IncomeSourceConfig
          sources={mockSources}
          onChange={mockOnChange}
          availableCategories={mockCategories}
          availableTags={mockTags}
        />
      );

      const listItems = screen.getAllByRole('listitem');
      expect(listItems).toHaveLength(mockSources.length);
    });
  });
});
