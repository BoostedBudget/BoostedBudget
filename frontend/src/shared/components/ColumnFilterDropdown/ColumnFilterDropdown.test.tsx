/**
 * ColumnFilterDropdown Component Tests
 * Issue #590: Enhanced Table Sorting and Inline Column Filtering
 *
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ColumnFilterDropdown } from './ColumnFilterDropdown'

describe('ColumnFilterDropdown', () => {
  beforeEach(() => {
    // mocks cleared individually
  })

  describe('Rendering', () => {
    it('should render filter button', () => {
      render(
        <ColumnFilterDropdown
          options={[{ value: 'opt1', label: 'Option 1' }]}
          onChange={() => {}}
        />
      )

      expect(screen.getByRole('button', { name: /filter/i })).toBeInTheDocument()
    })

    it('should show filter icon', () => {
      render(
        <ColumnFilterDropdown
          options={[{ value: 'opt1', label: 'Option 1' }]}
          onChange={() => {}}
        />
      )

      // Filter icon should be present (funnel icon)
      const button = screen.getByRole('button', { name: /filter/i })
      expect(button).toBeInTheDocument()
    })

    it('should show active indicator when filter is active', () => {
      render(
        <ColumnFilterDropdown
          options={[{ value: 'opt1', label: 'Option 1' }]}
          value={['opt1']}
          onChange={() => {}}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      expect(button).toHaveClass('column-filter__trigger--active')
    })

    it('should not show active indicator when filter is empty', () => {
      render(
        <ColumnFilterDropdown
          options={[{ value: 'opt1', label: 'Option 1' }]}
          value={[]}
          onChange={() => {}}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      expect(button).not.toHaveClass('column-filter__trigger--active')
    })
  })

  describe('Single Select Mode', () => {
    const options = [
      { value: 'checking', label: 'Checking' },
      { value: 'savings', label: 'Savings' },
      { value: 'credit', label: 'Credit Card' },
    ]

    it('should open dropdown when clicked', async () => {
      render(
        <ColumnFilterDropdown
          options={options}
          onChange={() => {}}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      await userEvent.click(button)

      expect(screen.getByText('Checking')).toBeInTheDocument()
      expect(screen.getByText('Savings')).toBeInTheDocument()
      expect(screen.getByText('Credit Card')).toBeInTheDocument()
    })

    it('should call onChange when option selected', async () => {
      const onChange = mock(() => {})
      render(
        <ColumnFilterDropdown
          options={options}
          onChange={onChange}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      await userEvent.click(button)
      await userEvent.click(screen.getByText('Checking'))

      expect(onChange).toHaveBeenCalledWith(['checking'])
    })

    it('should show "All" option by default', async () => {
      render(
        <ColumnFilterDropdown
          options={options}
          onChange={() => {}}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      await userEvent.click(button)

      expect(screen.getByText('All')).toBeInTheDocument()
    })

    it('should clear selection when "All" is clicked', async () => {
      const onChange = mock(() => {})
      render(
        <ColumnFilterDropdown
          options={options}
          value={['checking']}
          onChange={onChange}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      await userEvent.click(button)
      await userEvent.click(screen.getByText('All'))

      expect(onChange).toHaveBeenCalledWith([])
    })

    it('should highlight selected option', async () => {
      render(
        <ColumnFilterDropdown
          options={options}
          value={['checking']}
          onChange={() => {}}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      await userEvent.click(button)

      const checkingOption = screen.getByText('Checking').closest('button')
      expect(checkingOption).toHaveClass('column-filter__option--selected')
    })
  })

  describe('Multi Select Mode', () => {
    const options = [
      { value: 'cat1', label: 'Food & Dining' },
      { value: 'cat2', label: 'Transportation' },
      { value: 'cat3', label: 'Shopping' },
      { value: 'cat4', label: 'Entertainment' },
    ]

    it('should allow multiple selections', async () => {
      const onChange = mock(() => {})
      render(
        <ColumnFilterDropdown
          options={options}
          multiSelect
          onChange={onChange}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      await userEvent.click(button)
      await userEvent.click(screen.getByText('Food & Dining'))

      expect(onChange).toHaveBeenCalledWith(['cat1'])
    })

    it('should show checkboxes in multi-select mode', async () => {
      render(
        <ColumnFilterDropdown
          options={options}
          multiSelect
          onChange={() => {}}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      await userEvent.click(button)

      const checkboxes = screen.getAllByRole('checkbox')
      expect(checkboxes.length).toBeGreaterThan(0)
    })

    it('should support search in multi-select mode with search enabled', async () => {
      render(
        <ColumnFilterDropdown
          options={options}
          multiSelect
          searchable
          onChange={() => {}}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      await userEvent.click(button)

      const searchInput = screen.getByPlaceholderText(/search/i)
      expect(searchInput).toBeInTheDocument()

      await userEvent.type(searchInput, 'Food')

      expect(screen.getByText('Food & Dining')).toBeInTheDocument()
      expect(screen.queryByText('Transportation')).not.toBeInTheDocument()
    })

    it('should show select all and clear options', async () => {
      render(
        <ColumnFilterDropdown
          options={options}
          multiSelect
          onChange={() => {}}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      await userEvent.click(button)

      expect(screen.getByText('Select All')).toBeInTheDocument()
      expect(screen.getByText('Clear')).toBeInTheDocument()
    })

    it('should select all when clicking Select All', async () => {
      const onChange = mock(() => {})
      render(
        <ColumnFilterDropdown
          options={options}
          multiSelect
          onChange={onChange}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      await userEvent.click(button)
      await userEvent.click(screen.getByText('Select All'))

      expect(onChange).toHaveBeenCalledWith(['cat1', 'cat2', 'cat3', 'cat4'])
    })

    it('should clear all when clicking Clear', async () => {
      const onChange = mock(() => {})
      render(
        <ColumnFilterDropdown
          options={options}
          multiSelect
          value={['cat1', 'cat2']}
          onChange={onChange}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      await userEvent.click(button)
      await userEvent.click(screen.getByText('Clear'))

      expect(onChange).toHaveBeenCalledWith([])
    })
  })

  describe('Range Filter Mode', () => {
    it('should render min/max inputs in range mode', async () => {
      render(
        <ColumnFilterDropdown
          type="range"
          onChange={() => {}}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      await userEvent.click(button)

      expect(screen.getByLabelText(/min/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/max/i)).toBeInTheDocument()
    })

    it('should call onChange with min/max values', async () => {
      const onChange = mock(() => {})
      render(
        <ColumnFilterDropdown
          type="range"
          onChange={onChange}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      await userEvent.click(button)

      const minInput = screen.getByLabelText(/min/i)
      const maxInput = screen.getByLabelText(/max/i)

      await userEvent.type(minInput, '100')
      await userEvent.type(maxInput, '500')

      const applyButton = screen.getByRole('button', { name: /apply/i })
      await userEvent.click(applyButton)

      expect(onChange).toHaveBeenCalledWith({ min: 100, max: 500 })
    })

    it('should show clear button in range mode', async () => {
      render(
        <ColumnFilterDropdown
          type="range"
          rangeValue={{ min: 100, max: 500 }}
          onChange={() => {}}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      await userEvent.click(button)

      expect(screen.getByRole('button', { name: /clear/i })).toBeInTheDocument()
    })

    it('should clear range when clear clicked', async () => {
      const onChange = mock(() => {})
      render(
        <ColumnFilterDropdown
          type="range"
          rangeValue={{ min: 100, max: 500 }}
          onChange={onChange}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      await userEvent.click(button)

      const clearButton = screen.getByRole('button', { name: /clear/i })
      await userEvent.click(clearButton)

      expect(onChange).toHaveBeenCalledWith(null)
    })
  })

  describe('Dropdown Behavior', () => {
    it('should close dropdown when clicking outside', async () => {
      render(
        <div>
          <ColumnFilterDropdown
            options={[{ value: 'opt1', label: 'Option 1' }]}
            onChange={() => {}}
          />
          <button>Outside</button>
        </div>
      )

      const button = screen.getByRole('button', { name: /filter/i })
      await userEvent.click(button)

      expect(screen.getByText('Option 1')).toBeInTheDocument()

      await userEvent.click(screen.getByText('Outside'))

      await waitFor(() => {
        expect(screen.queryByText('Option 1')).not.toBeInTheDocument()
      })
    })

    it('should close dropdown when Escape is pressed', async () => {
      render(
        <ColumnFilterDropdown
          options={[{ value: 'opt1', label: 'Option 1' }]}
          onChange={() => {}}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      await userEvent.click(button)

      expect(screen.getByText('Option 1')).toBeInTheDocument()

      await userEvent.keyboard('{Escape}')

      await waitFor(() => {
        expect(screen.queryByText('Option 1')).not.toBeInTheDocument()
      })
    })

    it('should be disabled when disabled prop is true', () => {
      render(
        <ColumnFilterDropdown
          options={[{ value: 'opt1', label: 'Option 1' }]}
          onChange={() => {}}
          disabled
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      expect(button).toBeDisabled()
    })
  })

  describe('Accessibility', () => {
    it('should have accessible button with aria-label', () => {
      render(
        <ColumnFilterDropdown
          options={[{ value: 'opt1', label: 'Option 1' }]}
          onChange={() => {}}
          ariaLabel="Filter by category"
        />
      )

      expect(screen.getByRole('button', { name: 'Filter by category' })).toBeInTheDocument()
    })

    it('should have aria-expanded when open', async () => {
      render(
        <ColumnFilterDropdown
          options={[{ value: 'opt1', label: 'Option 1' }]}
          onChange={() => {}}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      expect(button).toHaveAttribute('aria-expanded', 'false')

      await userEvent.click(button)

      expect(button).toHaveAttribute('aria-expanded', 'true')
    })

    it('should have aria-haspopup', () => {
      render(
        <ColumnFilterDropdown
          options={[{ value: 'opt1', label: 'Option 1' }]}
          onChange={() => {}}
        />
      )

      const button = screen.getByRole('button', { name: /filter/i })
      expect(button).toHaveAttribute('aria-haspopup', 'listbox')
    })
  })

  describe('Custom Styling', () => {
    it('should apply custom className', () => {
      render(
        <ColumnFilterDropdown
          options={[{ value: 'opt1', label: 'Option 1' }]}
          onChange={() => {}}
          className="custom-filter"
        />
      )

      const container = screen.getByRole('button', { name: /filter/i }).parentElement
      expect(container).toHaveClass('custom-filter')
    })
  })
})
