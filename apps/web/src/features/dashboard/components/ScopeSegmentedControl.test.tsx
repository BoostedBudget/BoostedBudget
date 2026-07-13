/**
 * ScopeSegmentedControl Tests
 * Issue #1370: Household feature — Personal/Household dashboard scope toggle
 *
 * Uses the centralized dashboard-context mock (scope/setScope) and the
 * centralized household-api mock (useGetMyHousehold/useGetHouseholdEntitlement,
 * both preloaded via bunfig.toml) to control `useHousehold()`'s real return
 * value. `useHousehold()` itself is NOT mocked — it calls `useQueryClient()`
 * internally, hence the QueryClientProvider wrapper (mirrors
 * useHousehold.test.tsx's own wrapper).
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'

import '@/test/mocks/registry'
import { mockUseDashboardContext, mockSetScope } from '@/test/mocks/dashboard-context'
import {
  mockUseGetMyHousehold,
  mockUseGetHouseholdEntitlement,
  mockRefetchMyHousehold,
  mockRefetchHouseholdEntitlement,
} from '@/test/mocks/household-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics'

import { ScopeSegmentedControl, resolveScopeSelection } from './ScopeSegmentedControl'
import type { DashboardScope } from '../types'

function createWrapper() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

function baseContextValue(scope: DashboardScope = 'personal') {
  return {
    filters: { startDate: '2024-01-01', endDate: '2024-01-31' },
    setFilters: mock(() => {}),
    timeframe: 'this_month' as const,
    setTimeframe: mock(() => {}),
    isEditing: false,
    setIsEditing: mock(() => {}),
    hasAccounts: true,
    accountsLoading: false,
    scope,
    setScope: mockSetScope,
  }
}

describe('ScopeSegmentedControl', () => {
  beforeEach(() => {
    mockUseDashboardContext.mockClear()
    mockSetScope.mockClear()
    mockUseGetMyHousehold.mockClear()
    mockUseGetHouseholdEntitlement.mockClear()
    mockRefetchMyHousehold.mockClear()
    mockRefetchHouseholdEntitlement.mockClear()
    mockTrackEvent.mockClear()

    mockUseDashboardContext.mockReturnValue(baseContextValue('personal'))

    mockUseGetHouseholdEntitlement.mockReturnValue({
      data: { seats_active: 2, seats_pending: 0, users_included: 3, is_owner: true, source: 'household', tier: 'household_3' },
      isLoading: false,
      error: null,
      refetch: mockRefetchHouseholdEntitlement,
    })
  })

  function renderControl() {
    return render(<ScopeSegmentedControl />, { wrapper: createWrapper() })
  }

  describe('Visibility', () => {
    it('renders nothing when not in a household', () => {
      mockUseGetMyHousehold.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: null,
        refetch: mockRefetchMyHousehold,
      })

      const { container } = renderControl()

      expect(container).toBeEmptyDOMElement()
      expect(screen.queryByTestId('scope-segmented-control')).not.toBeInTheDocument()
    })

    it('renders the toggle when in a household', () => {
      mockUseGetMyHousehold.mockReturnValue({
        data: { household: { id: 'hh-1', name: 'The Smiths' } },
        isLoading: false,
        error: null,
        refetch: mockRefetchMyHousehold,
      })

      renderControl()

      expect(screen.getByTestId('scope-segmented-control')).toBeInTheDocument()
      expect(screen.getByText('Personal')).toBeInTheDocument()
      expect(screen.getByText('Household')).toBeInTheDocument()
    })
  })

  describe('Selection', () => {
    beforeEach(() => {
      mockUseGetMyHousehold.mockReturnValue({
        data: { household: { id: 'hh-1', name: 'The Smiths' } },
        isLoading: false,
        error: null,
        refetch: mockRefetchMyHousehold,
      })
    })

    it('calls setScope("household") when Household is clicked (starting from personal)', () => {
      // default context scope is 'personal' — clicking the OTHER item is a real
      // selection change, not the Radix already-selected/deselect case below.
      renderControl()

      fireEvent.click(screen.getByText('Household'))

      expect(mockSetScope).toHaveBeenCalledWith('household')
    })

    it('fires DASHBOARD_SCOPE_CHANGED analytics event on a real selection change', () => {
      renderControl()

      fireEvent.click(screen.getByText('Household'))

      expect(mockTrackEvent).toHaveBeenCalledWith(
        ANALYTICS_EVENTS.DASHBOARD_SCOPE_CHANGED,
        { scope: 'household' }
      )
    })

    it('does not fire the analytics event when clicking the already-selected item', () => {
      renderControl()

      fireEvent.click(screen.getByText('Personal'))

      expect(mockTrackEvent).not.toHaveBeenCalled()
    })

    it('calls setScope("personal") when Personal is clicked (starting from household)', () => {
      mockUseDashboardContext.mockReturnValue(baseContextValue('household'))
      renderControl()

      fireEvent.click(screen.getByText('Personal'))

      expect(mockSetScope).toHaveBeenCalledWith('personal')
    })

    // This test file runs isolated (scripts/test-conflict-files.sh Group B) WITHOUT
    // the centralized @/components/ui/toggle-group mock, so the REAL Radix
    // ToggleGroup is exercised here — including its actual re-click/deselect
    // behavior: clicking the already-selected item fires onValueChange('').
    // resolveScopeSelection('') === null must suppress that, matching the guard
    // documented in ScopeSegmentedControl.tsx and unit-tested below.
    it('does not call setScope when clicking the already-selected item (real Radix deselect quirk)', () => {
      // default context scope is 'personal'; "Personal" is already selected.
      renderControl()

      fireEvent.click(screen.getByText('Personal'))

      expect(mockSetScope).not.toHaveBeenCalled()
    })
  })

  describe('Reset on leaving household (Issue #1370 design decision)', () => {
    it('resets scope to personal when household membership is lost while scope=household', () => {
      mockUseDashboardContext.mockReturnValue(baseContextValue('household'))
      mockUseGetMyHousehold.mockReturnValue({
        data: { household: { id: 'hh-1', name: 'The Smiths' } },
        isLoading: false,
        error: null,
        refetch: mockRefetchMyHousehold,
      })

      const { rerender } = renderControl()
      expect(mockSetScope).not.toHaveBeenCalled()

      // Simulate leaving the household: useGetMyHousehold now returns no household.
      mockUseGetMyHousehold.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: null,
        refetch: mockRefetchMyHousehold,
      })
      rerender(<ScopeSegmentedControl />)

      expect(mockSetScope).toHaveBeenCalledWith('personal')
    })

    it('does not call setScope when scope is already personal and household is lost', () => {
      mockUseGetMyHousehold.mockReturnValue({
        data: { household: { id: 'hh-1', name: 'The Smiths' } },
        isLoading: false,
        error: null,
        refetch: mockRefetchMyHousehold,
      })

      const { rerender } = renderControl()

      mockUseGetMyHousehold.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: null,
        refetch: mockRefetchMyHousehold,
      })
      rerender(<ScopeSegmentedControl />)

      expect(mockSetScope).not.toHaveBeenCalled()
    })
  })

  describe('resolveScopeSelection (Radix empty-string guard)', () => {
    it('resolves "personal" and "household" to themselves', () => {
      expect(resolveScopeSelection('personal')).toBe('personal')
      expect(resolveScopeSelection('household')).toBe('household')
    })

    it('resolves an empty string (Radix deselect-already-selected quirk) to null', () => {
      expect(resolveScopeSelection('')).toBeNull()
    })

    it('resolves any other unexpected value to null', () => {
      expect(resolveScopeSelection('bogus')).toBeNull()
    })
  })
})
