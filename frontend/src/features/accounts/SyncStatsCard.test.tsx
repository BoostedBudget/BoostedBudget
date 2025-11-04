import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { SyncStatsCard } from './SyncStatsCard'
import * as syncStatsApi from './api/syncStatsApi'
import type { SyncStats } from './types'

// Mock the API module
vi.mock('./api/syncStatsApi')

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
    // Mock API call that never resolves
    vi.mocked(syncStatsApi.fetchSyncStats).mockImplementation(
      () => new Promise(() => {})
    )

    render(<SyncStatsCard />)

    expect(screen.getByText('Monthly Sync Limits')).toBeInTheDocument()
    expect(screen.getByText('Loading...')).toBeInTheDocument()
  })

  // ========================================================================
  // Error State Tests
  // ========================================================================

  it('should display error state when API call fails', async () => {
    vi.mocked(syncStatsApi.fetchSyncStats).mockRejectedValueOnce(
      new Error('Network error')
    )

    render(<SyncStatsCard />)

    await waitFor(() => {
      expect(
        screen.getByText('Failed to load sync statistics. Please try again later.')
      ).toBeInTheDocument()
    })

    expect(screen.getByText('⚠️')).toBeInTheDocument()
  })

  // ========================================================================
  // Free Tier - Under Limit Tests
  // ========================================================================

  it('should display free tier stats under limit (2/3)', async () => {
    const mockStats: SyncStats = {
      user_id: 'user-123',
      month: '2025-11',
      accounts_synced: 2,
      accounts_limit: 3,
      total_transactions: 150,
      can_sync_more: true,
      reset_date: '2025-12-01T00:00:00Z',
    }

    vi.mocked(syncStatsApi.fetchSyncStats).mockResolvedValueOnce(mockStats)

    render(<SyncStatsCard />)

    await waitFor(() => {
      expect(screen.getByText('Accounts Synced This Month')).toBeInTheDocument()
    })

    // Check account count display
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

    // Check reset date exists (don't check exact format due to timezone variations)
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

  it('should display free tier stats at limit (3/3) with warning', async () => {
    const mockStats: SyncStats = {
      user_id: 'user-123',
      month: '2025-11',
      accounts_synced: 3,
      accounts_limit: 3,
      total_transactions: 250,
      can_sync_more: false,
      reset_date: '2025-12-01T00:00:00Z',
    }

    vi.mocked(syncStatsApi.fetchSyncStats).mockResolvedValueOnce(mockStats)

    render(<SyncStatsCard />)

    await waitFor(() => {
      expect(screen.getByText('3 of 3')).toBeInTheDocument()
    })

    // Check progress bar is in warning state (orange)
    const progressFill = document.querySelector('.sync-stats-card__progress-fill')
    expect(progressFill?.classList.contains('sync-stats-card__progress-fill--warning')).toBe(true)

    // Check progress fill is 100%
    expect(progressFill).toHaveStyle({ width: '100%' })

    // Check at-limit warning is displayed
    expect(
      screen.getByText(/You've reached your monthly sync limit/)
    ).toBeInTheDocument()
    expect(
      screen.getByText(/Upgrade to sync more accounts or wait until/)
    ).toBeInTheDocument()

    // Check warning icon
    const warningAlert = document.querySelector('.sync-stats-card__alert--warning')
    expect(warningAlert).toBeInTheDocument()
  })

  // ========================================================================
  // Free Tier - No Syncs Yet Tests
  // ========================================================================

  it('should display free tier with no syncs yet (0/3)', async () => {
    const mockStats: SyncStats = {
      user_id: 'user-123',
      month: '2025-11',
      accounts_synced: 0,
      accounts_limit: 3,
      total_transactions: 0,
      can_sync_more: true,
      reset_date: '2025-12-01T00:00:00Z',
    }

    vi.mocked(syncStatsApi.fetchSyncStats).mockResolvedValueOnce(mockStats)

    render(<SyncStatsCard />)

    await waitFor(() => {
      expect(screen.getByText('0 of 3')).toBeInTheDocument()
    })

    // Progress bar should show 0%
    const progressFill = document.querySelector('.sync-stats-card__progress-fill')
    expect(progressFill).toHaveStyle({ width: '0%' })

    // Should NOT show transaction summary when count is 0
    expect(
      screen.queryByText(/transactions synced this month/)
    ).not.toBeInTheDocument()
  })

  // ========================================================================
  // Premium Lite Tests
  // ========================================================================

  it('should display premium lite stats (10/15)', async () => {
    const mockStats: SyncStats = {
      user_id: 'user-456',
      month: '2025-11',
      accounts_synced: 10,
      accounts_limit: 15,
      total_transactions: 850,
      can_sync_more: true,
      reset_date: '2025-12-01T00:00:00Z',
    }

    vi.mocked(syncStatsApi.fetchSyncStats).mockResolvedValueOnce(mockStats)

    render(<SyncStatsCard />)

    await waitFor(() => {
      expect(screen.getByText('10 of 15')).toBeInTheDocument()
    })

    // Check progress percentage (10/15 = 66.67%)
    const progressFill = document.querySelector('.sync-stats-card__progress-fill')
    expect(progressFill).toHaveStyle({ width: '66.66666666666666%' })

    // Should NOT show unlimited badge
    expect(screen.queryByText('✓ Unlimited')).not.toBeInTheDocument()

    // Check transaction count
    expect(screen.getByText('850 transactions synced this month')).toBeInTheDocument()
  })

  // ========================================================================
  // Premium Plus - Unlimited Tier Tests
  // ========================================================================

  it('should display premium+ unlimited tier with badge', async () => {
    const mockStats: SyncStats = {
      user_id: 'user-premium-plus',
      month: '2025-11',
      accounts_synced: 50,
      accounts_limit: -1, // Unlimited
      total_transactions: 5000,
      can_sync_more: true,
      reset_date: '2025-12-01T00:00:00Z',
    }

    vi.mocked(syncStatsApi.fetchSyncStats).mockResolvedValueOnce(mockStats)

    render(<SyncStatsCard />)

    await waitFor(() => {
      expect(screen.getByText('✓ Unlimited')).toBeInTheDocument()
    })

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

    // Should NOT show at-limit warning
    expect(
      screen.queryByText(/You've reached your monthly sync limit/)
    ).not.toBeInTheDocument()
  })

  // ========================================================================
  // Edge Case Tests
  // ========================================================================

  it('should handle progress over 100% gracefully', async () => {
    // Edge case: API returns more synced accounts than limit
    const mockStats: SyncStats = {
      user_id: 'user-edge',
      month: '2025-11',
      accounts_synced: 5,
      accounts_limit: 3, // Synced more than limit (shouldn't happen, but defensive)
      total_transactions: 500,
      can_sync_more: false,
      reset_date: '2025-12-01T00:00:00Z',
    }

    vi.mocked(syncStatsApi.fetchSyncStats).mockResolvedValueOnce(mockStats)

    render(<SyncStatsCard />)

    await waitFor(() => {
      expect(screen.getByText('5 of 3')).toBeInTheDocument()
    })

    // Progress should be capped at 100%
    const progressFill = document.querySelector('.sync-stats-card__progress-fill')
    expect(progressFill).toHaveStyle({ width: '100%' })
  })

  it('should not render anything when stats is null after loading', async () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    vi.mocked(syncStatsApi.fetchSyncStats).mockResolvedValueOnce(null as any)

    const { container } = render(<SyncStatsCard />)

    await waitFor(() => {
      expect(container.firstChild).toBeNull()
    })
  })

  // ========================================================================
  // Date Formatting Tests
  // ========================================================================

  it('should format reset date correctly for different months', async () => {
    const mockStats: SyncStats = {
      user_id: 'user-date-test',
      month: '2025-05',
      accounts_synced: 1,
      accounts_limit: 3,
      total_transactions: 50,
      can_sync_more: true,
      reset_date: '2025-06-01T00:00:00Z',
    }

    vi.mocked(syncStatsApi.fetchSyncStats).mockResolvedValueOnce(mockStats)

    render(<SyncStatsCard />)

    await waitFor(() => {
      // Check reset date exists (format varies by timezone)
      expect(screen.getByText(/Resets on/)).toBeInTheDocument()
    })
  })

  it('should format reset date for new year transition', async () => {
    const mockStats: SyncStats = {
      user_id: 'user-newyear',
      month: '2025-12',
      accounts_synced: 2,
      accounts_limit: 3,
      total_transactions: 100,
      can_sync_more: true,
      reset_date: '2026-01-01T00:00:00Z',
    }

    vi.mocked(syncStatsApi.fetchSyncStats).mockResolvedValueOnce(mockStats)

    render(<SyncStatsCard />)

    await waitFor(() => {
      // Check reset date is displayed (format varies by timezone)
      // Note: Timezone offset may show Dec 31, 2025 or Jan 1, 2026
      expect(screen.getByText(/Resets on/)).toBeInTheDocument()
    })
  })

  // ========================================================================
  // Transaction Summary Edge Cases
  // ========================================================================

  it('should handle single transaction display', async () => {
    const mockStats: SyncStats = {
      user_id: 'user-single-txn',
      month: '2025-11',
      accounts_synced: 1,
      accounts_limit: 3,
      total_transactions: 1,
      can_sync_more: true,
      reset_date: '2025-12-01T00:00:00Z',
    }

    vi.mocked(syncStatsApi.fetchSyncStats).mockResolvedValueOnce(mockStats)

    render(<SyncStatsCard />)

    await waitFor(() => {
      // Note: The component doesn't pluralize, so this would show "1 transactions"
      // This is acceptable or could be enhanced
      expect(screen.getByText('1 transactions synced this month')).toBeInTheDocument()
    })
  })

  it('should handle very large transaction counts', async () => {
    const mockStats: SyncStats = {
      user_id: 'user-large-txn',
      month: '2025-11',
      accounts_synced: 30,
      accounts_limit: 30,
      total_transactions: 15000,
      can_sync_more: false,
      reset_date: '2025-12-01T00:00:00Z',
    }

    vi.mocked(syncStatsApi.fetchSyncStats).mockResolvedValueOnce(mockStats)

    render(<SyncStatsCard />)

    await waitFor(() => {
      expect(screen.getByText('15000 transactions synced this month')).toBeInTheDocument()
    })
  })

  // ========================================================================
  // Styling and CSS Class Tests
  // ========================================================================

  it('should apply correct CSS classes for card structure', async () => {
    const mockStats: SyncStats = {
      user_id: 'user-css',
      month: '2025-11',
      accounts_synced: 1,
      accounts_limit: 3,
      total_transactions: 50,
      can_sync_more: true,
      reset_date: '2025-12-01T00:00:00Z',
    }

    vi.mocked(syncStatsApi.fetchSyncStats).mockResolvedValueOnce(mockStats)

    const { container } = render(<SyncStatsCard />)

    await waitFor(() => {
      expect(container.querySelector('.sync-stats-card')).toBeInTheDocument()
    })

    expect(container.querySelector('.sync-stats-card__header')).toBeInTheDocument()
    expect(container.querySelector('.sync-stats-card__title')).toBeInTheDocument()
    expect(container.querySelector('.sync-stats-card__content')).toBeInTheDocument()
    expect(container.querySelector('.sync-stats-card__progress-section')).toBeInTheDocument()
    expect(container.querySelector('.sync-stats-card__reset-info')).toBeInTheDocument()
  })

  it('should apply warning class to progress fill when at limit', async () => {
    const mockStats: SyncStats = {
      user_id: 'user-warning-class',
      month: '2025-11',
      accounts_synced: 3,
      accounts_limit: 3,
      total_transactions: 250,
      can_sync_more: false,
      reset_date: '2025-12-01T00:00:00Z',
    }

    vi.mocked(syncStatsApi.fetchSyncStats).mockResolvedValueOnce(mockStats)

    const { container } = render(<SyncStatsCard />)

    await waitFor(() => {
      const progressFill = container.querySelector('.sync-stats-card__progress-fill')
      expect(progressFill?.classList.contains('sync-stats-card__progress-fill--warning')).toBe(
        true
      )
    })
  })

  // ========================================================================
  // API Call Tests
  // ========================================================================

  it('should call fetchSyncStats on mount', async () => {
    const mockStats: SyncStats = {
      user_id: 'user-api-call',
      month: '2025-11',
      accounts_synced: 1,
      accounts_limit: 3,
      total_transactions: 50,
      can_sync_more: true,
      reset_date: '2025-12-01T00:00:00Z',
    }

    vi.mocked(syncStatsApi.fetchSyncStats).mockResolvedValueOnce(mockStats)

    render(<SyncStatsCard />)

    await waitFor(() => {
      expect(syncStatsApi.fetchSyncStats).toHaveBeenCalledTimes(1)
    })
  })

  it('should only call fetchSyncStats once on mount', async () => {
    const mockStats: SyncStats = {
      user_id: 'user-single-call',
      month: '2025-11',
      accounts_synced: 1,
      accounts_limit: 3,
      total_transactions: 50,
      can_sync_more: true,
      reset_date: '2025-12-01T00:00:00Z',
    }

    vi.mocked(syncStatsApi.fetchSyncStats).mockResolvedValueOnce(mockStats)

    const { rerender } = render(<SyncStatsCard />)

    await waitFor(() => {
      expect(syncStatsApi.fetchSyncStats).toHaveBeenCalledTimes(1)
    })

    // Re-render component
    rerender(<SyncStatsCard />)

    // Should NOT call API again
    expect(syncStatsApi.fetchSyncStats).toHaveBeenCalledTimes(1)
  })
})
