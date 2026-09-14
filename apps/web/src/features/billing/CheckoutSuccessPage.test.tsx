import { beforeEach, describe, expect, it } from 'bun:test'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { CheckoutSuccessPage } from './CheckoutSuccessPage'
import { mockRefreshRevenueCatSubscription } from '@/test/mocks/subscription-api'
import { mockUseAuth, createMockClerkAuth } from '@/test/mocks/clerk'
import type { RevenueCatSummary } from '@boostedbudget/api-client/models'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

const authority = (paid: unknown = null) => ({ access_resolved: true, paid }) as RevenueCatSummary
let queryClient: QueryClient
const page = () => <QueryClientProvider client={queryClient}><MemoryRouter initialEntries={['/billing/success?tier=Forged&success=true']}><CheckoutSuccessPage /></MemoryRouter></QueryClientProvider>

describe('CheckoutSuccessPage', () => {
  beforeEach(() => {
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
    queryClient.setQueryData(['cached-profile'], { subscription_status: 'inactive' })
    mockUseAuth.mockReturnValue(createMockClerkAuth({ userId: 'user_current', isSignedIn: true }))
    mockRefreshRevenueCatSubscription.mockReset()
    mockRefreshRevenueCatSubscription.mockResolvedValue(authority())
  })
  it('does not claim activation from URL or complimentary access', async () => {
    render(page())
    await waitFor(() => expect(screen.getByText(/awaiting confirmation/i)).toBeInTheDocument())
    expect(screen.queryByText(/activated/i)).toBeNull()
    expect(screen.queryByText(/Forged/)).toBeNull()
  })
  it('confirms paid access only after a backend refresh', async () => {
    mockRefreshRevenueCatSubscription.mockResolvedValue(authority({ status: 'active', tier: 'household_2' }))
    render(page())
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Subscription confirmed' })).toBeInTheDocument())
    expect(mockRefreshRevenueCatSubscription).toHaveBeenCalledTimes(1)
    expect(queryClient.getQueryState(['cached-profile'])?.isInvalidated).toBe(true)
  })
  it('offers retry during a provider outage', async () => {
    mockRefreshRevenueCatSubscription.mockRejectedValueOnce(new Error('offline'))
    render(page())
    await waitFor(() => expect(screen.getByRole('button', { name: /check again/i })).toBeInTheDocument())
    mockRefreshRevenueCatSubscription.mockResolvedValue(authority({ status: 'active', tier: 'premium' }))
    fireEvent.click(screen.getByRole('button', { name: /check again/i }))
    await waitFor(() => expect(screen.getByText('Subscription confirmed')).toBeInTheDocument())
  })
  it('ignores a previous account confirmation after an identity switch', async () => {
    let resolve!: (value: RevenueCatSummary) => void
    mockRefreshRevenueCatSubscription.mockImplementationOnce(() => new Promise(yes => { resolve = yes }))
    const view = render(page())
    mockUseAuth.mockReturnValue(createMockClerkAuth({ userId: 'user_other', isSignedIn: true }))
    view.rerender(page())
    resolve(authority({ status: 'active', tier: 'premium' }))
    await waitFor(() => expect(screen.getByText(/awaiting confirmation/i)).toBeInTheDocument())
    expect(screen.queryByText('Subscription confirmed')).toBeNull()
    expect(queryClient.getQueryState(['cached-profile'])?.isInvalidated).toBe(false)
  })
})
