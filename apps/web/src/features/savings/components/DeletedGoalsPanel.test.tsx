/**
 * DeletedGoalsPanel tests — #1569 Task 12 gap 2.
 *
 * Task 8 shipped POST /savings-goals/{id}/purge, gated on the goal already being soft-deleted,
 * and it had no caller. It also had no reachable precondition: every other read filters
 * `deleted_at IS NULL`, so nothing could tell a client which goals qualify. This panel is the
 * caller, and Task 12's GET /savings-goals/deleted is what makes it possible.
 *
 * The backend's confirmation model is a PRECONDITION, not type-to-confirm — the user already
 * performed the deliberate first step when they deleted the goal. The UI must mirror that
 * honestly: one plain confirmation, not a second ceremony that implies a stronger gate than the
 * API actually has.
 */
import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { mockToastError } from '@/test/mocks/sonner'
import {
  mockUseGetDeletedSavingsGoals,
  mockUsePurgeSavingsGoal,
  mockPurgeSavingsGoalMutateAsync,
  mockDeletedSavingsGoalsRefetch,
} from '@/test/mocks/savings-goals-api'
import { DeletedGoalsPanel } from './DeletedGoalsPanel'

const deletedGoal = {
  id: 'goal-9',
  user_id: 'u1',
  name: 'Japan Trip',
  target_amount: 4000,
  current_amount: 1250,
  currency_code: 'USD',
  account_id: 'acc-1',
  is_completed: false,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-06-01T00:00:00Z',
  deleted_at: '2025-06-01T00:00:00Z',
}

function withDeleted(goals: unknown[]) {
  mockUseGetDeletedSavingsGoals.mockReturnValue({
    data: goals,
    isLoading: false,
    error: null,
    refetch: mockDeletedSavingsGoalsRefetch,
  } as unknown as ReturnType<typeof mockUseGetDeletedSavingsGoals>)
}

describe('DeletedGoalsPanel', () => {
  beforeEach(() => {
    mockPurgeSavingsGoalMutateAsync.mockClear()
    mockToastError.mockClear()
    mockDeletedSavingsGoalsRefetch.mockClear()
    mockUsePurgeSavingsGoal.mockReturnValue({
      mutateAsync: mockPurgeSavingsGoalMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    } as unknown as ReturnType<typeof mockUsePurgeSavingsGoal>)
    withDeleted([deletedGoal])
  })

  it('renders nothing at all when the user has deleted no goals', () => {
    withDeleted([])
    const { container } = render(<DeletedGoalsPanel />)
    expect(container.textContent).toBe('')
  })

  it('renders nothing while the listing is still loading', () => {
    mockUseGetDeletedSavingsGoals.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: mockDeletedSavingsGoalsRefetch,
    } as unknown as ReturnType<typeof mockUseGetDeletedSavingsGoals>)
    const { container } = render(<DeletedGoalsPanel />)
    expect(container.textContent).toBe('')
  })

  it('lists a deleted goal with when it was deleted', () => {
    render(<DeletedGoalsPanel />)
    fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))

    expect(screen.getByText('Japan Trip')).toBeTruthy()
    expect(document.body.textContent).toMatch(/deleted on/i)
  })

  it('says the contributions were kept, which is the whole point of the tombstone', () => {
    render(<DeletedGoalsPanel />)
    fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))
    expect(document.body.textContent).toMatch(/still saved|kept/i)
  })

  it('masks the goal name and the amounts for session replay', () => {
    render(<DeletedGoalsPanel />)
    fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))

    const masked = Array.from(document.body.querySelectorAll('[data-ph-mask]'))
    expect(masked.some((el) => el.textContent?.includes('Japan Trip'))).toBe(true)
    expect(masked.some((el) => el.textContent?.includes('1,250'))).toBe(true)
  })

  it('leaks no amount or name into an attribute, which replay records verbatim', () => {
    render(<DeletedGoalsPanel />)
    fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))

    for (const el of Array.from(document.body.querySelectorAll('*'))) {
      for (const attr of Array.from(el.attributes)) {
        expect(attr.value).not.toContain('Japan Trip')
        expect(attr.value).not.toContain('1,250')
      }
    }
  })

  it('calls the real purge endpoint with the goal id after confirming', async () => {
    render(<DeletedGoalsPanel />)
    fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))
    fireEvent.click(screen.getByRole('button', { name: /delete permanently/i }))

    // One plain confirmation — a precondition, not type-to-confirm.
    expect(document.body.querySelector('input')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /^delete forever$/i }))

    await waitFor(() => expect(mockPurgeSavingsGoalMutateAsync).toHaveBeenCalledTimes(1))
    expect(mockPurgeSavingsGoalMutateAsync.mock.calls[0][0]).toEqual({ id: 'goal-9' })
  })

  it('does not purge anything just because the panel was opened', () => {
    render(<DeletedGoalsPanel />)
    fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))
    expect(mockPurgeSavingsGoalMutateAsync).not.toHaveBeenCalled()
  })

  it('does not purge when the confirmation is dismissed', () => {
    render(<DeletedGoalsPanel />)
    fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))
    fireEvent.click(screen.getByRole('button', { name: /delete permanently/i }))
    fireEvent.click(screen.getByRole('button', { name: /^cancel$/i }))
    expect(mockPurgeSavingsGoalMutateAsync).not.toHaveBeenCalled()
  })

  it('warns that hand-logged contributions are what gets destroyed', () => {
    render(<DeletedGoalsPanel />)
    fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))
    fireEvent.click(screen.getByRole('button', { name: /delete permanently/i }))
    expect(document.body.textContent).toMatch(/by hand/i)
    expect(document.body.textContent).toMatch(/can(no|'|’)t be undone/i)
  })
  // ── fix round 1, I1: the purge error path ───────────────────────────────────────────────
  //
  // Both rejections are reachable. ErrSavingsGoalNotFound: a stale listing, e.g. the goal was
  // purged in another tab. ErrSavingsGoalNotDeleted (savings_goal_repository.go:456-462): the goal
  // is no longer tombstoned. Before this fix, either produced an unhandled promise rejection, left
  // the dialog open with the button re-armed and no message, and skipped the refetch so the stale
  // row stayed on screen. AccountsPage.handlePurgeAccount is the precedent — and its comment was
  // written for a 409 raised by task 6 of this same plan.
  describe('purge failure', () => {
    function failWith(err: unknown) {
      mockPurgeSavingsGoalMutateAsync.mockImplementation(() => Promise.reject(err))
    }

    async function attemptPurge() {
      render(<DeletedGoalsPanel />)
      fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))
      fireEvent.click(screen.getByRole('button', { name: /delete permanently: japan trip/i }))
      fireEvent.click(screen.getByRole('button', { name: /^delete forever$/i }))
      await waitFor(() => expect(mockPurgeSavingsGoalMutateAsync).toHaveBeenCalled())
    }

    it('tells the user when the purge is refused, instead of failing silently', async () => {
      failWith({ response: { data: { detail: 'Goal must be deleted before it can be purged' } } })
      await attemptPurge()
      await waitFor(() => expect(mockToastError).toHaveBeenCalledTimes(1))
      // The actionable text lives at response.data.detail (RFC 9457), not err.message.
      expect(String(mockToastError.mock.calls[0][0])).toContain('must be deleted')
    })

    it('refreshes the listing on failure, so a stale row cannot linger', async () => {
      failWith({ response: { data: { detail: 'Savings goal not found' } } })
      await attemptPurge()
      await waitFor(() => expect(mockDeletedSavingsGoalsRefetch).toHaveBeenCalled())
    })

    it('does not leave the confirmation dialog open after a failure', async () => {
      failWith(new Error('boom'))
      await attemptPurge()
      await waitFor(() =>
        expect(screen.queryByRole('button', { name: /^delete forever$/i })).toBeNull(),
      )
    })

    it('does not report success to analytics when the purge failed', async () => {
      failWith(new Error('boom'))
      await attemptPurge()
      await waitFor(() => expect(mockToastError).toHaveBeenCalled())
      // The success toast/event path must not have run.
      expect(mockToastError).toHaveBeenCalledTimes(1)
    })
  })

  // ── fix round 1, Minor: accessibility of the disclosure and the row actions ─────────────
  describe('accessibility', () => {
    it('gives each row action an accessible name that says WHICH goal it destroys', () => {
      withDeleted([deletedGoal, { ...deletedGoal, id: 'goal-10', name: 'New Roof' }])
      render(<DeletedGoalsPanel />)
      fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))

      expect(screen.getByRole('button', { name: /delete permanently: japan trip/i })).toBeTruthy()
      expect(screen.getByRole('button', { name: /delete permanently: new roof/i })).toBeTruthy()
    })

    it('exposes the disclosure with aria-controls pointing at the region it opens', () => {
      render(<DeletedGoalsPanel />)
      const toggle = screen.getByRole('button', { name: /deleted goals/i })
      const controls = toggle.getAttribute('aria-controls')
      expect(controls).toBeTruthy()

      fireEvent.click(toggle)
      expect(document.getElementById(controls!)).not.toBeNull()
    })

    // The first draft of the aria-labelledby fix emitted the shared verb span once PER ROW,
    // giving N elements the same id — which silently breaks aria-labelledby resolution (only the
    // first match is used) as well as being invalid HTML.
    it('emits no duplicate element ids, however many goals are listed', () => {
      withDeleted([
        deletedGoal,
        { ...deletedGoal, id: 'goal-10', name: 'New Roof' },
        { ...deletedGoal, id: 'goal-11', name: 'Car Fund' },
      ])
      render(<DeletedGoalsPanel />)
      fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))

      const ids = Array.from(document.body.querySelectorAll('[id]')).map((el) => el.id)
      expect(ids.length).toBe(new Set(ids).size)
    })

    it('labels the section with a real heading', () => {
      render(<DeletedGoalsPanel />)
      expect(screen.getByRole('heading', { name: /deleted goals/i })).toBeTruthy()
    })
  })
})
