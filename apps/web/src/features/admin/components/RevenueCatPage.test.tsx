import { beforeEach, describe, expect, it } from 'bun:test'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act } from '@testing-library/react'
import { getGetAdminRevenueCatHealthQueryKey } from '@boostedbudget/api-client/endpoints/admin-subscriptions/admin-subscriptions'
import { MemoryRouter } from 'react-router'
import { RevenueCatPage } from './RevenueCatPage'
import { RevenueCatHealthSummary } from './RevenueCatHealthSummary'
import { mockGetAdminRevenueCatHealth, mockListAdminRevenueCatWebhooks, mockReplayAdminRevenueCatWebhook } from '@/test/mocks/admin-subscriptions-api'
import { mockToastInfo } from '@/test/mocks/sonner'
import type { RevenueCatHealth, RevenueCatWebhookReceipt } from '@boostedbudget/api-client/models'

const metrics: RevenueCatHealth = { customers: 5, deliveries: 12, events: 8, processed_events: 7, processing_attempts: 9, access_failures: 0, billing_failures: 1, failed_operations: 2, failed_webhooks: 1, pending_operations: 3, oldest_pending_received_at: '2026-09-13T10:00:00Z', stale_active_customers: 1, unresolved_customers: 0 }
const receipt: RevenueCatWebhookReceipt = { event_id: 'event-sensitive', user_id: 'user-sensitive', event_type: 'RENEWAL', deliveries: 3, attempts: 2, received_at: '2026-09-13T10:00:00Z', last_received_at: '2026-09-13T10:05:00Z', processed_at: null, last_error: 'private diagnostic' }
const mount = (element: React.ReactNode) => render(<MemoryRouter><QueryClientProvider client={new QueryClient({defaultOptions: {queries: {retry: false}, mutations: {retry: false}}})}>{element}</QueryClientProvider></MemoryRouter>)

beforeEach(() => {
  mockGetAdminRevenueCatHealth.mockResolvedValue({configured: true, available: true, metrics})
  mockListAdminRevenueCatWebhooks.mockReset().mockResolvedValue([receipt])
  mockReplayAdminRevenueCatWebhook.mockReset().mockResolvedValue({status: 'queued'})
  mockToastInfo.mockClear()
})

describe('RevenueCat operations', () => {
  it('does not keep old metrics looking current after a failed health refresh', async () => {
    const client = new QueryClient({defaultOptions: {queries: {retry: false}}})
    render(<QueryClientProvider client={client}><RevenueCatHealthSummary /></QueryClientProvider>)
    await screen.findByText('5')
    mockGetAdminRevenueCatHealth.mockRejectedValue(new Error('refresh failed'))
    await act(async () => { await client.invalidateQueries({queryKey: getGetAdminRevenueCatHealthQueryKey()}) })
    await waitFor(() => expect(screen.queryByText('5')).not.toBeInTheDocument())
    expect(screen.getAllByText('Unavailable').length).toBeGreaterThan(0)
  })
  it('sorts numeric deliveries on the current page', async () => {
    mockListAdminRevenueCatWebhooks.mockResolvedValue([{...receipt, event_id: 'many', deliveries: 12}, {...receipt, event_id: 'few', deliveries: 2}])
    mount(<RevenueCatPage />)
    await screen.findByText('many')
    fireEvent.click(screen.getByRole('button', {name: /Deliveries/}))
    const rows = screen.getAllByRole('row').slice(1)
    expect(rows[0].textContent).toContain('few')
  })
  it('shows configuration and unavailable metrics without claiming zero', async () => {
    mockGetAdminRevenueCatHealth.mockResolvedValue({configured: false, available: false, metrics: null})
    mount(<RevenueCatHealthSummary />)
    expect(await screen.findByText('Not configured')).toBeInTheDocument()
    expect(screen.getAllByText('Unavailable').length).toBeGreaterThan(0)
    expect(screen.queryByText('5')).not.toBeInTheDocument()
    expect(screen.getByRole('link', {name: 'Open RevenueCat operations'})).toHaveAttribute('href', '/admin/revenuecat')
  })

  it('renders receipts with distinct delivery and processing counts and masks diagnostics', async () => {
    mount(<RevenueCatPage />)
    const cell = await screen.findByText('event-sensitive')
    const row = cell.closest('tr')!
    expect(cell.closest('[data-ph-mask]')).not.toBeNull()
    expect(screen.getByText('private diagnostic').closest('[data-ph-mask]')).not.toBeNull()
    expect(row).not.toHaveAttribute('data-row-id')
    expect(within(row).getByText('3')).toBeInTheDocument()
    expect(within(row).getByText('2')).toBeInTheDocument()
    expect(within(row).getByText('Retry pending')).toBeInTheDocument()
    expect(screen.getByRole('link', {name: 'Sync dead letters'})).toHaveAttribute('href', '/admin/dead-letter?kind=revenuecat_sync')
    expect(screen.getByRole('link', {name: 'Repair dead letters'})).toHaveAttribute('href', '/admin/dead-letter?kind=revenuecat_repair')
  })

  it('replays an existing receipt and reports queued rather than applied', async () => {
    mount(<RevenueCatPage />)
    fireEvent.click(await screen.findByRole('button', {name: 'Replay receipt'}))
    await waitFor(() => expect(mockReplayAdminRevenueCatWebhook).toHaveBeenCalledWith('event-sensitive'))
    expect(mockToastInfo).toHaveBeenCalledWith('Replay queued. Access changes appear after provider readback.')
    expect(screen.getByText(/Completed receipts refresh the customer/)).toBeInTheDocument()
  })

  it('pages receipts with server offsets without pretending to know the total', async () => {
    mockListAdminRevenueCatWebhooks.mockResolvedValueOnce(Array.from({length: 20}, (_, i) => ({...receipt, event_id: `event-${i}`}))).mockResolvedValue([])
    mount(<RevenueCatPage />)
    await screen.findByText('event-0')
    fireEvent.click(screen.getByRole('button', {name: 'Next page'}))
    await waitFor(() => expect(mockListAdminRevenueCatWebhooks).toHaveBeenCalledWith({limit: 20, offset: 20}))
    expect(await screen.findByText('No receipts on this page.')).toBeInTheDocument()
    expect(screen.getByRole('button', {name: 'Next page'})).toBeDisabled()
  })

  it('shows read failure rather than an empty receipt list', async () => {
    mockListAdminRevenueCatWebhooks.mockRejectedValue(new Error('private server message'))
    mount(<RevenueCatPage />)
    expect(await screen.findByText('Could not load webhook receipts. Try again.')).toBeInTheDocument()
    expect(screen.queryByText('No receipts on this page.')).not.toBeInTheDocument()
    expect(screen.queryByText('private server message')).not.toBeInTheDocument()
  })
})
