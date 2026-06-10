/**
 * BoostDetailPage Tests
 * Issue #1267 Gap W5: /boosts/:id — boost detail + recent execution history
 *
 * TDD RED: Tests written before implementation.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import React from 'react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import type { Boost } from '@boostedbudget/api-client/models'
import { mockUseGetBoostByID } from '@/test/mocks/boosts-api'
import { BoostDetailPage } from './BoostDetailPage'

const mockBoost: Boost = {
  id: 'boost-detail-1',
  user_id: 'u1',
  name: 'Save Extra Cash',
  description: 'Transfer surplus to high-yield savings',
  boost_type: 'transfer_suggestion',
  enabled: true,
  priority: 10,
  execution_count: 8,
  trigger_config: { type: 'schedule', cron: '0 9 * * 1', timezone: 'America/New_York' },
  conditions: [{ field: 'budget.percent_used', operator: 'lte', value: 70 }],
  condition_logic: 'AND',
  actions: { amount: 200 },
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-10T00:00:00Z',
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
} as any

const renderDetail = (id = 'boost-detail-1') =>
  render(
    <MemoryRouter initialEntries={[`/boosts/${id}`]}>
      <Routes>
        <Route path="/boosts/:id" element={<BoostDetailPage />} />
      </Routes>
    </MemoryRouter>
  )

describe('BoostDetailPage', () => {
  beforeEach(() => {
    mockUseGetBoostByID.mockReturnValue({
      data: mockBoost,
      isLoading: false,
      error: null,
    })
  })

  it('renders the boost name as the page heading', () => {
    renderDetail()
    expect(screen.getByText('Save Extra Cash')).toBeTruthy()
  })

  it('masks boost name with data-ph-mask in the heading (PII — boost names can contain financial context)', () => {
    const { container } = renderDetail()
    const maskedSpan = container.querySelector('h1 [data-ph-mask]')
    expect(maskedSpan).toBeTruthy()
    expect(maskedSpan?.textContent).toBe('Save Extra Cash')
  })

  it('masks boost description with data-ph-mask (PII — descriptions can contain financial context)', () => {
    const { container } = renderDetail()
    const allMasked = container.querySelectorAll('[data-ph-mask]')
    const descMasked = Array.from(allMasked).some(
      (el) => el.textContent === 'Transfer surplus to high-yield savings'
    )
    expect(descMasked).toBe(true)
  })

  it('renders a back link to the boosts tab', () => {
    renderDetail()
    const bodyText = document.body.textContent ?? ''
    expect(/back.*boosts|boosts/i.test(bodyText)).toBe(true)
  })

  it('renders BoostExecutionHistory section', () => {
    renderDetail()
    // BoostExecutionHistory renders "No executions yet" when empty (mock returns [])
    const bodyText = document.body.textContent ?? ''
    expect(/recent executions|executions/i.test(bodyText)).toBe(true)
  })

  it('shows loading state when boost is loading', () => {
    mockUseGetBoostByID.mockReturnValue({ data: null, isLoading: true, error: null })
    const { container } = renderDetail()
    expect(container.querySelector('.animate-pulse') || screen.queryByText(/loading/i)).toBeTruthy()
  })

  it('shows not-found message when boost fetch returns null', () => {
    mockUseGetBoostByID.mockReturnValue({ data: null, isLoading: false, error: null })
    renderDetail('nonexistent-id')
    const bodyText = document.body.textContent ?? ''
    expect(/not found|not exist|couldn't find/i.test(bodyText)).toBe(true)
  })
})
