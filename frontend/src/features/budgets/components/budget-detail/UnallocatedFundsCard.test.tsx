/**
 * UnallocatedFundsCard Component Tests
 * Issue #498: Budget Detail Page for Envelope Budgeting
 *
 * TDD RED: These tests are written first and will initially fail.
 * Tests the unallocated funds display card with action buttons.
 */

import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { UnallocatedFundsCard } from './UnallocatedFundsCard';

/**
 * Helper to render with router context (for Link components)
 */
function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>);
}

describe('UnallocatedFundsCard', () => {
  const defaultProps = {
    unallocatedAmount: 900,
    totalIncome: 4000,
    currencyCode: 'USD',
  };

  describe('Amount Display', () => {
    it('should display unallocated amount formatted as currency', () => {
      renderWithRouter(<UnallocatedFundsCard {...defaultProps} />);

      expect(screen.getByText('$900.00')).toBeInTheDocument();
    });

    it('should show percentage of total income', () => {
      renderWithRouter(<UnallocatedFundsCard {...defaultProps} />);

      // 900 / 4000 * 100 = 22.5%, rounded to 23%
      expect(screen.getByText(/23%/)).toBeInTheDocument();
    });

    it('should format amounts with different currency codes', () => {
      renderWithRouter(
        <UnallocatedFundsCard
          {...defaultProps}
          currencyCode="EUR"
        />
      );

      expect(screen.getByText(/€900\.00/)).toBeInTheDocument();
    });

    it('should handle large unallocated amounts', () => {
      renderWithRouter(
        <UnallocatedFundsCard
          unallocatedAmount={50000}
          totalIncome={100000}
          currencyCode="USD"
        />
      );

      expect(screen.getByText('$50,000.00')).toBeInTheDocument();
      expect(screen.getByText(/50%/)).toBeInTheDocument();
    });

    it('should handle decimal amounts correctly', () => {
      renderWithRouter(
        <UnallocatedFundsCard
          unallocatedAmount={123.45}
          totalIncome={1000}
          currencyCode="USD"
        />
      );

      expect(screen.getByText('$123.45')).toBeInTheDocument();
    });
  });

  describe('Positive Funds (Available)', () => {
    it('should show positive styling when funds available', () => {
      const { container } = renderWithRouter(<UnallocatedFundsCard {...defaultProps} />);

      expect(container.querySelector('.unallocated-card--positive')).toBeInTheDocument();
    });

    it('should display "available" text for positive unallocated', () => {
      renderWithRouter(<UnallocatedFundsCard {...defaultProps} />);

      expect(screen.getByText(/available/i)).toBeInTheDocument();
    });

    it('should show descriptive text about unallocated funds', () => {
      renderWithRouter(<UnallocatedFundsCard {...defaultProps} />);

      expect(screen.getByText(/not assigned to any.*category/i)).toBeInTheDocument();
    });
  });

  describe('Zero Funds (Fully Allocated)', () => {
    it('should display "fully allocated" message when amount is 0', () => {
      renderWithRouter(
        <UnallocatedFundsCard
          unallocatedAmount={0}
          totalIncome={4000}
          currencyCode="USD"
        />
      );

      expect(screen.getByText(/fully allocated/i)).toBeInTheDocument();
    });

    it('should show neutral styling when fully allocated', () => {
      const { container } = renderWithRouter(
        <UnallocatedFundsCard
          unallocatedAmount={0}
          totalIncome={4000}
          currencyCode="USD"
        />
      );

      expect(container.querySelector('.unallocated-card--neutral')).toBeInTheDocument();
    });

    it('should show success indicator when fully allocated', () => {
      renderWithRouter(
        <UnallocatedFundsCard
          unallocatedAmount={0}
          totalIncome={4000}
          currencyCode="USD"
        />
      );

      expect(screen.getByTestId('allocation-complete-icon')).toBeInTheDocument();
    });
  });

  describe('Over-Allocated (Negative)', () => {
    it('should show warning styling when over-allocated (negative)', () => {
      const { container } = renderWithRouter(
        <UnallocatedFundsCard
          unallocatedAmount={-500}
          totalIncome={4000}
          currencyCode="USD"
        />
      );

      expect(container.querySelector('.unallocated-card--warning')).toBeInTheDocument();
    });

    it('should display "over-allocated" text for negative amounts', () => {
      renderWithRouter(
        <UnallocatedFundsCard
          unallocatedAmount={-500}
          totalIncome={4000}
          currencyCode="USD"
        />
      );

      expect(screen.getByText(/over-allocated/i)).toBeInTheDocument();
    });

    it('should show the over-allocation amount as positive', () => {
      renderWithRouter(
        <UnallocatedFundsCard
          unallocatedAmount={-500}
          totalIncome={4000}
          currencyCode="USD"
        />
      );

      // Should display the absolute value
      expect(screen.getByText('$500.00')).toBeInTheDocument();
    });
  });

  describe('Action Buttons', () => {
    it('should render "Create Savings Goal" button when callback provided', () => {
      const onCreateSavingsGoal = vi.fn();
      renderWithRouter(
        <UnallocatedFundsCard
          {...defaultProps}
          onCreateSavingsGoal={onCreateSavingsGoal}
        />
      );

      expect(screen.getByRole('button', { name: /create savings goal/i })).toBeInTheDocument();
    });

    it('should call onCreateSavingsGoal when button clicked', () => {
      const onCreateSavingsGoal = vi.fn();
      renderWithRouter(
        <UnallocatedFundsCard
          {...defaultProps}
          onCreateSavingsGoal={onCreateSavingsGoal}
        />
      );

      const button = screen.getByRole('button', { name: /create savings goal/i });
      fireEvent.click(button);

      expect(onCreateSavingsGoal).toHaveBeenCalledTimes(1);
    });

    it('should render "Allocate to Category" button when callback provided', () => {
      const onAllocateToCategory = vi.fn();
      renderWithRouter(
        <UnallocatedFundsCard
          {...defaultProps}
          onAllocateToCategory={onAllocateToCategory}
        />
      );

      expect(screen.getByRole('button', { name: /allocate to category/i })).toBeInTheDocument();
    });

    it('should call onAllocateToCategory when button clicked', () => {
      const onAllocateToCategory = vi.fn();
      renderWithRouter(
        <UnallocatedFundsCard
          {...defaultProps}
          onAllocateToCategory={onAllocateToCategory}
        />
      );

      const button = screen.getByRole('button', { name: /allocate to category/i });
      fireEvent.click(button);

      expect(onAllocateToCategory).toHaveBeenCalledTimes(1);
    });

    it('should not show action buttons when callbacks not provided', () => {
      renderWithRouter(<UnallocatedFundsCard {...defaultProps} />);

      expect(screen.queryByRole('button', { name: /create savings goal/i })).not.toBeInTheDocument();
      expect(screen.queryByRole('button', { name: /allocate to category/i })).not.toBeInTheDocument();
    });

    it('should hide action buttons when over-allocated', () => {
      const onCreateSavingsGoal = vi.fn();
      renderWithRouter(
        <UnallocatedFundsCard
          unallocatedAmount={-500}
          totalIncome={4000}
          currencyCode="USD"
          onCreateSavingsGoal={onCreateSavingsGoal}
        />
      );

      // Action buttons should not appear when over-allocated
      expect(screen.queryByRole('button', { name: /create savings goal/i })).not.toBeInTheDocument();
    });
  });

  describe('Accessibility', () => {
    it('should have accessible card structure', () => {
      renderWithRouter(<UnallocatedFundsCard {...defaultProps} />);

      const card = screen.getByRole('region', { name: /unallocated/i });
      expect(card).toBeInTheDocument();
    });

    it('should have proper heading', () => {
      renderWithRouter(<UnallocatedFundsCard {...defaultProps} />);

      expect(screen.getByRole('heading', { name: /unallocated funds/i })).toBeInTheDocument();
    });

    it('should include screen reader text for percentage', () => {
      const { container } = renderWithRouter(<UnallocatedFundsCard {...defaultProps} />);

      // Percentage should be accessible
      const srText = container.querySelector('.sr-only');
      expect(srText).toBeInTheDocument();
    });

    it('should have proper contrast for warning state', () => {
      const { container } = renderWithRouter(
        <UnallocatedFundsCard
          unallocatedAmount={-500}
          totalIncome={4000}
          currencyCode="USD"
        />
      );

      // Warning state should have distinct styling
      expect(container.querySelector('.unallocated-card--warning')).toBeInTheDocument();
    });
  });

  describe('Section Title', () => {
    it('should display "UNALLOCATED FUNDS" as section title', () => {
      renderWithRouter(<UnallocatedFundsCard {...defaultProps} />);

      expect(screen.getByText(/unallocated funds/i)).toBeInTheDocument();
    });

    it('should include icon in header', () => {
      renderWithRouter(<UnallocatedFundsCard {...defaultProps} />);

      expect(screen.getByTestId('unallocated-icon')).toBeInTheDocument();
    });
  });

  describe('Edge Cases', () => {
    it('should handle zero total income gracefully', () => {
      renderWithRouter(
        <UnallocatedFundsCard
          unallocatedAmount={0}
          totalIncome={0}
          currencyCode="USD"
        />
      );

      // Should not crash and show reasonable output
      expect(screen.getByText(/unallocated funds/i)).toBeInTheDocument();
    });

    it('should handle very small percentages', () => {
      renderWithRouter(
        <UnallocatedFundsCard
          unallocatedAmount={1}
          totalIncome={10000}
          currencyCode="USD"
        />
      );

      // 1 / 10000 = 0.01% - should show at least "< 1%" or round appropriately
      expect(screen.getByText(/0%|<\s*1%/)).toBeInTheDocument();
    });

    it('should handle percentage over 100%', () => {
      renderWithRouter(
        <UnallocatedFundsCard
          unallocatedAmount={5000}
          totalIncome={4000}
          currencyCode="USD"
        />
      );

      // 5000 / 4000 = 125%
      expect(screen.getByText(/125%/)).toBeInTheDocument();
    });
  });
});
