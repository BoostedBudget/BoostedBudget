/**
 * HealthDashboardPage Component Tests
 * Issue #747: Updated to include Paddle Status section
 */

import { render, screen, waitFor } from '@testing-library/react'
import { describe, it, expect, beforeEach } from 'bun:test'
import { HealthDashboardPage } from './HealthDashboardPage'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { mockUseGetSystemHealth } from '@/test/mocks/admin-api'

const createTestQueryClient = () =>
  new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

const mockHealthData = {
  status: 'healthy',
  timestamp: '2026-01-25T12:00:00Z',
  services: {
    database: {
      status: 'up',
      connections: {
        active: 10,
        idle: 5,
        max: 100,
        constructing: 2,
        total: 17,
      },
      latency_ms: 5,
    },
    clerk: {
      status: 'up',
      latency_ms: 100,
    },
    plaid: {
      status: 'up',
      environment: 'sandbox',
    },
  },
  job_queues: {
    default: {
      pending: 5,
      running: 2,
      failed: 0,
      completed_last_hour: 100,
    },
    critical: {
      pending: 1,
      running: 0,
      failed: 0,
      completed_last_hour: 50,
    },
  },
  sync_status: {
    active_syncs: 3,
    failed_last_hour: 1,
    average_duration_ms: 3500,
  },
  dead_letter_queue: {
    count: 2,
    oldest_job_age_hours: 5,
  },
  paddle_status: {
    active_subscriptions: 150,
    webhook_success_rate_24h: 98.5,
    failed_webhooks_24h: 2,
    total_webhooks24h: 134,
    last_reconciliation_at: '2026-03-22T10:00:00Z',
    reconciliation_discrepancies: 0,
  },
  // Issue #1458: the 3 DB-derived Plaid-cleanup/manual-tier-override backlog gauges.
  // Issue #1458 follow-up G16: `available: true` here represents a SUCCESSFUL query that
  // legitimately found zero backlog — a verified zero, not an unknown. See the "unavailable" test
  // below for the failed-query case this fixture is deliberately NOT exercising.
  job_backlog: {
    plaid_cleanup_outstanding_users: 0,
    plaid_cleanup_oldest_outstanding_seconds: 0,
    manual_tier_override_oldest_uncleared_expired_seconds: 0,
    available: true,
  },
}

const renderWithQueryClient = (component: React.ReactElement) => {
  const queryClient = createTestQueryClient()
  return render(<QueryClientProvider client={queryClient}>{component}</QueryClientProvider>)
}

describe('HealthDashboardPage', () => {
  beforeEach(() => {
    mockUseGetSystemHealth.mockClear()
  })

  it('should render page title', () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: mockHealthData,
      isLoading: false,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)
    expect(screen.getByText(/System Health/i)).toBeInTheDocument()
  })

  it('should display service status cards', async () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: mockHealthData,
      isLoading: false,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)

    await waitFor(() => {
      expect(screen.getByText(/Database/i)).toBeInTheDocument()
      expect(screen.getByText(/Clerk/i)).toBeInTheDocument()
      expect(screen.getByText(/sandbox/i)).toBeInTheDocument()
    })
  })

  it('should show loading state', () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)
    expect(screen.getByText(/Loading/i)).toBeInTheDocument()
  })

  it('should show error state', () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('Failed to fetch'),
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)
    expect(screen.getByText(/Error/i)).toBeInTheDocument()
  })

  it('should render queue cards', async () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: mockHealthData,
      isLoading: false,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)

    await waitFor(() => {
      expect(screen.getByText(/default/i)).toBeInTheDocument()
      expect(screen.getByText(/critical/i)).toBeInTheDocument()
    })
  })

  it('should show dead letter alert when count > 0', async () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: mockHealthData,
      isLoading: false,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)

    await waitFor(() => {
      expect(screen.getByText(/dead letter/i)).toBeInTheDocument()
    })
  })

  it('should render Paddle Status section', async () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: mockHealthData,
      isLoading: false,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)

    await waitFor(() => {
      expect(screen.getByText(/Paddle Status/i)).toBeInTheDocument()
      expect(screen.getByText('Active Subscriptions')).toBeInTheDocument()
      expect(screen.getByText('150')).toBeInTheDocument()
    })
  })

  it('should display webhook success rate', async () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: mockHealthData,
      isLoading: false,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)

    await waitFor(() => {
      expect(screen.getByText('Webhook Success (24h)')).toBeInTheDocument()
      expect(screen.getByText('98.5%')).toBeInTheDocument()
    })
  })

  it('should show warning status when webhook success rate is below 90%', async () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: {
        ...mockHealthData,
        paddle_status: {
          ...mockHealthData.paddle_status,
          webhook_success_rate_24h: 85.0,
          failed_webhooks_24h: 15,
        },
      },
      isLoading: false,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)

    await waitFor(() => {
      expect(screen.getByText('85.0%')).toBeInTheDocument()
    })
  })

  it('should show "Never" when last_reconciliation_at is null', async () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: {
        ...mockHealthData,
        paddle_status: {
          ...mockHealthData.paddle_status,
          last_reconciliation_at: null,
        },
      },
      isLoading: false,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)

    await waitFor(() => {
      expect(screen.getByText('Never')).toBeInTheDocument()
    })
  })

  // Issue #1458: the 3 DB-derived Plaid-cleanup/manual-tier-override backlog gauges, surfaced via
  // the same SummaryCard grid idiom as the Paddle Status section above (there is no Dead Letter
  // Queue SummaryCard to copy — DLQ renders as a conditional alert banner instead).
  describe('Job Backlog section (Issue #1458)', () => {
    // Reads a SummaryCard's value by its title, scoped via StatCard's own `data-slot="stat-value"`
    // — this file's other numeric fixtures (active_syncs, job queue counts, etc.) can coincide
    // with these gauges' values, so an unscoped `screen.getByText('<number>')` is ambiguous.
    function backlogCardValue(title: string): string {
      const label = screen.getByText(title)
      const card = label.closest('[data-slot="card"]')
      if (!card) throw new Error(`Could not find card for "${title}"`)
      return card.querySelector('[data-slot="stat-value"]')?.textContent ?? ''
    }

    it('should render the Job Backlog section with all three gauges', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          job_backlog: {
            plaid_cleanup_outstanding_users: 42,
            plaid_cleanup_oldest_outstanding_seconds: 7500,
            manual_tier_override_oldest_uncleared_expired_seconds: 5400,
            available: true,
          },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(/Job Backlog/i)).toBeInTheDocument()
        expect(screen.getByText('Plaid Cleanup Outstanding Users')).toBeInTheDocument()
        expect(screen.getByText('Plaid Cleanup Oldest Outstanding')).toBeInTheDocument()
        expect(screen.getByText('Manual Override Oldest Uncleared Expired')).toBeInTheDocument()
      })

      expect(backlogCardValue('Plaid Cleanup Outstanding Users')).toBe('42')
      expect(backlogCardValue('Plaid Cleanup Oldest Outstanding')).toBe('2h 5m')
      expect(backlogCardValue('Manual Override Oldest Uncleared Expired')).toBe('1h 30m')
    })

    it('should render zero-state values without crashing when there is no backlog', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: mockHealthData, // job_backlog all zeros, available: true (a verified zero)
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText('Plaid Cleanup Outstanding Users')).toBeInTheDocument()
      })

      expect(backlogCardValue('Plaid Cleanup Outstanding Users')).toBe('0')
      expect(backlogCardValue('Plaid Cleanup Oldest Outstanding')).toBe('0s')
      expect(backlogCardValue('Manual Override Oldest Uncleared Expired')).toBe('0s')
    })

    // Issue #1458 follow-up G16: before this fix, a FAILED backlog query and a genuinely EMPTY
    // backlog were both represented by the identical zero-valued fields, so this section rendered
    // a confident green "0" in both cases — indistinguishable, and wrong exactly when the operator
    // most needed to know the gauge could not currently be trusted.
    it('should render an "Unavailable" warning state, not a green zero, when the backlog query failed', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          job_backlog: {
            plaid_cleanup_outstanding_users: 0,
            plaid_cleanup_oldest_outstanding_seconds: 0,
            manual_tier_override_oldest_uncleared_expired_seconds: 0,
            available: false,
          },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText('Plaid Cleanup Outstanding Users')).toBeInTheDocument()
      })

      // Every card in the section must read "Unavailable" (not the numeric "0" a real empty
      // backlog would show, and not a green success accent).
      expect(backlogCardValue('Plaid Cleanup Outstanding Users')).toBe('Unavailable')
      expect(backlogCardValue('Plaid Cleanup Oldest Outstanding')).toBe('Unavailable')
      expect(backlogCardValue('Manual Override Oldest Uncleared Expired')).toBe('Unavailable')

      // Confirms the cards are NOT rendered with the 'success' (green) accent StatCard maps
      // status='success' to — the whole point being fixed here is that "unavailable" must never
      // read as a healthy/confident state.
      const outstandingUsersCard = screen.getByText('Plaid Cleanup Outstanding Users').closest('[data-slot="card"]')
      expect(outstandingUsersCard).not.toBeNull()
      expect(outstandingUsersCard?.className ?? '').not.toMatch(/success/i)
    })

    // Same "unknown != zero" signal, but for a client with an OLDER cached response predating
    // this field's addition — `available` is simply absent (undefined), not explicitly `false`.
    // Must degrade the same fail-closed way, not accidentally read as available.
    it('treats a missing `available` field (older cached response) the same as unavailable', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          job_backlog: {
            plaid_cleanup_outstanding_users: 0,
            plaid_cleanup_oldest_outstanding_seconds: 0,
            manual_tier_override_oldest_uncleared_expired_seconds: 0,
          },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText('Plaid Cleanup Outstanding Users')).toBeInTheDocument()
      })

      expect(backlogCardValue('Plaid Cleanup Outstanding Users')).toBe('Unavailable')
    })
  })
})
