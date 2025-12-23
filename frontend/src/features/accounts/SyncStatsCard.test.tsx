import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { SyncStatsCard } from './SyncStatsCard'
import * as syncHooks from '@/api/generated/endpoints/sync/sync'
import type { SyncStats } from './types'

// Mock the generated hooks module
vi.mock('@/api/generated/endpoints/sync/sync')

describe('SyncStatsCard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  // ========================================================================
  // Loading State Tests
  // ========================================================================

  it('should display loading state initially', () => {
    vi.mocked(syncHooks.useGetSyncStats).mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: vi.fn(),
    } as unknown as ReturnType<typeof syncHooks.useGetSyncStats>)

    render(<SyncStatsCard />)

    expect(screen.getByText('Monthly Sync Limits')).toBeInTheDocument()
    expect(screen.getByText('Loading...')).toBeInTheDocument()
  })

  // ========================================================================
  // Error State Tests
  // ========================================================================

  it('should display error state when query fails', () => {
    vi.mocked(syncHooks.useGetSyncStats).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('Network error'),
      refetch: vi.fn(),
    } as unknown as ReturnType<typeof syncHooks.useGetSyncStats>)

    render(<SyncStatsCard />)

    expect(
      screen.getByText('Failed to load sync statistics. Please try again later.')
    ).toBeInTheDocument()
    expect(screen.getByText('⚠️')).toBeInTheDocument()
  })

  // ========================================================================
  // Free Tier - Under Limit Tests
  // ========================================================================

  it('should display free tier stats under limit (2/3)', () => {
    const mockStats: SyncStats = {
      user_id: 'user-123',
      month: '2025-11',
      accounts_synced: 2,
      accounts_limit: 3,
      total_transactions: 150,
      can_sync_more: true,
      reset_date: '2025-12-01T00:00:00Z',
    }

    vi.mocked(syncHooks.useGetSyncStats).mockReturnValue({
      data: mockStats,
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    } as unknown as ReturnType<typeof syncHooks.useGetSyncStats>)

    render(<SyncStatsCard />)

    expect(screen.getByText('Accounts Synced This Month')).toBeInTheDocument()
    expect(screen.getByText('2 of 3')).toBeInTheDocument()

    // Check progress bar exists
    const progressBar = document.querySelector('.sync-stats-card__progress-bar')
    expect(progressBar).toBeInTheDocument()

    // Check progress fill width (2/3 = 66.67%)
    const progressFill = document.querySelector('.sync-stats-card__progress-fill')
    expect(progressFill).toBeInTheDocument()
    expect(progressFill).toHaveStyle({ width: '66.66666666666666%' })

    // Check progress bar is NOT in warning state
    expect(progressFill?.classList.contains('sync-stats-card__progress-fill--warning')).toBe(false)

    // Check reset date exists
    expect(screen.getByText(/Resets on/)).toBeInTheDocument()

    // Check transaction summary
    expect(screen.getByText('150 transactions synced this month')).toBeInTheDocument()

    // Should NOT show unlimited badge
    expect(screen.queryByText('✓ Unlimited')).not.toBeInTheDocument()

    // Should NOT show at-limit warning
    expect(
      screen.queryByText(/You've reached your monthly sync limit/)
    ).not.toBeInTheDocument()
  })

  // ========================================================================
  // Free Tier - At Limit Tests
  // ========================================================================

  it('should display free tier stats at limit (3/3) with warning', () => {
    const mockStats: SyncStats = {
      user_id: 'user-123',
      month: '2025-11',
      accounts_synced: 3,
      accounts_limit: 3,
      total_transactions: 250,
      can_sync_more: false,
      reset_date: '2025-12-01T00:00:00Z',
    }

    vi.mocked(syncHooks.useGetSyncStats).mockReturnValue({
      data: mockStats,
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    } as unknown as ReturnType<typeof syncHooks.useGetSyncStats>)

    render(<SyncStatsCard />)

    expect(screen.getByText('3 of 3')).toBeInTheDocument()

    // Check progress bar is in warning state (orange)
    const progressFill = document.querySelector('.sync-stats-card__progress-fill')
    expect(progressFill?.classList.contains('sync-stats-card__progress-fill--warning')).toBe(true)

    // Check progress fill is 100%
    expect(progressFill).toHaveStyle({ width: '100%' })

    // Check at-limit warning is displayed
    expect(
      screen.getByText(/You've reached your monthly sync limit/)
    ).toBeInTheDocument()

    // Check warning icon
    const warningAlert = document.querySelector('.sync-stats-card__alert--warning')
    expect(warningAlert).toBeInTheDocument()
  })

  // ========================================================================
  // Premium Plus - Unlimited Tier Tests
  // ========================================================================

  it('should display premium+ unlimited tier with badge', () => {
    const mockStats: SyncStats = {
      user_id: 'user-premium-plus',
      month: '2025-11',
      accounts_synced: 50,
      accounts_limit: -1, // Unlimited
      total_transactions: 5000,
      can_sync_more: true,
      reset_date: '2025-12-01T00:00:00Z',
    }

    vi.mocked(syncHooks.useGetSyncStats).mockReturnValue({
      data: mockStats,
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    } as unknown as ReturnType<typeof syncHooks.useGetSyncStats>)

    render(<SyncStatsCard />)

    expect(screen.getByText('✓ Unlimited')).toBeInTheDocument()

    // Check account count display (should NOT show "of X")
    expect(screen.getByText('50')).toBeInTheDocument()
    expect(screen.queryByText(/of -1/)).not.toBeInTheDocument()

    // Should NOT show progress bar for unlimited tier
    const progressBar = document.querySelector('.sync-stats-card__progress-bar')
    expect(progressBar).not.toBeInTheDocument()

    // Check unlimited badge styling
    const badge = document.querySelector('.sync-stats-card__badge')
    expect(badge).toBeInTheDocument()
    expect(badge).toHaveTextContent('✓ Unlimited')
  })

  // ========================================================================
  // Null Data Test
  // ========================================================================

  it('should not render anything when stats is null after loading', () => {
    vi.mocked(syncHooks.useGetSyncStats).mockReturnValue({
      data: null,
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    } as unknown as ReturnType<typeof syncHooks.useGetSyncStats>)

    const { container } = render(<SyncStatsCard />)

    expect(container.firstChild).toBeNull()
  })
})
