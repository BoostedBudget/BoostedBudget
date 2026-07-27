// Issue #1300: PlanningPage sub-tabs + URL state tests
// API hooks are mocked via bunfig.toml preloads (budgets-api.ts, savings-goals-api.ts,
// track-event.ts). Do NOT add mock.module() here — it leaks across test files in bun:test.
import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import React from 'react'
import { MemoryRouter, Routes, Route } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

import { PlanningPage } from './PlanningPage'

function renderPlanningPage(search = '') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[`/planning${search}`]}>
        <Routes>
          <Route path="/planning" element={<PlanningPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

describe('PlanningPage', () => {
  beforeEach(() => {
    mock.restore()
  })

  it('renders the Planning heading', () => {
    renderPlanningPage()
    expect(screen.getByRole('heading', { name: /planning/i })).toBeDefined()
  })

  it('defaults to Budgets tab', () => {
    renderPlanningPage()
    const tab = screen.getByRole('tab', { name: 'Budgets' })
    expect(tab).toBeDefined()
    expect(tab.getAttribute('data-state')).toBe('active')
  })

  it('activates Savings tab when ?tab=savings', () => {
    renderPlanningPage('?tab=savings')
    const tab = screen.getByRole('tab', { name: 'Savings' })
    expect(tab).toBeDefined()
    expect(tab.getAttribute('data-state')).toBe('active')
  })

  it('renders both Budgets and Savings tab triggers', () => {
    renderPlanningPage()
    expect(screen.getByRole('tab', { name: 'Budgets' })).toBeDefined()
    expect(screen.getByRole('tab', { name: 'Savings' })).toBeDefined()
  })
})
