/**
 * BoostAutomationHistoryPanel tests — Issue #1569 Task 7.3, item 1.
 *
 * `boosts` is a tombstone table: deleting a boost sets `deleted_at` and destroys nothing, so the
 * rows in `boost_executions` are RETAINED. But every boost-scoped read filters on `boost_id`, and
 * `GetBoostExecutions` first calls `GetBoostByID` (which excludes tombstoned rows), so the only
 * in-app consumer could not reach them. `GET /api/v1/boost-executions` exists and its published
 * description advertises exactly this affordance — and had zero consumers in `apps/web/src`.
 *
 * The RED these tests encode: a user can see the executions of a boost they deleted.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { Boost, BoostExecution } from '@boostedbudget/api-client/models'
import {
  mockUseGetBoosts,
  mockUseGetUserBoostExecutions,
  mockRefetchUserBoostExecutions,
} from '@/test/mocks/boosts-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { BoostAutomationHistoryPanel } from './BoostAutomationHistoryPanel'

const liveBoost = { id: 'boost-live', name: 'Payday sweep' } as Boost

const liveExecution: BoostExecution = {
  id: 'exec-live',
  boost_id: 'boost-live',
  boost_name: 'Payday sweep',
  user_id: 'user-1',
  status: 'completed',
  started_at: '2024-03-01T10:00:00Z',
  execution_duration_ms: 1200,
  retry_count: 0,
  max_retries: 3,
  trigger_context: { type: 'schedule' },
}

/** Boost tombstoned: the row still holds a boost_id, but no live boost has that id. */
const tombstonedExecution: BoostExecution = {
  id: 'exec-tombstoned',
  boost_id: 'boost-deleted',
  boost_name: 'Old rainy-day rule',
  user_id: 'user-1',
  status: 'completed',
  started_at: '2024-02-01T10:00:00Z',
  execution_duration_ms: 900,
  retry_count: 0,
  max_retries: 3,
  trigger_context: { type: 'schedule' },
}

/** Boost PURGED: fk_boost_executions_boost_id is ON DELETE SET NULL, so boost_id is gone. */
const orphanedExecution: BoostExecution = {
  id: 'exec-orphan',
  boost_id: undefined,
  boost_name: 'Purged transfer rule',
  user_id: 'user-1',
  status: 'failed',
  started_at: '2024-01-01T10:00:00Z',
  execution_duration_ms: 400,
  error_message: 'Transfer of $250.00 to Chase Savings failed',
  retry_count: 3,
  max_retries: 3,
  trigger_context: { type: 'manual' },
}

function renderPanel() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <BoostAutomationHistoryPanel />
    </QueryClientProvider>
  )
}

function setExecutions(executions: BoostExecution[], overrides: Record<string, unknown> = {}) {
  mockUseGetUserBoostExecutions.mockReturnValue({
    data: { executions, total: executions.length, limit: 20, offset: 0 },
    isLoading: false,
    error: null,
    refetch: mockRefetchUserBoostExecutions,
    ...overrides,
  } as ReturnType<typeof mockUseGetUserBoostExecutions>)
}

function expand() {
  fireEvent.click(screen.getByRole('button', { name: /automation history/i }))
}

describe('BoostAutomationHistoryPanel', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
    mockRefetchUserBoostExecutions.mockClear()
    mockUseGetBoosts.mockReturnValue({
      data: [liveBoost],
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    } as ReturnType<typeof mockUseGetBoosts>)
    setExecutions([liveExecution, tombstonedExecution, orphanedExecution])
  })

  it('reaches the account-wide endpoint, not the per-boost one', () => {
    renderPanel()
    expect(mockUseGetUserBoostExecutions).toHaveBeenCalled()
  })

  it('shows the run history of a boost the user DELETED (the whole point)', () => {
    renderPanel()
    expand()
    expect(screen.getByText('Old rainy-day rule')).toBeInTheDocument()
  })

  it('shows the run history of a boost that was PURGED, which carries no boost_id at all', () => {
    renderPanel()
    expand()
    expect(screen.getByText('Purged transfer rule')).toBeInTheDocument()
  })

  it('marks deleted-boost rows in TEXT, never by colour alone', () => {
    renderPanel()
    expand()
    const deletedMarkers = screen.getAllByText(/^Deleted boost$/)
    expect(deletedMarkers.length).toBe(2)
  })

  it('does not mark a row whose boost is still live', () => {
    setExecutions([liveExecution])
    renderPanel()
    expand()
    expect(screen.queryByText(/^Deleted boost$/)).not.toBeInTheDocument()
  })

  it('masks the boost name and the error text for session replay', () => {
    renderPanel()
    expand()
    const name = screen.getByText('Old rainy-day rule')
    expect(name.closest('[data-ph-mask]')).not.toBeNull()

    const error = screen.getByText(/Transfer of \$250\.00/)
    expect(error.closest('[data-ph-mask]')).not.toBeNull()
  })

  it('leaves the "Deleted boost" marker OUTSIDE the mask so replay keeps the signal', () => {
    renderPanel()
    expand()
    const marker = screen.getAllByText(/^Deleted boost$/)[0]
    expect(marker.closest('[data-ph-mask]')).toBeNull()
  })

  it('is collapsed by default — retained history should not compete with live boosts', () => {
    renderPanel()
    expect(screen.queryByText('Old rainy-day rule')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /automation history/i })).toHaveAttribute(
      'aria-expanded',
      'false'
    )
  })

  it('wires aria-controls to the region it discloses', () => {
    renderPanel()
    const toggle = screen.getByRole('button', { name: /automation history/i })
    const regionId = toggle.getAttribute('aria-controls')
    expect(regionId).toBeTruthy()
    expand()
    expect(document.getElementById(regionId as string)).not.toBeNull()
  })

  it('fires the view event on expand, with a boolean discriminator and no identity', () => {
    renderPanel()
    expand()
    expect(mockTrackEvent).toHaveBeenCalledWith(
      ANALYTICS_EVENTS.BOOST_EXECUTION_HISTORY_VIEWED,
      { has_deleted_boost_rows: true, deleted_markers_unavailable: false }
    )
    const [, props] = mockTrackEvent.mock.calls[0] as unknown as [string, Record<string, unknown>]
    expect(JSON.stringify(props)).not.toMatch(/Old rainy-day rule|boost-deleted|exec-/)
  })

  it('does not fire the view event again when collapsing', () => {
    renderPanel()
    expand()
    expand()
    expect(mockTrackEvent).toHaveBeenCalledTimes(1)
  })

  it('names a loading state', () => {
    setExecutions([], { isLoading: true, data: undefined })
    renderPanel()
    expand()
    expect(screen.getByRole('status')).toBeInTheDocument()
  })

  it('names an error state that does not claim the history is gone', () => {
    setExecutions([], { error: new Error('boom'), data: undefined })
    renderPanel()
    expand()
    const alert = screen.getByRole('alert')
    expect(alert.textContent ?? '').toMatch(/still kept/i)
    expect(alert.textContent ?? '').not.toMatch(/lost|gone|deleted/i)
  })

  it('names an empty state', () => {
    setExecutions([], { error: new Error('boom'), data: undefined })
    const { unmount } = renderPanel()
    unmount()

    setExecutions([])
    renderPanel()
    // Nothing has ever run: the section renders no toggle at all rather than an empty promise.
    expect(screen.queryByRole('button', { name: /automation history/i })).not.toBeInTheDocument()
  })

  it('offers a retry from the error state', () => {
    setExecutions([], { error: new Error('boom'), data: undefined })
    renderPanel()
    expand()
    fireEvent.click(screen.getByRole('button', { name: /retry/i }))
    expect(mockRefetchUserBoostExecutions).toHaveBeenCalled()
  })

  it('renders each execution once', () => {
    renderPanel()
    expand()
    const region = document.getElementById(
      screen.getByRole('button', { name: /automation history/i }).getAttribute('aria-controls') as string
    )
    expect(within(region as HTMLElement).getAllByText('Old rainy-day rule').length).toBe(1)
  })
})

// Issue #1569 T7-FIX, finding C. The live-boost query's error was ignored: on failure `liveBoosts`
// is undefined, `liveBoostIds` stays null and EVERY row is treated as non-deleted, so the panel
// rendered a complete-looking list with none of the "Deleted boost" markers it promises. Failing
// open on a "this data was retained" marker is the wrong direction for this issue.
describe('BoostAutomationHistoryPanel when the live-boost list fails (#1569)', () => {
  function withFailedLiveBoosts() {
    mockUseGetBoosts.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('boosts unavailable'),
      refetch: () => Promise.resolve(),
    } as unknown as ReturnType<typeof mockUseGetBoosts>)
  }

  it('says the markers are missing rather than silently rendering none', () => {
    withFailedLiveBoosts()
    renderPanel()
    expand()

    expect(screen.queryAllByText(/^Deleted boost$/).length).toBe(0)
    expect(screen.getByText(/could not check which of your boosts are still active/i)).toBeInTheDocument()
  })

  // The retained rows themselves are NOT withheld: a second, decorative request failing must not
  // take down the history this panel exists to show.
  it('still lists every retained execution', () => {
    withFailedLiveBoosts()
    renderPanel()
    expand()

    expect(screen.getByText('Old rainy-day rule')).toBeInTheDocument()
    expect(screen.getByText('Purged transfer rule')).toBeInTheDocument()
  })

  it('tells analytics the view was degraded, so it is not counted as "nothing was deleted"', () => {
    withFailedLiveBoosts()
    renderPanel()
    expand()

    expect(mockTrackEvent).toHaveBeenCalledWith(
      expect.anything(),
      expect.objectContaining({ deleted_markers_unavailable: true })
    )
  })

  // #1569 T8-FIX, finding B. React Query keeps the last SUCCESSFUL live-boost list when a later
  // refetch fails, so `data` and `error` are set at the same time. Every fixture above uses
  // `data: undefined` — the never-succeeded case only — and with stale data the panel went on
  // classifying rows from a set it had, two elements higher, told the user it could not check. A
  // row could be marked deleted from a stale empty list, or stay unmarked after its boost had
  // gone; either way the UI contradicted its own banner.
  it('marks nothing from a STALE live list while the banner says markers are unavailable', () => {
    setExecutions([liveExecution, tombstonedExecution, orphanedExecution])
    mockUseGetBoosts.mockReturnValue({
      data: [liveBoost],
      isLoading: false,
      error: new Error('boosts unavailable'),
      refetch: () => Promise.resolve(),
    } as unknown as ReturnType<typeof mockUseGetBoosts>)
    renderPanel()
    expand()

    expect(
      screen.getByText(/could not check which of your boosts are still active/i)
    ).toBeInTheDocument()
    expect(screen.queryAllByText(/^Deleted boost$/).length).toBe(0)
  })

  it('shows no such notice while the live list is merely still loading', () => {
    mockUseGetBoosts.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: () => Promise.resolve(),
    } as unknown as ReturnType<typeof mockUseGetBoosts>)
    renderPanel()
    expand()

    expect(screen.queryByText(/could not check which of your boosts are still active/i)).toBeNull()
  })

  it('shows no such notice on the happy path', () => {
    renderPanel()
    expand()

    expect(screen.queryByText(/could not check which of your boosts are still active/i)).toBeNull()
    expect(mockTrackEvent).toHaveBeenCalledWith(
      expect.anything(),
      expect.objectContaining({ deleted_markers_unavailable: false })
    )
  })
})
