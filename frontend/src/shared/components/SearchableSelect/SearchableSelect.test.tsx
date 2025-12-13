import { useState } from 'react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { SearchableSelect } from './SearchableSelect'
import type { SearchableSelectOption, SearchableSelectProps } from './SearchableSelect'

// Wrapper for controlled component tests
function ControlledSearchableSelect(props: Omit<SearchableSelectProps, 'value' | 'onChange'> & { initialValue?: string | null; onChange?: (value: string | null) => void }) {
  const [value, setValue] = useState<string | null>(props.initialValue ?? null)
  return (
    <SearchableSelect
      {...props}
      value={value}
      onChange={(newValue) => {
        setValue(newValue)
        props.onChange?.(newValue)
      }}
    />
  )
}

const mockOptions: SearchableSelectOption[] = [
  { value: 'cat-1', label: 'Food & Drink' },
  { value: 'cat-2', label: 'Shopping' },
  { value: 'cat-3', label: 'Transportation' },
  { value: 'cat-4', label: 'Entertainment' },
  { value: 'cat-5', label: 'Food & Drink > Coffee', sublabel: 'Subcategory' },
]

const mockAccountOptions: SearchableSelectOption[] = [
  { value: 'acc-1', label: 'Checking Account', sublabel: 'depository ••••1234' },
  { value: 'acc-2', label: 'Savings Account', sublabel: 'depository ••••5678' },
  { value: 'acc-3', label: 'Credit Card', sublabel: 'credit ••••9012' },
]

describe('SearchableSelect', () => {
  const defaultProps = {
    options: mockOptions,
    value: null,
    onChange: vi.fn(),
    placeholder: 'Search...',
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render with placeholder when no selection', () => {
      render(<SearchableSelect {...defaultProps} />)

      const input = screen.getByPlaceholderText('Search...')
      expect(input).toBeInTheDocument()
      expect(input).toHaveValue('')
    })

    it('should display selected value label when provided', () => {
      render(<SearchableSelect {...defaultProps} value="cat-1" />)

      const input = screen.getByRole('combobox')
      expect(input).toHaveValue('Food & Drink')
    })

    it('should show dropdown when input is focused', async () => {
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} />)

      const input = screen.getByRole('combobox')
      await user.click(input)

      expect(screen.getByRole('listbox')).toBeInTheDocument()
      expect(screen.getAllByRole('option')).toHaveLength(mockOptions.length)
    })

    it('should hide dropdown when clicking outside', async () => {
      const user = userEvent.setup({ delay: null })
      render(
        <div>
          <SearchableSelect {...defaultProps} />
          <button>Outside</button>
        </div>
      )

      const input = screen.getByRole('combobox')
      await user.click(input)

      expect(screen.getByRole('listbox')).toBeInTheDocument()

      await user.click(screen.getByText('Outside'))

      await waitFor(() => {
        expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
      })
    })

    it('should render options with sublabels when provided', async () => {
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} options={mockAccountOptions} />)

      const input = screen.getByRole('combobox')
      await user.click(input)

      expect(screen.getByText('Checking Account')).toBeInTheDocument()
      expect(screen.getByText('depository ••••1234')).toBeInTheDocument()
    })
  })

  describe('Filtering', () => {
    it('should filter options based on search text', async () => {
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} />)

      const input = screen.getByRole('combobox')
      await user.click(input)
      await user.type(input, 'food')

      await waitFor(() => {
        // Should show Food & Drink and Food & Drink > Coffee
        expect(screen.getAllByRole('option')).toHaveLength(2)
      })
    })

    it('should show all options when search is empty', async () => {
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} />)

      const input = screen.getByRole('combobox')
      await user.click(input)

      expect(screen.getAllByRole('option')).toHaveLength(mockOptions.length)
    })

    it('should show "No results" when filter matches nothing', async () => {
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} />)

      const input = screen.getByRole('combobox')
      await user.click(input)
      await user.type(input, 'xyz123')

      await waitFor(() => {
        expect(screen.getByText('No results found')).toBeInTheDocument()
      })
    })

    it('should be case-insensitive when filtering', async () => {
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} />)

      const input = screen.getByRole('combobox')
      await user.click(input)
      await user.type(input, 'FOOD')

      await waitFor(() => {
        // Should still find Food & Drink options
        expect(screen.getAllByRole('option').length).toBeGreaterThan(0)
        expect(screen.getByText('Food & Drink')).toBeInTheDocument()
      })
    })

    it('should filter by sublabel as well', async () => {
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} options={mockAccountOptions} />)

      const input = screen.getByRole('combobox')
      await user.click(input)
      await user.type(input, 'credit')

      await waitFor(() => {
        expect(screen.getAllByRole('option')).toHaveLength(1)
        expect(screen.getByText('Credit Card')).toBeInTheDocument()
      })
    })
  })

  describe('Selection', () => {
    it('should call onChange with selected value on click', async () => {
      const onChange = vi.fn()
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} onChange={onChange} />)

      const input = screen.getByRole('combobox')
      await user.click(input)
      await user.click(screen.getByText('Shopping'))

      expect(onChange).toHaveBeenCalledWith('cat-2')
    })

    it('should close dropdown after selection', async () => {
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} />)

      const input = screen.getByRole('combobox')
      await user.click(input)
      await user.click(screen.getByText('Shopping'))

      await waitFor(() => {
        expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
      })
    })

    it('should clear search text after selection', async () => {
      const user = userEvent.setup({ delay: null })
      // Use controlled wrapper to properly test selection updating the value
      render(<ControlledSearchableSelect options={mockOptions} placeholder="Search..." />)

      const input = screen.getByRole('combobox')
      await user.click(input)
      await user.type(input, 'shop')
      await user.click(screen.getByText('Shopping'))

      // After selection, input should show selected label, not search text
      await waitFor(() => {
        expect(input).toHaveValue('Shopping')
      })
    })

    it('should allow clearing selection with empty option', async () => {
      const onChange = vi.fn()
      const user = userEvent.setup({ delay: null })
      render(
        <SearchableSelect
          {...defaultProps}
          value="cat-1"
          onChange={onChange}
          allowClear
          clearLabel="No selection"
        />
      )

      const input = screen.getByRole('combobox')
      await user.click(input)
      await user.click(screen.getByText('No selection'))

      expect(onChange).toHaveBeenCalledWith(null)
    })
  })

  describe('Keyboard Navigation', () => {
    it('should navigate options with arrow keys', async () => {
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} />)

      const input = screen.getByRole('combobox')
      await user.click(input)

      // Press down arrow
      await user.keyboard('{ArrowDown}')

      const options = screen.getAllByRole('option')
      expect(options[0]).toHaveClass('searchable-select__option--highlighted')

      // Press down arrow again
      await user.keyboard('{ArrowDown}')
      expect(options[1]).toHaveClass('searchable-select__option--highlighted')

      // Press up arrow
      await user.keyboard('{ArrowUp}')
      expect(options[0]).toHaveClass('searchable-select__option--highlighted')
    })

    it('should select highlighted option on Enter', async () => {
      const onChange = vi.fn()
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} onChange={onChange} />)

      const input = screen.getByRole('combobox')
      await user.click(input)
      await user.keyboard('{ArrowDown}')
      await user.keyboard('{Enter}')

      expect(onChange).toHaveBeenCalledWith('cat-1')
    })

    it('should close dropdown on Escape', async () => {
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} />)

      const input = screen.getByRole('combobox')
      await user.click(input)

      expect(screen.getByRole('listbox')).toBeInTheDocument()

      await user.keyboard('{Escape}')

      await waitFor(() => {
        expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
      })
    })

    it('should wrap around when navigating past end', async () => {
      const shortOptions = [
        { value: '1', label: 'First' },
        { value: '2', label: 'Second' },
      ]
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} options={shortOptions} />)

      const input = screen.getByRole('combobox')
      await user.click(input)

      // Navigate to second option
      await user.keyboard('{ArrowDown}')
      await user.keyboard('{ArrowDown}')

      // Navigate down again should wrap to first
      await user.keyboard('{ArrowDown}')

      const options = screen.getAllByRole('option')
      expect(options[0]).toHaveClass('searchable-select__option--highlighted')
    })
  })

  describe('Accessibility', () => {
    it('should have proper ARIA roles', async () => {
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} aria-label="Select category" />)

      const input = screen.getByRole('combobox')
      expect(input).toHaveAttribute('aria-label', 'Select category')
      expect(input).toHaveAttribute('aria-expanded', 'false')

      await user.click(input)

      expect(input).toHaveAttribute('aria-expanded', 'true')
      expect(screen.getByRole('listbox')).toBeInTheDocument()
      expect(screen.getAllByRole('option').length).toBeGreaterThan(0)
    })

    it('should have aria-activedescendant when option is highlighted', async () => {
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} />)

      const input = screen.getByRole('combobox')
      await user.click(input)
      await user.keyboard('{ArrowDown}')

      expect(input).toHaveAttribute('aria-activedescendant')
    })

    it('should mark selected option with aria-selected', async () => {
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} value="cat-2" />)

      const input = screen.getByRole('combobox')
      await user.click(input)

      const selectedOption = screen.getByText('Shopping').closest('[role="option"]')
      expect(selectedOption).toHaveAttribute('aria-selected', 'true')
    })
  })

  describe('Edge Cases', () => {
    it('should handle empty options array', async () => {
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} options={[]} />)

      const input = screen.getByRole('combobox')
      await user.click(input)

      expect(screen.getByText('No results found')).toBeInTheDocument()
    })

    it('should handle disabled state', () => {
      render(<SearchableSelect {...defaultProps} disabled />)

      const input = screen.getByRole('combobox')
      expect(input).toBeDisabled()
    })

    it('should not open dropdown when disabled', async () => {
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} disabled />)

      const input = screen.getByRole('combobox')
      await user.click(input)

      expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    })

    it('should handle very long option labels', async () => {
      const longOptions = [
        { value: '1', label: 'This is a very long category name that should be handled properly without breaking the layout' },
      ]
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} options={longOptions} />)

      const input = screen.getByRole('combobox')
      await user.click(input)

      expect(screen.getByRole('option')).toBeInTheDocument()
    })

    it('should handle value that does not exist in options', () => {
      render(<SearchableSelect {...defaultProps} value="non-existent" />)

      const input = screen.getByRole('combobox')
      // Should show empty or placeholder when value doesn't match any option
      expect(input).toHaveValue('')
    })

    it('should handle special characters in search', async () => {
      const specialOptions = [
        { value: '1', label: 'Food & Drink' },
        { value: '2', label: 'ATM/Cash' },
      ]
      const user = userEvent.setup({ delay: null })
      render(<SearchableSelect {...defaultProps} options={specialOptions} />)

      const input = screen.getByRole('combobox')
      await user.click(input)
      await user.type(input, '&')

      await waitFor(() => {
        expect(screen.getByText('Food & Drink')).toBeInTheDocument()
      })
    })
  })

  describe('Custom className', () => {
    it('should apply custom className', () => {
      render(<SearchableSelect {...defaultProps} className="my-custom-class" />)

      const container = screen.getByRole('combobox').closest('.searchable-select')
      expect(container).toHaveClass('my-custom-class')
    })
  })
})
