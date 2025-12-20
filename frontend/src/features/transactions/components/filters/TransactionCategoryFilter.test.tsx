/**
 * TransactionCategoryFilter Component Tests
 * Issue #529: Transaction page filtering
 */

import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TransactionCategoryFilter } from './TransactionCategoryFilter'

describe('TransactionCategoryFilter', () => {
  const mockCategories = ['Food', 'Shopping', 'Travel', 'Entertainment']

  const defaultProps = {
    categories: mockCategories,
    selectedCategories: [] as string[],
    onChange: vi.fn(),
  }

  it('should render dropdown button', () => {
    render(<TransactionCategoryFilter {...defaultProps} />)

    expect(
      screen.getByRole('button', { expanded: false })
    ).toBeInTheDocument()
  })

  it('should show "All categories" when none selected', () => {
    render(<TransactionCategoryFilter {...defaultProps} />)

    expect(screen.getByText('All categories')).toBeInTheDocument()
  })

  it('should show selected category name when one selected', () => {
    render(
      <TransactionCategoryFilter {...defaultProps} selectedCategories={['Food']} />
    )

    expect(screen.getByText('Food')).toBeInTheDocument()
  })

  it('should show count when multiple selected', () => {
    render(
      <TransactionCategoryFilter
        {...defaultProps}
        selectedCategories={['Food', 'Travel']}
      />
    )

    expect(screen.getByText('2 categories')).toBeInTheDocument()
  })

  it('should open dropdown on click', async () => {
    const user = userEvent.setup()

    render(<TransactionCategoryFilter {...defaultProps} />)

    const button = screen.getByRole('button', { expanded: false })
    await user.click(button)

    // Check dropdown opened - listbox should be visible
    expect(screen.getByRole('listbox')).toBeInTheDocument()
  })

  it('should show search input in dropdown', async () => {
    const user = userEvent.setup()

    render(<TransactionCategoryFilter {...defaultProps} />)

    await user.click(screen.getByRole('button', { expanded: false }))

    expect(screen.getByPlaceholderText('Search categories...')).toBeInTheDocument()
  })

  it('should filter categories on search', async () => {
    const user = userEvent.setup()

    render(<TransactionCategoryFilter {...defaultProps} />)

    await user.click(screen.getByRole('button', { expanded: false }))
    await user.type(screen.getByPlaceholderText('Search categories...'), 'foo')

    // Only Food should be visible
    expect(screen.getByText('Food')).toBeInTheDocument()
    expect(screen.queryByText('Shopping')).not.toBeInTheDocument()
    expect(screen.queryByText('Travel')).not.toBeInTheDocument()
  })

  it('should toggle category selection', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()

    render(<TransactionCategoryFilter {...defaultProps} onChange={onChange} />)

    await user.click(screen.getByRole('button', { expanded: false }))
    await user.click(screen.getByText('Food'))

    expect(onChange).toHaveBeenCalledWith(['Food'])
  })

  it('should deselect already selected category', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()

    render(
      <TransactionCategoryFilter
        {...defaultProps}
        selectedCategories={['Food', 'Travel']}
        onChange={onChange}
      />
    )

    await user.click(screen.getByRole('button', { expanded: false }))
    await user.click(screen.getByText('Food'))

    expect(onChange).toHaveBeenCalledWith(['Travel'])
  })

  it('should select all categories', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()

    render(<TransactionCategoryFilter {...defaultProps} onChange={onChange} />)

    await user.click(screen.getByRole('button', { expanded: false }))
    await user.click(screen.getByText('Select All'))

    expect(onChange).toHaveBeenCalledWith(mockCategories)
  })

  it('should clear all selections', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()

    render(
      <TransactionCategoryFilter
        {...defaultProps}
        selectedCategories={['Food', 'Travel']}
        onChange={onChange}
      />
    )

    await user.click(screen.getByRole('button', { expanded: false }))
    await user.click(screen.getByText('Clear All'))

    expect(onChange).toHaveBeenCalledWith([])
  })

  it('should show loading state', () => {
    render(<TransactionCategoryFilter {...defaultProps} loading />)

    expect(screen.getByText('Loading categories...')).toBeInTheDocument()
  })

  it('should show empty message when no categories', async () => {
    const user = userEvent.setup()

    render(<TransactionCategoryFilter {...defaultProps} categories={[]} />)

    await user.click(screen.getByRole('button', { expanded: false }))

    expect(screen.getByText('No categories available')).toBeInTheDocument()
  })

  it('should disable dropdown when disabled prop is true', () => {
    render(<TransactionCategoryFilter {...defaultProps} disabled />)

    const button = screen.getByRole('button')
    expect(button).toBeDisabled()
  })

  it('should render label', () => {
    render(<TransactionCategoryFilter {...defaultProps} />)

    expect(screen.getByText('Categories')).toBeInTheDocument()
  })
})
