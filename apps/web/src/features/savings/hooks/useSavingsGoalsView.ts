import { useState, useMemo } from 'react'
import type { SavingsGoalWithProgress } from './useSavingsGoals'

export type SortKey = 'progress_desc' | 'deadline_asc' | 'target_desc'
export type TabKey = 'active' | 'completed' | 'all'

export interface UseSavingsGoalsViewResult {
  view: SavingsGoalWithProgress[]   // filtered + sorted
  tab: TabKey
  setTab: (tab: TabKey) => void
  sortKey: SortKey
  setSortKey: (key: SortKey) => void
}

export function useSavingsGoalsView(goals: SavingsGoalWithProgress[]): UseSavingsGoalsViewResult {
  const [tab, setTab] = useState<TabKey>('active')
  const [sortKey, setSortKey] = useState<SortKey>('progress_desc')

  const view = useMemo(() => {
    // 1. Filter
    let filtered = goals
    if (tab === 'active') filtered = goals.filter(g => !g.is_completed)
    else if (tab === 'completed') filtered = goals.filter(g => g.is_completed)

    // 2. Sort
    return [...filtered].sort((a, b) => {
      if (sortKey === 'progress_desc') return b.progress - a.progress
      if (sortKey === 'target_desc') return b.target_amount - a.target_amount
      if (sortKey === 'deadline_asc') {
        // null deadline goes LAST
        if (!a.deadline && !b.deadline) return a.name.localeCompare(b.name)
        if (!a.deadline) return 1
        if (!b.deadline) return -1
        return new Date(a.deadline).getTime() - new Date(b.deadline).getTime()
      }
      return 0
    })
  }, [goals, tab, sortKey])

  return { view, tab, setTab, sortKey, setSortKey }
}
