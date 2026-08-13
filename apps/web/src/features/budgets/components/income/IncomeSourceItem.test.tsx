import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { IncomeSourceItem } from './IncomeSourceItem'
import type { IncomeSource } from '../../types'
import { scanForUnmaskedPII, type PIISentinel } from '@/test/pii-masking'

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
    it('has an accessible remove button whose name carries the tag value via aria-labelledby, not aria-label', () => {
      // Issue #1514: the value must not appear in any ATTRIBUTE -- aria-label is
      // recorded verbatim by session replay and cannot be masked like element text.
      // The accessible name now comes from a referenced sr-only span's text (see
      // AccountCard.tsx pattern), so the value is free to appear in the computed name.
      const source: IncomeSource = {
        type: 'tag',
        value: 'Freelance',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      const removeButton = screen.getByRole('button', { name: /remove freelance tag income source/i });
      expect(removeButton).toBeInTheDocument();
      expect(removeButton.getAttribute('aria-label')).toBeNull();
      expect(removeButton.getAttribute('aria-labelledby')).not.toContain('Freelance');
    });

    it('has an accessible remove button whose name carries the category value for pfc_primary sources', () => {
      const source: IncomeSource = {
        type: 'pfc_primary',
        value: 'INCOME',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      const removeButton = screen.getByRole('button', { name: /remove income category income source/i });
      expect(removeButton).toBeInTheDocument();
    });

    it('gives two rows of the same type but different values distinct accessible names (WCAG 2.1 AA rotor navigation)', () => {
      // Issue #1514 follow-up: a generic value-free label made byte-identical accessible
      // names for every row of a given type, which is invisible to linear reading (list
      // semantics + adjacent text rescue it) but breaks rotor/"buttons list" navigation,
      // where only the control's own accessible name is shown.
      const sourceA: IncomeSource = { type: 'tag', value: 'Freelance' };
      const sourceB: IncomeSource = { type: 'tag', value: 'Consulting' };

      const { unmount } = render(<IncomeSourceItem source={sourceA} onRemove={mockOnRemove} />);
      const buttonA = screen.getByRole('button', { name: /remove/i });
      const nameA = buttonA.getAttribute('aria-labelledby')
        ? document.getElementById(buttonA.getAttribute('aria-labelledby')!)?.textContent
        : buttonA.getAttribute('aria-label');
      unmount();

      render(<IncomeSourceItem source={sourceB} onRemove={mockOnRemove} />);
      const buttonB = screen.getByRole('button', { name: /remove/i });
      const nameB = buttonB.getAttribute('aria-labelledby')
        ? document.getElementById(buttonB.getAttribute('aria-labelledby')!)?.textContent
        : buttonB.getAttribute('aria-label');

      expect(nameA).toBeTruthy();
      expect(nameB).toBeTruthy();
      expect(nameA).not.toEqual(nameB);
    });

    it('backs the accessible name with an sr-only, data-ph-mask span whose text (not any attribute) carries the value', () => {
      const source: IncomeSource = {
        type: 'tag',
        value: 'ZzDistinctSentinelZz',
      };

      render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />);

      const removeButton = screen.getByRole('button', { name: /remove/i });
      const labelledbyId = removeButton.getAttribute('aria-labelledby');
      expect(labelledbyId).toBeTruthy();
      expect(labelledbyId).not.toContain('ZzDistinctSentinelZz');

      const labelSpan = document.getElementById(labelledbyId!);
      expect(labelSpan).not.toBeNull();
      expect(labelSpan).toHaveClass('sr-only');
      expect(labelSpan!.getAttribute('data-ph-mask')).not.toBeNull();
      expect(labelSpan!.textContent).toContain('ZzDistinctSentinelZz');

      // No attribute anywhere on the button carries the value.
      const values = Array.from(removeButton.attributes).map((a) => a.value);
      expect(values.some((v) => v.includes('ZzDistinctSentinelZz'))).toBe(false);
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

describe('remove button accessible name excludes PII (issue #1514: aria-label is unmaskable)', () => {
  const mockOnRemove = mock(() => {})

  // Every DOM attribute on the button -- not just aria-label -- must be free of the PII.
  // aria-label text is recorded verbatim by PostHog session replay and data-ph-mask only
  // covers element TEXT content, never attribute values.
  function attributeValues(el: Element): string[] {
    return Array.from(el.attributes).map((a) => a.value)
  }

  it('does not include the user-entered tag value in any DOM attribute on the remove button', () => {
    const source: IncomeSource = {
      type: 'tag',
      value: 'ZzTagSentinelZz-Freelance-Client-Acme',
    }

    render(<IncomeSourceItem source={source} onRemove={mockOnRemove} />)

    const button = screen.getByRole('button', { name: /remove/i })
    const values = attributeValues(button)
    expect(values.some((v) => v.includes('ZzTagSentinelZz-Freelance-Client-Acme'))).toBe(false)

    // Scope the scanner to the button itself: the visible tag text (a separate span) is
    // intentionally left unmasked per the code comment above data-ph-mask at ~line 73
    // ("pfc/tag labels aren't financial PII") -- that policy is out of scope here. This
    // test is only about the button's own attributes, which must never carry the value.
    const sentinels: PIISentinel[] = [
      { value: 'ZzTagSentinelZz-Freelance-Client-Acme', fieldDescription: 'source.value (tag)' },
    ]
    expect(scanForUnmaskedPII(button, sentinels)).toEqual([])
  })
})

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
