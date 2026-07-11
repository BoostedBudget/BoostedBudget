/**
 * DisbandHouseholdDialog component tests
 * Issue #1370: Household feature — disband-household flow
 * TDD RED: written before implementation.
 *
 * Uses the REAL `useHousehold` hook (composed here exactly like
 * useHousehold.test.tsx does), driven through the centralized api-client
 * mocks in `@/test/mocks/household-api` (preloaded via bunfig.toml), plus
 * the real `useDeleteHousehold` mutation.
 */
import { describe, it, expect, beforeEach, spyOn } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { Household, HouseholdEntitlementSummary } from '../types'
import type { GetMyHouseholdResponseBody } from '@boostedbudget/api-client/models'
import {
  getGetMyHouseholdQueryKey,
  getGetHouseholdEntitlementQueryKey,
} from '@boostedbudget/api-client/endpoints/household/household'
import {
  mockUseGetMyHousehold,
  mockUseGetHouseholdEntitlement,
  mockRefetchMyHousehold,
  mockRefetchHouseholdEntitlement,
  mockUseDeleteHousehold,
  mockDeleteHouseholdMutateAsync,
} from '@/test/mocks/household-api'
import { DisbandHouseholdDialog } from './DisbandHouseholdDialog'

const mockHousehold: Household = {
  id: 'household-1',
  name: 'The Smiths',
  owner_user_id: 'user-1',
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

function makeEntitlement(overrides: Partial<HouseholdEntitlementSummary> = {}): HouseholdEntitlementSummary {
  return {
    is_owner: true,
    seats_active: 1,
    seats_pending: 0,
    source: 'household',
    tier: 'household_3',
    users_included: 3,
    ...overrides,
  }
}

function setHousehold(entitlement: HouseholdEntitlementSummary) {
  const responseBody: GetMyHouseholdResponseBody = { household: mockHousehold, summary: entitlement }
  mockUseGetMyHousehold.mockReturnValue({
    data: responseBody,
    isLoading: false,
    error: null,
    refetch: mockRefetchMyHousehold,
  })
  mockUseGetHouseholdEntitlement.mockReturnValue({
    data: entitlement,
    isLoading: false,
    error: null,
    refetch: mockRefetchHouseholdEntitlement,
  })
}

function renderDialog(onOpenChange: (open: boolean) => void = () => {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
  return { ...render(<DisbandHouseholdDialog open onOpenChange={onOpenChange} />, { wrapper: Wrapper }), queryClient }
}

describe('DisbandHouseholdDialog', () => {
  beforeEach(() => {
    mockUseGetMyHousehold.mockClear()
    mockUseGetHouseholdEntitlement.mockClear()
    mockRefetchMyHousehold.mockClear()
    mockRefetchHouseholdEntitlement.mockClear()
    mockUseDeleteHousehold.mockClear()
    mockDeleteHouseholdMutateAsync.mockClear()

    mockUseDeleteHousehold.mockReturnValue({
      mutateAsync: mockDeleteHouseholdMutateAsync,
      isPending: false,
    })
  })

  describe('eligible to disband (only the owner remains)', () => {
    beforeEach(() => {
      setHousehold(makeEntitlement({ seats_active: 1 }))
    })

    it('keeps the confirm button disabled until the correct text is typed', () => {
      renderDialog()

      const confirmButton = screen.getByRole('button', { name: /disband household/i })
      expect(confirmButton).toBeDisabled()

      fireEvent.change(screen.getByLabelText(/type/i), { target: { value: 'wrong text' } })
      expect(confirmButton).toBeDisabled()

      fireEvent.change(screen.getByLabelText(/type/i), { target: { value: 'DISBAND' } })
      expect(confirmButton).not.toBeDisabled()
    })

    it('calls useDeleteHousehold after typing the confirm text, invalidates getMyHousehold + the entitlement summary, and closes', async () => {
      mockDeleteHouseholdMutateAsync.mockResolvedValueOnce(undefined)
      const { queryClient } = renderDialog()
      const invalidateSpy = spyOn(queryClient, 'invalidateQueries')

      fireEvent.change(screen.getByLabelText(/type/i), { target: { value: 'DISBAND' } })
      fireEvent.click(screen.getByRole('button', { name: /disband household/i }))

      await waitFor(() => {
        expect(mockDeleteHouseholdMutateAsync).toHaveBeenCalled()
      })
      await waitFor(() => {
        expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetMyHouseholdQueryKey() })
        expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetHouseholdEntitlementQueryKey() })
      })
    })

    it('is case-insensitive when matching the confirm text', () => {
      renderDialog()

      fireEvent.change(screen.getByLabelText(/type/i), { target: { value: 'disband' } })

      expect(screen.getByRole('button', { name: /disband household/i })).not.toBeDisabled()
    })
  })

  describe('guarded when other active members still exist', () => {
    beforeEach(() => {
      setHousehold(makeEntitlement({ seats_active: 2, users_included: 3 }))
    })

    it('shows explanatory copy instead of the destructive disband flow', () => {
      renderDialog()

      expect(screen.getByText(/remove.*before you can disband/i)).toBeInTheDocument()
      expect(screen.queryByLabelText(/type/i)).not.toBeInTheDocument()
    })

    it('never calls useDeleteHousehold, even when acknowledging the notice', () => {
      renderDialog()

      fireEvent.click(screen.getByRole('button', { name: /got it/i }))

      expect(mockDeleteHouseholdMutateAsync).not.toHaveBeenCalled()
    })
  })

  describe('guarded while household entitlement is still loading', () => {
    beforeEach(() => {
      // seats_active defaults to 0 (falsy) via useHousehold() before the
      // entitlement query resolves — isLoading:true must still block the
      // destructive flow even though seatsUsed alone would look "eligible".
      mockUseGetMyHousehold.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: mockRefetchMyHousehold,
      })
      mockUseGetHouseholdEntitlement.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: mockRefetchHouseholdEntitlement,
      })
    })

    it('shows a loading guard instead of the destructive disband flow', () => {
      renderDialog()

      expect(screen.getByText(/still loading your household/i)).toBeInTheDocument()
      expect(screen.queryByLabelText(/type/i)).not.toBeInTheDocument()
    })

    it('never calls useDeleteHousehold while loading', () => {
      renderDialog()

      fireEvent.click(screen.getByRole('button', { name: /got it/i }))

      expect(mockDeleteHouseholdMutateAsync).not.toHaveBeenCalled()
    })
  })

  describe('guarded when the current user is not the owner', () => {
    beforeEach(() => {
      setHousehold(makeEntitlement({ is_owner: false, seats_active: 1 }))
    })

    it('shows an owner-only guard instead of the destructive disband flow', () => {
      renderDialog()

      expect(screen.getByText(/only the household owner can disband/i)).toBeInTheDocument()
      expect(screen.queryByLabelText(/type/i)).not.toBeInTheDocument()
    })

    it('never calls useDeleteHousehold for a non-owner', () => {
      renderDialog()

      fireEvent.click(screen.getByRole('button', { name: /got it/i }))

      expect(mockDeleteHouseholdMutateAsync).not.toHaveBeenCalled()
    })
  })
})
