/**
 * CreateBudgetDialog Tests
 * Issue #111: Goals Dashboard & Progress Visualization
 *
 * TDD RED Phase: Tests for budget creation dialog.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { CreateBudgetDialog } from './CreateBudgetDialog';
import * as budgetsApi from '../api/budgetsApi';
import type { Budget } from '../types';
import { fillInputByLabel } from '@/test/utils/formTestHelpers';

// Mock APIs
vi.mock('../api/budgetsApi');

// Issue #496: Mock APIs for income sources
vi.mock('@/features/categories/api/categoriesApi', () => ({
  fetchPlaidCategories: vi.fn().mockResolvedValue(['INCOME', 'TRANSFER_IN', 'LOAN_PAYMENTS']),
}));

vi.mock('@/features/tags/api/tagsApi', () => ({
  fetchTags: vi.fn().mockResolvedValue([
    { id: '1', user_id: 'user-1', name: 'Salary', color: '#22c55e', usage_count: 5, created_at: '', updated_at: '' },
    { id: '2', user_id: 'user-1', name: 'Freelance', color: '#3b82f6', usage_count: 2, created_at: '', updated_at: '' },
  ]),
}));

const mockCreatedBudget: Budget = {
  id: 'budget-1',
  user_id: 'user-1',
  name: 'Monthly Budget',
  description: 'My monthly spending budget',
  period: 'monthly',
  start_date: '2025-01-01',
  end_date: '2025-01-31',
  amount: 2000,
  currency_code: 'USD',
  is_active: true,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
  // Issue #495: Envelope budgeting fields
  income_amount: 0,
  income_sources: [],
};

describe('CreateBudgetDialog', () => {
  const mockOnCreate = vi.fn();
  const mockOnCancel = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(budgetsApi.createBudget).mockResolvedValue(mockCreatedBudget);
  });

  // ==========================================================================
  // Rendering Tests
  // ==========================================================================

  describe('Rendering', () => {
    it('renders dialog when isOpen is true', async () => {
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('dialog')).toBeInTheDocument();
      });
      // Dialog title should be visible
      expect(screen.getByRole('heading', { name: /create budget/i })).toBeInTheDocument();
    });

    it('does not render dialog when isOpen is false', () => {
      render(
        <CreateBudgetDialog
          isOpen={false}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });

    it('renders all form fields', async () => {
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });

      expect(screen.getByLabelText(/description/i)).toBeInTheDocument();
      expect(screen.getByLabelText(/period/i)).toBeInTheDocument();
      expect(screen.getByLabelText(/amount/i)).toBeInTheDocument();
      expect(screen.getByLabelText(/start date/i)).toBeInTheDocument();
    });

    it('renders period options', async () => {
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/period/i)).toBeInTheDocument();
      });

      const periodSelect = screen.getByLabelText(/period/i) as HTMLSelectElement;
      expect(periodSelect.options.length).toBeGreaterThan(0);

      // Check that options exist
      const optionTexts = Array.from(periodSelect.options).map(opt => opt.text.toLowerCase());
      expect(optionTexts).toContain('weekly');
      expect(optionTexts).toContain('monthly');
      expect(optionTexts).toContain('quarterly');
      expect(optionTexts).toContain('yearly');
      expect(optionTexts).toContain('custom');
    });

    it('shows end date field when custom period is selected', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/period/i)).toBeInTheDocument();
      });

      // End date should not be visible initially (monthly is default)
      expect(screen.queryByLabelText(/end date/i)).not.toBeInTheDocument();

      // Select custom period
      const periodSelect = screen.getByLabelText(/period/i) as HTMLSelectElement;
      await user.selectOptions(periodSelect, 'custom');

      // End date should now be visible
      await waitFor(() => {
        expect(screen.getByLabelText(/end date/i)).toBeInTheDocument();
      });
    });
  });

  // ==========================================================================
  // Validation Tests
  // ==========================================================================

  describe('Validation', () => {
    it('shows error when name is empty', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });

      // Fill amount but not name
      await user.clear(screen.getByLabelText(/amount/i));
      await user.type(screen.getByLabelText(/amount/i), '1000');

      await user.click(screen.getByRole('button', { name: /create budget/i }));

      expect(screen.getByText(/name is required/i)).toBeInTheDocument();
    });

    it('shows error when amount is invalid', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });

      // Fill name but enter 0 for amount
      await user.type(screen.getByLabelText(/name/i), 'Test Budget');
      await user.clear(screen.getByLabelText(/amount/i));
      await user.type(screen.getByLabelText(/amount/i), '0');

      await user.click(screen.getByRole('button', { name: /create budget/i }));

      expect(screen.getByText(/amount must be greater than 0/i)).toBeInTheDocument();
    });

    it('shows error when end date is missing for custom period', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });

      // Fill required fields
      await user.type(screen.getByLabelText(/name/i), 'Test Budget');
      await user.clear(screen.getByLabelText(/amount/i));
      await user.type(screen.getByLabelText(/amount/i), '1000');

      // Select custom period (end date required)
      const periodSelect = screen.getByLabelText(/period/i) as HTMLSelectElement;
      await user.selectOptions(periodSelect, 'custom');

      await user.click(screen.getByRole('button', { name: /create budget/i }));

      expect(screen.getByText(/end date is required for custom period/i)).toBeInTheDocument();
    }, 15000);
  });

  // ==========================================================================
  // Submission Tests
  // ==========================================================================

  describe('Submission', () => {
    it('calls createBudget with correct payload on submit', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });

      // Fill form
      await user.type(screen.getByLabelText(/name/i), 'Monthly Budget');
      await user.clear(screen.getByLabelText(/amount/i));
      await user.type(screen.getByLabelText(/amount/i), '2000');

      // Period defaults to 'monthly'
      // Start date should have a default

      // Submit
      await user.click(screen.getByRole('button', { name: /create budget/i }));

      await waitFor(() => {
        expect(budgetsApi.createBudget).toHaveBeenCalledWith(
          expect.objectContaining({
            name: 'Monthly Budget',
            amount: 2000,
            period: 'monthly',
          })
        );
      });
    }, 15000);

    it('shows loading state during API call', async () => {
      // Make API call slow - use longer delay to ensure loading state is visible
      vi.mocked(budgetsApi.createBudget).mockImplementation(
        () => new Promise((resolve) => setTimeout(() => resolve(mockCreatedBudget), 3000))
      );

      const user = userEvent.setup({ delay: null });
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });

      // Fill minimal valid form
      await user.type(screen.getByLabelText(/name/i), 'Test Budget');
      await user.clear(screen.getByLabelText(/amount/i));
      await user.type(screen.getByLabelText(/amount/i), '1000');

      // Submit
      await user.click(screen.getByRole('button', { name: /create budget/i }));

      // Should show loading state
      await waitFor(() => {
        expect(screen.getByText(/creating/i)).toBeInTheDocument();
      });
    }, 15000);

    it('closes dialog and calls onBudgetCreated on successful creation', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });

      // Fill form
      await user.type(screen.getByLabelText(/name/i), 'Monthly Budget');
      await user.clear(screen.getByLabelText(/amount/i));
      await user.type(screen.getByLabelText(/amount/i), '2000');

      // Submit
      await user.click(screen.getByRole('button', { name: /create budget/i }));

      await waitFor(() => {
        expect(mockOnCreate).toHaveBeenCalledWith(mockCreatedBudget);
      });
    }, 15000);

    it('shows error message on API failure', async () => {
      vi.mocked(budgetsApi.createBudget).mockRejectedValue(
        new Error('Failed to create budget')
      );

      const user = userEvent.setup({ delay: null });
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });

      // Fill form using fast utilities
      fillInputByLabel(/name/i, 'Test Budget');
      fillInputByLabel(/amount/i, '1000');

      // Submit
      await user.click(screen.getByRole('button', { name: /create budget/i }));

      await waitFor(() => {
        expect(screen.getByText(/failed to create budget/i)).toBeInTheDocument();
      });
    }, 15000);

    it('calls onCancel when cancel button clicked', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument();
      });

      await user.click(screen.getByRole('button', { name: /cancel/i }));

      expect(mockOnCancel).toHaveBeenCalled();
    });

    it('resets form when reopened after cancel', async () => {
      const { rerender } = render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });

      // Fill name using fast utility
      fillInputByLabel(/name/i, 'Test Budget');

      // Close dialog
      rerender(
        <CreateBudgetDialog
          isOpen={false}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      // Reopen dialog
      rerender(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });

      // Name should be reset
      expect(screen.getByLabelText(/name/i)).toHaveValue('');
    });
  });

  // ==========================================================================
  // Issue #460: Dialog Close Behavior
  // ==========================================================================

  describe('Dialog Close Behavior (Issue #460)', () => {
    it('does not close when clicking outside the dialog (on backdrop)', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('dialog')).toBeInTheDocument();
      });

      // Click on the backdrop (the overlay element)
      // The backdrop is the element with bg-black/25 class
      const backdrop = document.querySelector('.fixed.inset-0.bg-black\\/25');
      if (backdrop) {
        await user.click(backdrop);
      }

      // onCancel should NOT have been called
      expect(mockOnCancel).not.toHaveBeenCalled();

      // Dialog should still be visible
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    it('closes when X button is clicked', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('dialog')).toBeInTheDocument();
      });

      // Find and click the X button (it's a button with XMarkIcon)
      const closeButtons = screen.getAllByRole('button');
      const xButton = closeButtons.find(btn =>
        btn.querySelector('svg') && !btn.textContent?.includes('Cancel') && !btn.textContent?.includes('Create')
      );

      if (xButton) {
        await user.click(xButton);
      }

      expect(mockOnCancel).toHaveBeenCalled();
    });

    it('closes when Cancel button is clicked', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument();
      });

      await user.click(screen.getByRole('button', { name: /cancel/i }));

      expect(mockOnCancel).toHaveBeenCalled();
    });
  });

  // ==========================================================================
  // Issue #496: Income Tab Tests
  // ==========================================================================

  describe('Income Tab (Issue #496)', () => {
    it('renders Budget and Income tabs', async () => {
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('dialog')).toBeInTheDocument();
      });

      // Both tabs should be visible
      expect(screen.getByRole('tab', { name: /budget/i })).toBeInTheDocument();
      expect(screen.getByRole('tab', { name: /income/i })).toBeInTheDocument();
    });

    it('switches between Budget and Income tabs', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('dialog')).toBeInTheDocument();
      });

      // Budget tab should be active by default, form fields visible
      expect(screen.getByLabelText(/name/i)).toBeInTheDocument();

      // Click Income tab
      await user.click(screen.getByRole('tab', { name: /income/i }));

      // Income tab content should be visible (empty state message)
      await waitFor(() => {
        expect(screen.getByText(/no income sources configured/i)).toBeInTheDocument();
      });

      // Click back to Budget tab
      await user.click(screen.getByRole('tab', { name: /budget/i }));

      // Budget form fields should be visible again
      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });
    });

    it('renders IncomeSourceConfig in Income tab', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('dialog')).toBeInTheDocument();
      });

      // Navigate to Income tab
      await user.click(screen.getByRole('tab', { name: /income/i }));

      // Should see the IncomeSourceConfig component (empty state)
      await waitFor(() => {
        expect(screen.getByText(/no income sources configured/i)).toBeInTheDocument();
      });

      // Should see the type selector for adding income sources
      expect(screen.getByLabelText(/source type/i)).toBeInTheDocument();
    });

    it('renders track actual income checkbox', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('dialog')).toBeInTheDocument();
      });

      // Navigate to Income tab
      await user.click(screen.getByRole('tab', { name: /income/i }));

      // Should see the track actual income checkbox
      await waitFor(() => {
        expect(screen.getByLabelText(/track actual income/i)).toBeInTheDocument();
      });

      // Should be unchecked by default
      expect(screen.getByLabelText(/track actual income/i)).not.toBeChecked();
    });

    it('includes income_sources in submission payload when sources are added', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('dialog')).toBeInTheDocument();
      });

      // Fill required budget fields first
      await user.type(screen.getByLabelText(/name/i), 'Test Budget');
      await user.clear(screen.getByLabelText(/amount/i));
      await user.type(screen.getByLabelText(/amount/i), '2000');

      // Navigate to Income tab
      await user.click(screen.getByRole('tab', { name: /income/i }));

      // Wait for Income tab content
      await waitFor(() => {
        expect(screen.getByLabelText(/source type/i)).toBeInTheDocument();
      });

      // Select Manual type and add an income source
      const typeSelector = screen.getByLabelText(/source type/i);
      await user.selectOptions(typeSelector, 'manual');

      // Fill manual amount
      const amountInput = screen.getByLabelText(/amount/i);
      await user.type(amountInput, '3000');

      // Click Add button
      await user.click(screen.getByRole('button', { name: /add income source/i }));

      // Submit the form
      await user.click(screen.getByRole('button', { name: /create budget/i }));

      // Verify createBudget was called with income_sources
      await waitFor(() => {
        expect(budgetsApi.createBudget).toHaveBeenCalledWith(
          expect.objectContaining({
            name: 'Test Budget',
            amount: 2000,
            income_sources: [
              expect.objectContaining({
                type: 'manual',
                value: '3000',
              }),
            ],
          })
        );
      });
    }, 20000);

    it('resets Income tab when dialog reopens', async () => {
      const user = userEvent.setup({ delay: null });
      const { rerender } = render(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('dialog')).toBeInTheDocument();
      });

      // Navigate to Income tab
      await user.click(screen.getByRole('tab', { name: /income/i }));

      // Wait for Income tab content
      await waitFor(() => {
        expect(screen.getByLabelText(/source type/i)).toBeInTheDocument();
      });

      // Add a manual income source
      const typeSelector = screen.getByLabelText(/source type/i);
      await user.selectOptions(typeSelector, 'manual');
      const amountInput = screen.getByLabelText(/amount/i);
      await user.type(amountInput, '1000');
      await user.click(screen.getByRole('button', { name: /add income source/i }));

      // Verify income source was added (no longer shows empty state)
      await waitFor(() => {
        expect(screen.queryByText(/no income sources configured/i)).not.toBeInTheDocument();
      });

      // Close dialog
      rerender(
        <CreateBudgetDialog
          isOpen={false}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      // Reopen dialog
      rerender(
        <CreateBudgetDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onBudgetCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('dialog')).toBeInTheDocument();
      });

      // Should be back on Budget tab (reset)
      expect(screen.getByLabelText(/name/i)).toBeInTheDocument();

      // Navigate to Income tab
      await user.click(screen.getByRole('tab', { name: /income/i }));

      // Income sources should be reset (empty state)
      await waitFor(() => {
        expect(screen.getByText(/no income sources configured/i)).toBeInTheDocument();
      });
    }, 20000);
  });
});
