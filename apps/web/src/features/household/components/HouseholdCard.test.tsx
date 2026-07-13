/**
 * HouseholdCard component tests
 * Issue #1370: Household feature — household info card
 * TDD RED: written before implementation.
 *
 * Uses the REAL `useHousehold` hook (composed here exactly like
 * useHousehold.test.tsx does) driven through the centralized api-client
 * mocks in `@/test/mocks/household-api` (preloaded via bunfig.toml), plus
 * the real `useUpdateHouseholdName` mutation for the rename flow.
 */
import { describe, it, expect, beforeEach, spyOn } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { Household, HouseholdEntitlementSummary } from '../types'
import type { GetMyHouseholdResponseBody } from '@boostedbudget/api-client/models'
import { getGetMyHouseholdQueryKey } from '@boostedbudget/api-client/endpoints/household/household'
import {
  mockUseGetMyHousehold,
  mockUseGetHouseholdEntitlement,
  mockRefetchMyHousehold,
  mockRefetchHouseholdEntitlement,
  mockUseUpdateHouseholdName,
  mockUpdateHouseholdNameMutateAsync,
} from '@/test/mocks/household-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { HouseholdCard } from './HouseholdCard'

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
    seats_active: 2,
    seats_pending: 1,
    source: 'household',
    tier: 'household_3',
    users_included: 3,
    ...overrides,
  }
}

function setHousehold(entitlement: HouseholdEntitlementSummary, household: Household | null = mockHousehold) {
  const responseBody: GetMyHouseholdResponseBody | undefined = household
    ? { household, summary: entitlement }
    : undefined
  mockUseGetMyHousehold.mockReturnValue({
    data: responseBody,
    isLoading: false,
    error: household ? null : { response: { status: 404 } },
    refetch: mockRefetchMyHousehold,
  })
  mockUseGetHouseholdEntitlement.mockReturnValue({
    data: entitlement,
    isLoading: false,
    error: null,
    refetch: mockRefetchHouseholdEntitlement,
  })
}

function renderCard(queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
  return { ...render(<HouseholdCard />, { wrapper: Wrapper }), queryClient }
}

describe('HouseholdCard', () => {
  beforeEach(() => {
    mockUseGetMyHousehold.mockClear()
    mockUseGetHouseholdEntitlement.mockClear()
    mockRefetchMyHousehold.mockClear()
    mockRefetchHouseholdEntitlement.mockClear()
    mockUseUpdateHouseholdName.mockClear()
    mockUpdateHouseholdNameMutateAsync.mockClear()
    mockTrackEvent.mockClear()

    mockUseUpdateHouseholdName.mockReturnValue({
      mutateAsync: mockUpdateHouseholdNameMutateAsync,
      isPending: false,
    })
  })

  it('renders the household name, seat usage badge, and owner role badge', () => {
    setHousehold(makeEntitlement({ is_owner: true, seats_active: 2, users_included: 3 }))
    renderCard()

    expect(screen.getByText('The Smiths')).toBeInTheDocument()
    expect(screen.getByText('2/3')).toBeInTheDocument()
    expect(screen.getByText('Owner')).toBeInTheDocument()
  })

  it('renders the member role badge for a non-owner', () => {
    setHousehold(makeEntitlement({ is_owner: false, seats_active: 2, users_included: 3 }))
    renderCard()

    expect(screen.getByText('Member')).toBeInTheDocument()
    expect(screen.queryByText('Owner')).not.toBeInTheDocument()
  })

  it('renders nothing when there is no household', () => {
    setHousehold(makeEntitlement(), null)
    const { container } = renderCard()

    expect(container).toBeEmptyDOMElement()
  })

  it('shows the rename affordance for the owner', () => {
    setHousehold(makeEntitlement({ is_owner: true }))
    renderCard()

    expect(screen.getByRole('button', { name: /rename household/i })).toBeInTheDocument()
  })

  it('shows no rename affordance at all for a non-owner', () => {
    setHousehold(makeEntitlement({ is_owner: false }))
    renderCard()

    expect(screen.queryByRole('button', { name: /rename household/i })).not.toBeInTheDocument()
  })

  it('renames the household: calls useUpdateHouseholdName with the new name and invalidates getMyHousehold', async () => {
    mockUpdateHouseholdNameMutateAsync.mockResolvedValueOnce({
      ...mockHousehold,
      name: 'The Joneses',
    })
    setHousehold(makeEntitlement({ is_owner: true }))
    const { queryClient } = renderCard()
    const invalidateSpy = spyOn(queryClient, 'invalidateQueries')

    fireEvent.click(screen.getByRole('button', { name: /rename household/i }))
    const input = screen.getByDisplayValue('The Smiths')
    fireEvent.change(input, { target: { value: 'The Joneses' } })
    fireEvent.click(screen.getByRole('button', { name: /save household name/i }))

    await waitFor(() => {
      expect(mockUpdateHouseholdNameMutateAsync).toHaveBeenCalledWith({ data: { name: 'The Joneses' } })
    })
    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetMyHouseholdQueryKey() })
    })
  })

  it('shows a validation error and does not submit when the renamed value is empty', () => {
    setHousehold(makeEntitlement({ is_owner: true }))
    renderCard()

    fireEvent.click(screen.getByRole('button', { name: /rename household/i }))
    const input = screen.getByDisplayValue('The Smiths')
    fireEvent.change(input, { target: { value: '   ' } })
    fireEvent.click(screen.getByRole('button', { name: /save household name/i }))

    expect(screen.getByRole('alert')).toHaveTextContent(/household name is required/i)
    expect(mockUpdateHouseholdNameMutateAsync).not.toHaveBeenCalled()
  })

  it('cancels editing without calling the mutation', () => {
    setHousehold(makeEntitlement({ is_owner: true }))
    renderCard()

    fireEvent.click(screen.getByRole('button', { name: /rename household/i }))
    const input = screen.getByDisplayValue('The Smiths')
    fireEvent.change(input, { target: { value: 'Something else' } })
    fireEvent.click(screen.getByRole('button', { name: /^cancel$/i }))

    expect(screen.getByText('The Smiths')).toBeInTheDocument()
    expect(mockUpdateHouseholdNameMutateAsync).not.toHaveBeenCalled()
  })

  describe('Analytics (#1370 Task 10)', () => {
    it('fires HOUSEHOLD_RENAMED after a successful rename', async () => {
      mockUpdateHouseholdNameMutateAsync.mockResolvedValueOnce({
        ...mockHousehold,
        name: 'The Joneses',
      })
      setHousehold(makeEntitlement({ is_owner: true }))
      renderCard()

      fireEvent.click(screen.getByRole('button', { name: /rename household/i }))
      const input = screen.getByDisplayValue('The Smiths')
      fireEvent.change(input, { target: { value: 'The Joneses' } })
      fireEvent.click(screen.getByRole('button', { name: /save household name/i }))

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.HOUSEHOLD_RENAMED)
      })
    })

    it('does NOT fire HOUSEHOLD_RENAMED when the rename fails', async () => {
      mockUpdateHouseholdNameMutateAsync.mockRejectedValueOnce({
        response: { status: 422, data: { detail: 'Household name is invalid.' } },
      })
      setHousehold(makeEntitlement({ is_owner: true }))
      renderCard()

      fireEvent.click(screen.getByRole('button', { name: /rename household/i }))
      const input = screen.getByDisplayValue('The Smiths')
      fireEvent.change(input, { target: { value: 'Bad Name' } })
      fireEvent.click(screen.getByRole('button', { name: /save household name/i }))

      await screen.findByText('Household name is invalid.')
      expect(mockTrackEvent).not.toHaveBeenCalledWith(ANALYTICS_EVENTS.HOUSEHOLD_RENAMED)
    })
  })
})
