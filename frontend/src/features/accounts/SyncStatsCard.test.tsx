import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import type { SyncStats } from './types'

const mockRefetch = mock(() => {})
const mockUseGetSyncStats = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetch,
}))

mock.module('@/api/generated/endpoints/sync/sync', () => ({
  useGetSyncStats: mockUseGetSyncStats,
}))

import { SyncStatsCard } from './SyncStatsCard'

describe('SyncStatsCard', () => {
  beforeEach(() => {
    mockUseGetSyncStats.mockClear()
    mockRefetch.mockClear()
  })

  it('displays loading state initially', () => {
    mockUseGetSyncStats.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: mockRefetch,
    })

    render(<SyncStatsCard />)

    expect(screen.getByText('Monthly Sync Limits')).toBeInTheDocument()
    expect(screen.getByText('Loading...')).toBeInTheDocument()
  })

  it('displays error state when query fails', () => {
    mockUseGetSyncStats.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('Network error'),
      refetch: mockRefetch,
    })

    render(<SyncStatsCard />)

    expect(screen.getByText('Failed to load sync statistics. Please try again later.')).toBeInTheDocument()
    expect(screen.getByText('⚠️')).toBeInTheDocument()
  })

  it('displays free tier stats under limit (2/3)', () => {
    const mockStats: SyncStats = {
      user_id: 'user-123',
      month: '2025-11',
      accounts_synced: 2,
      accounts_limit: 3,
      total_transactions: 150,
      can_sync_more: true,
      reset_date: '2025-12-01T00:00:00Z',
    }

    mockUseGetSyncStats.mockReturnValue({
      data: mockStats,
      isLoading: false,
      error: null,
      refetch: mockRefetch,
    })

    render(<SyncStatsCard />)

    expect(screen.getByText('Accounts Synced This Month')).toBeInTheDocument()
    expect(screen.getByText('2 of 3')).toBeInTheDocument()

    const progressFill = document.querySelector('.sync-stats-card__progress-fill')
    expect(progressFill).toBeInTheDocument()
    expect(progressFill?.getAttribute('style')).toContain('width:')
    expect(progressFill?.classList.contains('sync-stats-card__progress-fill--warning')).toBe(false)

    expect(screen.getByText(/Resets on/)).toBeInTheDocument()
    expect(screen.getByText('150 transactions synced this month')).toBeInTheDocument()
    expect(screen.queryByText('✓ Unlimited')).not.toBeInTheDocument()
  })

  it('displays free tier stats at limit (3/3) with warning', () => {
    const mockStats: SyncStats = {
      user_id: 'user-123',
      month: '2025-11',
      accounts_synced: 3,
      accounts_limit: 3,
      total_transactions: 250,
      can_sync_more: false,
      reset_date: '2025-12-01T00:00:00Z',
    }

    mockUseGetSyncStats.mockReturnValue({
      data: mockStats,
      isLoading: false,
      error: null,
      refetch: mockRefetch,
    })

    render(<SyncStatsCard />)

    expect(screen.getByText('3 of 3')).toBeInTheDocument()

    const progressFill = document.querySelector('.sync-stats-card__progress-fill')
    expect(progressFill?.classList.contains('sync-stats-card__progress-fill--warning')).toBe(true)
    expect(progressFill?.getAttribute('style')).toContain('width: 100%')

    expect(screen.getByText(/You've reached your monthly sync limit/)).toBeInTheDocument()

    const warningAlert = document.querySelector('.sync-stats-card__alert--warning')
    expect(warningAlert).toBeInTheDocument()
  })

  it('displays premium+ unlimited tier with badge', () => {
    const mockStats: SyncStats = {
      user_id: 'user-premium-plus',
      month: '2025-11',
      accounts_synced: 50,
      accounts_limit: -1, // Unlimited
      total_transactions: 5000,
      can_sync_more: true,
      reset_date: '2025-12-01T00:00:00Z',
    }

    mockUseGetSyncStats.mockReturnValue({
      data: mockStats,
      isLoading: false,
      error: null,
      refetch: mockRefetch,
    })

    render(<SyncStatsCard />)

    expect(screen.getByText('✓ Unlimited')).toBeInTheDocument()
    expect(screen.getByText('50')).toBeInTheDocument()
    expect(screen.queryByText(/of -1/)).not.toBeInTheDocument()

    const progressBar = document.querySelector('.sync-stats-card__progress-bar')
    expect(progressBar).not.toBeInTheDocument()

    const badge = document.querySelector('.sync-stats-card__badge')
    expect(badge).toBeInTheDocument()
    expect(badge).toHaveTextContent('✓ Unlimited')
  })

  it('renders nothing when stats is null after loading', () => {
    mockUseGetSyncStats.mockReturnValue({
      data: null,
      isLoading: false,
      error: null,
      refetch: mockRefetch,
    })

    const { container } = render(<SyncStatsCard />)

    expect(container.firstChild).toBeNull()
  })
})
