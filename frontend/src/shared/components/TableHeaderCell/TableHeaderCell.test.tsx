/**
 * TableHeaderCell Component Tests
 * Issue #590: Enhanced Table Sorting and Inline Column Filtering
 *
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TableHeaderCell } from './TableHeaderCell'

describe('TableHeaderCell', () => {
  beforeEach(() => {
    // mocks cleared individually
  })

  describe('Basic Rendering', () => {
    it('should render label text', () => {
      render(<TableHeaderCell label="Account" />)
      expect(screen.getByText('Account')).toBeInTheDocument()
    })

    it('should apply custom className', () => {
      const { container } = render(
        <TableHeaderCell label="Test" className="custom-class" />
      )
      expect(container.firstChild).toHaveClass('custom-class')
    })
  })

  describe('Sorting', () => {
    it('should render sort button when sortable', () => {
      render(
        <TableHeaderCell
          label="Date"
          sortable
          sortColumn="date"
          onSort={() => {}}
        />
      )
      expect(screen.getByRole('button', { name: /date/i })).toBeInTheDocument()
    })

    it('should not render sort button when not sortable', () => {
      render(<TableHeaderCell label="Actions" />)
      expect(screen.queryByRole('button')).not.toBeInTheDocument()
    })

    it('should call onSort when sort button clicked', async () => {
      const onSort = mock(() => {})
      render(
        <TableHeaderCell
          label="Date"
          sortable
          sortColumn="date"
          onSort={onSort}
        />
      )

      await userEvent.click(screen.getByRole('button', { name: /date/i }))
      expect(onSort).toHaveBeenCalledWith('date', 'asc')
    })

    it('should toggle sort direction when same column clicked', async () => {
      const onSort = mock(() => {})
      render(
        <TableHeaderCell
          label="Date"
          sortable
          sortColumn="date"
          currentSortColumn="date"
          currentSortDirection="asc"
          onSort={onSort}
        />
      )

      await userEvent.click(screen.getByRole('button', { name: /date/i }))
      expect(onSort).toHaveBeenCalledWith('date', 'desc')
    })

    it('should show ascending icon when sorted ascending', () => {
      render(
        <TableHeaderCell
          label="Date"
          sortable
          sortColumn="date"
          currentSortColumn="date"
          currentSortDirection="asc"
          onSort={() => {}}
        />
      )

      const button = screen.getByRole('button', { name: /date/i })
      expect(button).toHaveAttribute('aria-sort', 'ascending')
    })

    it('should show descending icon when sorted descending', () => {
      render(
        <TableHeaderCell
          label="Date"
          sortable
          sortColumn="date"
          currentSortColumn="date"
          currentSortDirection="desc"
          onSort={() => {}}
        />
      )

      const button = screen.getByRole('button', { name: /date/i })
      expect(button).toHaveAttribute('aria-sort', 'descending')
    })

    it('should not show aria-sort when column is not active', () => {
      render(
        <TableHeaderCell
          label="Date"
          sortable
          sortColumn="date"
          currentSortColumn="amount"
          currentSortDirection="asc"
          onSort={() => {}}
        />
      )

      const button = screen.getByRole('button', { name: /date/i })
      expect(button).not.toHaveAttribute('aria-sort')
    })
  })

  describe('Filtering', () => {
    const filterOptions = [
      { value: 'checking', label: 'Checking' },
      { value: 'savings', label: 'Savings' },
      { value: 'credit', label: 'Credit Card' },
    ]

    it('should render filter button when filterable', () => {
      render(
        <TableHeaderCell
          label="Type"
          filterable
          filterType="select"
          filterOptions={filterOptions}
          onFilterChange={() => {}}
        />
      )

      expect(screen.getByRole('button', { name: /filter/i })).toBeInTheDocument()
    })

    it('should not render filter button when not filterable', () => {
      render(<TableHeaderCell label="Actions" />)
      expect(screen.queryByRole('button', { name: /filter/i })).not.toBeInTheDocument()
    })

    it('should open filter dropdown when filter button clicked', async () => {
      render(
        <TableHeaderCell
          label="Type"
          filterable
          filterType="select"
          filterOptions={filterOptions}
          onFilterChange={() => {}}
        />
      )

      await userEvent.click(screen.getByRole('button', { name: /filter/i }))

      expect(screen.getByText('Checking')).toBeInTheDocument()
      expect(screen.getByText('Savings')).toBeInTheDocument()
      expect(screen.getByText('Credit Card')).toBeInTheDocument()
    })

    it('should call onFilterChange when filter option selected', async () => {
      const onFilterChange = mock(() => {})
      render(
        <TableHeaderCell
          label="Type"
          filterable
          filterType="select"
          filterOptions={filterOptions}
          onFilterChange={onFilterChange}
        />
      )

      await userEvent.click(screen.getByRole('button', { name: /filter/i }))
      await userEvent.click(screen.getByText('Checking'))

      expect(onFilterChange).toHaveBeenCalledWith(['checking'])
    })

    it('should show active filter indicator when filter has value', () => {
      render(
        <TableHeaderCell
          label="Type"
          filterable
          filterType="select"
          filterOptions={filterOptions}
          filterValue={['checking']}
          onFilterChange={() => {}}
        />
      )

      const filterButton = screen.getByRole('button', { name: /filter/i })
      expect(filterButton).toHaveClass('column-filter__trigger--active')
    })

    it('should support multi-select filter', async () => {
      const onFilterChange = mock(() => {})
      render(
        <TableHeaderCell
          label="Category"
          filterable
          filterType="select"
          filterOptions={filterOptions}
          multiSelect
          onFilterChange={onFilterChange}
        />
      )

      await userEvent.click(screen.getByRole('button', { name: /filter/i }))
      await userEvent.click(screen.getByText('Checking'))

      expect(onFilterChange).toHaveBeenCalledWith(['checking'])
    })

    it('should support range filter', async () => {
      const onFilterChange = mock(() => {})
      render(
        <TableHeaderCell
          label="Amount"
          filterable
          filterType="range"
          onFilterChange={onFilterChange}
        />
      )

      await userEvent.click(screen.getByRole('button', { name: /filter/i }))

      expect(screen.getByLabelText(/min/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/max/i)).toBeInTheDocument()
    })

    it('should apply range filter values', async () => {
      const onFilterChange = mock(() => {})
      render(
        <TableHeaderCell
          label="Amount"
          filterable
          filterType="range"
          onFilterChange={onFilterChange}
        />
      )

      await userEvent.click(screen.getByRole('button', { name: /filter/i }))

      const minInput = screen.getByLabelText(/min/i)
      const maxInput = screen.getByLabelText(/max/i)

      await userEvent.type(minInput, '100')
      await userEvent.type(maxInput, '500')

      await userEvent.click(screen.getByRole('button', { name: /apply/i }))

      expect(onFilterChange).toHaveBeenCalledWith({ min: 100, max: 500 })
    })
  })

  describe('Sort + Filter Combined', () => {
    const filterOptions = [
      { value: 'opt1', label: 'Option 1' },
      { value: 'opt2', label: 'Option 2' },
    ]

    it('should render both sort button and filter button', () => {
      render(
        <TableHeaderCell
          label="Type"
          sortable
          sortColumn="type"
          onSort={() => {}}
          filterable
          filterType="select"
          filterOptions={filterOptions}
          onFilterChange={() => {}}
        />
      )

      // Should have two buttons - sort and filter
      const buttons = screen.getAllByRole('button')
      expect(buttons).toHaveLength(2)
      // Sort button contains the label text
      expect(buttons[0]).toHaveTextContent('Type')
      // Filter button has filter aria-label
      expect(buttons[1]).toHaveAttribute('aria-label', 'Filter by type')
    })

    it('should allow sorting and filtering independently', async () => {
      const onSort = mock(() => {})
      const onFilterChange = mock(() => {})

      render(
        <TableHeaderCell
          label="Type"
          sortable
          sortColumn="type"
          onSort={onSort}
          filterable
          filterType="select"
          filterOptions={filterOptions}
          onFilterChange={onFilterChange}
        />
      )

      const buttons = screen.getAllByRole('button')
      const sortButton = buttons[0]
      const filterButton = buttons[1]

      // Click sort
      await userEvent.click(sortButton)
      expect(onSort).toHaveBeenCalledWith('type', 'asc')

      // Click filter
      await userEvent.click(filterButton)
      await userEvent.click(screen.getByText('Option 1'))
      expect(onFilterChange).toHaveBeenCalledWith(['opt1'])
    })
  })

  describe('Accessibility', () => {
    it('should have accessible sort button', () => {
      render(
        <TableHeaderCell
          label="Date"
          sortable
          sortColumn="date"
          onSort={() => {}}
        />
      )

      const button = screen.getByRole('button')
      expect(button).toHaveAccessibleName(/date/i)
    })

    it('should have accessible filter button', () => {
      render(
        <TableHeaderCell
          label="Type"
          filterable
          filterType="select"
          filterOptions={[{ value: 'v', label: 'L' }]}
          onFilterChange={() => {}}
          filterAriaLabel="Filter by type"
        />
      )

      expect(screen.getByRole('button', { name: 'Filter by type' })).toBeInTheDocument()
    })
  })

  describe('Close behavior', () => {
    it('should close filter dropdown on Escape', async () => {
      const filterOptions = [
        { value: 'opt1', label: 'Option 1' },
      ]

      render(
        <TableHeaderCell
          label="Type"
          filterable
          filterType="select"
          filterOptions={filterOptions}
          onFilterChange={() => {}}
        />
      )

      await userEvent.click(screen.getByRole('button', { name: /filter/i }))
      expect(screen.getByText('Option 1')).toBeInTheDocument()

      await userEvent.keyboard('{Escape}')

      await waitFor(() => {
        expect(screen.queryByText('Option 1')).not.toBeInTheDocument()
      })
    })
  })
})
