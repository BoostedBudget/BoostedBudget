/**
 * CreateHouseholdForm component tests
 * Issue #1370: Household feature — create-household flow
 * TDD RED: written before implementation.
 *
 * Uses the centralized api-client mocks in `@/test/mocks/household-api`
 * (preloaded via bunfig.toml), driving the real `useCreateHousehold`
 * mutation hook exactly as InvitePanel.test.tsx does for its mutations.
 * `getGetMyHouseholdQueryKey` is left un-mocked (pass-through), so
 * `queryClient.invalidateQueries` can be asserted against the real,
 * production-accurate key.
 */
import { describe, it, expect, beforeEach, spyOn } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import {
  getGetMyHouseholdQueryKey,
  getGetHouseholdEntitlementQueryKey,
} from '@boostedbudget/api-client/endpoints/household/household'
import {
  mockUseCreateHousehold,
  mockCreateHouseholdMutateAsync,
} from '@/test/mocks/household-api'
import { CreateHouseholdForm } from './CreateHouseholdForm'

function renderForm(queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
  return { ...render(<CreateHouseholdForm />, { wrapper: Wrapper }), queryClient }
}

describe('CreateHouseholdForm', () => {
  beforeEach(() => {
    mockUseCreateHousehold.mockClear()
    mockCreateHouseholdMutateAsync.mockClear()
    mockUseCreateHousehold.mockReturnValue({
      mutateAsync: mockCreateHouseholdMutateAsync,
      isPending: false,
    })
  })

  it('submits with a valid name, calls useCreateHousehold with the correct payload, and invalidates getMyHousehold + the entitlement summary', async () => {
    mockCreateHouseholdMutateAsync.mockResolvedValueOnce({
      id: 'household-1',
      name: 'The Smiths',
      owner_user_id: 'user-1',
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    })
    const { queryClient } = renderForm()
    const invalidateSpy = spyOn(queryClient, 'invalidateQueries')

    fireEvent.change(screen.getByLabelText(/household name/i), { target: { value: 'The Smiths' } })
    fireEvent.click(screen.getByRole('button', { name: /create household/i }))

    await waitFor(() => {
      expect(mockCreateHouseholdMutateAsync).toHaveBeenCalledWith({ data: { name: 'The Smiths' } })
    })

    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetMyHouseholdQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetHouseholdEntitlementQueryKey() })
    })
  })

  it('trims whitespace from the submitted name', async () => {
    mockCreateHouseholdMutateAsync.mockResolvedValueOnce({
      id: 'household-1',
      name: 'The Smiths',
      owner_user_id: 'user-1',
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    })
    renderForm()

    fireEvent.change(screen.getByLabelText(/household name/i), { target: { value: '  The Smiths  ' } })
    fireEvent.click(screen.getByRole('button', { name: /create household/i }))

    await waitFor(() => {
      expect(mockCreateHouseholdMutateAsync).toHaveBeenCalledWith({ data: { name: 'The Smiths' } })
    })
  })

  it('shows a validation error and does not submit when the name is empty', () => {
    renderForm()

    fireEvent.click(screen.getByRole('button', { name: /create household/i }))

    expect(screen.getByRole('alert')).toHaveTextContent(/household name is required/i)
    expect(mockCreateHouseholdMutateAsync).not.toHaveBeenCalled()
  })

  it('shows a validation error and does not submit when the name is whitespace-only', () => {
    renderForm()

    fireEvent.change(screen.getByLabelText(/household name/i), { target: { value: '   ' } })
    fireEvent.click(screen.getByRole('button', { name: /create household/i }))

    expect(screen.getByRole('alert')).toHaveTextContent(/household name is required/i)
    expect(mockCreateHouseholdMutateAsync).not.toHaveBeenCalled()
  })

  it('surfaces a friendly backend validation error', async () => {
    // NOTE: deliberately a normal-length name here (not something exceeding
    // the input's maxLength=255) — a value longer than maxLength trips
    // native HTML5 constraint validation (the `tooLong` flag) in happy-dom,
    // which silently blocks the form's submit event before React ever sees
    // it, independent of this component's own logic. This test is only
    // about surfacing whatever `detail` the backend returns on rejection.
    mockCreateHouseholdMutateAsync.mockRejectedValueOnce({
      response: { status: 422, data: { detail: 'Household name is invalid.' } },
    })
    renderForm()

    fireEvent.change(screen.getByLabelText(/household name/i), { target: { value: 'The Smiths' } })
    fireEvent.click(screen.getByRole('button', { name: /create household/i }))

    expect(await screen.findByText('Household name is invalid.')).toBeInTheDocument()
  })

  it('disables the input and button while the mutation is in flight', async () => {
    let resolveCreate: (value: unknown) => void = () => {}
    const pending = new Promise((resolve) => {
      resolveCreate = resolve
    })
    mockCreateHouseholdMutateAsync.mockImplementationOnce(() => pending)
    renderForm()

    fireEvent.change(screen.getByLabelText(/household name/i), { target: { value: 'The Smiths' } })
    fireEvent.click(screen.getByRole('button', { name: /create household/i }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /creating/i })).toBeDisabled()
      expect(screen.getByLabelText(/household name/i)).toBeDisabled()
    })

    resolveCreate({
      id: 'household-1',
      name: 'The Smiths',
      owner_user_id: 'user-1',
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    })

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /^create household$/i })).not.toBeDisabled()
    })
  })
})
