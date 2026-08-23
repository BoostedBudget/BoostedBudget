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
})
