/**
 * DeleteSavingsGoalDialog Tests
 * Issue #521: Enable Budget and Savings Goal Deletion in UI
 *
 * Tests the confirmation dialog for deleting savings goals.
 * Includes name confirmation validation - user must type exact goal name to enable Delete.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { DeleteSavingsGoalDialog } from './DeleteSavingsGoalDialog';

describe('DeleteSavingsGoalDialog', () => {
  const mockOnConfirm = vi.fn();
  const mockOnCancel = vi.fn();

  const defaultProps = {
    isOpen: true,
    goalName: 'Vacation Fund',
    currentAmount: 2500,
    targetAmount: 5000,
    currencyCode: 'USD',
    onConfirm: mockOnConfirm,
    onCancel: mockOnCancel,
    isDeleting: false,
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('Display', () => {
    it('should render when open', () => {
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      expect(screen.getByText('Delete Savings Goal')).toBeInTheDocument();
    });

    it('should not render when closed', () => {
      render(<DeleteSavingsGoalDialog {...defaultProps} isOpen={false} />);

      expect(screen.queryByText('Delete Savings Goal')).not.toBeInTheDocument();
    });

    it('should display goal name in message', () => {
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      expect(screen.getByText(/delete "Vacation Fund"\?/i)).toBeInTheDocument();
    });

    it('should display current amount saved', () => {
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      expect(screen.getByText(/\$2,500\.00/)).toBeInTheDocument();
    });

    it('should display target amount', () => {
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      expect(screen.getByText(/\$5,000\.00/)).toBeInTheDocument();
    });

    it('should display progress percentage', () => {
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      expect(screen.getByText(/50%/)).toBeInTheDocument();
    });

    it('should display confirmation input instructions', () => {
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      const label = screen.getByText(/To confirm deletion, type/i);
      expect(label).toBeInTheDocument();
      expect(label.textContent).toContain('Vacation Fund');
    });

    it('should have a confirmation text input', () => {
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      expect(screen.getByPlaceholderText(/type name to confirm/i)).toBeInTheDocument();
    });

    it('should handle zero progress gracefully', () => {
      render(
        <DeleteSavingsGoalDialog {...defaultProps} currentAmount={0} targetAmount={1000} />
      );

      expect(screen.getByText(/0%/)).toBeInTheDocument();
    });

    it('should handle completed goals (100% progress)', () => {
      render(
        <DeleteSavingsGoalDialog {...defaultProps} currentAmount={5000} targetAmount={5000} />
      );

      expect(screen.getByText(/100%/)).toBeInTheDocument();
    });
  });

  describe('Name Confirmation Validation', () => {
    it('should have Delete button disabled by default', () => {
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      const deleteButton = screen.getByRole('button', { name: /delete/i });
      expect(deleteButton).toBeDisabled();
    });

    it('should keep Delete button disabled when input is partial', async () => {
      const user = userEvent.setup();
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      const input = screen.getByPlaceholderText(/type name to confirm/i);
      await user.type(input, 'Vacation');

      const deleteButton = screen.getByRole('button', { name: /delete/i });
      expect(deleteButton).toBeDisabled();
    });

    it('should keep Delete button disabled when input has wrong case', async () => {
      const user = userEvent.setup();
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      const input = screen.getByPlaceholderText(/type name to confirm/i);
      await user.type(input, 'vacation fund');

      const deleteButton = screen.getByRole('button', { name: /delete/i });
      expect(deleteButton).toBeDisabled();
    });

    it('should enable Delete button when input matches exactly', async () => {
      const user = userEvent.setup();
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      const input = screen.getByPlaceholderText(/type name to confirm/i);
      await user.type(input, 'Vacation Fund');

      const deleteButton = screen.getByRole('button', { name: /delete/i });
      expect(deleteButton).toBeEnabled();
    });

    it('should disable Delete button when input is cleared after matching', async () => {
      const user = userEvent.setup();
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      const input = screen.getByPlaceholderText(/type name to confirm/i);
      await user.type(input, 'Vacation Fund');
      await user.clear(input);

      const deleteButton = screen.getByRole('button', { name: /delete/i });
      expect(deleteButton).toBeDisabled();
    });

    it('should clear input when dialog closes and reopens', async () => {
      const user = userEvent.setup();
      const { rerender } = render(<DeleteSavingsGoalDialog {...defaultProps} />);

      // Type in input
      const input = screen.getByPlaceholderText(/type name to confirm/i);
      await user.type(input, 'Vacation Fund');

      // Close dialog via cancel button (triggers handleOpenChange which clears input)
      const cancelButton = screen.getByRole('button', { name: /cancel/i });
      await user.click(cancelButton);

      // Reopen dialog
      rerender(<DeleteSavingsGoalDialog {...defaultProps} isOpen={true} />);

      // Input should be cleared
      const newInput = screen.getByPlaceholderText(/type name to confirm/i);
      expect(newInput).toHaveValue('');
    });
  });

  describe('Actions', () => {
    it('should call onConfirm when Delete button clicked after confirmation', async () => {
      const user = userEvent.setup();
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      const input = screen.getByPlaceholderText(/type name to confirm/i);
      await user.type(input, 'Vacation Fund');

      const deleteButton = screen.getByRole('button', { name: /delete/i });
      await user.click(deleteButton);

      expect(mockOnConfirm).toHaveBeenCalledTimes(1);
    });

    it('should not call onConfirm when Delete button is disabled', async () => {
      const user = userEvent.setup();
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      const deleteButton = screen.getByRole('button', { name: /delete/i });
      await user.click(deleteButton);

      expect(mockOnConfirm).not.toHaveBeenCalled();
    });

    it('should call onCancel when Cancel button clicked', async () => {
      const user = userEvent.setup();
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      const cancelButton = screen.getByRole('button', { name: /cancel/i });
      await user.click(cancelButton);

      expect(mockOnCancel).toHaveBeenCalledTimes(1);
    });

    it('should always have Cancel button enabled', () => {
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      const cancelButton = screen.getByRole('button', { name: /cancel/i });
      expect(cancelButton).toBeEnabled();
    });
  });

  describe('Loading State', () => {
    it('should disable Delete button when isDeleting is true even with correct input', async () => {
      const user = userEvent.setup();
      const { rerender } = render(<DeleteSavingsGoalDialog {...defaultProps} />);

      const input = screen.getByPlaceholderText(/type name to confirm/i);
      await user.type(input, 'Vacation Fund');

      rerender(<DeleteSavingsGoalDialog {...defaultProps} isDeleting={true} />);

      const deleteButton = screen.getByRole('button', { name: /deleting/i });
      expect(deleteButton).toBeDisabled();
    });

    it('should disable Cancel button when isDeleting is true', () => {
      render(<DeleteSavingsGoalDialog {...defaultProps} isDeleting={true} />);

      const cancelButton = screen.getByRole('button', { name: /cancel/i });
      expect(cancelButton).toBeDisabled();
    });

    it('should show loading text when isDeleting is true', () => {
      render(<DeleteSavingsGoalDialog {...defaultProps} isDeleting={true} />);

      expect(screen.getByText(/deleting/i)).toBeInTheDocument();
    });

    it('should disable the confirmation input when isDeleting is true', () => {
      render(<DeleteSavingsGoalDialog {...defaultProps} isDeleting={true} />);

      const input = screen.getByPlaceholderText(/type name to confirm/i);
      expect(input).toBeDisabled();
    });
  });

  describe('Accessibility', () => {
    it('should have dialog role', () => {
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    it('should have proper heading', () => {
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      expect(
        screen.getByRole('heading', { name: 'Delete Savings Goal' })
      ).toBeInTheDocument();
    });

    it('should have labeled text input', () => {
      render(<DeleteSavingsGoalDialog {...defaultProps} />);

      const input = screen.getByPlaceholderText(/type name to confirm/i);
      expect(input).toHaveAttribute('aria-label', 'Confirmation input');
    });
  });

  describe('Currency Formatting', () => {
    it('should format EUR currency correctly', () => {
      render(
        <DeleteSavingsGoalDialog
          {...defaultProps}
          currentAmount={1234.56}
          targetAmount={5000}
          currencyCode="EUR"
        />
      );

      // EUR formatting may vary by locale, but should show the value
      expect(screen.getByText(/1,234\.56|1\.234,56/)).toBeInTheDocument();
    });

    it('should handle GBP currency', () => {
      render(
        <DeleteSavingsGoalDialog
          {...defaultProps}
          currentAmount={999.99}
          targetAmount={2000}
          currencyCode="GBP"
        />
      );

      expect(screen.getByText(/999\.99/)).toBeInTheDocument();
    });
  });
});
