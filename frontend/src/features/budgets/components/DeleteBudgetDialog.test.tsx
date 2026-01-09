/**
 * DeleteBudgetDialog Tests
 * Issue #521: Enable Budget and Savings Goal Deletion in UI
 *
 * Tests the confirmation dialog for deleting budgets.
 * Includes name confirmation validation - user must type exact budget name to enable Delete.
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DeleteBudgetDialog } from './DeleteBudgetDialog'

describe('DeleteBudgetDialog', () => {
  const mockOnConfirm = mock(() => {})
  const mockOnCancel = mock(() => {})

  const defaultProps = {
    isOpen: true,
    budgetName: 'Monthly Groceries',
    categoryCount: 5,
    onConfirm: mockOnConfirm,
    onCancel: mockOnCancel,
    isDeleting: false,
  }

  beforeEach(() => {
    mockOnConfirm.mockClear()
    mockOnCancel.mockClear()
  })

  describe('Display', () => {
    it('should render when open', () => {
      render(<DeleteBudgetDialog {...defaultProps} />);

      expect(screen.getByText('Delete Budget')).toBeInTheDocument();
    });

    it('should not render when closed', () => {
      render(<DeleteBudgetDialog {...defaultProps} isOpen={false} />);

      expect(screen.queryByText('Delete Budget')).not.toBeInTheDocument();
    });

    it('should display budget name in message', () => {
      render(<DeleteBudgetDialog {...defaultProps} />);

      // The message paragraph contains the budget name
      expect(screen.getByText(/delete "Monthly Groceries"\?/i)).toBeInTheDocument();
    });

    it('should display category count warning with plural', () => {
      render(<DeleteBudgetDialog {...defaultProps} categoryCount={5} />);

      expect(screen.getByText(/5 categories/)).toBeInTheDocument();
    });

    it('should display singular category when count is 1', () => {
      render(<DeleteBudgetDialog {...defaultProps} categoryCount={1} />);

      expect(screen.getByText(/1 category/)).toBeInTheDocument();
    });

    it('should display zero categories message', () => {
      render(<DeleteBudgetDialog {...defaultProps} categoryCount={0} />);

      expect(screen.getByText(/0 categories/)).toBeInTheDocument();
    });

    it('should display permanent deletion warning', () => {
      render(<DeleteBudgetDialog {...defaultProps} />);

      expect(screen.getByText(/permanently deleted/i)).toBeInTheDocument();
    });

    it('should display confirmation input instructions', () => {
      render(<DeleteBudgetDialog {...defaultProps} />);

      // The label contains the instruction text with budget name
      const label = screen.getByText(/To confirm deletion, type/i);
      expect(label).toBeInTheDocument();
      expect(label.textContent).toContain('Monthly Groceries');
    });

    it('should have a confirmation text input', () => {
      render(<DeleteBudgetDialog {...defaultProps} />);

      expect(screen.getByPlaceholderText(/type name to confirm/i)).toBeInTheDocument();
    });
  });

  describe('Name Confirmation Validation', () => {
    it('should have Delete button disabled by default', () => {
      render(<DeleteBudgetDialog {...defaultProps} />);

      const deleteButton = screen.getByRole('button', { name: /delete/i });
      expect(deleteButton).toBeDisabled();
    });

    it('should keep Delete button disabled when input is partial', async () => {
      const user = userEvent.setup();
      render(<DeleteBudgetDialog {...defaultProps} />);

      const input = screen.getByPlaceholderText(/type name to confirm/i);
      await user.type(input, 'Monthly');

      const deleteButton = screen.getByRole('button', { name: /delete/i });
      expect(deleteButton).toBeDisabled();
    });

    it('should keep Delete button disabled when input has wrong case', async () => {
      const user = userEvent.setup();
      render(<DeleteBudgetDialog {...defaultProps} />);

      const input = screen.getByPlaceholderText(/type name to confirm/i);
      await user.type(input, 'monthly groceries');

      const deleteButton = screen.getByRole('button', { name: /delete/i });
      expect(deleteButton).toBeDisabled();
    });

    it('should enable Delete button when input matches exactly', async () => {
      const user = userEvent.setup();
      render(<DeleteBudgetDialog {...defaultProps} />);

      const input = screen.getByPlaceholderText(/type name to confirm/i);
      await user.type(input, 'Monthly Groceries');

      const deleteButton = screen.getByRole('button', { name: /delete/i });
      expect(deleteButton).toBeEnabled();
    });

    it('should disable Delete button when input is cleared after matching', async () => {
      const user = userEvent.setup();
      render(<DeleteBudgetDialog {...defaultProps} />);

      const input = screen.getByPlaceholderText(/type name to confirm/i);
      await user.type(input, 'Monthly Groceries');
      await user.clear(input);

      const deleteButton = screen.getByRole('button', { name: /delete/i });
      expect(deleteButton).toBeDisabled();
    });

    it('should clear input when dialog closes and reopens', async () => {
      const user = userEvent.setup();
      const { rerender } = render(<DeleteBudgetDialog {...defaultProps} />);

      // Type in input
      const input = screen.getByPlaceholderText(/type name to confirm/i);
      await user.type(input, 'Monthly Groceries');

      // Close dialog via cancel button (triggers handleOpenChange which clears input)
      const cancelButton = screen.getByRole('button', { name: /cancel/i });
      await user.click(cancelButton);

      // Reopen dialog
      rerender(<DeleteBudgetDialog {...defaultProps} isOpen={true} />);

      // Input should be cleared
      const newInput = screen.getByPlaceholderText(/type name to confirm/i);
      expect(newInput).toHaveValue('');
    });
  });

  describe('Actions', () => {
    it('should call onConfirm when Delete button clicked after confirmation', async () => {
      const user = userEvent.setup();
      render(<DeleteBudgetDialog {...defaultProps} />);

      const input = screen.getByPlaceholderText(/type name to confirm/i);
      await user.type(input, 'Monthly Groceries');

      const deleteButton = screen.getByRole('button', { name: /delete/i });
      await user.click(deleteButton);

      expect(mockOnConfirm).toHaveBeenCalledTimes(1);
    }, 60000); // Extended timeout for CI

    it('should not call onConfirm when Delete button is disabled', async () => {
      const user = userEvent.setup();
      render(<DeleteBudgetDialog {...defaultProps} />);

      const deleteButton = screen.getByRole('button', { name: /delete/i });
      // Try to click disabled button
      await user.click(deleteButton);

      expect(mockOnConfirm).not.toHaveBeenCalled();
    });

    it('should call onCancel when Cancel button clicked', async () => {
      const user = userEvent.setup();
      render(<DeleteBudgetDialog {...defaultProps} />);

      const cancelButton = screen.getByRole('button', { name: /cancel/i });
      await user.click(cancelButton);

      expect(mockOnCancel).toHaveBeenCalledTimes(1);
    });

    it('should always have Cancel button enabled', () => {
      render(<DeleteBudgetDialog {...defaultProps} />);

      const cancelButton = screen.getByRole('button', { name: /cancel/i });
      expect(cancelButton).toBeEnabled();
    });
  });

  describe('Loading State', () => {
    it('should disable Delete button when isDeleting is true even with correct input', async () => {
      const user = userEvent.setup();
      const { rerender } = render(<DeleteBudgetDialog {...defaultProps} />);

      const input = screen.getByPlaceholderText(/type name to confirm/i);
      await user.type(input, 'Monthly Groceries');

      rerender(<DeleteBudgetDialog {...defaultProps} isDeleting={true} />);

      const deleteButton = screen.getByRole('button', { name: /deleting/i });
      expect(deleteButton).toBeDisabled();
    });

    it('should disable Cancel button when isDeleting is true', () => {
      render(<DeleteBudgetDialog {...defaultProps} isDeleting={true} />);

      const cancelButton = screen.getByRole('button', { name: /cancel/i });
      expect(cancelButton).toBeDisabled();
    });

    it('should show loading text when isDeleting is true', () => {
      render(<DeleteBudgetDialog {...defaultProps} isDeleting={true} />);

      expect(screen.getByText(/deleting/i)).toBeInTheDocument();
    });

    it('should disable the confirmation input when isDeleting is true', () => {
      render(<DeleteBudgetDialog {...defaultProps} isDeleting={true} />);

      const input = screen.getByPlaceholderText(/type name to confirm/i);
      expect(input).toBeDisabled();
    });
  });

  describe('Accessibility', () => {
    it('should have dialog role', () => {
      render(<DeleteBudgetDialog {...defaultProps} />);

      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    it('should have proper heading', () => {
      render(<DeleteBudgetDialog {...defaultProps} />);

      expect(
        screen.getByRole('heading', { name: 'Delete Budget' })
      ).toBeInTheDocument();
    });

    it('should have labeled text input', () => {
      render(<DeleteBudgetDialog {...defaultProps} />);

      const input = screen.getByPlaceholderText(/type name to confirm/i);
      expect(input).toHaveAttribute('aria-label', 'Confirmation input');
    });
  });
});
