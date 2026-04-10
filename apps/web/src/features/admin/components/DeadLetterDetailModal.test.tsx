/**
 * DeadLetterDetailModal Component Tests
 * Issue #633: Dead Letter Queue Admin UI
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  mockUseGetAdminDeadLetterJobDetail,
  mockUseGetAdminDeadLetterJobArgs,
  mockUseRetryAdminDeadLetterJob,
  mockUseAddAdminDeadLetterJobNotes,
  mockUseDeleteAdminDeadLetterJob,
} from '@/test/mocks/admin-api'
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

    // Click Delete button to show confirmation
    fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))
    expect(screen.getByRole('button', { name: /confirm delete/i })).toBeInTheDocument()

    // Confirm
    fireEvent.click(screen.getByRole('button', { name: /confirm delete/i }))
    expect(deleteMutateMock).toHaveBeenCalledWith({ id: 1 }, expect.any(Object))
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
})
