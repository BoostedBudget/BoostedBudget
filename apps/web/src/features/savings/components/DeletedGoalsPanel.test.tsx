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
import { mockTrackEvent } from '@/test/mocks/track-event'
import {
  mockUseGetDeletedSavingsGoals,
  mockUseListDeletedSavingsGoalContributions,
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

// ── #1569 T7 / C2: the retained ledger has to be reachable from the user's seat ──────────────
//
// T2 made a deleted goal's contributions readable at
// GET /api/v1/savings-goals/deleted/{goalID}/contributions. With no UI, the DELETE description's
// promise ("every contribution logged against the goal is retained") was true for an API consumer
// and still false for the person who typed the amounts in. Hand-logged contributions are the case
// that matters: no provider can supply them again, so if the user cannot see them, retaining them
// buys the user nothing.
describe('DeletedGoalsPanel — retained contributions', () => {
  const manualContribution = {
    id: 'contrib-1',
    savings_goal_id: 'goal-9',
    amount: 500,
    occurred_on: '2025-03-14',
    // The tuple Insert (the hand-logging path) actually writes — see
    // savings_contribution_repository.go. A fixture with any other source is a row production
    // cannot produce; the valid_contribution_source CHECK rejects invented ones outright.
    source: 'manual',
    status: 'confirmed',
    confirmed_by_user: true,
    managed_by_reconciliation: false,
    created_at: '2025-03-14T00:00:00Z',
    updated_at: '2025-03-14T00:00:00Z',
  }
  const detectedContribution = {
    ...manualContribution,
    id: 'contrib-2',
    amount: 750,
    occurred_on: '2025-04-02',
    // The tuple UpsertProposedFromTransaction writes once confirmed.
    source: 'confirmed_transfer',
    confirmed_by_user: false,
    managed_by_reconciliation: true,
  }
  const proposedContribution = {
    ...detectedContribution,
    id: 'contrib-3',
    amount: 99,
    occurred_on: '2025-05-05',
    status: 'proposed',
  }

  function withLedger(body: unknown, extra: Record<string, unknown> = {}) {
    mockUseListDeletedSavingsGoalContributions.mockReturnValue({
      data: body,
      isLoading: false,
      error: null,
      ...extra,
    } as unknown as ReturnType<typeof mockUseListDeletedSavingsGoalContributions>)
  }

  beforeEach(() => {
    mockUseListDeletedSavingsGoalContributions.mockClear()
    mockTrackEvent.mockClear()
    withDeleted([deletedGoal])
    withLedger({
      contributions: [manualContribution, detectedContribution, proposedContribution],
      total_confirmed: 1250,
    })
  })

  function openLedger() {
    render(<DeletedGoalsPanel />)
    fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))
    fireEvent.click(screen.getByRole('button', { name: /contributions: japan trip/i }))
  }

  it('does not read the ledger until the user asks for it', () => {
    render(<DeletedGoalsPanel />)
    fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))
    expect(mockUseListDeletedSavingsGoalContributions).not.toHaveBeenCalled()
  })

  it('reads the ledger for the goal whose row was expanded', () => {
    openLedger()
    expect(mockUseListDeletedSavingsGoalContributions).toHaveBeenCalled()
    expect(mockUseListDeletedSavingsGoalContributions.mock.calls[0][0]).toBe('goal-9')
  })

  it('shows every retained contribution, with its date and amount', () => {
    openLedger()
    expect(screen.getByText('$500.00')).toBeTruthy()
    expect(screen.getByText('$750.00')).toBeTruthy()
    expect(screen.getByText('$99.00')).toBeTruthy()

    // fix round 1, item 2: this used to be `toMatch(/2025/)` against document.body, which the
    // PANEL'S OWN header line satisfied ("Deleted on June 1, 2025", from the fixture's
    // deleted_at) — deleting the ledger's entire date span left the suite green, and the test's
    // name became a false claim. Assert the formatted strings, which also pins the format AND
    // the field: these are `occurred_on`, not `created_at` (both are in the fixture and they
    // differ for the later two rows).
    expect(screen.getByText('Mar 14, 2025')).toBeTruthy()
    expect(screen.getByText('Apr 2, 2025')).toBeTruthy()
    expect(screen.getByText('May 5, 2025')).toBeTruthy()
  })

  it('dates each row by occurred_on, not by when the row was written', () => {
    openLedger()
    // created_at on every fixture row is 2025-03-14; occurred_on differs. A ledger keyed on
    // created_at would render three identical dates and still satisfy a /2025/ match.
    expect(screen.queryAllByText('Mar 14, 2025')).toHaveLength(1)
  })

  it('marks the hand-logged contribution as hand-logged — the one no provider can replace', () => {
    openLedger()
    expect(screen.getByText(/entered by hand/i)).toBeTruthy()
  })

  it('distinguishes a proposed contribution from a confirmed one in TEXT, not colour alone', () => {
    openLedger()
    expect(screen.getByText(/proposed/i)).toBeTruthy()
  })

  it('shows the confirmed total the endpoint computed, not a client-side guess', () => {
    openLedger()
    // 500 + 750; the proposed 99 is excluded by the endpoint, so a UI that summed the rows
    // itself would show $1,349.00 here.
    expect(screen.getByText('$1,250.00')).toBeTruthy()
  })

  it('says so plainly when a deleted goal has no contributions at all', () => {
    withLedger({ contributions: [], total_confirmed: 0 })
    openLedger()
    // fix round 1, item 3: NOT "were ever logged". A user can hard-delete an individual
    // contribution (DELETE /savings-goals/{goalID}/contributions/{id}, wired at
    // GoalContributionsSheet.tsx; DeleteContribution is in allowedHistoryDeletes), so an empty
    // ledger can mean "you deleted them" — and asserting the opposite is a false claim about the
    // user's money on the one surface built to avoid exactly that.
    expect(screen.getByText(/no contributions are recorded for this goal/i)).toBeTruthy()
    expect(document.body.textContent).not.toMatch(/ever logged/i)
  })

  it('treats a null contributions array as empty rather than crashing', () => {
    withLedger({ contributions: null, total_confirmed: 0 })
    openLedger()
    expect(screen.getByText(/no contributions are recorded for this goal/i)).toBeTruthy()
  })

  it('reports a failed read instead of implying the money was never there', () => {
    withLedger(undefined, { error: new Error('boom') })
    openLedger()
    const alert = screen.getByRole('alert')
    expect(alert.textContent).toMatch(/could not load/i)
    // The dangerous failure mode is a silent empty list that reads as "nothing was kept".
    expect(document.body.textContent).not.toMatch(/no contributions are recorded/i)
  })

  it('announces the load rather than rendering a blank region', () => {
    withLedger(undefined, { isLoading: true })
    openLedger()
    expect(screen.getByRole('status').textContent).toMatch(/loading/i)
  })

  it('collapses again, and stops reading, when the user closes the row', () => {
    openLedger()
    expect(screen.getByText('$500.00')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /contributions: japan trip/i }))
    expect(screen.queryByText('$500.00')).toBeNull()
  })

  it('exposes the row disclosure state with aria-expanded', () => {
    render(<DeletedGoalsPanel />)
    fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))
    const toggle = screen.getByRole('button', { name: /contributions: japan trip/i })
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    fireEvent.click(toggle)
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
  })

  it('names each row disclosure after its own goal', () => {
    withDeleted([deletedGoal, { ...deletedGoal, id: 'goal-10', name: 'New Roof' }])
    render(<DeletedGoalsPanel />)
    fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))
    expect(screen.getByRole('button', { name: /contributions: japan trip/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /contributions: new roof/i })).toBeTruthy()
  })

  // Mutation-driven: hard-coding the child's goalId to goals[0].id survived every other test,
  // because they all expand the first row. Opening the SECOND goal's ledger and reading back
  // which id was requested is the only thing that distinguishes "this goal's money" from
  // "whichever goal happens to be first".
  it('reads the SECOND goal\'s ledger when the second goal is the one expanded', () => {
    withDeleted([deletedGoal, { ...deletedGoal, id: 'goal-10', name: 'New Roof' }])
    render(<DeletedGoalsPanel />)
    fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))
    fireEvent.click(screen.getByRole('button', { name: /contributions: new roof/i }))

    expect(mockUseListDeletedSavingsGoalContributions).toHaveBeenCalled()
    const requested = mockUseListDeletedSavingsGoalContributions.mock.calls.map((c) => c[0])
    expect(requested).toContain('goal-10')
    expect(requested).not.toContain('goal-9')
  })

  // ── fix round 1, item 4: the region the toggle's aria-controls targets must be NAMED ────────
  //
  // Removing aria-labelledby from the populated branch left the suite green, and the isLoading,
  // error and empty branches never had it at all — so in three of four states a screen-reader
  // user following aria-controls landed in an unnamed region.
  describe('the ledger region is named in every state', () => {
    function regionFor(state: 'loaded' | 'loading' | 'error' | 'empty') {
      if (state === 'loading') withLedger(undefined, { isLoading: true })
      if (state === 'error') withLedger(undefined, { error: new Error('boom') })
      if (state === 'empty') withLedger({ contributions: [], total_confirmed: 0 })
      render(<DeletedGoalsPanel />)
      fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))
      const toggle = screen.getByRole('button', { name: /contributions: japan trip/i })
      fireEvent.click(toggle)
      const controls = toggle.getAttribute('aria-controls')
      expect(controls).toBeTruthy()
      const region = document.getElementById(controls!)
      expect(region).not.toBeNull()
      return region!
    }

    for (const state of ['loaded', 'loading', 'error', 'empty'] as const) {
      it(`names the region in the ${state} state`, () => {
        const region = regionFor(state)
        const labelledBy = region.getAttribute('aria-labelledby')
        expect(labelledBy).toBeTruthy()
        // It must point at the already-masked goal-name element, not carry the name itself.
        const label = document.getElementById(labelledBy!)
        expect(label).not.toBeNull()
        expect(label!.textContent).toBe('Japan Trip')
        expect(label!.hasAttribute('data-ph-mask')).toBe(true)
      })
    }
  })

  // ── fix round 1, item 5: WCAG AA ───────────────────────────────────────────────────────────
  //
  // The row's status pill was bg-muted/text-muted-foreground, which computes 4.34:1 at 12px in
  // the light theme — under the 4.5:1 floor. That pill is exactly where "Entered by hand /
  // Detected transfer / Proposed / Rejected" lives, i.e. the information that must not ride on
  // colour. The shared Badge's `secondary` pair clears AA in both themes.
  it('renders each row label through the shared Badge, not an ad-hoc muted pill', () => {
    openLedger()
    const pill = screen.getByText('Entered by hand')
    expect(pill.getAttribute('data-slot')).toBe('badge')
    expect(pill.className).toContain('bg-secondary')
    expect(pill.className).toContain('text-secondary-foreground')
    expect(pill.className).not.toContain('text-muted-foreground')
  })

  // ── fix round 1, item 6: measurement ───────────────────────────────────────────────────────
  //
  // "Did anyone actually go and look at the money we kept?" is the single question #1569 most
  // wants answered, and nothing was recording it.
  describe('analytics', () => {
    it('records that a user opened a retained ledger', () => {
      openLedger()
      expect(mockTrackEvent).toHaveBeenCalledWith('savings_goal_deleted_ledger_viewed')
    })

    it('does not record a view merely for listing the deleted goals', () => {
      render(<DeletedGoalsPanel />)
      fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))
      expect(mockTrackEvent).not.toHaveBeenCalled()
    })

    it('does not record a second view when the row is collapsed again', () => {
      openLedger()
      fireEvent.click(screen.getByRole('button', { name: /contributions: japan trip/i }))
      expect(mockTrackEvent).toHaveBeenCalledTimes(1)
    })
  })

  it('emits no duplicate element ids once a ledger is open', () => {
    withDeleted([deletedGoal, { ...deletedGoal, id: 'goal-10', name: 'New Roof' }])
    render(<DeletedGoalsPanel />)
    fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))
    fireEvent.click(screen.getByRole('button', { name: /contributions: japan trip/i }))
    const ids = Array.from(document.body.querySelectorAll('[id]')).map((el) => el.id)
    expect(ids.length).toBe(new Set(ids).size)
  })

  it('masks every ledger amount for session replay', () => {
    openLedger()
    const masked = Array.from(document.body.querySelectorAll('[data-ph-mask]'))
    for (const amount of ['$500.00', '$750.00', '$99.00', '$1,250.00']) {
      expect(masked.some((el) => el.textContent?.includes(amount))).toBe(true)
    }
  })

  it('writes no ledger amount into an attribute, which replay records verbatim', () => {
    openLedger()
    for (const el of Array.from(document.body.querySelectorAll('*'))) {
      for (const attr of Array.from(el.attributes)) {
        for (const amount of ['500.00', '750.00', '99.00', '1,250.00']) {
          expect(attr.value).not.toContain(amount)
        }
        expect(attr.value).not.toContain('Japan Trip')
      }
    }
  })
})
