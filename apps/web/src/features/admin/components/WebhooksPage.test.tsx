/**
 * WebhooksPage Component Tests
 * Issue #747: Paddle Webhook Admin UI
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router'
import {
  mockUseListAdminPaddleWebhooks,
  mockUseGetAdminPaddleWebhookStats,
  mockUseReplayAdminPaddleWebhook,
  mockUseGetAdminPaddleWebhookDetail,
} from '@/test/mocks/admin-api'
import { mockToastError, mockToastSuccess } from '@/test/mocks/sonner'
import {
  getListAdminPaddleWebhooksQueryKey,
  getGetAdminPaddleWebhookStatsQueryKey,
} from '@boostedbudget/api-client/endpoints/admin/admin'
import { WebhooksPage } from './WebhooksPage'

// Note: WebhookDetailModal is not explicitly mocked here to avoid worker-level contamination.
// The preloaded admin-api mocks handle useGetAdminPaddleWebhookDetail and
// useReplayAdminPaddleWebhook, so the modal renders safely with no real API calls.

const mockEvents = [
  {
    id: 'wh-1',
    event_id: 'evt_01abc',
    event_type: 'subscription.created',
    payload: { data: {} },
    processed: true,
    processed_at: '2026-03-22T10:00:00Z',
    processing_error: null,
    processing_attempts: 1,
    created_at: '2026-03-22T09:00:00Z',
    updated_at: '2026-03-22T10:00:00Z',
  },
  {
    id: 'wh-2',
    event_id: 'evt_02def',
    event_type: 'transaction.payment_failed',
    payload: { data: {} },
    processed: false,
    processed_at: null,
    processing_error: 'Connection timeout',
    processing_attempts: 3,
    created_at: '2026-03-22T08:00:00Z',
    updated_at: '2026-03-22T08:30:00Z',
  },
]

const mockListResponse = {
  Events: mockEvents,
  Total: 2,
  Page: 1,
  PageSize: 20,
  TotalPages: 1,
  HasMore: false,
}

const mockStatsResponse = {
  Total: 134,
  Processed: 130,
  Failed: 4,
  Unprocessed: 0,
  AvgProcessingTimeMs: 42,
  EventTypeBreakdown: null,
  TimeRangeHours: 24,
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

describe('WebhooksPage', () => {
  beforeEach(() => {
    mockUseListAdminPaddleWebhooks.mockClear()
    mockUseGetAdminPaddleWebhookStats.mockClear()
    mockUseReplayAdminPaddleWebhook.mockClear()
    // WebhookDetailModal uses this — set default return so modal renders as closed
    mockUseGetAdminPaddleWebhookDetail.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
    })
  })

  it('renders loading state', () => {
    mockUseListAdminPaddleWebhooks.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    })
    mockUseGetAdminPaddleWebhookStats.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: () => {},
      isPending: false,
    })

    render(<WebhooksPage />, { wrapper: createWrapper() })

    expect(screen.getByRole('status')).toBeInTheDocument()
    expect(screen.getByText(/loading webhooks/i)).toBeInTheDocument()
  })

  it('renders error state', () => {
    mockUseListAdminPaddleWebhooks.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('Network error'),
    })
    mockUseGetAdminPaddleWebhookStats.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: () => {},
      isPending: false,
    })

    render(<WebhooksPage />, { wrapper: createWrapper() })

    expect(screen.getByText(/failed to load webhooks/i)).toBeInTheDocument()
  })

  it('renders empty state with no data', () => {
    mockUseListAdminPaddleWebhooks.mockReturnValue({
      data: { Events: [], Total: 0, Page: 1, PageSize: 20, TotalPages: 0, HasMore: false },
      isLoading: false,
      error: null,
    })
    mockUseGetAdminPaddleWebhookStats.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: () => {},
      isPending: false,
    })

    render(<WebhooksPage />, { wrapper: createWrapper() })

    expect(screen.getByText(/no webhook events found/i)).toBeInTheDocument()
  })

  it('renders list of webhook events', () => {
    mockUseListAdminPaddleWebhooks.mockReturnValue({
      data: mockListResponse,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminPaddleWebhookStats.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: () => {},
      isPending: false,
    })

    render(<WebhooksPage />, { wrapper: createWrapper() })

    // Event types appear in both dropdown options and table rows — use getAllByText
    expect(screen.getAllByText('subscription.created').length).toBeGreaterThan(0)
    expect(screen.getAllByText('transaction.payment_failed').length).toBeGreaterThan(0)
    expect(screen.getByText('evt_01abc')).toBeInTheDocument()
    expect(screen.getByText('evt_02def')).toBeInTheDocument()
    // Status badges — "Processed" also appears in filter dropdown, use getAllByText
    expect(screen.getAllByText('Processed').length).toBeGreaterThan(0)
    expect(screen.getByText('Failed')).toBeInTheDocument()
  })

  it('displays stats cards when stats data is available', () => {
    mockUseListAdminPaddleWebhooks.mockReturnValue({
      data: mockListResponse,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminPaddleWebhookStats.mockReturnValue({
      data: mockStatsResponse,
      isLoading: false,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: () => {},
      isPending: false,
    })

    render(<WebhooksPage />, { wrapper: createWrapper() })

    expect(screen.getByText('Total (24h)')).toBeInTheDocument()
    expect(screen.getByText('134')).toBeInTheDocument()
    expect(screen.getByText('130')).toBeInTheDocument()
    expect(screen.getByText('4')).toBeInTheDocument()
    expect(screen.getByText('42ms')).toBeInTheDocument()
  })

  it('event type filter renders options', () => {
    mockUseListAdminPaddleWebhooks.mockReturnValue({
      data: mockListResponse,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminPaddleWebhookStats.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: () => {},
      isPending: false,
    })

    render(<WebhooksPage />, { wrapper: createWrapper() })

    const eventTypeSelect = screen.getByLabelText(/filter by event type/i)
    expect(eventTypeSelect).toBeInTheDocument()

    fireEvent.change(eventTypeSelect, { target: { value: 'subscription.created' } })

    expect(mockUseListAdminPaddleWebhooks).toHaveBeenCalled()
  })

  it('processed filter changes selection', () => {
    mockUseListAdminPaddleWebhooks.mockReturnValue({
      data: mockListResponse,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminPaddleWebhookStats.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: () => {},
      isPending: false,
    })

    render(<WebhooksPage />, { wrapper: createWrapper() })

    const processedSelect = screen.getByLabelText(/filter by processed status/i)
    expect(processedSelect).toBeInTheDocument()

    fireEvent.change(processedSelect, { target: { value: 'false' } })
    expect(mockUseListAdminPaddleWebhooks).toHaveBeenCalled()
  })

  it('shows pagination controls when multiple pages', () => {
    mockUseListAdminPaddleWebhooks.mockReturnValue({
      data: {
        Events: mockEvents,
        Total: 50,
        Page: 1,
        PageSize: 20,
        TotalPages: 3,
        HasMore: true,
      },
      isLoading: false,
      error: null,
    })
    mockUseGetAdminPaddleWebhookStats.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: () => {},
      isPending: false,
    })

    render(<WebhooksPage />, { wrapper: createWrapper() })

    expect(screen.getByText(/page 1 of 3/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /next page/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /previous page/i })).toBeDisabled()
  })

  it('shows total event count in header', () => {
    mockUseListAdminPaddleWebhooks.mockReturnValue({
      data: mockListResponse,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminPaddleWebhookStats.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: () => {},
      isPending: false,
    })

    render(<WebhooksPage />, { wrapper: createWrapper() })

    expect(screen.getByText(/2 events/i)).toBeInTheDocument()
  })

  it('renders date filter inputs', () => {
    mockUseListAdminPaddleWebhooks.mockReturnValue({
      data: mockListResponse,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminPaddleWebhookStats.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: () => {},
      isPending: false,
    })

    render(<WebhooksPage />, { wrapper: createWrapper() })

    expect(screen.getByLabelText(/filter by start date/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/filter by end date/i)).toBeInTheDocument()
  })

  it('shows reset button when filters active', () => {
    mockUseListAdminPaddleWebhooks.mockReturnValue({
      data: mockListResponse,
      isLoading: false,
      error: null,
    })
    mockUseGetAdminPaddleWebhookStats.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: () => {},
      isPending: false,
    })

    render(<WebhooksPage />, { wrapper: createWrapper() })

    fireEvent.change(screen.getByLabelText(/filter by start date/i), { target: { value: '2026-01-01' } })

    expect(screen.getByRole('button', { name: /reset filters/i })).toBeInTheDocument()
  })

  describe('Phase 6 – DataTable migration', () => {
    it('shows column visibility menu with hideable columns', () => {
      mockUseListAdminPaddleWebhooks.mockReturnValue({
        data: mockListResponse,
        isLoading: false,
        error: null,
      })
      mockUseGetAdminPaddleWebhookStats.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: null,
      })
      mockUseReplayAdminPaddleWebhook.mockReturnValue({
        mutate: () => {},
        isPending: false,
      })

      render(<WebhooksPage />, { wrapper: createWrapper() })

      const columnsBtn = screen.getByRole('button', { name: /columns/i })
      fireEvent.click(columnsBtn)

      expect(screen.getByRole('menuitemcheckbox', { name: /event id/i })).toBeInTheDocument()
      expect(screen.getByRole('menuitemcheckbox', { name: /attempts/i })).toBeInTheDocument()
      expect(screen.getByRole('menuitemcheckbox', { name: /error/i })).toBeInTheDocument()
    })
  })

  // Round 15, Tier E item 2 — Issue #1496's defect, still live on the Paddle replay. The mutation
  // was invoked as `.mutate({ id })` with NO options object at all: no onSuccess, no onError, no
  // invalidation. So a rejected replay looked exactly like a successful one, and a SUCCESSFUL replay
  // left the row's processed/attempts/error and the summary counts stale until the 30s poll — or
  // indefinitely, with auto-refresh off, which is when an operator clicks Replay a second time.
  //
  // Copied from DeadLetterPage.test.tsx's #1496 suite rather than reinvented, because the fix is a
  // copy of that fix.
  describe('Issue #1496 – Paddle replay outcome is reported to the operator', () => {
    type ReplayOptions = {
      onSuccess?: (data: unknown) => void
      onError?: (err: unknown) => void
    } | undefined

    const axiosError = (status: number, detail: string) => ({
      response: { status, data: { detail } },
      message: `Request failed with status code ${status}`,
      isAxiosError: true,
    })

    const REPLAY_FAILURE_DETAIL = 'webhook event has already been processed; replay refused'

    function primeQueries() {
      mockUseListAdminPaddleWebhooks.mockReturnValue({ data: mockListResponse, isLoading: false, error: null })
      mockUseGetAdminPaddleWebhookStats.mockReturnValue({ data: mockStatsResponse, isLoading: false, error: null })
    }

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

      render(<WebhooksPage />, { wrapper: Wrapper })
      return { invalidateQueries }
    }

    beforeEach(() => {
      mockToastError.mockClear()
      mockToastSuccess.mockClear()
    })

    it('surfaces a rejected replay to the operator', async () => {
      const user = userEvent.setup()
      primeQueries()
      mockUseReplayAdminPaddleWebhook.mockReturnValue({
        mutate: mock((_vars: unknown, options: ReplayOptions) => {
          options?.onError?.(axiosError(409, REPLAY_FAILURE_DETAIL))
        }),
        isPending: false,
      })

      renderPage()
      await user.click(screen.getAllByRole('button', { name: /replay .* webhook/i })[0])

      expect(mockToastError).toHaveBeenCalledTimes(1)
      expect(mockToastError).toHaveBeenCalledWith(expect.stringContaining(REPLAY_FAILURE_DETAIL))
      expect(mockToastSuccess).not.toHaveBeenCalled()
    })

    it('falls back to the transport message when the body carries no detail', async () => {
      const user = userEvent.setup()
      primeQueries()
      mockUseReplayAdminPaddleWebhook.mockReturnValue({
        mutate: mock((_vars: unknown, options: ReplayOptions) => {
          options?.onError?.(new Error('Network Error'))
        }),
        isPending: false,
      })

      renderPage()
      await user.click(screen.getAllByRole('button', { name: /replay .* webhook/i })[0])

      expect(mockToastError).toHaveBeenCalledWith(expect.stringContaining('Network Error'))
    })

    // ⚠ SIBLING, MANDATORY. Without it the two tests above would also pass if replay were broken
    // outright — never issuing the request, never reporting success. This one pins the id actually
    // sent, the success toast, and — the half the old code omitted entirely — the invalidation.
    it('reports success and refreshes the list and the stats when the replay succeeds', async () => {
      const user = userEvent.setup()
      primeQueries()
      const replayMutate = mock((_vars: unknown, options: ReplayOptions) => {
        options?.onSuccess?.({})
      })
      mockUseReplayAdminPaddleWebhook.mockReturnValue({ mutate: replayMutate, isPending: false })

      const { invalidateQueries } = renderPage()
      await user.click(screen.getAllByRole('button', { name: /replay .* webhook/i })[0])

      expect(replayMutate).toHaveBeenCalledWith({ id: 'wh-1' }, expect.any(Object))
      expect(mockToastSuccess).toHaveBeenCalledTimes(1)
      expect(mockToastError).not.toHaveBeenCalled()
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getListAdminPaddleWebhooksQueryKey() })
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getGetAdminPaddleWebhookStatsQueryKey() })
    })

    // The toast is operator-facing text and must not carry the Paddle event id, for the same reason
    // the button's aria-label does not: it is a per-customer identifier, and PostHog session replay
    // records both verbatim. The event TYPE is the non-PII operational fact.
    it('names the event type, never the Paddle event id, in either toast', async () => {
      const user = userEvent.setup()
      primeQueries()
      mockUseReplayAdminPaddleWebhook.mockReturnValue({
        mutate: mock((_vars: unknown, options: ReplayOptions) => {
          options?.onSuccess?.({})
        }),
        isPending: false,
      })

      renderPage()
      await user.click(screen.getAllByRole('button', { name: /replay .* webhook/i })[0])

      const message = mockToastSuccess.mock.calls[0]?.[0] as string
      expect(message).toContain('subscription.created')
      expect(message).not.toContain('evt_01abc')
      expect(message).not.toContain('wh-1')
    })
  })
})
