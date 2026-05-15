import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import {
  mockUseListAdminDeadLetterJobs,
  mockUseGetAdminDeadLetterJobStats,
  mockUseRetryAdminDeadLetterJob,
  mockUseDeleteAdminDeadLetterJob,
  mockUseBulkDeleteAdminDeadLetterJobs,
  mockUseGetAdminDeadLetterJobDetail,
  mockUseGetAdminDeadLetterJobArgs,
  mockUseAddAdminDeadLetterJobNotes,
} from '@/test/mocks/admin-api'
import { DeadLetterPage } from './DeadLetterPage'

const mockJobs = [
  {
    id: 1,
    original_job_id: 100,
    kind: 'sync_transactions',
    queue: 'default',
    args: { '$schema': '', user_id: '[REDACTED]' },
    errors: [],
    max_attempts: 3,
    created_at: '2026-04-01T10:00:00Z',
    failed_at: '2026-04-01T11:00:00Z',
    user_id: 'user_abc',
    notified: false,
    retried_at: undefined,
    retry_job_id: undefined,
    notes: undefined,
  },
  {
    id: 2,
    original_job_id: 101,
    kind: 'send_email',
    queue: 'low',
    args: { '$schema': '', email: '[REDACTED]' },
    errors: [{ error: 'timeout' }],
    max_attempts: 5,
    created_at: '2026-04-02T10:00:00Z',
    failed_at: '2026-04-02T11:00:00Z',
    user_id: undefined,
    notified: true,
    retried_at: '2026-04-02T12:00:00Z',
    retry_job_id: 200,
    notes: 'Under investigation',
  },
]

const mockListResponse = {
  jobs: mockJobs,
  total: 2,
  page: 1,
  page_size: 20,
  total_pages: 1,
  has_more: false,
}

const mockStatsResponse = {
  total: 10,
  unnotified: 3,
  retried: 4,
  last_24_hours: 2,
  by_kind: {
    sync_transactions: 6,
    send_email: 4,
  },
}

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>{children}</BrowserRouter>
      </QueryClientProvider>
    )
  }
}

describe('DeadLetterPage', () => {
  beforeEach(() => {
    mockUseListAdminDeadLetterJobs.mockClear()
    mockUseGetAdminDeadLetterJobStats.mockClear()
    mockUseRetryAdminDeadLetterJob.mockClear()
    mockUseDeleteAdminDeadLetterJob.mockClear()
    mockUseBulkDeleteAdminDeadLetterJobs.mockClear()
    mockUseGetAdminDeadLetterJobDetail.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminDeadLetterJobArgs.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    mockUseAddAdminDeadLetterJobNotes.mockReturnValue({
      mutate: () => {},
      isPending: false,
    })
  })

  it('renders loading state', () => {
    mockUseListAdminDeadLetterJobs.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    })
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: () => {}, isPending: false })

    render(<DeadLetterPage />, { wrapper: createWrapper() })

    expect(screen.getByRole('status')).toBeInTheDocument()
    expect(screen.getByText(/loading dead letter jobs/i)).toBeInTheDocument()
  })

  it('renders error state', () => {
    mockUseListAdminDeadLetterJobs.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('Network error'),
    })
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({ data: undefined, isLoading: false, error: null })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: () => {}, isPending: false })

    render(<DeadLetterPage />, { wrapper: createWrapper() })

    expect(screen.getByText(/failed to load dead letter jobs/i)).toBeInTheDocument()
  })

  it('renders empty state with no jobs', () => {
    mockUseListAdminDeadLetterJobs.mockReturnValue({
      data: { jobs: [], total: 0, page: 1, page_size: 20, total_pages: 0, has_more: false },
      isLoading: false,
      error: null,
    })
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({ data: undefined, isLoading: false, error: null })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: () => {}, isPending: false })

    render(<DeadLetterPage />, { wrapper: createWrapper() })

    expect(screen.getByText(/no dead letter jobs found/i)).toBeInTheDocument()
  })

  it('renders list of jobs', () => {
    mockUseListAdminDeadLetterJobs.mockReturnValue({
      data: mockListResponse,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({ data: undefined, isLoading: false, error: null })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: () => {}, isPending: false })

    render(<DeadLetterPage />, { wrapper: createWrapper() })

    expect(screen.getAllByText('sync_transactions').length).toBeGreaterThan(0)
    expect(screen.getAllByText('send_email').length).toBeGreaterThan(0)
    expect(screen.getByText('user_abc')).toBeInTheDocument()
    expect(screen.getByText('Under investigation')).toBeInTheDocument()
  })

  it('displays stats cards when stats available', () => {
    mockUseListAdminDeadLetterJobs.mockReturnValue({
      data: mockListResponse,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({
      data: mockStatsResponse,
      isLoading: false,
      error: null,
    })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: () => {}, isPending: false })

    render(<DeadLetterPage />, { wrapper: createWrapper() })

    expect(screen.getByText('Total')).toBeInTheDocument()
    expect(screen.getByText('10')).toBeInTheDocument()
    expect(screen.getByText('Last 24h')).toBeInTheDocument()
  })

  it('shows by-kind badges when stats available', () => {
    mockUseListAdminDeadLetterJobs.mockReturnValue({
      data: mockListResponse,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({
      data: mockStatsResponse,
      isLoading: false,
      error: null,
    })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: () => {}, isPending: false })

    render(<DeadLetterPage />, { wrapper: createWrapper() })

    expect(screen.getByRole('button', { name: /filter by sync_transactions/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /filter by send_email/i })).toBeInTheDocument()
  })

  it('kind filter renders options', () => {
    mockUseListAdminDeadLetterJobs.mockReturnValue({
      data: mockListResponse,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({
      data: mockStatsResponse,
      isLoading: false,
      error: null,
    })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: () => {}, isPending: false })

    render(<DeadLetterPage />, { wrapper: createWrapper() })

    const kindSelect = screen.getByLabelText(/filter by job kind/i)
    expect(kindSelect).toBeInTheDocument()
    fireEvent.change(kindSelect, { target: { value: 'sync_transactions' } })
    expect(mockUseListAdminDeadLetterJobs).toHaveBeenCalled()
  })

  it('shows pagination controls when multiple pages', () => {
    mockUseListAdminDeadLetterJobs.mockReturnValue({
      data: {
        jobs: mockJobs,
        total: 50,
        page: 1,
        page_size: 20,
        total_pages: 3,
        has_more: true,
      },
      isLoading: false,
      error: null,
    })
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({ data: undefined, isLoading: false, error: null })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: () => {}, isPending: false })

    render(<DeadLetterPage />, { wrapper: createWrapper() })

    expect(screen.getByText(/page 1 of 3/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /next page/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /previous page/i })).toBeDisabled()
  })

  it('shows total job count in header', () => {
    mockUseListAdminDeadLetterJobs.mockReturnValue({
      data: mockListResponse,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({ data: undefined, isLoading: false, error: null })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: () => {}, isPending: false })

    render(<DeadLetterPage />, { wrapper: createWrapper() })

    expect(screen.getByText(/2 jobs/i)).toBeInTheDocument()
  })

  it('shows reset button when filters active', () => {
    mockUseListAdminDeadLetterJobs.mockReturnValue({
      data: mockListResponse,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({ data: undefined, isLoading: false, error: null })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: () => {}, isPending: false })

    render(<DeadLetterPage />, { wrapper: createWrapper() })

    fireEvent.change(screen.getByLabelText(/filter by start date/i), { target: { value: '2026-01-01' } })
    expect(screen.getByRole('button', { name: /reset filters/i })).toBeInTheDocument()
  })

  it('shows bulk delete button when filters active and jobs present', () => {
    mockUseListAdminDeadLetterJobs.mockReturnValue({
      data: mockListResponse,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({
      data: mockStatsResponse,
      isLoading: false,
      error: null,
    })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: () => {}, isPending: false })

    render(<DeadLetterPage />, { wrapper: createWrapper() })

    // Use user ID filter (text input always works regardless of stats data)
    fireEvent.change(screen.getByLabelText(/filter by user id/i), { target: { value: 'user_abc' } })

    expect(screen.getByRole('button', { name: /delete all matching/i })).toBeInTheDocument()
  })

  it('bulk delete button opens ConfirmationDialog', async () => {
    const user = userEvent.setup()
    mockUseListAdminDeadLetterJobs.mockReturnValue({
      data: mockListResponse,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({ data: mockStatsResponse, isLoading: false, error: null })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: mock(() => {}), isPending: false })

    render(<DeadLetterPage />, { wrapper: createWrapper() })

    fireEvent.change(screen.getByLabelText(/filter by user id/i), { target: { value: 'user_abc' } })
    await user.click(screen.getByRole('button', { name: /delete all matching/i }))

    expect(screen.getByRole('alertdialog')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /confirm delete all/i })).not.toBeInTheDocument()
  })

  it('bulk delete Cancel does not call mutation', async () => {
    const user = userEvent.setup()
    const bulkMutate = mock(() => {})
    mockUseListAdminDeadLetterJobs.mockReturnValue({ data: mockListResponse, isLoading: false, error: null })
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({ data: undefined, isLoading: false, error: null })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: bulkMutate, isPending: false })

    render(<DeadLetterPage />, { wrapper: createWrapper() })

    fireEvent.change(screen.getByLabelText(/filter by user id/i), { target: { value: 'user_abc' } })
    await user.click(screen.getByRole('button', { name: /delete all matching/i }))
    const dialog = screen.getByRole('alertdialog')
    await user.click(within(dialog).getByRole('button', { name: /cancel/i }))

    expect(bulkMutate).not.toHaveBeenCalled()
  })

  it('bulk delete Confirm calls mutation', async () => {
    const user = userEvent.setup()
    const bulkMutate = mock(() => {})
    mockUseListAdminDeadLetterJobs.mockReturnValue({ data: mockListResponse, isLoading: false, error: null })
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({ data: undefined, isLoading: false, error: null })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: bulkMutate, isPending: false })

    render(<DeadLetterPage />, { wrapper: createWrapper() })

    fireEvent.change(screen.getByLabelText(/filter by user id/i), { target: { value: 'user_abc' } })
    await user.click(screen.getByRole('button', { name: /delete all matching/i }))
    const dialog = screen.getByRole('alertdialog')
    await user.click(within(dialog).getByRole('button', { name: /^delete$/i }))

    expect(bulkMutate).toHaveBeenCalledTimes(1)
  })

  it('per-row delete button opens ConfirmationDialog', async () => {
    const user = userEvent.setup()
    mockUseListAdminDeadLetterJobs.mockReturnValue({ data: mockListResponse, isLoading: false, error: null })
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({ data: undefined, isLoading: false, error: null })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: mock(() => {}), isPending: false })

    render(<DeadLetterPage />, { wrapper: createWrapper() })

    await user.click(screen.getByRole('button', { name: /delete job 1/i }))

    expect(screen.getByRole('alertdialog')).toBeInTheDocument()
  })

  it('per-row delete Confirm calls mutation with correct id', async () => {
    const user = userEvent.setup()
    const deleteMutate = mock(() => {})
    mockUseListAdminDeadLetterJobs.mockReturnValue({ data: mockListResponse, isLoading: false, error: null })
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({ data: undefined, isLoading: false, error: null })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: deleteMutate, isPending: false })
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: mock(() => {}), isPending: false })

    render(<DeadLetterPage />, { wrapper: createWrapper() })

    await user.click(screen.getByRole('button', { name: /delete job 1/i }))
    const dialog = screen.getByRole('alertdialog')
    await user.click(within(dialog).getByRole('button', { name: /^delete$/i }))

    expect(deleteMutate).toHaveBeenCalledWith({ id: 1 }, expect.any(Object))
  })

  it('shows date filter inputs', () => {
    mockUseListAdminDeadLetterJobs.mockReturnValue({
      data: mockListResponse,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({ data: undefined, isLoading: false, error: null })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: () => {}, isPending: false })

    render(<DeadLetterPage />, { wrapper: createWrapper() })

    expect(screen.getByLabelText(/filter by start date/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/filter by end date/i)).toBeInTheDocument()
  })
})
