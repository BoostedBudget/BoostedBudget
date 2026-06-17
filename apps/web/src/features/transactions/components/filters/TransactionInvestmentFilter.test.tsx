/**
 * TransactionInvestmentFilter Component Tests
 *
 * Issue #1299: Investment transactions in unified feed — filter UI
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TransactionInvestmentFilter } from './TransactionInvestmentFilter'

describe('TransactionInvestmentFilter', () => {
  it('renders the "Include investment activity" toggle', () => {
    render(
      <TransactionInvestmentFilter
        includeInvestmentActivity={true}
        activityTypes={[]}
        onIncludeChange={() => {}}
        onActivityTypesChange={() => {}}
      />
    )

    expect(screen.getByRole('switch', { name: /include investment activity/i })).toBeInTheDocument()
  })

  it('hides activity-type multiselect when toggle is OFF', () => {
    render(
      <TransactionInvestmentFilter
        includeInvestmentActivity={false}
        activityTypes={[]}
        onIncludeChange={() => {}}
        onActivityTypesChange={() => {}}
      />
    )

    expect(screen.queryByRole('group', { name: /activity types/i })).not.toBeInTheDocument()
  })

  it('shows activity-type multiselect when toggle is ON', () => {
    render(
      <TransactionInvestmentFilter
        includeInvestmentActivity={true}
        activityTypes={[]}
        onIncludeChange={() => {}}
        onActivityTypesChange={() => {}}
      />
    )

    expect(screen.getByRole('group', { name: /activity types/i })).toBeInTheDocument()
  })

  it('calls onIncludeChange(false) when toggled off', async () => {
    const user = userEvent.setup()
    const onIncludeChange = mock(() => {})

    render(
      <TransactionInvestmentFilter
        includeInvestmentActivity={true}
        activityTypes={[]}
        onIncludeChange={onIncludeChange}
        onActivityTypesChange={() => {}}
      />
    )

    await user.click(screen.getByRole('switch', { name: /include investment activity/i }))

    expect(onIncludeChange).toHaveBeenCalledWith(false)
  })

  it('calls onIncludeChange(true) when toggled on', async () => {
    const user = userEvent.setup()
    const onIncludeChange = mock(() => {})

    render(
      <TransactionInvestmentFilter
        includeInvestmentActivity={false}
        activityTypes={[]}
        onIncludeChange={onIncludeChange}
        onActivityTypesChange={() => {}}
      />
    )

    await user.click(screen.getByRole('switch', { name: /include investment activity/i }))

    expect(onIncludeChange).toHaveBeenCalledWith(true)
  })

  it('calls onActivityTypesChange when an activity type checkbox is clicked', async () => {
    const user = userEvent.setup()
    const onActivityTypesChange = mock(() => {})

    render(
      <TransactionInvestmentFilter
        includeInvestmentActivity={true}
        activityTypes={[]}
        onIncludeChange={() => {}}
        onActivityTypesChange={onActivityTypesChange}
      />
    )

    await user.click(screen.getByRole('checkbox', { name: /buy/i }))

    expect(onActivityTypesChange).toHaveBeenCalledWith(['buy'])
  })

  it('removes an activity type when its checkbox is unchecked', async () => {
    const user = userEvent.setup()
    const onActivityTypesChange = mock(() => {})

    render(
      <TransactionInvestmentFilter
        includeInvestmentActivity={true}
        activityTypes={['buy', 'sell']}
        onIncludeChange={() => {}}
        onActivityTypesChange={onActivityTypesChange}
      />
    )

    await user.click(screen.getByRole('checkbox', { name: /buy/i }))

    expect(onActivityTypesChange).toHaveBeenCalledWith(['sell'])
  })

  it('shows "All types" label when no activity types are selected', () => {
    render(
      <TransactionInvestmentFilter
        includeInvestmentActivity={true}
        activityTypes={[]}
        onIncludeChange={() => {}}
        onActivityTypesChange={() => {}}
      />
    )

    expect(screen.getByText('All types')).toBeInTheDocument()
  })

  it('shows selected count label when activity types are selected', () => {
    render(
      <TransactionInvestmentFilter
        includeInvestmentActivity={true}
        activityTypes={['buy', 'sell']}
        onIncludeChange={() => {}}
        onActivityTypesChange={() => {}}
      />
    )

    expect(screen.getByText('2 selected')).toBeInTheDocument()
  })
})
