import { describe, it, expect } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { BudgetIncomeSection } from './BudgetIncomeSection'
import type { IncomeSource } from '../../types'

describe('BudgetIncomeSection', () => {
  const mockIncomeSources: IncomeSource[] = [
    {
      type: 'manual',
      value: '3500',
      description: 'Main Paycheck',
    },
    {
      type: 'manual',
      value: '500',
      description: 'Side Income',
    },
  ];

  const defaultProps = {
    expectedIncome: 4000,
    actualIncome: 3750,
    incomeSources: mockIncomeSources,
    currencyCode: 'USD',
  };

  describe('Income Display', () => {
    it('should display expected income total', () => {
      render(<BudgetIncomeSection {...defaultProps} />);

      // Use getByRole to find the stat label more specifically
      const expectedLabel = screen.getByText('Expected');
      expect(expectedLabel).toBeInTheDocument();
      expect(expectedLabel).toHaveClass('income-section__label');
      expect(screen.getByText('$4,000.00')).toBeInTheDocument();
    });

    it('should display actual income total', () => {
      render(<BudgetIncomeSection {...defaultProps} />);

      expect(screen.getByText(/actual/i)).toBeInTheDocument();
      expect(screen.getByText('$3,750.00')).toBeInTheDocument();
    });

    it('should display the difference between expected and actual', () => {
      render(<BudgetIncomeSection {...defaultProps} />);

      // Difference: 3750 - 4000 = -250
      expect(screen.getByText('-$250.00')).toBeInTheDocument();
    });

    it('should format amounts with different currency codes', () => {
      render(
        <BudgetIncomeSection
          {...defaultProps}
          currencyCode="EUR"
        />
      );

      // Euro formatting
      expect(screen.getByText(/€4,000\.00/)).toBeInTheDocument();
    });
  });

  describe('Surplus/Shortfall Indicators', () => {
    it('should show positive difference as surplus with green styling', () => {
      const { container } = render(
        <BudgetIncomeSection
          {...defaultProps}
          actualIncome={4500}
        />
      );

      // Surplus: 4500 - 4000 = +500
      expect(screen.getByText('+$500.00')).toBeInTheDocument();
      expect(container.querySelector('.income-section__difference--surplus')).toBeInTheDocument();
    });

    it('should show negative difference as shortfall with red styling', () => {
      const { container } = render(<BudgetIncomeSection {...defaultProps} />);

      // Shortfall: 3750 - 4000 = -250
      expect(screen.getByText('-$250.00')).toBeInTheDocument();
      expect(container.querySelector('.income-section__difference--shortfall')).toBeInTheDocument();
    });

    it('should show neutral styling when expected equals actual', () => {
      const { container } = render(
        <BudgetIncomeSection
          {...defaultProps}
          actualIncome={4000}
        />
      );

      expect(screen.getByText('$0.00')).toBeInTheDocument();
      expect(container.querySelector('.income-section__difference--neutral')).toBeInTheDocument();
    });
  });

  describe('Progress Bar', () => {
    it('should render progress bar with correct percentage', () => {
      render(<BudgetIncomeSection {...defaultProps} />);

      const progressbar = screen.getByRole('progressbar');
      expect(progressbar).toBeInTheDocument();
      // 3750 / 4000 * 100 = 93.75%, rounded to 94%
      expect(progressbar).toHaveAttribute('aria-valuenow', '94');
    });

    it('should cap progress bar at 100% even if actual > expected', () => {
      render(
        <BudgetIncomeSection
          {...defaultProps}
          actualIncome={5000}
        />
      );

      const progressbar = screen.getByRole('progressbar');
      // Visual should cap at 100%, but actual percentage shows 125%
      expect(progressbar).toHaveAttribute('aria-valuenow', '100');
    });

    it('should show 0% when actual income is 0', () => {
      render(
        <BudgetIncomeSection
          {...defaultProps}
          actualIncome={0}
        />
      );

      const progressbar = screen.getByRole('progressbar');
      expect(progressbar).toHaveAttribute('aria-valuenow', '0');
    });

    it('should handle zero expected income gracefully', () => {
      render(
        <BudgetIncomeSection
          {...defaultProps}
          expectedIncome={0}
          actualIncome={0}
        />
      );

      const progressbar = screen.getByRole('progressbar');
      expect(progressbar).toHaveAttribute('aria-valuenow', '0');
    });

    it('should display percentage text', () => {
      render(<BudgetIncomeSection {...defaultProps} />);

      expect(screen.getByText('94%')).toBeInTheDocument();
    });
  });

  describe('Income Sources List', () => {
    it('should list income sources with descriptions', () => {
      render(<BudgetIncomeSection {...defaultProps} />);

      expect(screen.getByText('Main Paycheck')).toBeInTheDocument();
      expect(screen.getByText('Side Income')).toBeInTheDocument();
    });

    it('should show expected amount for each manual income source', () => {
      render(<BudgetIncomeSection {...defaultProps} />);

      expect(screen.getByText(/\$3,500\.00.*expected/i)).toBeInTheDocument();
      expect(screen.getByText(/\$500\.00.*expected/i)).toBeInTheDocument();
    });

    it('should show checkmark icon for manual sources marked as received', () => {
      const sourcesWithReceived: IncomeSource[] = [
        {
          type: 'manual',
          value: '3500',
          description: 'Main Paycheck',
        },
      ];

      render(
        <BudgetIncomeSection
          expectedIncome={3500}
          actualIncome={3500}
          incomeSources={sourcesWithReceived}
          currencyCode="USD"
          sourceStatuses={{ 'Main Paycheck': 'received' }}
        />
      );

      expect(screen.getByTestId('income-source-received')).toBeInTheDocument();
    });

    it('should show partial amount for incomplete income', () => {
      render(
        <BudgetIncomeSection
          {...defaultProps}
          sourceStatuses={{ 'Side Income': 'partial' }}
          sourceActuals={{ 'Side Income': 250 }}
        />
      );

      // Side income: expected $500, actual $250
      expect(screen.getByText('$250.00')).toBeInTheDocument();
    });

    it('should hide sources section when no sources defined', () => {
      const { container } = render(
        <BudgetIncomeSection
          {...defaultProps}
          incomeSources={[]}
        />
      );

      expect(container.querySelector('.income-section__sources')).not.toBeInTheDocument();
    });

    it('should show source type indicator for pfc_primary sources', () => {
      const pfcSources: IncomeSource[] = [
        {
          type: 'pfc_primary',
          value: 'INCOME',
          description: 'Payroll deposits',
        },
      ];

      render(
        <BudgetIncomeSection
          expectedIncome={5000}
          actualIncome={5000}
          incomeSources={pfcSources}
          currencyCode="USD"
        />
      );

      expect(screen.getByText(/auto-tracked/i)).toBeInTheDocument();
    });

    it('should show source type indicator for plaid_category sources (backward compat)', () => {
      const plaidSources: IncomeSource[] = [
        {
          type: 'plaid_category',
          value: 'Income',
          description: 'Payroll deposits',
        },
      ];

      render(
        <BudgetIncomeSection
          expectedIncome={5000}
          actualIncome={5000}
          incomeSources={plaidSources}
          currencyCode="USD"
        />
      );

      expect(screen.getByText(/auto-tracked/i)).toBeInTheDocument();
    });

    it('should show source type indicator for tag sources', () => {
      const tagSources: IncomeSource[] = [
        {
          type: 'tag',
          value: 'freelance',
          description: 'Freelance Income',
        },
      ];

      render(
        <BudgetIncomeSection
          expectedIncome={1000}
          actualIncome={800}
          incomeSources={tagSources}
          currencyCode="USD"
        />
      );

      expect(screen.getByText(/tag:/i)).toBeInTheDocument();
    });
  });

  describe('Expandable', () => {
    it('should be collapsible/expandable', () => {
      render(<BudgetIncomeSection {...defaultProps} />);

      // Should have an expand/collapse button
      const toggleButton = screen.getByRole('button', { name: /income sources/i });
      expect(toggleButton).toBeInTheDocument();
    });

    it('should start expanded by default', () => {
      render(<BudgetIncomeSection {...defaultProps} />);

      // Sources should be visible by default
      expect(screen.getByText('Main Paycheck')).toBeVisible();
    });

    it('should collapse sources when toggle clicked', () => {
      const { container } = render(<BudgetIncomeSection {...defaultProps} />);

      const toggleButton = screen.getByRole('button', { name: /income sources/i });
      fireEvent.click(toggleButton);

      // Sources container should have collapsed class (display: none)
      const sourcesContainer = container.querySelector('.income-section__sources');
      expect(sourcesContainer).toHaveClass('income-section__sources--collapsed');
    });

    it('should expand sources when toggle clicked again', () => {
      const { container } = render(<BudgetIncomeSection {...defaultProps} />);

      const toggleButton = screen.getByRole('button', { name: /income sources/i });

      // Collapse
      fireEvent.click(toggleButton);
      const sourcesContainer = container.querySelector('.income-section__sources');
      expect(sourcesContainer).toHaveClass('income-section__sources--collapsed');

      // Expand
      fireEvent.click(toggleButton);
      expect(sourcesContainer).not.toHaveClass('income-section__sources--collapsed');
      expect(screen.getByText('Main Paycheck')).toBeInTheDocument();
    });

    it('should show expand indicator when collapsed', () => {
      render(<BudgetIncomeSection {...defaultProps} />);

      const toggleButton = screen.getByRole('button', { name: /income sources/i });
      fireEvent.click(toggleButton);

      expect(toggleButton).toHaveAttribute('aria-expanded', 'false');
    });

    it('should allow starting collapsed via prop', () => {
      const { container } = render(<BudgetIncomeSection {...defaultProps} defaultExpanded={false} />);

      const sourcesContainer = container.querySelector('.income-section__sources');
      expect(sourcesContainer).toHaveClass('income-section__sources--collapsed');
    });
  });

  describe('Accessibility', () => {
    it('should have section with aria-labelledby', () => {
      render(<BudgetIncomeSection {...defaultProps} />);

      const section = screen.getByRole('region', { name: /income/i });
      expect(section).toBeInTheDocument();
    });

    it('should have correct aria attributes on progress bar', () => {
      render(<BudgetIncomeSection {...defaultProps} />);

      const progressbar = screen.getByRole('progressbar');
      expect(progressbar).toHaveAttribute('aria-valuemin', '0');
      expect(progressbar).toHaveAttribute('aria-valuemax', '100');
      // Note: aria-label is "Income progress" (capital I)
      expect(progressbar).toHaveAttribute('aria-label', expect.stringMatching(/income/i));
    });

    it('should announce difference as surplus or shortfall for screen readers', () => {
      const { container } = render(<BudgetIncomeSection {...defaultProps} />);

      // Should have visually hidden text describing the difference type
      const srText = container.querySelector('.sr-only');
      expect(srText?.textContent).toMatch(/shortfall/i);
    });

    it('should have keyboard navigable expand/collapse', () => {
      render(<BudgetIncomeSection {...defaultProps} />);

      const toggleButton = screen.getByRole('button', { name: /income sources/i });
      toggleButton.focus();

      // Should be focusable
      expect(document.activeElement).toBe(toggleButton);
    });
  });

  describe('Section Header', () => {
    it('should display "INCOME" as section title', () => {
      render(<BudgetIncomeSection {...defaultProps} />);

      expect(screen.getByRole('heading', { name: /income/i })).toBeInTheDocument();
    });
  });

  describe('Edge Cases', () => {
    it('should handle very large amounts', () => {
      render(
        <BudgetIncomeSection
          expectedIncome={1000000}
          actualIncome={999999.99}
          incomeSources={[]}
          currencyCode="USD"
        />
      );

      expect(screen.getByText('$1,000,000.00')).toBeInTheDocument();
      expect(screen.getByText('$999,999.99')).toBeInTheDocument();
    });

    it('should handle decimal amounts correctly', () => {
      render(
        <BudgetIncomeSection
          expectedIncome={1000.50}
          actualIncome={1000.25}
          incomeSources={[]}
          currencyCode="USD"
        />
      );

      expect(screen.getByText('$1,000.50')).toBeInTheDocument();
      expect(screen.getByText('$1,000.25')).toBeInTheDocument();
    });

    it('should handle income sources without descriptions', () => {
      const sourcesWithoutDescription: IncomeSource[] = [
        {
          type: 'manual',
          value: '1000',
        },
      ];

      render(
        <BudgetIncomeSection
          expectedIncome={1000}
          actualIncome={1000}
          incomeSources={sourcesWithoutDescription}
          currencyCode="USD"
        />
      );

      // Should show the type as fallback (may appear in both name and type fields)
      const manualElements = screen.getAllByText(/manual/i);
      expect(manualElements.length).toBeGreaterThan(0);
    });
  });
});
