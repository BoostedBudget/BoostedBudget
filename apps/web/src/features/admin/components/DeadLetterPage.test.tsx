import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router'
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
import { mockToastError, mockToastSuccess } from '@/test/mocks/sonner'
import {
  getListAdminDeadLetterJobsQueryKey,
  getGetAdminDeadLetterJobStatsQueryKey,
} from '@boostedbudget/api-client/endpoints/admin/admin'
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
  it('keeps RevenueCat filters available even when the stats omit those job kinds', () => {
    render(<DeadLetterPage />, {wrapper: createWrapper()})
    expect(screen.getByRole('option', {name: 'revenuecat_sync'})).toBeInTheDocument()
    expect(screen.getByRole('option', {name: 'revenuecat_repair'})).toBeInTheDocument()
  })
  it('explains that deleting a RevenueCat dead letter does not cancel grant intent', () => {
    render(<DeadLetterPage />, {wrapper: createWrapper()})
    fireEvent.change(screen.getByRole('combobox', {name: 'Filter by job kind'}), {target: {value: 'revenuecat_repair'}})
    expect(screen.getByText(/Deleting a RevenueCat dead letter does not cancel a pending grant/)).toBeInTheDocument()
    expect(screen.getByRole('link', {name: 'Open RevenueCat operations'})).toHaveAttribute('href', '/admin/revenuecat')
  })
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

  // Issue #1496 (UI half): retry used to be invoked with `onSuccess` only, so a rejected
  // replay produced no user-visible signal at all — the operator clicked Retry, the request
  // 409'd or 422'd, and the page looked exactly as it does on success.
  //
  // The two reachable failure modes are the ones covered here. A *stranded* row (already
  // retried) cannot produce the symptom: its button is disabled, so no request is sent.
  describe('Issue #1496 – retry outcome is reported to the operator', () => {
    type RetryOptions = {
      onSuccess?: (data: unknown) => void
      onError?: (err: unknown) => void
    } | undefined

    // Shape matches an axios rejection carrying a Huma ErrorModel body.
    const axiosError = (status: number, detail: string) => ({
      response: { status, data: { detail } },
      message: `Request failed with status code ${status}`,
      isAxiosError: true,
    })

    const DEDUP_DETAIL = 'a matching job already exists for this kind; retry was deduplicated and not enqueued'
    const UNKNOWN_KIND_DETAIL = 'this job kind has no registered uniqueness policy; retry refused'

    function renderPage() {
      const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
      const invalidateQueries = mock(() => Promise.resolve())
      queryClient.invalidateQueries = invalidateQueries as unknown as typeof queryClient.invalidateQueries

      function Wrapper({ children }: { children: React.ReactNode }) {
        return (
          <QueryClientProvider client={queryClient}>
            <BrowserRouter>{children}</BrowserRouter>
          </QueryClientProvider>
        )
      }

      render(<DeadLetterPage />, { wrapper: Wrapper })
      return { invalidateQueries }
    }

    function primeQueries() {
      mockUseListAdminDeadLetterJobs.mockReturnValue({ data: mockListResponse, isLoading: false, error: null })
      mockUseGetAdminDeadLetterJobStats.mockReturnValue({ data: undefined, isLoading: false, error: null })
      mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
      mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    }

    beforeEach(() => {
      mockToastError.mockClear()
      mockToastSuccess.mockClear()
    })

    it('surfaces the 409 deduplicated failure to the operator', async () => {
      const user = userEvent.setup()
      primeQueries()
      mockUseRetryAdminDeadLetterJob.mockReturnValue({
        mutate: mock((_vars: unknown, options: RetryOptions) => {
          options?.onError?.(axiosError(409, DEDUP_DETAIL))
        }),
        isPending: false,
      })

      renderPage()
      await user.click(screen.getByRole('button', { name: /retry job 1/i }))

      expect(mockToastError).toHaveBeenCalledTimes(1)
      expect(mockToastError).toHaveBeenCalledWith(expect.stringContaining(DEDUP_DETAIL))
      expect(mockToastSuccess).not.toHaveBeenCalled()
    })

    it('surfaces the 422 unknown-kind failure to the operator', async () => {
      const user = userEvent.setup()
      primeQueries()
      mockUseRetryAdminDeadLetterJob.mockReturnValue({
        mutate: mock((_vars: unknown, options: RetryOptions) => {
          options?.onError?.(axiosError(422, UNKNOWN_KIND_DETAIL))
        }),
        isPending: false,
      })

      renderPage()
      await user.click(screen.getByRole('button', { name: /retry job 1/i }))

      expect(mockToastError).toHaveBeenCalledTimes(1)
      expect(mockToastError).toHaveBeenCalledWith(expect.stringContaining(UNKNOWN_KIND_DETAIL))
      expect(mockToastSuccess).not.toHaveBeenCalled()
    })

    it('falls back to the transport message when the body carries no detail', async () => {
      const user = userEvent.setup()
      primeQueries()
      mockUseRetryAdminDeadLetterJob.mockReturnValue({
        mutate: mock((_vars: unknown, options: RetryOptions) => {
          options?.onError?.(new Error('Network Error'))
        }),
        isPending: false,
      })

      renderPage()
      await user.click(screen.getByRole('button', { name: /retry job 1/i }))

      expect(mockToastError).toHaveBeenCalledWith(expect.stringContaining('Network Error'))
    })

    // Sibling positive assertion: without it, the error tests above would also pass if retry
    // were broken outright (never enqueuing, never reporting success).
    it('still reports success and refreshes the list when the retry succeeds', async () => {
      const user = userEvent.setup()
      primeQueries()
      const retryMutate = mock((_vars: unknown, options: RetryOptions) => {
        options?.onSuccess?.({ dead_letter_job_id: 1, retry_job_id: 999 })
      })
      mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: retryMutate, isPending: false })

      const { invalidateQueries } = renderPage()
      await user.click(screen.getByRole('button', { name: /retry job 1/i }))

      expect(retryMutate).toHaveBeenCalledWith({ id: 1 }, expect.any(Object))
      expect(mockToastSuccess).toHaveBeenCalledTimes(1)
      expect(mockToastError).not.toHaveBeenCalled()
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getListAdminDeadLetterJobsQueryKey() })
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getGetAdminDeadLetterJobStatsQueryKey() })
    })
  })

  // Issue #1496 (follow-up): the retry fix above was scoped too narrowly — the identical
  // defect (mutation reports success only) was still live on per-row delete, bulk delete,
  // and (in the modal) save-notes and delete. Per-row delete had a worse symptom: because
  // `setConfirmDeleteJobId(null)` ran only in `onSuccess`, a failed delete left the
  // confirmation dialog stuck open with no explanation at all.
  describe('Issue #1496 – delete outcome is reported to the operator', () => {
    type DeleteOptions = {
      onSuccess?: (data: unknown) => void
      onError?: (err: unknown) => void
    } | undefined

    const axiosError = (status: number, detail: string) => ({
      response: { status, data: { detail } },
      message: `Request failed with status code ${status}`,
      isAxiosError: true,
    })

    const DELETE_FAILURE_DETAIL = 'job is referenced by an active retry chain; delete refused'

    function primeQueries() {
      mockUseListAdminDeadLetterJobs.mockReturnValue({ data: mockListResponse, isLoading: false, error: null })
      mockUseGetAdminDeadLetterJobStats.mockReturnValue({ data: undefined, isLoading: false, error: null })
      mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
      mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    }

    beforeEach(() => {
      mockToastError.mockClear()
      mockToastSuccess.mockClear()
    })

    async function openAndConfirmDelete(user: ReturnType<typeof userEvent.setup>) {
      await user.click(screen.getByRole('button', { name: /delete job 1/i }))
      const dialog = screen.getByRole('alertdialog')
      await user.click(within(dialog).getByRole('button', { name: /^delete$/i }))
    }

    it('surfaces a delete failure to the operator and dismisses the stuck-open dialog', async () => {
      const user = userEvent.setup()
      primeQueries()
      mockUseDeleteAdminDeadLetterJob.mockReturnValue({
        mutate: mock((_vars: unknown, options: DeleteOptions) => {
          options?.onError?.(axiosError(409, DELETE_FAILURE_DETAIL))
        }),
        isPending: false,
      })

      render(<DeadLetterPage />, { wrapper: createWrapper() })
      await openAndConfirmDelete(user)

      expect(mockToastError).toHaveBeenCalledTimes(1)
      expect(mockToastError).toHaveBeenCalledWith(expect.stringContaining(DELETE_FAILURE_DETAIL))
      expect(mockToastSuccess).not.toHaveBeenCalled()
      // The worse symptom: without the fix, the confirmation dialog stays open on failure with
      // no explanation at all.
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    })

    // Sibling positive assertion: without it, the failure test above would also pass if delete
    // were broken outright (never deleting, never reporting success, never closing the dialog).
    it('still reports success, tracks the event, and closes the dialog when delete succeeds', async () => {
      const user = userEvent.setup()
      primeQueries()
      const deleteMutate = mock((_vars: unknown, options: DeleteOptions) => {
        options?.onSuccess?.(undefined)
      })
      mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: deleteMutate, isPending: false })

      render(<DeadLetterPage />, { wrapper: createWrapper() })
      await openAndConfirmDelete(user)

      expect(deleteMutate).toHaveBeenCalledWith({ id: 1 }, expect.any(Object))
      expect(mockToastSuccess).toHaveBeenCalledTimes(1)
      expect(mockToastError).not.toHaveBeenCalled()
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    })
  })

  describe('Issue #1496 – bulk delete outcome is reported to the operator', () => {
    type BulkDeleteOptions = {
      onSuccess?: (data: unknown) => void
      onError?: (err: unknown) => void
    } | undefined

    const axiosError = (status: number, detail: string) => ({
      response: { status, data: { detail } },
      message: `Request failed with status code ${status}`,
      isAxiosError: true,
    })

    const BULK_DELETE_FAILURE_DETAIL = 'bulk delete refused: filter matched more than the safety cap'

    function primeQueries() {
      mockUseListAdminDeadLetterJobs.mockReturnValue({ data: mockListResponse, isLoading: false, error: null })
      mockUseGetAdminDeadLetterJobStats.mockReturnValue({ data: undefined, isLoading: false, error: null })
      mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
      mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    }

    beforeEach(() => {
      mockToastError.mockClear()
      mockToastSuccess.mockClear()
    })

    async function openAndConfirmBulkDelete(user: ReturnType<typeof userEvent.setup>) {
      fireEvent.change(screen.getByLabelText(/filter by user id/i), { target: { value: 'user_abc' } })
      await user.click(screen.getByRole('button', { name: /delete all matching/i }))
      const dialog = screen.getByRole('alertdialog')
      await user.click(within(dialog).getByRole('button', { name: /^delete$/i }))
    }

    it('surfaces a bulk delete failure to the operator, dismisses the dialog, and leaves filters untouched', async () => {
      const user = userEvent.setup()
      primeQueries()
      mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({
        mutate: mock((_vars: unknown, options: BulkDeleteOptions) => {
          options?.onError?.(axiosError(422, BULK_DELETE_FAILURE_DETAIL))
        }),
        isPending: false,
      })

      render(<DeadLetterPage />, { wrapper: createWrapper() })
      await openAndConfirmBulkDelete(user)

      expect(mockToastError).toHaveBeenCalledTimes(1)
      expect(mockToastError).toHaveBeenCalledWith(expect.stringContaining(BULK_DELETE_FAILURE_DETAIL))
      expect(mockToastSuccess).not.toHaveBeenCalled()
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
      // Nothing was actually deleted, so the filter the operator set is still applied.
      expect(screen.getByLabelText(/filter by user id/i)).toHaveValue('user_abc')
    })

    // Sibling positive assertion: without it, the failure test above would also pass if bulk
    // delete were broken outright (never deleting, never resetting filters, never closing).
    it('still reports success, resets filters, and closes the dialog when bulk delete succeeds', async () => {
      const user = userEvent.setup()
      primeQueries()
      const bulkDeleteMutate = mock((_vars: unknown, options: BulkDeleteOptions) => {
        options?.onSuccess?.({ deleted: 3 })
      })
      mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: bulkDeleteMutate, isPending: false })

      render(<DeadLetterPage />, { wrapper: createWrapper() })
      await openAndConfirmBulkDelete(user)

      expect(bulkDeleteMutate).toHaveBeenCalledTimes(1)
      expect(mockToastSuccess).toHaveBeenCalledTimes(1)
      expect(mockToastError).not.toHaveBeenCalled()
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
      expect(screen.getByLabelText(/filter by user id/i)).toHaveValue('')
    })
  })

  // Z1/F2: the survivor of the bug commit 5d189f02 fixed for the modal — the fix went to the
  // modal only. Neither delete on this page invalidated anything, and the suite above documents
  // the gap rather than catching it: it asserts invalidation for retry and NOTHING for either
  // delete.
  //
  // ⚠ Bulk delete refetched the LIST only incidentally: its `onSuccess` resets the filters, which
  // changes the list query key. The stats query takes no params (DeadLetterPage.tsx:63), so its
  // key never changes and the summary cards stayed wrong indefinitely. With Auto-refresh: OFF —
  // a first-class toggle — nothing else ever corrects either one, the deleted row stays on
  // screen, and a second delete on that phantom produces a spurious error toast.
  describe('deleting refreshes the list and the summary cards', () => {
    type MutationOptions = {
      onSuccess?: (data: unknown) => void
      onError?: (err: unknown) => void
    } | undefined

    function renderWithSpy() {
      const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
      const invalidateQueries = mock(() => Promise.resolve())
      queryClient.invalidateQueries = invalidateQueries as unknown as typeof queryClient.invalidateQueries

      function Wrapper({ children }: { children: React.ReactNode }) {
        return (
          <QueryClientProvider client={queryClient}>
            <BrowserRouter>{children}</BrowserRouter>
          </QueryClientProvider>
        )
      }

      render(<DeadLetterPage />, { wrapper: Wrapper })
      return { invalidateQueries }
    }

    function primeQueries() {
      mockUseListAdminDeadLetterJobs.mockReturnValue({ data: mockListResponse, isLoading: false, error: null })
      mockUseGetAdminDeadLetterJobStats.mockReturnValue({ data: undefined, isLoading: false, error: null })
      mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    }

    beforeEach(() => {
      mockToastError.mockClear()
      mockToastSuccess.mockClear()
    })

    async function openAndConfirmDelete(user: ReturnType<typeof userEvent.setup>) {
      await user.click(screen.getByRole('button', { name: /delete job 1/i }))
      const dialog = screen.getByRole('alertdialog')
      await user.click(within(dialog).getByRole('button', { name: /^delete$/i }))
    }

    async function openAndConfirmBulkDelete(user: ReturnType<typeof userEvent.setup>) {
      fireEvent.change(screen.getByLabelText(/filter by user id/i), { target: { value: 'user_abc' } })
      await user.click(screen.getByRole('button', { name: /delete all matching/i }))
      const dialog = screen.getByRole('alertdialog')
      await user.click(within(dialog).getByRole('button', { name: /^delete$/i }))
    }

    it('per-row delete invalidates the list and the stats', async () => {
      const user = userEvent.setup()
      primeQueries()
      mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: mock(() => {}), isPending: false })
      mockUseDeleteAdminDeadLetterJob.mockReturnValue({
        mutate: mock((_vars: unknown, options: MutationOptions) => {
          options?.onSuccess?.(undefined)
        }),
        isPending: false,
      })

      const { invalidateQueries } = renderWithSpy()
      await openAndConfirmDelete(user)

      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getListAdminDeadLetterJobsQueryKey() })
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getGetAdminDeadLetterJobStatsQueryKey() })
    })

    // Sibling: nothing was deleted, so nothing is stale. Without it, the test above would pass on
    // a component that invalidates unconditionally.
    it('per-row delete invalidates nothing when it fails', async () => {
      const user = userEvent.setup()
      primeQueries()
      mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: mock(() => {}), isPending: false })
      mockUseDeleteAdminDeadLetterJob.mockReturnValue({
        mutate: mock((_vars: unknown, options: MutationOptions) => {
          options?.onError?.(new Error('Network Error'))
        }),
        isPending: false,
      })

      const { invalidateQueries } = renderWithSpy()
      await openAndConfirmDelete(user)

      expect(invalidateQueries).not.toHaveBeenCalled()
    })

    it('bulk delete invalidates the list and the stats', async () => {
      const user = userEvent.setup()
      primeQueries()
      mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
      mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({
        mutate: mock((_vars: unknown, options: MutationOptions) => {
          options?.onSuccess?.({ deleted: 3 })
        }),
        isPending: false,
      })

      const { invalidateQueries } = renderWithSpy()
      await openAndConfirmBulkDelete(user)

      // ⚠ The list assertion is not redundant with the filter reset: the reset only happens to
      // change the key, and would stop refreshing the list the moment bulk delete is invoked
      // with no filters set.
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getListAdminDeadLetterJobsQueryKey() })
      // The one the filter reset can never fix — the stats key takes no params.
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getGetAdminDeadLetterJobStatsQueryKey() })
    })

    it('bulk delete invalidates nothing when it fails', async () => {
      const user = userEvent.setup()
      primeQueries()
      mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
      mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({
        mutate: mock((_vars: unknown, options: MutationOptions) => {
          options?.onError?.(new Error('Network Error'))
        }),
        isPending: false,
      })

      const { invalidateQueries } = renderWithSpy()
      await openAndConfirmBulkDelete(user)

      expect(invalidateQueries).not.toHaveBeenCalled()
    })
  })

  describe('Phase 6 – DataTable migration', () => {
    it('renders data-ph-mask on user_id column cells', () => {
      mockUseListAdminDeadLetterJobs.mockReturnValue({
        data: { ...mockListResponse, jobs: [mockJobs[0]] },
        isLoading: false,
        error: null,
      })
      mockUseGetAdminDeadLetterJobStats.mockReturnValue({ data: undefined, isLoading: false, error: null })
      mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
      mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false })
      mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: () => {}, isPending: false })

      const { container } = render(<DeadLetterPage />, { wrapper: createWrapper() })

      const maskedCells = container.querySelectorAll('td[data-ph-mask]')
      expect(maskedCells.length).toBeGreaterThanOrEqual(1)
    })

    it('shows column visibility menu with hideable columns', () => {
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

      const columnsBtn = screen.getByRole('button', { name: /columns/i })
      fireEvent.click(columnsBtn)

      expect(screen.getByRole('menuitemcheckbox', { name: /queue/i })).toBeInTheDocument()
      expect(screen.getByRole('menuitemcheckbox', { name: /notes/i })).toBeInTheDocument()
    })
  })
})

// #1631: an operator scanning the queue could not tell two failing jobs apart without opening
// each one. The SQLSTATE and constraint name are schema identifiers — no row values — so they
// are safe to surface inline, and they are what distinguishes "the same job failing five times
// on one constraint" from "five unrelated failures".
describe('DeadLetterPage — inline Postgres error summary', () => {
  const pgJob = {
    ...mockJobs[0],
    id: 31,
    errors: [
      {
        attempt: 5,
        message: 'webhook processing failed: ERROR: duplicate key value (SQLSTATE 23505)',
        timestamp: '2026-04-01T11:00:00Z',
        chain: ['webhook processing failed'],
        pg_error: {
          code: '23505',
          constraint_name: 'unique_account_security',
          detail: 'Key (email)=(leak@example.com) already exists.',
          message: 'duplicate key value violates unique constraint',
        },
      },
    ],
  }

  beforeEach(() => {
    mockUseListAdminDeadLetterJobs.mockReturnValue({
      data: { jobs: [pgJob], total: 1, page: 1, page_size: 20, total_pages: 1, has_more: false },
      isLoading: false,
      error: null,
    } as never)
  })

  it('shows the SQLSTATE and constraint name in the table', () => {
    render(<DeadLetterPage />, { wrapper: createWrapper() })
    const table = document.body.querySelector('table') as HTMLElement
    expect(within(table).getByText('23505')).toBeInTheDocument()
    expect(within(table).getByText('unique_account_security')).toBeInTheDocument()
  })

  it('never puts a row-value-bearing SQL field in the table, masked or not', () => {
    render(<DeadLetterPage />, { wrapper: createWrapper() })
    const table = document.body.querySelector('table') as HTMLElement
    expect(table.textContent ?? '').not.toContain('leak@example.com')
    expect(table.textContent ?? '').not.toContain('duplicate key value')
    for (const el of Array.from(table.querySelectorAll('*'))) {
      for (const attr of el.getAttributeNames()) {
        expect(el.getAttribute(attr) ?? '').not.toContain('leak@example.com')
      }
    }
  })

  it('renders an em-dash for a row with no Postgres context (every pre-#1631 production row)', () => {
    mockUseListAdminDeadLetterJobs.mockReturnValue({
      data: {
        jobs: [{ ...mockJobs[0], id: 32, errors: [{ attempt: 1, message: 'timeout', timestamp: '2026-04-01T11:00:00Z' }] }],
        total: 1, page: 1, page_size: 20, total_pages: 1, has_more: false,
      },
      isLoading: false,
      error: null,
    } as never)
    render(<DeadLetterPage />, { wrapper: createWrapper() })
    const table = document.body.querySelector('table') as HTMLElement
    expect(within(table).getByTestId('dlq-no-pg-error')).toBeInTheDocument()
  })
})
