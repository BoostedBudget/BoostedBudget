/**
 * TransactionTypeFilter Component Tests
 * Issue #529: Transaction page filtering
 */

import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TransactionTypeFilter } from './TransactionTypeFilter'

describe('TransactionTypeFilter', () => {
  it('should render All/Income/Expenses buttons', () => {
    render(<TransactionTypeFilter value="all" onChange={() => {}} />)

    expect(screen.getByRole('button', { name: 'All' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Income' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Expenses' })).toBeInTheDocument()
  })

  it('should highlight selected type', () => {
    render(<TransactionTypeFilter value="income" onChange={() => {}} />)

    const incomeButton = screen.getByRole('button', { name: 'Income' })
    expect(incomeButton).toHaveAttribute('aria-pressed', 'true')

    const allButton = screen.getByRole('button', { name: 'All' })
    expect(allButton).toHaveAttribute('aria-pressed', 'false')
  })

  it('should call onChange when button is clicked', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()

    render(<TransactionTypeFilter value="all" onChange={onChange} />)

    await user.click(screen.getByRole('button', { name: 'Expenses' }))

    expect(onChange).toHaveBeenCalledWith('expense')
  })

  it('should call onChange with income when Income clicked', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()

    render(<TransactionTypeFilter value="all" onChange={onChange} />)

    await user.click(screen.getByRole('button', { name: 'Income' }))

    expect(onChange).toHaveBeenCalledWith('income')
  })

  it('should disable buttons when disabled prop is true', () => {
    render(<TransactionTypeFilter value="all" onChange={() => {}} disabled />)

    expect(screen.getByRole('button', { name: 'All' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Income' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Expenses' })).toBeDisabled()
  })

  it('should render label', () => {
    render(<TransactionTypeFilter value="all" onChange={() => {}} />)

    expect(screen.getByText('Type')).toBeInTheDocument()
  })
})
