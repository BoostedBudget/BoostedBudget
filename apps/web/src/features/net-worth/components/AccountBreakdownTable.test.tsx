import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { AccountBreakdownTable } from './AccountBreakdownTable'
import type { AccountBreakdownRow } from '../types'

const asset: AccountBreakdownRow = {
  id: 'acc-1',
  name: 'Checking',
  type: 'depository',
  currencyCode: 'USD',
  currentBalance: 5000,
  percentOfNetWorth: 50,
  side: 'assets',
}

const liability: AccountBreakdownRow = {
  id: 'acc-2',
  name: 'Visa Card',
  type: 'credit',
  currencyCode: 'USD',
  currentBalance: -2000,
  percentOfNetWorth: 20,
  side: 'liabilities',
}

describe('AccountBreakdownTable', () => {
  it('renders Assets section caption', () => {
    render(<AccountBreakdownTable assets={[asset]} liabilities={[]} netWorth={10000} loading={false} error={null} />)
    expect(screen.getByText('Assets')).toBeDefined()
  })

  it('renders Liabilities section caption', () => {
    render(<AccountBreakdownTable assets={[]} liabilities={[liability]} netWorth={10000} loading={false} error={null} />)
    expect(screen.getByText('Liabilities')).toBeDefined()
  })

  it('renders account name in each row', () => {
    render(<AccountBreakdownTable assets={[asset]} liabilities={[liability]} netWorth={10000} loading={false} error={null} />)
    expect(screen.getByText('Checking')).toBeDefined()
    expect(screen.getByText('Visa Card')).toBeDefined()
  })

  it('applies data-ph-mask to balance cells', () => {
    const { container } = render(
      <AccountBreakdownTable assets={[asset]} liabilities={[]} netWorth={10000} loading={false} error={null} />
    )
    const masked = container.querySelectorAll('[data-ph-mask]')
    expect(masked.length).toBeGreaterThan(0)
  })

  it('shows empty state when both sides empty', () => {
    render(<AccountBreakdownTable assets={[]} liabilities={[]} netWorth={0} loading={false} error={null} />)
    expect(screen.getByRole('status')).toBeDefined()
  })

  it('shows loading state', () => {
    render(<AccountBreakdownTable assets={[]} liabilities={[]} netWorth={0} loading={true} error={null} />)
    expect(screen.getByRole('status')).toBeDefined()
  })

  it('shows error state', () => {
    render(<AccountBreakdownTable assets={[]} liabilities={[]} netWorth={0} loading={false} error={new Error('oops')} />)
    expect(screen.getByText(/oops/)).toBeDefined()
  })

  it('omits empty asset section', () => {
    render(<AccountBreakdownTable assets={[]} liabilities={[liability]} netWorth={10000} loading={false} error={null} />)
    expect(screen.queryByText('Assets')).toBeNull()
  })

  it('omits empty liabilities section', () => {
    render(<AccountBreakdownTable assets={[asset]} liabilities={[]} netWorth={10000} loading={false} error={null} />)
    expect(screen.queryByText('Liabilities')).toBeNull()
  })

  it('uses overflow-x-auto wrapper for mobile', () => {
    const { container } = render(
      <AccountBreakdownTable assets={[asset]} liabilities={[]} netWorth={10000} loading={false} error={null} />
    )
    const wrapper = container.querySelector('.overflow-x-auto')
    expect(wrapper).not.toBeNull()
  })
})
