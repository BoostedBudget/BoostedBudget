import { describe, it, expect } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { useSavingsGoalsView } from './useSavingsGoalsView'
import type { SavingsGoalWithProgress } from './useSavingsGoals'

const makeGoal = (overrides: Partial<SavingsGoalWithProgress>): SavingsGoalWithProgress => ({
  id: '1',
  user_id: 'u1',
  name: 'Goal',
  description: undefined,
  target_amount: 1000,
  current_amount: 100,
  currency_code: 'USD',
  account_id: 'acc1',
  deadline: undefined,
  is_completed: false,
  completed_at: undefined,
  created_at: '2024-01-01',
  updated_at: '2024-01-01',
  progress: 10,
  ...overrides,
})

describe('useSavingsGoalsView', () => {
  it('default tab is active — shows only active goals', () => {
    const goals = [
      makeGoal({ id: '1', name: 'Active', is_completed: false }),
      makeGoal({ id: '2', name: 'Done', is_completed: true }),
    ]
    const { result } = renderHook(() => useSavingsGoalsView(goals))
    expect(result.current.tab).toBe('active')
    expect(result.current.view).toHaveLength(1)
    expect(result.current.view[0].id).toBe('1')
  })

  it('switching to completed tab shows only completed goals', () => {
    const goals = [
      makeGoal({ id: '1', name: 'Active', is_completed: false }),
      makeGoal({ id: '2', name: 'Done', is_completed: true }),
    ]
    const { result } = renderHook(() => useSavingsGoalsView(goals))
    act(() => result.current.setTab('completed'))
    expect(result.current.view).toHaveLength(1)
    expect(result.current.view[0].id).toBe('2')
  })

  it('switching to all tab shows all goals', () => {
    const goals = [
      makeGoal({ id: '1', name: 'Active', is_completed: false }),
      makeGoal({ id: '2', name: 'Done', is_completed: true }),
    ]
    const { result } = renderHook(() => useSavingsGoalsView(goals))
    act(() => result.current.setTab('all'))
    expect(result.current.view).toHaveLength(2)
  })

  it('sort: progress_desc — higher progress comes first', () => {
    const goals = [
      makeGoal({ id: '1', name: 'Low', progress: 20, is_completed: false }),
      makeGoal({ id: '2', name: 'High', progress: 80, is_completed: false }),
    ]
    const { result } = renderHook(() => useSavingsGoalsView(goals))
    expect(result.current.sortKey).toBe('progress_desc')
    expect(result.current.view[0].id).toBe('2')
    expect(result.current.view[1].id).toBe('1')
  })

  it('sort: target_desc — higher target_amount comes first', () => {
    const goals = [
      makeGoal({ id: '1', name: 'Small', target_amount: 500, is_completed: false }),
      makeGoal({ id: '2', name: 'Big', target_amount: 5000, is_completed: false }),
    ]
    const { result } = renderHook(() => useSavingsGoalsView(goals))
    act(() => result.current.setSortKey('target_desc'))
    expect(result.current.view[0].id).toBe('2')
    expect(result.current.view[1].id).toBe('1')
  })

  it('sort: deadline_asc — earlier deadline comes first; no-deadline goes last', () => {
    const goals = [
      makeGoal({ id: '1', name: 'Far', deadline: '2025-12-01', is_completed: false }),
      makeGoal({ id: '2', name: 'Near', deadline: '2025-06-01', is_completed: false }),
      makeGoal({ id: '3', name: 'NoDate', deadline: undefined, is_completed: false }),
    ]
    const { result } = renderHook(() => useSavingsGoalsView(goals))
    act(() => result.current.setSortKey('deadline_asc'))
    expect(result.current.view[0].id).toBe('2')
    expect(result.current.view[1].id).toBe('1')
    expect(result.current.view[2].id).toBe('3')
  })

  it('sort: deadline_asc null stability — two null-deadline goals sorted by name', () => {
    const goals = [
      makeGoal({ id: '1', name: 'Zebra', deadline: undefined, is_completed: false }),
      makeGoal({ id: '2', name: 'Apple', deadline: undefined, is_completed: false }),
    ]
    const { result } = renderHook(() => useSavingsGoalsView(goals))
    act(() => result.current.setSortKey('deadline_asc'))
    expect(result.current.view[0].id).toBe('2') // Apple before Zebra
    expect(result.current.view[1].id).toBe('1')
  })

  it('tab change updates view', () => {
    const goals = [
      makeGoal({ id: '1', name: 'Active', is_completed: false }),
      makeGoal({ id: '2', name: 'Done', is_completed: true }),
    ]
    const { result } = renderHook(() => useSavingsGoalsView(goals))
    expect(result.current.view).toHaveLength(1)
    act(() => result.current.setTab('completed'))
    expect(result.current.view).toHaveLength(1)
    expect(result.current.view[0].id).toBe('2')
  })

  it('sort change updates view', () => {
    const goals = [
      makeGoal({ id: '1', name: 'Small', target_amount: 500, progress: 80, is_completed: false }),
      makeGoal({ id: '2', name: 'Big', target_amount: 5000, progress: 10, is_completed: false }),
    ]
    const { result } = renderHook(() => useSavingsGoalsView(goals))
    // default: progress_desc -> id '1' first (80% > 10%)
    expect(result.current.view[0].id).toBe('1')
    act(() => result.current.setSortKey('target_desc'))
    // after: target_desc -> id '2' first (5000 > 500)
    expect(result.current.view[0].id).toBe('2')
  })

  it('empty goals array returns empty view regardless of tab/sort', () => {
    const { result } = renderHook(() => useSavingsGoalsView([]))
    expect(result.current.view).toHaveLength(0)
    act(() => result.current.setTab('all'))
    expect(result.current.view).toHaveLength(0)
    act(() => result.current.setSortKey('target_desc'))
    expect(result.current.view).toHaveLength(0)
  })
})
