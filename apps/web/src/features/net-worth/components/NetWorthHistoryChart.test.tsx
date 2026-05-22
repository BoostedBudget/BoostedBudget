import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import '@/test/mocks/shared-charts'
import { mockUseDarkMode, MockEChartsChart } from '@/test/mocks/shared-charts'
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

  it('renders three series with correct names', () => {
    render(<NetWorthHistoryChart history={historyWithData} />)
    const chart = screen.getByTestId('echarts-chart')
    const names = JSON.parse(chart.dataset.seriesNames ?? '[]') as string[]
    expect(names).toEqual(expect.arrayContaining(['Net Worth', 'Assets', 'Liabilities']))
    expect(chart.dataset.seriesCount).toBe('3')
  })

  it('shows insufficient history fallback when < 2 snapshots', () => {
    render(<NetWorthHistoryChart history={historyOneSnapshot} />)
    expect(screen.getByText(/Not enough history/i)).toBeDefined()
  })

  it('shows fallback when no snapshots', () => {
    render(<NetWorthHistoryChart history={historyNoSnapshots} />)
    expect(screen.getByText(/Not enough history/i)).toBeDefined()
  })

  it('passes isDark=true to EChartsChart when dark mode is active', () => {
    mockUseDarkMode.mockReturnValue(true)
    render(<NetWorthHistoryChart history={historyWithData} />)
    expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('true')
  })

  it('passes isDark=false to EChartsChart when light mode is active', () => {
    mockUseDarkMode.mockReturnValue(false)
    render(<NetWorthHistoryChart history={historyWithData} />)
    expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('false')
  })

  it('renders with net-worth-history-chart testid', () => {
    render(<NetWorthHistoryChart history={historyWithData} />)
    expect(screen.getByTestId('net-worth-history-chart')).toBeInTheDocument()
  })

  it('tooltip formatter escapes malicious series names (XSS regression — #1169)', () => {
    render(<NetWorthHistoryChart history={historyWithData} />)
    const option = MockEChartsChart.mock.calls.at(-1)?.[0]?.option as {
      tooltip?: { formatter?: (p: unknown) => string }
    }
    const formatted = option.tooltip!.formatter!([{ seriesName: '<svg onload=alert(1)>', value: 9000, dataIndex: 0 }])
    expect(formatted).not.toMatch(/<(svg|script|img)/i)
    expect(formatted).toContain('&lt;svg')
  })
})
