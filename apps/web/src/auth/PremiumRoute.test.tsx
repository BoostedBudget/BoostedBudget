// Issue #930: PremiumRoute three-state routing tests
// Issue #898: past_due added to read-only; banner removed from PremiumRoute (moved to App shell)
// Verifies active users get full access, read-only users see children read-only, others redirect.

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Routes, Route, useLocation } from 'react-router'
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

/** Route probe rendered at /pricing to inspect the Navigate redirect's location.state */
function LocationProbe() {
  const location = useLocation()
  const frozen = (location.state as { frozen?: boolean } | null)?.frozen
  return <div data-testid="location-probe" data-frozen={String(frozen)} />
}

interface BuildUserOverrides {
  is_comped?: boolean
  comp_ends_at?: string
  effective_status?: string
}

function buildUser(subscriptionStatus: string, overrides: BuildUserOverrides = {}) {
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
    ...overrides,
  }
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

/** Renders PremiumRoute behind an actual /pricing route so redirects can be inspected. */
function renderPremiumRouteWithRouting(children = <div data-testid="content">Content</div>) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/protected']}>
        <Routes>
          <Route path="/protected" element={<PremiumRoute>{children}</PremiumRoute>} />
          <Route path="/pricing" element={<LocationProbe />} />
        </Routes>
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

  it('shows children for inactive user (banner rendered by App shell)', async () => {
    setupMock('inactive')
    renderPremiumRoute()
    await waitFor(() => {
      expect(screen.getByTestId('content')).toBeInTheDocument()
    })
  })

  it('shows children for cancelled user (banner rendered by App shell)', async () => {
    setupMock('cancelled')
    renderPremiumRoute()
    await waitFor(() => {
      expect(screen.getByTestId('content')).toBeInTheDocument()
    })
  })

  it('shows children for past_due user read-only (banner rendered by App shell)', async () => {
    setupMock('past_due')
    renderPremiumRoute(<ReadOnlyConsumer />)
    await waitFor(() => {
      expect(screen.getByTestId('readonly-consumer')).toBeInTheDocument()
    })
    expect(screen.getByTestId('readonly-consumer')).toHaveAttribute('data-is-readonly', 'true')
    expect(screen.getByTestId('readonly-consumer')).toHaveAttribute('data-status', 'past_due')
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

  // Issue #1413: admin-comped users must get real write access, regardless of the raw
  // subscription_status left behind on the user row.
  describe('Comped users (#1413)', () => {
    it('the core bug: grants write access for a comped user even with a raw status of inactive', async () => {
      mockUseGetUserProfile.mockReturnValue({
        data: buildUser('inactive', { is_comped: true }),
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve(),
      })
      renderPremiumRoute(<ReadOnlyConsumer />)
      await waitFor(() => {
        expect(screen.getByTestId('readonly-consumer')).toBeInTheDocument()
      })
      expect(screen.getByTestId('readonly-consumer')).toHaveAttribute('data-is-readonly', 'false')
    })

    it("sets contextStatus to 'comped' for a comped user", async () => {
      mockUseGetUserProfile.mockReturnValue({
        data: buildUser('active', { is_comped: true }),
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve(),
      })
      renderPremiumRoute(<ReadOnlyConsumer />)
      await waitFor(() => {
        expect(screen.getByTestId('readonly-consumer')).toBeInTheDocument()
      })
      expect(screen.getByTestId('readonly-consumer')).toHaveAttribute('data-status', 'comped')
    })

    it('does not redirect a comped user whose raw status is unrecognised', async () => {
      mockUseGetUserProfile.mockReturnValue({
        data: buildUser('some_future_enum_value', { is_comped: true }),
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve(),
      })
      renderPremiumRoute()
      await waitFor(() => {
        expect(screen.getByTestId('content')).toBeInTheDocument()
      })
    })
  })

  // This branch previously had no test at all.
  it('redirects to /pricing with state.frozen === true for an unrecognised status', async () => {
    mockUseGetUserProfile.mockReturnValue({
      data: buildUser('some_future_enum_value'),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    renderPremiumRouteWithRouting()
    await waitFor(() => {
      expect(screen.getByTestId('location-probe')).toBeInTheDocument()
    })
    expect(screen.getByTestId('location-probe')).toHaveAttribute('data-frozen', 'true')
  })
})
