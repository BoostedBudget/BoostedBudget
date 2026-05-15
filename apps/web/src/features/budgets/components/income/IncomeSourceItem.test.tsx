import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { IncomeSourceItem } from './IncomeSourceItem'
import type { IncomeSource } from '../../types'

describe('IncomeSourceItem', () => {
  const mockOnRemove = mock(() => {})

  beforeEach(() => {
    mockOnRemove.mockClear()
  })

  describe('Rendering', () => {
    it('renders pfc_primary source with category icon and formatted value', () => {
      const source: IncomeSource = {
        type: 'pfc_primary',
        value: 'INCOME',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      expect(screen.getByText('Income')).toBeInTheDocument();
      expect(screen.getByTestId('income-source-icon')).toBeInTheDocument();
    });

    it('prefers server-supplied value_display over local formatter for pfc_primary', () => {
      const source: IncomeSource = {
        type: 'pfc_primary',
        value: 'INCOME_WAGES',
        value_display: 'Wages',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      expect(screen.getByText('Wages')).toBeInTheDocument();
      expect(screen.queryByText('Income Wages')).not.toBeInTheDocument();
    });



    it('renders tag source with tag icon and value', () => {
      const source: IncomeSource = {
        type: 'tag',
        value: 'Freelance',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      expect(screen.getByText('Freelance')).toBeInTheDocument();
      expect(screen.getByTestId('income-source-icon')).toBeInTheDocument();
    });

    it('renders manual source with dollar icon and formatted amount', () => {
      const source: IncomeSource = {
        type: 'manual',
        value: '3500',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      expect(screen.getByText('$3,500.00')).toBeInTheDocument();
      expect(screen.getByTestId('income-source-icon')).toBeInTheDocument();
    });

    it('displays description when provided', () => {
      const source: IncomeSource = {
        type: 'manual',
        value: '500',
        description: 'Side gig income',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      expect(screen.getByText('Side gig income')).toBeInTheDocument();
    });

    it('does not render description when not provided', () => {
      const source: IncomeSource = {
        type: 'pfc_primary',
        value: 'INCOME',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      // Should only have the formatted value, no description element
      expect(screen.queryByTestId('income-source-description')).not.toBeInTheDocument();
    });

    it('renders remove button', () => {
      const source: IncomeSource = {
        type: 'tag',
        value: 'Salary',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      expect(screen.getByRole('button', { name: /remove/i })).toBeInTheDocument();
    });
  });

  // ==========================================================================
  // Interaction Tests
  // ==========================================================================

  describe('Interactions', () => {
    it('calls onRemove when remove button is clicked', async () => {
      const user = userEvent.setup({ delay: null });
      const source: IncomeSource = {
        type: 'tag',
        value: 'Salary',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      await user.click(screen.getByRole('button', { name: /remove/i }));

      expect(mockOnRemove).toHaveBeenCalledTimes(1);
    });

    it('disables remove button when disabled prop is true', () => {
      const source: IncomeSource = {
        type: 'tag',
        value: 'Salary',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} disabled />);

      const removeButton = screen.getByRole('button', { name: /remove/i });
      expect(removeButton).toBeDisabled();
    });

    it('does not call onRemove when button is disabled and clicked', async () => {
      const user = userEvent.setup({ delay: null });
      const source: IncomeSource = {
        type: 'tag',
        value: 'Salary',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} disabled />);

      const removeButton = screen.getByRole('button', { name: /remove/i });
      await user.click(removeButton);

      expect(mockOnRemove).not.toHaveBeenCalled();
    });
  });

  // ==========================================================================
  // Accessibility Tests
  // ==========================================================================

  describe('Accessibility', () => {
    it('has accessible remove button with aria-label', () => {
      const source: IncomeSource = {
        type: 'tag',
        value: 'Freelance',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      const removeButton = screen.getByRole('button', { name: /remove.*freelance/i });
      expect(removeButton).toBeInTheDocument();
    });

    it('has accessible remove button for pfc_primary with formatted name', () => {
      const source: IncomeSource = {
        type: 'pfc_primary',
        value: 'INCOME',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      const removeButton = screen.getByRole('button', { name: /remove.*income/i });
      expect(removeButton).toBeInTheDocument();
    });


    it('manual source has no remove button (legacy read-only, tested in Legacy manual read-only suite)', () => {
      const source: IncomeSource = {
        type: 'manual',
        value: '2500',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      // Manual sources are read-only — no remove button, only a "legacy" pill
      expect(screen.queryByRole('button', { name: /remove/i })).not.toBeInTheDocument();
      expect(screen.getByText('legacy')).toBeInTheDocument();
    });
  });

  // ==========================================================================
  // Issue #1048: Legacy manual entries are read-only (R8)
  // ==========================================================================

  describe('Legacy manual read-only', () => {
    it('does not render remove button for manual source type', () => {
      const source: IncomeSource = {
        type: 'manual',
        value: '500',
        description: 'Side gig',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      expect(screen.queryByRole('button', { name: /remove/i })).not.toBeInTheDocument();
    });

    it('still renders value and description for manual source', () => {
      const source: IncomeSource = {
        type: 'manual',
        value: '500',
        description: 'Side gig',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      expect(screen.getByText('$500.00')).toBeInTheDocument();
      expect(screen.getByText('Side gig')).toBeInTheDocument();
    });

    it('shows legacy pill for manual source', () => {
      const source: IncomeSource = {
        type: 'manual',
        value: '1000',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      expect(screen.getByText('legacy')).toBeInTheDocument();
    });
  });

  // ==========================================================================
  // Edge Cases
  // ==========================================================================

  describe('Edge Cases', () => {
    it('handles zero amount for manual source', () => {
      const source: IncomeSource = {
        type: 'manual',
        value: '0',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      expect(screen.getByText('$0.00')).toBeInTheDocument();
    });

    it('handles decimal amounts for manual source', () => {
      const source: IncomeSource = {
        type: 'manual',
        value: '1234.56',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      expect(screen.getByText('$1,234.56')).toBeInTheDocument();
    });

    it('handles empty description gracefully', () => {
      const source: IncomeSource = {
        type: 'tag',
        value: 'Bonus',
        description: '',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      // Empty description should not render
      expect(screen.queryByTestId('income-source-description')).not.toBeInTheDocument();
    });
  });
});

describe('Unknown type fallback', () => {
  const mockOnRemove = mock(() => {})

  it('renders "Unknown source type" label for unrecognized type', () => {
    const source = {
      type: 'stale_unknown_type' as never,
      value: 'some_value',
    } as IncomeSource

    render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />)
    expect(screen.getByText('Unknown source type')).toBeDefined()
  })
})
