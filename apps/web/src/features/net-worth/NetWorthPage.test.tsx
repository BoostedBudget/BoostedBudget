import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import '@/test/mocks/shared-charts'
import type { NetWorthSummary, NetWorthHistory } from '@/features/dashboard/types'

import {
  mockUseNetWorthData,
  mockNetWorthRefetch,
} from '@/test/mocks/net-worth-hooks'
import {
  mockUseGetAccounts,
} from '@/test/mocks/accounts-api'

const mockTrackEvent = mock(() => {})
mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))

const baseSummary: NetWorthSummary = {
  as_of_date: '2024-03-31',
  net_worth: 10000,
  total_assets: 15000,
  total_liabilities: 5000,
  by_category: {},
}

const baseHistory: NetWorthHistory = {
  start_date: '2024-01-01',
  end_date: '2024-03-31',
  granularity: 'daily',
  snapshots: [
    { date: '2024-01-01', net_worth: 8000, total_assets: 12000, total_liabilities: 4000 },
    { date: '2024-02-01', net_worth: 9000, total_assets: 13000, total_liabilities: 4000 },
    { date: '2024-03-01', net_worth: 10000, total_assets: 14000, total_liabilities: 4000 },
  ],
}

const { NetWorthPage } = await import('./NetWorthPage')

function renderPage() {
  return render(
    <MemoryRouter>
      <NetWorthPage />
    </MemoryRouter>
  )
}

describe('NetWorthPage', () => {
  beforeEach(() => {
    mockTrackEvent.mockReset()
    mockUseNetWorthData.mockClear()
    mockNetWorthRefetch.mockClear()
    mockUseGetAccounts.mockReturnValue({ data: null, isLoading: false, error: null })
  })

  it('renders page heading', () => {
    mockUseNetWorthData.mockReturnValue({ summary: baseSummary, history: baseHistory, loading: false, error: null, refetch: mockNetWorthRefetch })
    renderPage()
    expect(screen.getByRole('heading', { level: 1 })).toBeDefined()
  })

  it('fires NET_WORTH_PAGE_VIEWED on mount', async () => {
    mockUseNetWorthData.mockReturnValue({ summary: baseSummary, history: baseHistory, loading: false, error: null, refetch: mockNetWorthRefetch })
    renderPage()
    await act(async () => {})
    expect(mockTrackEvent).toHaveBeenCalledWith('net_worth_page_viewed')
  })

  it('renders loading state', () => {
    mockUseNetWorthData.mockReturnValue({ summary: null, history: null, loading: true, error: null, refetch: mockNetWorthRefetch })
    renderPage()
    expect(screen.getByRole('status')).toBeDefined()
  })

  it('renders error state with retry button', () => {
    mockUseNetWorthData.mockReturnValue({ summary: null, history: null, loading: false, error: new Error('API error'), refetch: mockNetWorthRefetch })
    renderPage()
    expect(screen.getByText(/API error/i)).toBeDefined()
    expect(screen.getByRole('button', { name: /retry/i })).toBeDefined()
  })

  it('renders empty state when no summary data', () => {
    mockUseNetWorthData.mockReturnValue({ summary: null, history: null, loading: false, error: null, refetch: mockNetWorthRefetch })
    renderPage()
    expect(screen.getByRole('status')).toBeDefined()
  })

  it('renders chart when history has multiple snapshots', () => {
    mockUseNetWorthData.mockReturnValue({ summary: baseSummary, history: baseHistory, loading: false, error: null, refetch: mockNetWorthRefetch })
    renderPage()
    expect(screen.getByTestId('plotly-chart')).toBeDefined()
  })

  it('hides chart when insufficient history', () => {
    const singleSnapshotHistory = { ...baseHistory, snapshots: baseHistory.snapshots.slice(0, 1) }
    mockUseNetWorthData.mockReturnValue({ summary: baseSummary, history: singleSnapshotHistory, loading: false, error: null, refetch: mockNetWorthRefetch })
    renderPage()
    expect(screen.queryByTestId('plotly-chart')).toBeNull()
  })

  it('renders breakdown table section', () => {
    mockUseNetWorthData.mockReturnValue({ summary: baseSummary, history: baseHistory, loading: false, error: null, refetch: mockNetWorthRefetch })
    renderPage()
    expect(screen.getByText('Breakdown')).toBeDefined()
  })

  it('renders date range picker', () => {
    mockUseNetWorthData.mockReturnValue({ summary: baseSummary, history: baseHistory, loading: false, error: null, refetch: mockNetWorthRefetch })
    renderPage()
    expect(screen.getByText('Last 3 Months')).toBeDefined()
  })

  it('renders empty state when summary is all-zero', () => {
    const zeroSummary: NetWorthSummary = {
      as_of_date: '2024-03-31',
      net_worth: 0,
      total_assets: 0,
      total_liabilities: 0,
      by_category: {},
    }
    mockUseNetWorthData.mockReturnValue({ summary: zeroSummary, history: baseHistory, loading: false, error: null, refetch: mockNetWorthRefetch })
    renderPage()
    expect(screen.getByText(/connect a bank account/i)).toBeDefined()
    expect(screen.queryByTestId('plotly-chart')).toBeNull()
  })
})
