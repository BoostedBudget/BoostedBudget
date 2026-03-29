import { describe, it, expect, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TransactionAccountFilter } from './TransactionAccountFilter'

describe('TransactionAccountFilter', () => {
  const mockAccounts = [
    {
      id: 'acc-1',
      name: 'Checking',
      official_name: 'Primary Checking',
      institution_name: 'Chase',
      type: 'depository',
    },
    {
      id: 'acc-2',
      name: 'Savings',
      official_name: 'High Yield Savings',
      institution_name: 'Marcus',
      type: 'depository',
    },
    {
      id: 'acc-3',
      name: 'Credit Card',
      official_name: 'Platinum Card',
      institution_name: 'Capital One',
      type: 'credit',
    },
  ]

  const defaultProps = {
    accounts: mockAccounts,
    selectedIds: [] as string[],
  }

  it('should render dropdown button', () => {
    render(<TransactionAccountFilter {...defaultProps} />)

    expect(
      screen.getByRole('button', { expanded: false })
    ).toBeInTheDocument()
  })

  it('should show "All accounts" when none selected', () => {
    render(<TransactionAccountFilter {...defaultProps} />)

    expect(screen.getByText('All accounts')).toBeInTheDocument()
  })

  it('should show account name when one selected', () => {
    render(
      <TransactionAccountFilter {...defaultProps} selectedIds={['acc-1']} />
    )

    expect(screen.getByText('Checking')).toBeInTheDocument()
  })

  it('should show count when multiple selected', () => {
    render(
      <TransactionAccountFilter
        {...defaultProps}
        selectedIds={['acc-1', 'acc-2']}
      />
    )

    expect(screen.getByText('2 accounts')).toBeInTheDocument()
  })

  it('should open dropdown on click', async () => {
    const user = userEvent.setup()

    render(<TransactionAccountFilter {...defaultProps} />)

    await user.click(screen.getByRole('button', { expanded: false }))

    expect(screen.getByRole('listbox')).toBeInTheDocument()
  })

  it('should show accounts with institution name', async () => {
    const user = userEvent.setup()

    render(<TransactionAccountFilter {...defaultProps} />)

    await user.click(screen.getByRole('button', { expanded: false }))

    expect(screen.getByText('Checking')).toBeInTheDocument()
    expect(screen.getByText('Chase')).toBeInTheDocument()
    expect(screen.getByText('Savings')).toBeInTheDocument()
    expect(screen.getByText('Marcus')).toBeInTheDocument()
  })

  it('should toggle account selection', async () => {
    const user = userEvent.setup()
    const onChange = mock(() => {})

    render(<TransactionAccountFilter {...defaultProps} onChange={onChange} />)

    await user.click(screen.getByRole('button', { expanded: false }))
    await user.click(screen.getByText('Checking'))

    expect(onChange).toHaveBeenCalledWith(['acc-1'])
  })

  it('should deselect already selected account', async () => {
    const user = userEvent.setup()
    const onChange = mock(() => {})

    render(
      <TransactionAccountFilter
        {...defaultProps}
        selectedIds={['acc-1', 'acc-2']}
        onChange={onChange}
      />
    )

    await user.click(screen.getByRole('button', { expanded: false }))
    await user.click(screen.getByText('Checking'))

    expect(onChange).toHaveBeenCalledWith(['acc-2'])
  })

  it('should select all accounts', async () => {
    const user = userEvent.setup()
    const onChange = mock(() => {})

    render(<TransactionAccountFilter {...defaultProps} onChange={onChange} />)

    await user.click(screen.getByRole('button', { expanded: false }))
    await user.click(screen.getByText('Select All'))

    expect(onChange).toHaveBeenCalledWith(['acc-1', 'acc-2', 'acc-3'])
  })

  it('should clear all selections', async () => {
    const user = userEvent.setup()
    const onChange = mock(() => {})

    render(
      <TransactionAccountFilter
        {...defaultProps}
        selectedIds={['acc-1', 'acc-2']}
        onChange={onChange}
      />
    )

    await user.click(screen.getByRole('button', { expanded: false }))
    await user.click(screen.getByText('Clear All'))

    expect(onChange).toHaveBeenCalledWith([])
  })

  it('should show loading state', () => {
    render(<TransactionAccountFilter {...defaultProps} loading />)

    expect(screen.getByText('Loading accounts...')).toBeInTheDocument()
  })

  it('should show empty message when no accounts', async () => {
    const user = userEvent.setup()

    render(<TransactionAccountFilter {...defaultProps} accounts={[]} />)

    await user.click(screen.getByRole('button', { expanded: false }))

    expect(screen.getByText('No accounts available')).toBeInTheDocument()
  })

  it('should disable dropdown when disabled prop is true', () => {
    render(<TransactionAccountFilter {...defaultProps} disabled />)

    const button = screen.getByRole('button')
    expect(button).toBeDisabled()
  })

  it('should render label', () => {
    render(<TransactionAccountFilter {...defaultProps} />)

    expect(screen.getByText('Accounts')).toBeInTheDocument()
  })
})
