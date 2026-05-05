import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import '@/test/mocks/shared-charts'
import { mockUseDarkMode } from '@/test/mocks/shared-charts'
import { NetWorthHistoryChart } from './NetWorthHistoryChart'
import type { NetWorthHistory } from '@/features/dashboard/types'

const historyWithData: NetWorthHistory = {
  start_date: '2024-01-01',
  end_date: '2024-03-31',
  granularity: 'daily',
  snapshots: [
    { date: '2024-01-01', net_worth: 8000, total_assets: 12000, total_liabilities: 4000 },
    { date: '2024-02-01', net_worth: 9000, total_assets: 13000, total_liabilities: 4000 },
    { date: '2024-03-01', net_worth: 10000, total_assets: 14000, total_liabilities: 4000 },
  ],
}

const historyOneSnapshot: NetWorthHistory = {
  ...historyWithData,
  snapshots: [{ date: '2024-01-01', net_worth: 8000, total_assets: 12000, total_liabilities: 4000 }],
}

const historyNoSnapshots: NetWorthHistory = {
  ...historyWithData,
  snapshots: [],
}

describe('NetWorthHistoryChart', () => {
  beforeEach(() => {
    mockUseDarkMode.mockReturnValue(false)
  })

  it('renders three traces with correct names', () => {
    render(<NetWorthHistoryChart history={historyWithData} filterKey="test" />)
    const chart = screen.getByTestId('plotly-chart')
    const names = JSON.parse(chart.getAttribute('data-trace-names') ?? '[]')
    expect(names).toContain('Net Worth')
    expect(names).toContain('Assets')
    expect(names).toContain('Liabilities')
    expect(chart.getAttribute('data-trace-count')).toBe('3')
  })

  it('shows insufficient history fallback when < 2 snapshots', () => {
    render(<NetWorthHistoryChart history={historyOneSnapshot} filterKey="test" />)
    expect(screen.queryByTestId('plotly-chart')).toBeNull()
    expect(screen.getByText(/Not enough history/i)).toBeDefined()
  })

  it('shows fallback when no snapshots', () => {
    render(<NetWorthHistoryChart history={historyNoSnapshots} filterKey="test" />)
    expect(screen.queryByTestId('plotly-chart')).toBeNull()
    expect(screen.getByText(/Not enough history/i)).toBeDefined()
  })

  it('renders chart in dark mode path', () => {
    mockUseDarkMode.mockReturnValue(true)
    render(<NetWorthHistoryChart history={historyWithData} filterKey="test-dark" />)
    expect(screen.getByTestId('plotly-chart')).toBeDefined()
  })
})
