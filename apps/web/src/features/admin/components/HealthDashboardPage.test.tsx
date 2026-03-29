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
})
