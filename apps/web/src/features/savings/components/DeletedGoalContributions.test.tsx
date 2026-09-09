// Uses preloaded API mocks from bunfig.toml — do NOT add mock.module() for
// savings-goals here; that is centralized in src/test/mocks/savings-goals-api.ts.

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { mockUseListDeletedSavingsGoalContributions } from '@/test/mocks/savings-goals-api'
import { render, screen } from '@testing-library/react'
import { DeletedGoalContributions } from './DeletedGoalContributions'
import type { SavingsGoalContribution } from '@boostedbudget/api-client/models'

const mockConfirmed: SavingsGoalContribution = {
  id: 'c-confirmed-1',
  savings_goal_id: 'goal-1',
  amount: 250,
  source: 'manual',
  status: 'confirmed',
  confirmed_by_user: true,
  managed_by_reconciliation: false,
  occurred_on: '2026-06-08',
  created_at: '2026-06-08T00:00:00Z',
  updated_at: '2026-06-08T00:00:00Z',
}

// #1625 Task 6: this row's period closed (progress_basis round-tripped) before the goal itself
// was ever deleted.
const mockArchived: SavingsGoalContribution = {
  id: 'c-archived-1',
  savings_goal_id: 'goal-1',
  amount: 400,
  source: 'manual',
  status: 'confirmed',
  confirmed_by_user: true,
  managed_by_reconciliation: false,
  occurred_on: '2026-05-01',
  created_at: '2026-05-01T00:00:00Z',
  updated_at: '2026-05-20T00:00:00Z',
  archived_at: '2026-05-20T00:00:00Z',
}

describe('DeletedGoalContributions', () => {
  beforeEach(() => {
    mockUseListDeletedSavingsGoalContributions.mockClear()
  })

  it('labels an archived row "Closed period" rather than by its source', () => {
    mockUseListDeletedSavingsGoalContributions.mockReturnValue({
      data: { contributions: [mockArchived], total_confirmed: 650 },
      isLoading: false,
      error: null,
      refetch: mock(() => Promise.resolve({})),
    })

    render(
      <DeletedGoalContributions goalId="goal-1" regionId="region-1" labelledBy="label-1" />
    )

    expect(screen.getByText('Closed period')).toBeInTheDocument()
    // Not mislabelled by source, the way a non-archived manual row would be.
    expect(screen.queryByText('Entered by hand')).not.toBeInTheDocument()
  })

  it('still labels a non-archived confirmed row by its source', () => {
    mockUseListDeletedSavingsGoalContributions.mockReturnValue({
      data: { contributions: [mockConfirmed], total_confirmed: 250 },
      isLoading: false,
      error: null,
      refetch: mock(() => Promise.resolve({})),
    })

    render(
      <DeletedGoalContributions goalId="goal-1" regionId="region-1" labelledBy="label-1" />
    )

    expect(screen.getByText('Entered by hand')).toBeInTheDocument()
    expect(screen.queryByText('Closed period')).not.toBeInTheDocument()
  })
})
