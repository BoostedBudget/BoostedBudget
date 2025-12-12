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
// Test utilities available: import { fillInputByLabel, selectOptionByLabel } from '@/test/utils/formTestHelpers';

// Mock APIs
vi.mock('../api/budgetsApi');

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

      // Fill form
      await user.type(screen.getByLabelText(/name/i), 'Test Budget');
      await user.clear(screen.getByLabelText(/amount/i));
      await user.type(screen.getByLabelText(/amount/i), '1000');

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
      const user = userEvent.setup({ delay: null });
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

      // Fill name
      await user.type(screen.getByLabelText(/name/i), 'Test Budget');

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
});
