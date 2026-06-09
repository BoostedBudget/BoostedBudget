/**
 * BoostsPage Tests
 * Issue #1267: Boosts page — shell with tabs, summary header, analytics
 *
 * TDD RED: Tests written before implementation.
 * Uses api-client-level mocks so custom hooks run through real code,
 * only mocking at the generated-client boundary.
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { screen, fireEvent, act } from '@testing-library/react'
import React from 'react'
import { MemoryRouter } from 'react-router-dom'

// Mock API client boundary (hooks call through to these)
import '@/test/mocks/transfer-suggestions-api'
import '@/test/mocks/savings-impact-api'
import '@/test/mocks/automation-rules-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import {
  mockUseGetTransferSuggestions,
  mockUseAcceptTransferSuggestion,
  mockUseRejectTransferSuggestion,
  mockUseConfirmTransferSuggestionMatch,
  mockUseDeclineTransferSuggestionMatch,
} from '@/test/mocks/transfer-suggestions-api'
import { mockUseGetSavingsImpactMe } from '@/test/mocks/savings-impact-api'

// Mock RulesPage to avoid rendering the full management UI in this test
mock.module('@/features/rules/components/RulesPage', () => ({
  RulesPage: () => <div data-testid="rules-page-mock">Rules Page</div>,
}))

// Ensure useTrackEvent returns our mock
mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))

import { renderWithQueryAndWrapper } from '@/test/utils/queryTestUtils'
import { BoostsPage } from './BoostsPage'

function renderBoostsPage(initialRoute = '/boosts') {
  return renderWithQueryAndWrapper(
    <BoostsPage />,
    ({ children }) => <MemoryRouter initialEntries={[initialRoute]}>{children}</MemoryRouter>
  )
}

describe('BoostsPage', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
    mockUseGetTransferSuggestions.mockClear()
    mockUseGetTransferSuggestions.mockReturnValue({ data: undefined, isLoading: false, error: null })
    mockUseAcceptTransferSuggestion.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseRejectTransferSuggestion.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseConfirmTransferSuggestionMatch.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeclineTransferSuggestionMatch.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseGetSavingsImpactMe.mockReturnValue({ data: null, isLoading: false, error: null })
  })

  it('renders the "Boosts" heading', () => {
    renderBoostsPage()
    expect(screen.getByRole('heading', { name: /boosts/i })).toBeTruthy()
  })

  it('renders all 4 tab triggers', () => {
    renderBoostsPage()
    expect(screen.getByRole('tab', { name: /active recommendations/i })).toBeTruthy()
    expect(screen.getByRole('tab', { name: /awaiting match/i })).toBeTruthy()
    expect(screen.getByRole('tab', { name: /history/i })).toBeTruthy()
    expect(screen.getByRole('tab', { name: /rules/i })).toBeTruthy()
  })

  it('defaults to Active Recommendations tab', () => {
    renderBoostsPage()
    const activeTab = screen.getByRole('tab', { name: /active recommendations/i })
    expect(activeTab.getAttribute('data-state')).toBe('active')
  })

  it('shows Active Recommendations tab content by default (empty state)', () => {
    renderBoostsPage()
    expect(screen.getByText(/no active recommendations/i)).toBeTruthy()
  })

  it('fires BOOSTS_PAGE_VIEWED analytics once on mount', async () => {
    renderBoostsPage()
    await act(async () => {})
    expect(mockTrackEvent).toHaveBeenCalledWith('boosts_page_viewed', expect.anything())
  })

  // Radix Tabs switches on focus (onFocus with activationMode !== 'manual').
  // Use fireEvent.focus to trigger the switch rather than fireEvent.click.

  it('switches to Awaiting Match tab when focused', async () => {
    renderBoostsPage()
    await act(async () => {
      fireEvent.focus(screen.getByRole('tab', { name: /awaiting match/i }))
    })
    const awaitingTab = screen.getByRole('tab', { name: /awaiting match/i })
    expect(awaitingTab.getAttribute('data-state')).toBe('active')
  })

  it('switches to History tab when focused', async () => {
    renderBoostsPage()
    await act(async () => {
      fireEvent.focus(screen.getByRole('tab', { name: /history/i }))
    })
    const historyTab = screen.getByRole('tab', { name: /history/i })
    expect(historyTab.getAttribute('data-state')).toBe('active')
  })

  it('switches to Rules tab when focused', async () => {
    renderBoostsPage()
    await act(async () => {
      fireEvent.focus(screen.getByRole('tab', { name: /rules/i }))
    })
    const rulesTab = screen.getByRole('tab', { name: /rules/i })
    expect(rulesTab.getAttribute('data-state')).toBe('active')
  })

  it('renders SavingsSummaryHeader when not loading', () => {
    mockUseGetSavingsImpactMe.mockReturnValue({
      data: { total_amount: 1500, month_amount: 200, prev_month_amount: 100, completed_count: 5, recent_items: null },
      isLoading: false,
      error: null,
    })
    renderBoostsPage()
    // The header shows the total moved value
    expect(screen.getByText(/1,500/)).toBeTruthy()
  })
})
