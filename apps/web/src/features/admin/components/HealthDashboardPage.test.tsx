import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { HealthDashboardPage } from './HealthDashboardPage';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

// Mock the API client
vi.mock('@boostedbudget/api-client/endpoints/admin/admin', () => ({
  useGetSystemHealth: vi.fn(),
}));

const createTestQueryClient = () =>
  new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

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
};

const mockUseGetSystemHealth = async (overrides = {}) => {
  const { useGetSystemHealth } = await import('@boostedbudget/api-client/endpoints/admin/admin');
  (useGetSystemHealth as ReturnType<typeof vi.fn>).mockReturnValue({
    data: mockHealthData,
    isLoading: false,
    error: null,
    refetch: vi.fn(),
    ...overrides,
  });
};

const renderWithQueryClient = (component: React.ReactElement) => {
  const queryClient = createTestQueryClient();
  return render(
    <QueryClientProvider client={queryClient}>
      {component}
    </QueryClientProvider>
  );
};

describe('HealthDashboardPage', () => {
  it('should render page title', async () => {
    await mockUseGetSystemHealth();
    renderWithQueryClient(<HealthDashboardPage />);
    expect(screen.getByText(/System Health/i)).toBeInTheDocument();
  });

  it('should display service status cards', async () => {
    await mockUseGetSystemHealth();
    renderWithQueryClient(<HealthDashboardPage />);

    await waitFor(() => {
      expect(screen.getByText(/Database/i)).toBeInTheDocument();
      expect(screen.getByText(/Clerk/i)).toBeInTheDocument();
      expect(screen.getByText(/sandbox/i)).toBeInTheDocument();
    });
  });

  it('should show loading state', async () => {
    await mockUseGetSystemHealth({ data: undefined, isLoading: true });
    renderWithQueryClient(<HealthDashboardPage />);
    expect(screen.getByText(/Loading/i)).toBeInTheDocument();
  });

  it('should show error state', async () => {
    await mockUseGetSystemHealth({ data: undefined, error: new Error('Failed to fetch') });
    renderWithQueryClient(<HealthDashboardPage />);
    expect(screen.getByText(/Error/i)).toBeInTheDocument();
  });

  it('should render queue cards', async () => {
    await mockUseGetSystemHealth();
    renderWithQueryClient(<HealthDashboardPage />);

    await waitFor(() => {
      expect(screen.getByText(/default/i)).toBeInTheDocument();
      expect(screen.getByText(/critical/i)).toBeInTheDocument();
    });
  });

  it('should show dead letter alert when count > 0', async () => {
    await mockUseGetSystemHealth();
    renderWithQueryClient(<HealthDashboardPage />);

    await waitFor(() => {
      expect(screen.getByText(/dead letter/i)).toBeInTheDocument();
    });
  });
});
