import { beforeEach, describe, expect, it } from 'bun:test'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { AdminRevenueCatUserOutputBody, RevenueCatOperation } from '@boostedbudget/api-client/models'
import { RevenueCatUserPanel } from './RevenueCatUserPanel'
import { mockGetAdminRevenueCatUser, mockRequestAdminRevenueCatGrant, mockRetryAdminRevenueCatOperation, mockRefreshAdminRevenueCatUser } from '@/test/mocks/admin-subscriptions-api'
import { mockToastInfo, mockToastError, mockToastSuccess } from '@/test/mocks/sonner'
import { mockTrackEvent } from '@/test/mocks/track-event'

const operation: RevenueCatOperation = {id: 'op-secret', user_id: 'user-secret', class: 'comp', key: 'comp_premium', stage: 'pending', attempts: 2, reason: 'private reason', last_error: 'private failure', actor_id: 'actor-secret', revoke: false, expires_at: null, version: 1, created_at: '2026-09-13T10:00:00Z'}
const inactive = {tier: '', status: 'inactive', source: 'none', expires_at: null}
const fixture = {
  state: {user_id: 'user-secret', access_updated_at: '2026-09-13T10:00:00Z', billing_updated_at: null, pending_operations: 1, grants: [], billing: [], access_error: 'access-secret', billing_error: 'billing-secret'},
  summary: {as_of: '2026-09-13T10:00:00Z', access_resolved: true, billing_available: true, own: inactive, paid: null, comp: null, trial: null, effective: {Tier: 'household_4', Status: 'active', Source: 'household', HouseholdID: 'house-secret', PayerUserID: 'payer-secret'}, pending_operations: 1, billing: []},
  operations: [operation, {...operation, id: 'completed', stage: 'completed'}],
} satisfies AdminRevenueCatUserOutputBody
function mount(data: AdminRevenueCatUserOutputBody = fixture) {
  mockGetAdminRevenueCatUser.mockResolvedValue(data)
  return render(<QueryClientProvider client={new QueryClient({defaultOptions: {queries: {retry: false}, mutations: {retry: false}}})}><RevenueCatUserPanel userId="user-secret" /></QueryClientProvider>)
}
beforeEach(() => {
  mockGetAdminRevenueCatUser.mockReset()
  mockRequestAdminRevenueCatGrant.mockReset().mockResolvedValue(operation)
  mockRetryAdminRevenueCatOperation.mockReset().mockResolvedValue({status: 'queued'})
  mockRefreshAdminRevenueCatUser.mockReset().mockResolvedValue({status: 'queued'})
  mockToastInfo.mockClear(); mockToastError.mockClear(); mockToastSuccess.mockClear(); mockTrackEvent.mockClear()
})
async function ready() { await screen.findByRole('heading', {name: 'Observed Access'}) }
function reason() { fireEvent.change(screen.getByLabelText('Reason'), {target: {value: '  support correction  '}}) }

describe('RevenueCat user diagnostics', () => {
  it('shows absent grants only when own access is confirmed', async () => {
    mount(); await ready()
    for (const title of ['Paid Access', 'Complimentary Access', 'Trial Access']) {
      expect(within(screen.getByText(title).parentElement!).getByText('No grant')).toBeInTheDocument()
    }
  })
  it('shows unknown access for null summaries and retains operation history', async () => {
    mount({...fixture, summary: {...fixture.summary, access_resolved: false, own: null, paid: null, comp: null, trial: null, effective: null}}); await ready()
    for (const title of ['Own Access', 'Paid Access', 'Complimentary Access', 'Trial Access', 'Effective Access']) {
      expect(within(screen.getByText(title).parentElement!).getByText('Unavailable')).toBeInTheDocument()
    }
    expect(screen.queryByText('No grant')).not.toBeInTheDocument()
    expect(screen.getAllByText('private reason')).toHaveLength(2)
    expect(screen.getByRole('button', {name: 'Queue change'})).toBeDisabled()
  })
  it('keeps history and reload available but blocks writes when state is null', async () => {
    mount({...fixture, state: null}); await ready()
    expect(screen.getByRole('alert')).toHaveTextContent('Customer state is unavailable')
    for (const name of ['Queue change', 'Refresh customer', 'Retry operation']) expect(screen.getByRole('button', {name})).toBeDisabled()
    expect(screen.getByRole('button', {name: 'Reload diagnostics'})).not.toBeDisabled()
    expect(screen.getAllByText('private reason')).toHaveLength(2)
    fireEvent.click(screen.getByRole('button', {name: 'Reload diagnostics'}))
    await waitFor(() => expect(mockGetAdminRevenueCatUser.mock.calls.length).toBeGreaterThan(1))
  })
  it('sorts operation attempts numerically', async () => {
    mount({...fixture, operations: [{...operation, id: 'many', attempts: 12}, {...operation, id: 'few', attempts: 2}]}); await ready()
    fireEvent.click(screen.getByRole('button', {name: /Attempts/}))
    expect(screen.getAllByRole('row')[1].textContent).toContain('few')
  })
  it('separates own, paid, comp, trial and household effective access and masks metadata', async () => {
    mount(); await ready()
    for (const label of ['Own Access', 'Paid Access', 'Complimentary Access', 'Trial Access', 'Effective Access']) expect(screen.getByText(label)).toBeInTheDocument()
    expect(screen.getByText('household_4')).toBeInTheDocument()
    expect(screen.getByText('No billing subscriptions.')).toBeInTheDocument()
    for (const value of ['payer-secret', 'house-secret', 'private reason', 'private failure', 'access-secret', 'billing-secret']) {
      for (const node of screen.getAllByText(value)) expect(node.closest('[data-ph-mask]')).not.toBeNull()
    }
    expect(screen.getAllByRole('button', {name: 'Retry operation'})).toHaveLength(1)
    expect(screen.getByText('op-secret').closest('tr')).not.toHaveAttribute('data-row-id')
  })
  it('refreshes and retries only the pending operation without asserting applied access', async () => {
    mount(); await ready()
    fireEvent.click(screen.getByRole('button', {name: 'Refresh customer'}))
    await waitFor(() => expect(mockRefreshAdminRevenueCatUser).toHaveBeenCalledWith('user-secret'))
    fireEvent.click(screen.getByRole('button', {name: 'Retry operation'}))
    await waitFor(() => expect(mockRetryAdminRevenueCatOperation).toHaveBeenCalledWith('user-secret', 'op-secret'))
    expect(mockToastInfo).toHaveBeenCalledWith('Retry queued. Access changes appear after provider readback.')
    expect(mockToastSuccess).not.toHaveBeenCalled()
  })
  it('queues a permanent household comp with the exact reason and no actor supplied', async () => {
    mount(); await ready(); reason()
    fireEvent.change(screen.getByLabelText('Tier'), {target: {value: 'household_3'}})
    fireEvent.click(screen.getByLabelText('Permanent'))
    fireEvent.click(screen.getByRole('button', {name: 'Queue change'}))
    await waitFor(() => expect(mockRequestAdminRevenueCatGrant).toHaveBeenCalledWith('user-secret', {class: 'comp', tier: 'household_3', expires_at: null, revoke: false, reason: 'support correction'}))
    expect(mockToastInfo).toHaveBeenCalledWith('Change queued. Access changes appear after provider readback.')
    expect(JSON.stringify(mockTrackEvent.mock.calls)).not.toMatch(/user-secret|support correction|actor-secret/)
  })
  it('requires a reason and future finite expiry for trial extension', async () => {
    mount(); await ready()
    fireEvent.change(screen.getByLabelText('Grant class'), {target: {value: 'trial'}})
    expect(screen.queryByLabelText('Permanent')).not.toBeInTheDocument()
    expect(within(screen.getByLabelText('Tier')).getAllByRole('option')).toHaveLength(1)
    fireEvent.click(screen.getByRole('button', {name: 'Queue change'}))
    expect(mockRequestAdminRevenueCatGrant).not.toHaveBeenCalled()
    reason()
    fireEvent.change(screen.getByLabelText('Expires at'), {target: {value: '2020-01-01T00:00'}})
    fireEvent.click(screen.getByRole('button', {name: 'Queue change'}))
    expect(await screen.findByText('Choose a future expiry.')).toBeInTheDocument()
    expect(mockRequestAdminRevenueCatGrant).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText('Expires at'), {target: {value: '2099-01-01T12:00'}})
    fireEvent.click(screen.getByRole('button', {name: 'Queue change'}))
    await waitFor(() => expect(mockRequestAdminRevenueCatGrant).toHaveBeenCalledWith('user-secret', {class: 'trial', tier: 'premium', revoke: false, expires_at: new Date('2099-01-01T12:00').toISOString(), reason: 'support correction'}))
    expect(screen.getByText(/does not reset trial eligibility/)).toBeInTheDocument()
  })
  it('ends trial with a reason without requiring an expiry', async () => {
    mount(); await ready(); reason()
    fireEvent.change(screen.getByLabelText('Grant class'), {target: {value: 'trial'}})
    fireEvent.change(screen.getByLabelText('Action'), {target: {value: 'revoke'}})
    fireEvent.click(screen.getByRole('button', {name: 'Queue change'}))
    await waitFor(() => expect(mockRequestAdminRevenueCatGrant).toHaveBeenCalledWith('user-secret', {class: 'trial', tier: 'premium', revoke: true, expires_at: null, reason: 'support correction'}))
  })
  it.each(['erasure_started_at', 'erasure_confirmed_at'] as const)('disables all writes for the %s fence', async field => {
    mount({...fixture, state: {...fixture.state, [field]: '2026-09-13T11:00:00Z'}}); await ready()
    expect(screen.getByRole('alert').textContent).toContain('Erasure fence')
    expect(screen.getByRole('button', {name: 'Queue change'})).toBeDisabled()
    expect(screen.getByRole('button', {name: 'Refresh customer'})).toBeDisabled()
    expect(screen.getByRole('button', {name: 'Retry operation'})).toBeDisabled()
  })
  it('reports billing unavailable separately and refuses grants while access is unresolved', async () => {
    mount({...fixture, summary: {...fixture.summary, billing_available: false, access_resolved: false}}); await ready()
    expect(screen.getByText('Billing unavailable.')).toBeInTheDocument()
    expect(screen.getByRole('button', {name: 'Queue change'})).toBeDisabled()
    expect(screen.getByRole('button', {name: 'Refresh customer'})).not.toBeDisabled()
  })
  it('preserves the form on a rejected grant and displays a safe error', async () => {
    mockRequestAdminRevenueCatGrant.mockRejectedValue(new Error('sensitive backend response'))
    mount(); await ready(); reason()
    fireEvent.click(screen.getByLabelText('Permanent'))
    fireEvent.click(screen.getByRole('button', {name: 'Queue change'}))
    await waitFor(() => expect(mockToastError).toHaveBeenCalledWith('Could not queue change. Check eligibility and try again.'))
    expect(screen.getByLabelText('Reason')).toHaveValue('  support correction  ')
    expect(mockToastSuccess).not.toHaveBeenCalled()
  })
})
