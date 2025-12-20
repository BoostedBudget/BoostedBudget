/**
 * TransactionStatusFilter Component Tests
 * Issue #529: Transaction page filtering
 */

import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TransactionStatusFilter } from './TransactionStatusFilter'

describe('TransactionStatusFilter', () => {
  it('should render All/Posted/Pending buttons', () => {
    render(<TransactionStatusFilter value="all" onChange={() => {}} />)

    expect(screen.getByRole('button', { name: 'All' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Posted' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /pending/i })).toBeInTheDocument()
  })

  it('should highlight selected status', () => {
    render(<TransactionStatusFilter value="posted" onChange={() => {}} />)

    const postedButton = screen.getByRole('button', { name: 'Posted' })
    expect(postedButton).toHaveAttribute('aria-pressed', 'true')

    const allButton = screen.getByRole('button', { name: 'All' })
    expect(allButton).toHaveAttribute('aria-pressed', 'false')
  })

  it('should call onChange when button is clicked', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()

    render(<TransactionStatusFilter value="all" onChange={onChange} />)

    await user.click(screen.getByRole('button', { name: 'Posted' }))

    expect(onChange).toHaveBeenCalledWith('posted')
  })

  it('should show pending count badge', () => {
    render(
      <TransactionStatusFilter value="all" onChange={() => {}} pendingCount={5} />
    )

    expect(screen.getByText('5')).toBeInTheDocument()
  })

  it('should not show pending count badge when count is 0', () => {
    render(
      <TransactionStatusFilter value="all" onChange={() => {}} pendingCount={0} />
    )

    expect(screen.queryByText('0')).not.toBeInTheDocument()
  })

  it('should not show pending count badge when pendingCount is undefined', () => {
    render(<TransactionStatusFilter value="all" onChange={() => {}} />)

    // Just the Pending label, no count badge
    const pendingButton = screen.getByRole('button', { name: 'Pending' })
    expect(pendingButton).toBeInTheDocument()
  })

  it('should disable buttons when disabled prop is true', () => {
    render(<TransactionStatusFilter value="all" onChange={() => {}} disabled />)

    expect(screen.getByRole('button', { name: 'All' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Posted' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Pending' })).toBeDisabled()
  })

  it('should render label', () => {
    render(<TransactionStatusFilter value="all" onChange={() => {}} />)

    expect(screen.getByText('Status')).toBeInTheDocument()
  })
})
