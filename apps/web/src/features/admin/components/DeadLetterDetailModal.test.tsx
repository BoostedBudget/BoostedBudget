import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { readdirSync, readFileSync, statSync } from 'fs'
import { join } from 'path'
import { render, screen, fireEvent, within, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  mockUseGetAdminDeadLetterJobDetail,
  mockUseGetAdminDeadLetterJobArgs,
  mockUseRetryAdminDeadLetterJob,
  mockUseAddAdminDeadLetterJobNotes,
  mockUseDeleteAdminDeadLetterJob,
} from '@/test/mocks/admin-api'
import { mockToastError, mockToastSuccess } from '@/test/mocks/sonner'
import {
  getListAdminDeadLetterJobsQueryKey,
  getGetAdminDeadLetterJobStatsQueryKey,
  getGetAdminDeadLetterJobDetailQueryKey,
} from '@boostedbudget/api-client/endpoints/admin/admin'
import { DeadLetterDetailModal } from './DeadLetterDetailModal'

const mockJob = {
  id: 1,
  original_job_id: 100,
  kind: 'sync_transactions',
  queue: 'default',
  args: { user_id: '[REDACTED]' },
  errors: [{ error: 'connection refused', attempt: 3 }],
  max_attempts: 3,
  created_at: '2026-04-01T10:00:00Z',
  failed_at: '2026-04-01T11:00:00Z',
  user_id: 'user_abc',
  notified: false,
  retried_at: undefined,
  retry_job_id: undefined,
  notes: undefined,
}

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('DeadLetterDetailModal', () => {
  beforeEach(() => {
    mockUseGetAdminDeadLetterJobDetail.mockClear()
    mockUseGetAdminDeadLetterJobArgs.mockClear()
    mockUseRetryAdminDeadLetterJob.mockClear()
    mockUseAddAdminDeadLetterJobNotes.mockClear()
    mockUseDeleteAdminDeadLetterJob.mockClear()
  })

  it('does not render content when jobId is null', () => {
    mockUseGetAdminDeadLetterJobDetail.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminDeadLetterJobArgs.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseAddAdminDeadLetterJobNotes.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })

    render(<DeadLetterDetailModal jobId={null} onClose={() => {}} />, { wrapper: createWrapper() })

    expect(screen.queryByText('sync_transactions')).not.toBeInTheDocument()
    expect(screen.queryByText(/loading job details/i)).not.toBeInTheDocument()
  })

  it('shows loading spinner when jobId is set and loading', () => {
    mockUseGetAdminDeadLetterJobDetail.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    })
    mockUseGetAdminDeadLetterJobArgs.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseAddAdminDeadLetterJobNotes.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })

    render(<DeadLetterDetailModal jobId={1} onClose={() => {}} />, { wrapper: createWrapper() })

    expect(screen.getByText(/loading job details/i)).toBeInTheDocument()
  })

  it('shows error state when loading fails', () => {
    mockUseGetAdminDeadLetterJobDetail.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('Not found'),
    })
    mockUseGetAdminDeadLetterJobArgs.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseAddAdminDeadLetterJobNotes.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })

    render(<DeadLetterDetailModal jobId={999} onClose={() => {}} />, { wrapper: createWrapper() })

    expect(screen.getByText(/failed to load job details/i)).toBeInTheDocument()
  })

  it('renders job metadata', () => {
    mockUseGetAdminDeadLetterJobDetail.mockReturnValue({ data: mockJob, isLoading: false, error: null })
    mockUseGetAdminDeadLetterJobArgs.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseAddAdminDeadLetterJobNotes.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })

    render(<DeadLetterDetailModal jobId={1} onClose={() => {}} />, { wrapper: createWrapper() })

    expect(screen.getByText('sync_transactions')).toBeInTheDocument()
    expect(screen.getByText('default')).toBeInTheDocument()
    expect(screen.getByText('user_abc')).toBeInTheDocument()
    expect(screen.getByText('3')).toBeInTheDocument()
    expect(screen.getByText('Pending Retry')).toBeInTheDocument()
  })

  it('retry button is enabled when not yet retried', () => {
    mockUseGetAdminDeadLetterJobDetail.mockReturnValue({ data: mockJob, isLoading: false, error: null })
    mockUseGetAdminDeadLetterJobArgs.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseAddAdminDeadLetterJobNotes.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })

    render(<DeadLetterDetailModal jobId={1} onClose={() => {}} />, { wrapper: createWrapper() })

    const retryBtn = screen.getByRole('button', { name: /^retry$/i })
    expect(retryBtn).not.toBeDisabled()
  })

  it('retry button is disabled when already retried', () => {
    mockUseGetAdminDeadLetterJobDetail.mockReturnValue({
      data: { ...mockJob, retried_at: '2026-04-02T12:00:00Z', retry_job_id: 200 },
      isLoading: false,
      error: null,
    })
    mockUseGetAdminDeadLetterJobArgs.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseAddAdminDeadLetterJobNotes.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })

    render(<DeadLetterDetailModal jobId={1} onClose={() => {}} />, { wrapper: createWrapper() })

    expect(screen.getByRole('button', { name: /already retried/i })).toBeDisabled()
    expect(screen.getByText('Retried')).toBeInTheDocument()
  })

  it('retry button calls mutation', () => {
    const mutateMock = mock(() => {})
    mockUseGetAdminDeadLetterJobDetail.mockReturnValue({ data: mockJob, isLoading: false, error: null })
    mockUseGetAdminDeadLetterJobArgs.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mutateMock, isPending: false })
    mockUseAddAdminDeadLetterJobNotes.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })

    render(<DeadLetterDetailModal jobId={1} onClose={() => {}} />, { wrapper: createWrapper() })

    fireEvent.click(screen.getByRole('button', { name: /^retry$/i }))
    expect(mutateMock).toHaveBeenCalledWith({ id: 1 }, expect.any(Object))
  })

  it('delete confirmation flow works', () => {
    const deleteMutateMock = mock(() => {})
    mockUseGetAdminDeadLetterJobDetail.mockReturnValue({ data: mockJob, isLoading: false, error: null })
    mockUseGetAdminDeadLetterJobArgs.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseAddAdminDeadLetterJobNotes.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: deleteMutateMock, isPending: false })

    render(<DeadLetterDetailModal jobId={1} onClose={() => {}} />, { wrapper: createWrapper() })

    // Click Delete button in the modal footer to open ConfirmationDialog
    fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))
    expect(screen.getByRole('alertdialog')).toBeInTheDocument()

    // Confirm deletion in the alertdialog
    const dialog = screen.getByRole('alertdialog')
    fireEvent.click(within(dialog).getByRole('button', { name: /^delete$/i }))
    expect(deleteMutateMock).toHaveBeenCalledWith({ id: 1 }, expect.any(Object))
  })

  it('closes confirmation dialog after successful delete', () => {
    let capturedOnSuccess: (() => void) | undefined
    const deleteMutateMock = mock((_args: unknown, opts: { onSuccess?: () => void }) => {
      capturedOnSuccess = opts?.onSuccess
    })
    mockUseGetAdminDeadLetterJobDetail.mockReturnValue({ data: mockJob, isLoading: false, error: null })
    mockUseGetAdminDeadLetterJobArgs.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseAddAdminDeadLetterJobNotes.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: deleteMutateMock, isPending: false })

    render(<DeadLetterDetailModal jobId={1} onClose={() => {}} />, { wrapper: createWrapper() })

    fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))
    fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: /^delete$/i }))

    act(() => { capturedOnSuccess?.() })

    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })

  it('notes editing flow shows textarea', () => {
    mockUseGetAdminDeadLetterJobDetail.mockReturnValue({ data: mockJob, isLoading: false, error: null })
    mockUseGetAdminDeadLetterJobArgs.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseAddAdminDeadLetterJobNotes.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })

    render(<DeadLetterDetailModal jobId={1} onClose={() => {}} />, { wrapper: createWrapper() })

    fireEvent.click(screen.getByRole('button', { name: /add/i }))
    expect(screen.getByPlaceholderText(/add investigation notes/i)).toBeInTheDocument()
  })

  it('reveal args button triggers fetch', () => {
    const refetchMock = mock(() => Promise.resolve())
    mockUseGetAdminDeadLetterJobDetail.mockReturnValue({ data: mockJob, isLoading: false, error: null })
    mockUseGetAdminDeadLetterJobArgs.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: refetchMock })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseAddAdminDeadLetterJobNotes.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })

    render(<DeadLetterDetailModal jobId={1} onClose={() => {}} />, { wrapper: createWrapper() })

    fireEvent.click(screen.getByRole('button', { name: /reveal/i }))
    expect(refetchMock).toHaveBeenCalled()
  })

  it('close button calls onClose', () => {
    const onCloseMock = mock(() => {})
    mockUseGetAdminDeadLetterJobDetail.mockReturnValue({ data: mockJob, isLoading: false, error: null })
    mockUseGetAdminDeadLetterJobArgs.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseAddAdminDeadLetterJobNotes.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })

    render(<DeadLetterDetailModal jobId={1} onClose={onCloseMock} />, { wrapper: createWrapper() })

    const closeBtns = screen.getAllByRole('button', { name: /close/i })
    fireEvent.click(closeBtns[closeBtns.length - 1])
    expect(onCloseMock).toHaveBeenCalled()
  })

  // Issue #1496 (UI half): the modal's Retry passed `onSuccess` only, so a rejected replay
  // left the modal looking identical to a successful one. The reachable rejections are the
  // 409 (deduplicated — nothing enqueued) and the 422 (unknown job kind); the already-retried
  // case cannot produce the symptom because its button is disabled and relabelled.
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

    function renderModal() {
      const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
      const invalidateQueries = mock(() => Promise.resolve())
      queryClient.invalidateQueries = invalidateQueries as unknown as typeof queryClient.invalidateQueries

      function Wrapper({ children }: { children: React.ReactNode }) {
        return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      }

      render(<DeadLetterDetailModal jobId={1} onClose={() => {}} />, { wrapper: Wrapper })
      return { invalidateQueries }
    }

    function primeQueries() {
      mockUseGetAdminDeadLetterJobDetail.mockReturnValue({ data: mockJob, isLoading: false, error: null })
      mockUseGetAdminDeadLetterJobArgs.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
      mockUseAddAdminDeadLetterJobNotes.mockReturnValue({ mutate: mock(() => {}), isPending: false })
      mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    }

    beforeEach(() => {
      mockToastError.mockClear()
      mockToastSuccess.mockClear()
    })

    it('surfaces the 409 deduplicated failure to the operator', () => {
      primeQueries()
      mockUseRetryAdminDeadLetterJob.mockReturnValue({
        mutate: mock((_vars: unknown, options: RetryOptions) => {
          options?.onError?.(axiosError(409, DEDUP_DETAIL))
        }),
        isPending: false,
      })

      renderModal()
      fireEvent.click(screen.getByRole('button', { name: /^retry$/i }))

      expect(mockToastError).toHaveBeenCalledTimes(1)
      expect(mockToastError).toHaveBeenCalledWith(expect.stringContaining(DEDUP_DETAIL))
      expect(mockToastSuccess).not.toHaveBeenCalled()
    })

    it('surfaces the 422 unknown-kind failure to the operator', () => {
      primeQueries()
      mockUseRetryAdminDeadLetterJob.mockReturnValue({
        mutate: mock((_vars: unknown, options: RetryOptions) => {
          options?.onError?.(axiosError(422, UNKNOWN_KIND_DETAIL))
        }),
        isPending: false,
      })

      renderModal()
      fireEvent.click(screen.getByRole('button', { name: /^retry$/i }))

      expect(mockToastError).toHaveBeenCalledTimes(1)
      expect(mockToastError).toHaveBeenCalledWith(expect.stringContaining(UNKNOWN_KIND_DETAIL))
      expect(mockToastSuccess).not.toHaveBeenCalled()
    })

    it('falls back to the transport message when the body carries no detail', () => {
      primeQueries()
      mockUseRetryAdminDeadLetterJob.mockReturnValue({
        mutate: mock((_vars: unknown, options: RetryOptions) => {
          options?.onError?.(new Error('Network Error'))
        }),
        isPending: false,
      })

      renderModal()
      fireEvent.click(screen.getByRole('button', { name: /^retry$/i }))

      expect(mockToastError).toHaveBeenCalledWith(expect.stringContaining('Network Error'))
    })

    // Sibling positive assertion: without it, the error tests above would also pass if retry
    // were broken outright (never enqueuing, never reporting success).
    it('still reports success and refreshes the job when the retry succeeds', () => {
      primeQueries()
      const retryMutate = mock((_vars: unknown, options: RetryOptions) => {
        options?.onSuccess?.({ dead_letter_job_id: 1, retry_job_id: 999 })
      })
      mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: retryMutate, isPending: false })

      const { invalidateQueries } = renderModal()
      fireEvent.click(screen.getByRole('button', { name: /^retry$/i }))

      expect(retryMutate).toHaveBeenCalledWith({ id: 1 }, expect.any(Object))
      expect(mockToastSuccess).toHaveBeenCalledTimes(1)
      expect(mockToastError).not.toHaveBeenCalled()
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getGetAdminDeadLetterJobDetailQueryKey(1) })
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getListAdminDeadLetterJobsQueryKey() })
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getGetAdminDeadLetterJobStatsQueryKey() })
    })
  })

  // Issue #1496 (follow-up): the retry fix above was scoped too narrowly — the identical
  // defect (mutation reports success only) was still live on this modal's save-notes and
  // delete actions too.
  describe('Issue #1496 – save-notes outcome is reported to the operator', () => {
    type NotesOptions = {
      onSuccess?: (data: unknown) => void
      onError?: (err: unknown) => void
    } | undefined

    const axiosError = (status: number, detail: string) => ({
      response: { status, data: { detail } },
      message: `Request failed with status code ${status}`,
      isAxiosError: true,
    })

    const NOTES_FAILURE_DETAIL = 'notes exceed the maximum length'

    function primeQueries() {
      mockUseGetAdminDeadLetterJobDetail.mockReturnValue({ data: mockJob, isLoading: false, error: null })
      mockUseGetAdminDeadLetterJobArgs.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
      mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
      mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    }

    beforeEach(() => {
      mockToastError.mockClear()
      mockToastSuccess.mockClear()
    })

    function openEditorAndSave() {
      fireEvent.click(screen.getByRole('button', { name: /add/i }))
      fireEvent.change(screen.getByPlaceholderText(/add investigation notes/i), { target: { value: 'new notes' } })
      fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    }

    it('surfaces a save-notes failure to the operator and keeps the editor open so the text is not lost', () => {
      primeQueries()
      mockUseAddAdminDeadLetterJobNotes.mockReturnValue({
        mutate: mock((_vars: unknown, options: NotesOptions) => {
          options?.onError?.(axiosError(422, NOTES_FAILURE_DETAIL))
        }),
        isPending: false,
      })

      render(<DeadLetterDetailModal jobId={1} onClose={() => {}} />, { wrapper: createWrapper() })
      openEditorAndSave()

      expect(mockToastError).toHaveBeenCalledTimes(1)
      expect(mockToastError).toHaveBeenCalledWith(expect.stringContaining(NOTES_FAILURE_DETAIL))
      expect(mockToastSuccess).not.toHaveBeenCalled()
      // The editor (and the operator's unsaved text) must still be visible.
      expect(screen.getByPlaceholderText(/add investigation notes/i)).toHaveValue('new notes')
    })

    // Sibling positive assertion: without it, the failure test above would also pass if
    // save-notes were broken outright (never saving, never reporting success, never closing).
    it('still reports success and closes the editor when save-notes succeeds', () => {
      primeQueries()
      const notesMutate = mock((_vars: unknown, options: NotesOptions) => {
        options?.onSuccess?.(undefined)
      })
      mockUseAddAdminDeadLetterJobNotes.mockReturnValue({ mutate: notesMutate, isPending: false })

      render(<DeadLetterDetailModal jobId={1} onClose={() => {}} />, { wrapper: createWrapper() })
      openEditorAndSave()

      expect(notesMutate).toHaveBeenCalledWith(
        { id: 1, data: { notes: 'new notes' } },
        expect.any(Object)
      )
      expect(mockToastSuccess).toHaveBeenCalledTimes(1)
      expect(mockToastError).not.toHaveBeenCalled()
      expect(screen.queryByPlaceholderText(/add investigation notes/i)).not.toBeInTheDocument()
    })
  })

  // Z1/F1: `handleRetry` and `handleDelete` both invalidated; save-notes was the one left out.
  // The write landed and the panel kept rendering the OLD `job.notes` — the detail query has no
  // `refetchInterval`, `staleTime` is 5 minutes and `refetchOnWindowFocus` is false (main.tsx),
  // so nothing else would ever refresh it. The operator sees the previous note, or "No notes
  // yet", and is invited to retype what they just saved.
  describe('save-notes refreshes what it changed', () => {
    type NotesOptions = {
      onSuccess?: (data: unknown) => void
      onError?: (err: unknown) => void
    } | undefined

    function primeQueries() {
      mockUseGetAdminDeadLetterJobDetail.mockReturnValue({ data: mockJob, isLoading: false, error: null })
      mockUseGetAdminDeadLetterJobArgs.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
      mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
      mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    }

    function renderWithSpy() {
      const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
      const invalidateQueries = mock(() => Promise.resolve())
      queryClient.invalidateQueries = invalidateQueries as unknown as typeof queryClient.invalidateQueries

      function Wrapper({ children }: { children: React.ReactNode }) {
        return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      }

      render(<DeadLetterDetailModal jobId={1} onClose={() => {}} />, { wrapper: Wrapper })
      return { invalidateQueries }
    }

    function openEditorAndSave() {
      fireEvent.click(screen.getByRole('button', { name: /add/i }))
      fireEvent.change(screen.getByPlaceholderText(/add investigation notes/i), { target: { value: 'new notes' } })
      fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    }

    beforeEach(() => {
      mockToastError.mockClear()
      mockToastSuccess.mockClear()
    })

    it('invalidates this job and the list behind it when the save succeeds', () => {
      primeQueries()
      mockUseAddAdminDeadLetterJobNotes.mockReturnValue({
        mutate: mock((_vars: unknown, options: NotesOptions) => {
          options?.onSuccess?.(undefined)
        }),
        isPending: false,
      })

      const { invalidateQueries } = renderWithSpy()
      openEditorAndSave()

      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getGetAdminDeadLetterJobDetailQueryKey(1) })
      // The list carries a `notes` column (DeadLetterPage.tsx), so the row behind the modal is
      // stale too.
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getListAdminDeadLetterJobsQueryKey() })
      // Notes change nothing the summary cards count (total / retried / last 24h / by kind), so
      // the stats query is deliberately left alone — invalidating it would be a refetch nobody
      // asked for.
      expect(invalidateQueries).not.toHaveBeenCalledWith({ queryKey: getGetAdminDeadLetterJobStatsQueryKey() })
    })

    // Sibling: a rejected save changed nothing on the server, so refetching would replace the
    // operator's unsaved text with the old note. Without this the test above would pass on a
    // component that invalidates unconditionally.
    it('invalidates nothing when the save fails', () => {
      primeQueries()
      mockUseAddAdminDeadLetterJobNotes.mockReturnValue({
        mutate: mock((_vars: unknown, options: NotesOptions) => {
          options?.onError?.(new Error('Network Error'))
        }),
        isPending: false,
      })

      const { invalidateQueries } = renderWithSpy()
      openEditorAndSave()

      expect(invalidateQueries).not.toHaveBeenCalled()
    })
  })

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
      mockUseGetAdminDeadLetterJobDetail.mockReturnValue({ data: mockJob, isLoading: false, error: null })
      mockUseGetAdminDeadLetterJobArgs.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
      mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
      mockUseAddAdminDeadLetterJobNotes.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    }

    beforeEach(() => {
      mockToastError.mockClear()
      mockToastSuccess.mockClear()
    })

    function openAndConfirmDelete() {
      fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))
      const dialog = screen.getByRole('alertdialog')
      fireEvent.click(within(dialog).getByRole('button', { name: /^delete$/i }))
    }

    it('surfaces a delete failure to the operator, dismisses the confirm dialog, and keeps the detail modal open', () => {
      primeQueries()
      mockUseDeleteAdminDeadLetterJob.mockReturnValue({
        mutate: mock((_vars: unknown, options: DeleteOptions) => {
          options?.onError?.(axiosError(409, DELETE_FAILURE_DETAIL))
        }),
        isPending: false,
      })

      render(<DeadLetterDetailModal jobId={1} onClose={() => {}} />, { wrapper: createWrapper() })
      openAndConfirmDelete()

      expect(mockToastError).toHaveBeenCalledTimes(1)
      expect(mockToastError).toHaveBeenCalledWith(expect.stringContaining(DELETE_FAILURE_DETAIL))
      expect(mockToastSuccess).not.toHaveBeenCalled()
      // The confirm sub-dialog is dismissed (same fix as the page's per-row delete)...
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
      // ...but the job still exists, so the detail modal itself stays open.
      expect(screen.getByText('sync_transactions')).toBeInTheDocument()
    })

    // Sibling positive assertion: without it, the failure test above would also pass if delete
    // were broken outright (never deleting, never reporting success, never closing anything).
    it('still reports success and closes the whole modal when delete succeeds', () => {
      primeQueries()
      const onCloseMock = mock(() => {})
      const deleteMutate = mock((_vars: unknown, options: DeleteOptions) => {
        options?.onSuccess?.(undefined)
      })
      mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: deleteMutate, isPending: false })

      render(<DeadLetterDetailModal jobId={1} onClose={onCloseMock} />, { wrapper: createWrapper() })
      openAndConfirmDelete()

      expect(deleteMutate).toHaveBeenCalledWith({ id: 1 }, expect.any(Object))
      expect(mockToastSuccess).toHaveBeenCalledTimes(1)
      expect(mockToastError).not.toHaveBeenCalled()
      expect(onCloseMock).toHaveBeenCalledTimes(1)
    })
  })

  // These two tests are the RED for the fix to the bug where DeadLetterPage never passed
  // `onDeleted`, so a successful delete from this modal invalidated nothing and the deleted
  // job stayed listed until the next 30s poll. Retry (above) already invalidates on success;
  // delete must mirror it.
  describe('delete refreshes the dead-letter list (mirrors retry invalidation)', () => {
    type DeleteOptions = {
      onSuccess?: (data: unknown) => void
      onError?: (err: unknown) => void
    } | undefined

    const axiosError = (status: number, detail: string) => ({
      response: { status, data: { detail } },
      message: `Request failed with status code ${status}`,
      isAxiosError: true,
    })

    function primeQueries() {
      mockUseGetAdminDeadLetterJobDetail.mockReturnValue({ data: mockJob, isLoading: false, error: null })
      mockUseGetAdminDeadLetterJobArgs.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
      mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: mock(() => {}), isPending: false })
      mockUseAddAdminDeadLetterJobNotes.mockReturnValue({ mutate: mock(() => {}), isPending: false })
    }

    function renderModal() {
      const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
      const invalidateQueries = mock(() => Promise.resolve())
      queryClient.invalidateQueries = invalidateQueries as unknown as typeof queryClient.invalidateQueries

      function Wrapper({ children }: { children: React.ReactNode }) {
        return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      }

      render(<DeadLetterDetailModal jobId={1} onClose={() => {}} />, { wrapper: Wrapper })
      return { invalidateQueries }
    }

    function openAndConfirmDelete() {
      fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))
      const dialog = screen.getByRole('alertdialog')
      fireEvent.click(within(dialog).getByRole('button', { name: /^delete$/i }))
    }

    beforeEach(() => {
      mockToastError.mockClear()
      mockToastSuccess.mockClear()
    })

    it('invalidates the dead-letter list query when delete succeeds', () => {
      primeQueries()
      const deleteMutate = mock((_vars: unknown, options: DeleteOptions) => {
        options?.onSuccess?.(undefined)
      })
      mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: deleteMutate, isPending: false })

      const { invalidateQueries } = renderModal()
      openAndConfirmDelete()

      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getListAdminDeadLetterJobsQueryKey() })
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getGetAdminDeadLetterJobStatsQueryKey() })
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getGetAdminDeadLetterJobDetailQueryKey(1) })
    })

    // Sibling assertion: without this, unconditionally invalidating on every render/mutate call
    // (rather than only on success) would still make the test above pass.
    it('does not invalidate anything when delete fails, and keeps the detail modal open', () => {
      primeQueries()
      const deleteMutate = mock((_vars: unknown, options: DeleteOptions) => {
        options?.onError?.(axiosError(409, 'job is referenced by an active retry chain; delete refused'))
      })
      mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: deleteMutate, isPending: false })

      const { invalidateQueries } = renderModal()
      openAndConfirmDelete()

      expect(mockToastError).toHaveBeenCalledTimes(1)
      expect(invalidateQueries).not.toHaveBeenCalled()
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
      expect(screen.getByText('sync_transactions')).toBeInTheDocument()
    })
  })
})

// The bug this fix removes was a declared-but-never-passed `onDeleted?` prop. Deleting a prop
// without checking for other consumers is exactly the failure mode this whole exercise is about
// (a mechanism that looks wired up but silently does nothing), so this is a repo-wide check
// rather than a local one, with a control proving the pattern can actually match.
describe('onDeleted prop removal (repo-wide check)', () => {
  function grepForOnDeleted(dir: string): { file: string; line: number }[] {
    const hits: { file: string; line: number }[] = []
    for (const entry of readdirSync(dir)) {
      const full = join(dir, entry)
      const stat = statSync(full)
      if (stat.isDirectory()) {
        hits.push(...grepForOnDeleted(full))
      } else if (
        (entry.endsWith('.tsx') || entry.endsWith('.ts')) &&
        !entry.endsWith('.test.tsx') &&
        !entry.endsWith('.test.ts')
      ) {
        const contents = readFileSync(full, 'utf-8')
        contents.split('\n').forEach((line, i) => {
          if (/onDeleted/.test(line)) hits.push({ file: full, line: i + 1 })
        })
      }
    }
    return hits
  }

  it('control: the onDeleted pattern still matches a legitimate, unrelated usage (savings goal contributions)', () => {
    const dir = join(import.meta.dir, '../../savings')
    const hits = grepForOnDeleted(dir)
    expect(hits.length).toBeGreaterThan(0)
  })

  it('no file under features/admin references onDeleted (the dead prop was fully removed, not left dangling)', () => {
    const dir = join(import.meta.dir, '..')
    const hits = grepForOnDeleted(dir)
    const report = hits.map(h => `  ${h.file}:${h.line}`).join('\n')
    expect(hits, `onDeleted still referenced:\n${report}`).toHaveLength(0)
  })
})
