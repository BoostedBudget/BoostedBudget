/**
 * SavingsFiltersBar Tests
 * Issue #954: Savings page — T9
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { SavingsFiltersBar } from './SavingsFiltersBar'
import type { TabKey, SortKey } from './SavingsFiltersBar'

function renderFiltersBar(overrides?: {
  tab?: TabKey
  sortKey?: SortKey
  onTabChange?: (tab: TabKey) => void
  onSortChange?: (key: SortKey) => void
}) {
  const onTabChange = overrides?.onTabChange ?? mock(() => {})
  const onSortChange = overrides?.onSortChange ?? mock(() => {})
  render(
    <SavingsFiltersBar
      tab={overrides?.tab ?? 'active'}
      onTabChange={onTabChange}
      sortKey={overrides?.sortKey ?? 'progress_desc'}
      onSortChange={onSortChange}
      goalCount={3}
    />,
  )
  return { onTabChange, onSortChange }
}

describe('SavingsFiltersBar', () => {
  it('renders a tablist with three tabs', () => {
    renderFiltersBar()
    expect(screen.getByRole('tablist')).toBeDefined()
    const tabs = screen.getAllByRole('tab')
    expect(tabs.length).toBe(3)
  })

  it('active tab has aria-selected="true" when tab="active"', () => {
    renderFiltersBar({ tab: 'active' })
    const activeTab = screen.getByRole('tab', { name: 'Active' })
    expect(activeTab.getAttribute('aria-selected')).toBe('true')
  })

  it('sort control has aria-label="Sort goals"', () => {
    renderFiltersBar()
    expect(screen.getByRole('combobox', { name: 'Sort goals' })).toBeDefined()
  })

  it('clicking a tab calls onTabChange with the correct key', async () => {
    const user = userEvent.setup()
    const onTabChange = mock((_tab: TabKey) => {})
    renderFiltersBar({ onTabChange })
    await user.click(screen.getByRole('tab', { name: 'Completed' }))
    expect(onTabChange).toHaveBeenCalledWith('completed')
  })

  it('clicking the All tab calls onTabChange with "all"', async () => {
    const user = userEvent.setup()
    const onTabChange = mock((_tab: TabKey) => {})
    renderFiltersBar({ onTabChange })
    await user.click(screen.getByRole('tab', { name: 'All' }))
    expect(onTabChange).toHaveBeenCalledWith('all')
  })

  it('changing the sort select calls onSortChange with the new sort key', () => {
    const onSortChange = mock((_key: SortKey) => {})
    renderFiltersBar({ onSortChange })
    fireEvent.change(screen.getByRole('combobox', { name: 'Sort goals' }), {
      target: { value: 'deadline_asc' },
    })
    expect(onSortChange).toHaveBeenCalledWith('deadline_asc')
  })

  it('renders all three tab labels', () => {
    renderFiltersBar()
    expect(screen.getByRole('tab', { name: 'Active' })).toBeDefined()
    expect(screen.getByRole('tab', { name: 'Completed' })).toBeDefined()
    expect(screen.getByRole('tab', { name: 'All' })).toBeDefined()
  })

  it('completed tab has aria-selected="true" when tab="completed"', () => {
    renderFiltersBar({ tab: 'completed' })
    const completedTab = screen.getByRole('tab', { name: 'Completed' })
    expect(completedTab.getAttribute('aria-selected')).toBe('true')
    const activeTab = screen.getByRole('tab', { name: 'Active' })
    expect(activeTab.getAttribute('aria-selected')).toBe('false')
  })

  it('sort select displays all sort options', () => {
    renderFiltersBar()
    expect(screen.getByRole('option', { name: 'Progress: highest first' })).toBeDefined()
    expect(screen.getByRole('option', { name: 'Deadline: soonest first' })).toBeDefined()
    expect(screen.getByRole('option', { name: 'Target: highest first' })).toBeDefined()
  })
})
