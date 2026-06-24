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

// Mock API client boundary (all preloaded via bunfig.toml)
import { mockTrackEvent } from '@/test/mocks/track-event'
import {
  mockUseGetTransferSuggestions,
  mockUseAcceptTransferSuggestion,
  mockUseRejectTransferSuggestion,
  mockUseConfirmTransferSuggestionMatch,
  mockUseDeclineTransferSuggestionMatch,
} from '@/test/mocks/transfer-suggestions-api'
import { mockUseGetSavingsImpactMe } from '@/test/mocks/savings-impact-api'
import { mockUseGetBoosts } from '@/test/mocks/boosts-api'

mock.module('./BoostsTab', () => ({
  BoostsTab: () => <div data-testid="boosts-page-mock">Boosts Page</div>,
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
    localStorage.clear()
    mockTrackEvent.mockClear()

    // Reset all API mocks to default (success/empty) state
    mockUseGetTransferSuggestions.mockClear()
    mockUseGetTransferSuggestions.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(() => {}) })

    const emptyMutate = { mutate: mock(() => {}), isPending: false }
    mockUseAcceptTransferSuggestion.mockReturnValue(emptyMutate)
    mockUseRejectTransferSuggestion.mockReturnValue(emptyMutate)
    mockUseConfirmTransferSuggestionMatch.mockReturnValue(emptyMutate)
    mockUseDeclineTransferSuggestionMatch.mockReturnValue(emptyMutate)

    mockUseGetSavingsImpactMe.mockClear()
    mockUseGetSavingsImpactMe.mockReturnValue({ data: null, isLoading: false, error: null, refetch: mock(() => {}) })

    mockUseGetBoosts.mockClear()
    mockUseGetBoosts.mockReturnValue({ data: [], isLoading: false, error: null, refetch: mock(() => {}) })
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
    expect(screen.getByRole('tab', { name: /boosts/i })).toBeTruthy()
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
    expect(mockTrackEvent).toHaveBeenCalledWith('boosts_page_viewed', { tab: 'active' })
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

  // R6-1: assert BOOSTS_TAB_CHANGED fires when switching tabs
  it('fires BOOSTS_TAB_CHANGED analytics when switching to Awaiting Match tab', async () => {
    renderBoostsPage()
    await act(async () => {
      fireEvent.focus(screen.getByRole('tab', { name: /awaiting match/i }))
    })
    expect(mockTrackEvent).toHaveBeenCalledWith('boosts_tab_changed', { tab: 'awaiting' })
  })

  it('switches to History tab when focused', async () => {
    renderBoostsPage()
    await act(async () => {
      fireEvent.focus(screen.getByRole('tab', { name: /history/i }))
    })
    const historyTab = screen.getByRole('tab', { name: /history/i })
    expect(historyTab.getAttribute('data-state')).toBe('active')
  })

  it('switches to Boosts tab when focused', async () => {
    renderBoostsPage()
    await act(async () => {
      fireEvent.focus(screen.getByRole('tab', { name: /boosts/i }))
    })
    const boostsTab = screen.getByRole('tab', { name: /boosts/i })
    expect(boostsTab.getAttribute('data-state')).toBe('active')
  })

  it('renders SavingsSummaryHeader when not loading', () => {
    mockUseGetSavingsImpactMe.mockReturnValue({
      data: { total_amount: 1500, month_amount: 200, prev_month_amount: 100, completed_count: 5, recent_items: null },
      isLoading: false,
      error: null,
      refetch: mock(() => {}),
    })
    renderBoostsPage()
    // The header shows the total moved value
    expect(screen.getByText(/1,500/)).toBeTruthy()
  })

  // R7-2: error states wired through BoostsPage
  it('shows error state in Active tab when transfer suggestions fetch fails', async () => {
    mockUseGetTransferSuggestions.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('network error'),
      refetch: mock(() => {}),
    })
    renderBoostsPage()
    // The active tab (default) should show ErrorState not empty state
    const { container } = renderBoostsPage()
    expect(container.querySelector('[data-slot="error-state"]')).toBeTruthy()
  })

  it('shows error state in Awaiting Match tab when transfer suggestions fetch fails', async () => {
    mockUseGetTransferSuggestions.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('network error'),
      refetch: mock(() => {}),
    })
    const { container } = renderBoostsPage()
    await act(async () => {
      fireEvent.focus(screen.getByRole('tab', { name: /awaiting match/i }))
    })
    expect(container.querySelector('[data-slot="error-state"]')).toBeTruthy()
  })

  it('shows error state in History tab when transfer suggestions fetch fails', async () => {
    mockUseGetTransferSuggestions.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('network error'),
      refetch: mock(() => {}),
    })
    const { container } = renderBoostsPage()
    await act(async () => {
      fireEvent.focus(screen.getByRole('tab', { name: /history/i }))
    })
    expect(container.querySelector('[data-slot="error-state"]')).toBeTruthy()
  })

  it('shows summary error fallback when savings impact fetch fails', async () => {
    mockUseGetSavingsImpactMe.mockReturnValue({
      data: null,
      isLoading: false,
      error: new Error('impact error'),
      refetch: mock(() => {}),
    })
    renderBoostsPage()
    expect(screen.getByText(/unable to load savings summary/i)).toBeTruthy()
  })

  it('calls summary refetch when Retry is clicked in summary error fallback', async () => {
    const mockSavingsRefetch = mock(() => Promise.resolve({ data: null }))
    mockUseGetSavingsImpactMe.mockReturnValue({
      data: null,
      isLoading: false,
      error: new Error('impact error'),
      refetch: mockSavingsRefetch,
    })
    renderBoostsPage()
    fireEvent.click(screen.getByRole('button', { name: /retry/i }))
    expect(mockSavingsRefetch).toHaveBeenCalledTimes(1)
  })

  // Issue #1300: always-visible Create Boost button + tab rename
  it('renders the "Create Boost" button in the page header', () => {
    renderBoostsPage()
    const createBtn = screen.getByRole('button', { name: /create new boost/i })
    expect(createBtn).toBeTruthy()
  })

  it('fires BOOSTS_CREATE_CLICKED analytics when Create Boost is clicked', async () => {
    renderBoostsPage()
    const createBtn = screen.getByRole('button', { name: /create new boost/i })
    fireEvent.click(createBtn)
    await act(async () => {})
    expect(mockTrackEvent).toHaveBeenCalledWith('boosts_create_clicked', { source: 'page_header' })
  })

  it('renames the management tab to "Manage Boosts" for discoverability', () => {
    renderBoostsPage()
    expect(screen.getByRole('tab', { name: /manage boosts/i })).toBeTruthy()
  })
})
