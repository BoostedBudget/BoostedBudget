// Issue #930: PremiumRoute three-state routing tests
// Verifies active users get full access, read-only users see banner, others redirect.

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { PremiumRoute } from './PremiumRoute'
import { useReadOnly } from './ReadOnlyContext'
import { mockUseGetUserProfile } from '@/test/mocks/profile-api'

/** Helper child that exposes ReadOnlyContext values via testid */
function ReadOnlyConsumer() {
  const { isReadOnly, subscriptionStatus } = useReadOnly()
  return (
    <div
      data-testid="readonly-consumer"
      data-is-readonly={String(isReadOnly)}
      data-status={subscriptionStatus}
    />
  )
}

function buildUser(subscriptionStatus: string) {
  return {
    id: 'user-1',
    email: 'test@example.com',
    subscription_status: subscriptionStatus,
    subscription_tier: 'premium' as const,
    display_name: 'Test User',
    timezone: 'America/New_York',
    currency_code: 'USD',
    locale: 'en-US',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  } as const
}

function renderPremiumRoute(children = <div data-testid="content">Content</div>) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <PremiumRoute>{children}</PremiumRoute>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

describe('PremiumRoute', () => {
  beforeEach(() => {
    mockUseGetUserProfile.mockClear()
  })

  it('shows loading skeleton while profile is loading', () => {
    mockUseGetUserProfile.mockReturnValue({ data: undefined, isLoading: true, error: null, refetch: () => Promise.resolve() })
    renderPremiumRoute()
    expect(screen.getByTestId('premium-loading')).toBeInTheDocument()
  })

  const setupMock = (status: string) => {
    mockUseGetUserProfile.mockReturnValue({ data: buildUser(status), isLoading: false, error: null, refetch: () => Promise.resolve() })
  }

  it('shows children for active user without banner', async () => {
    setupMock('active')
    renderPremiumRoute()
    await waitFor(() => {
      expect(screen.getByTestId('content')).toBeInTheDocument()
    })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows children for trialing user without banner', async () => {
    setupMock('trialing')
    renderPremiumRoute()
    await waitFor(() => {
      expect(screen.getByTestId('content')).toBeInTheDocument()
    })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows children for trial user without banner', async () => {
    setupMock('trial')
    renderPremiumRoute()
    await waitFor(() => {
      expect(screen.getByTestId('content')).toBeInTheDocument()
    })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows banner and children for inactive user', async () => {
    setupMock('inactive')
    renderPremiumRoute()
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    expect(screen.getByTestId('content')).toBeInTheDocument()
    expect(screen.getByText(/trial has ended/i)).toBeInTheDocument()
  })

  it('shows banner and children for cancelled user', async () => {
    setupMock('cancelled')
    renderPremiumRoute()
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    expect(screen.getByTestId('content')).toBeInTheDocument()
    expect(screen.getByText(/subscription has been cancelled/i)).toBeInTheDocument()
  })

  it('shows error state when profile API fails', () => {
    mockUseGetUserProfile.mockReturnValue({ data: undefined, isLoading: false, error: new Error('Network error'), refetch: () => Promise.resolve() })
    renderPremiumRoute()
    expect(screen.getByTestId('profile-error')).toBeInTheDocument()
  })

  it('shows loading when user is null without error', () => {
    mockUseGetUserProfile.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: () => Promise.resolve() })
    renderPremiumRoute()
    expect(screen.getByTestId('premium-loading')).toBeInTheDocument()
  })

  it('wires ReadOnlyProvider with isReadOnly=false for active user', async () => {
    setupMock('active')
    renderPremiumRoute(<ReadOnlyConsumer />)
    await waitFor(() => {
      expect(screen.getByTestId('readonly-consumer')).toBeInTheDocument()
    })
    expect(screen.getByTestId('readonly-consumer')).toHaveAttribute('data-is-readonly', 'false')
    expect(screen.getByTestId('readonly-consumer')).toHaveAttribute('data-status', 'active')
  })

  it('wires ReadOnlyProvider with isReadOnly=true for inactive user', async () => {
    setupMock('inactive')
    renderPremiumRoute(<ReadOnlyConsumer />)
    await waitFor(() => {
      expect(screen.getByTestId('readonly-consumer')).toBeInTheDocument()
    })
    expect(screen.getByTestId('readonly-consumer')).toHaveAttribute('data-is-readonly', 'true')
    expect(screen.getByTestId('readonly-consumer')).toHaveAttribute('data-status', 'inactive')
  })

  it('wires ReadOnlyProvider with isReadOnly=true for cancelled user', async () => {
    setupMock('cancelled')
    renderPremiumRoute(<ReadOnlyConsumer />)
    await waitFor(() => {
      expect(screen.getByTestId('readonly-consumer')).toBeInTheDocument()
    })
    expect(screen.getByTestId('readonly-consumer')).toHaveAttribute('data-is-readonly', 'true')
    expect(screen.getByTestId('readonly-consumer')).toHaveAttribute('data-status', 'cancelled')
  })
})
