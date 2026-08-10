/**
 * WebhookDetailModal Component Tests
 * Issue #747: Paddle Webhook Admin UI
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  mockUseGetAdminPaddleWebhookDetail,
  mockUseReplayAdminPaddleWebhook,
} from '@/test/mocks/admin-api'
import { mockToastError, mockToastSuccess } from '@/test/mocks/sonner'
import {
  getListAdminPaddleWebhooksQueryKey,
  getGetAdminPaddleWebhookStatsQueryKey,
  getGetAdminPaddleWebhookDetailQueryKey,
} from '@boostedbudget/api-client/endpoints/admin/admin'
import { WebhookDetailModal } from './WebhookDetailModal'

const mockEvent = {
  id: 'wh-1',
  event_id: 'evt_01abc',
  event_type: 'subscription.created',
  payload: { notification_id: 'ntf_01abc', event_type: 'subscription.created', data: {} },
  processed: true,
  processed_at: '2026-03-22T10:00:00Z',
  processing_error: null,
  processing_attempts: 1,
  created_at: '2026-03-22T09:00:00Z',
  updated_at: '2026-03-22T10:00:00Z',
}

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('WebhookDetailModal', () => {
  beforeEach(() => {
    mockUseGetAdminPaddleWebhookDetail.mockClear()
    mockUseReplayAdminPaddleWebhook.mockClear()
  })

  it('does not render dialog content when webhookId is null', () => {
    mockUseGetAdminPaddleWebhookDetail.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: mock(() => {}),
      isPending: false,
    })

    render(<WebhookDetailModal webhookId={null} onClose={() => {}} />, {
      wrapper: createWrapper(),
    })

    // Dialog is closed, no content rendered
    expect(screen.queryByText('subscription.created')).not.toBeInTheDocument()
    expect(screen.queryByText(/loading webhook/i)).not.toBeInTheDocument()
  })

  it('shows loading spinner when webhookId is set and loading', () => {
    mockUseGetAdminPaddleWebhookDetail.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: mock(() => {}),
      isPending: false,
    })

    render(<WebhookDetailModal webhookId="wh-1" onClose={() => {}} />, {
      wrapper: createWrapper(),
    })

    expect(screen.getByText(/loading webhook details/i)).toBeInTheDocument()
  })

  it('shows error state when loading fails', () => {
    mockUseGetAdminPaddleWebhookDetail.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('Not found'),
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: mock(() => {}),
      isPending: false,
    })

    render(<WebhookDetailModal webhookId="wh-missing" onClose={() => {}} />, {
      wrapper: createWrapper(),
    })

    expect(screen.getByText(/failed to load webhook/i)).toBeInTheDocument()
  })

  it('renders webhook event details', () => {
    mockUseGetAdminPaddleWebhookDetail.mockReturnValue({
      data: mockEvent,
      isLoading: false,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: mock(() => {}),
      isPending: false,
    })

    render(<WebhookDetailModal webhookId="wh-1" onClose={() => {}} />, {
      wrapper: createWrapper(),
    })

    expect(screen.getByText('subscription.created')).toBeInTheDocument()
    expect(screen.getByText('evt_01abc')).toBeInTheDocument()
    expect(screen.getByText('Processed')).toBeInTheDocument()
    expect(screen.getByText('1')).toBeInTheDocument()
  })

  it('renders processing error when present', () => {
    mockUseGetAdminPaddleWebhookDetail.mockReturnValue({
      data: {
        ...mockEvent,
        processed: false,
        processed_at: null,
        processing_error: 'Downstream service unavailable',
        processing_attempts: 3,
      },
      isLoading: false,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: mock(() => {}),
      isPending: false,
    })

    render(<WebhookDetailModal webhookId="wh-1" onClose={() => {}} />, {
      wrapper: createWrapper(),
    })

    expect(screen.getByText('Downstream service unavailable')).toBeInTheDocument()
  })

  it('replay button calls mutation with event id', () => {
    const mutateMock = mock(() => {})
    mockUseGetAdminPaddleWebhookDetail.mockReturnValue({
      data: mockEvent,
      isLoading: false,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: mutateMock,
      isPending: false,
    })

    render(<WebhookDetailModal webhookId="wh-1" onClose={() => {}} />, {
      wrapper: createWrapper(),
    })

    const replayBtn = screen.getByRole('button', { name: /replay/i })
    fireEvent.click(replayBtn)

    // ⚠ ROUND 15, TIER E ITEM 2 — THIS ASSERTION USED TO PIN THE DEFECT. It read
    // `toHaveBeenCalledWith({ id: 'wh-1' })`, i.e. it asserted that the mutation is invoked with NO
    // options object — the exact shape #1496 exists to forbid, stated here as the expected one. A
    // test that pins the bug is worse than no test: it makes the fix look like the regression.
    // The outcome-reporting contract is pinned in the #1496 suite at the bottom of this file.
    expect(mutateMock).toHaveBeenCalledWith({ id: 'wh-1' }, expect.any(Object))
  })

  it('replay button shows "Replaying..." when pending', () => {
    mockUseGetAdminPaddleWebhookDetail.mockReturnValue({
      data: mockEvent,
      isLoading: false,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: mock(() => {}),
      isPending: true,
    })

    render(<WebhookDetailModal webhookId="wh-1" onClose={() => {}} />, {
      wrapper: createWrapper(),
    })

    expect(screen.getByRole('button', { name: /replaying/i })).toBeInTheDocument()
  })

  it('close button calls onClose', () => {
    const onCloseMock = mock(() => {})
    mockUseGetAdminPaddleWebhookDetail.mockReturnValue({
      data: mockEvent,
      isLoading: false,
      error: null,
    })
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: mock(() => {}),
      isPending: false,
    })

    render(<WebhookDetailModal webhookId="wh-1" onClose={onCloseMock} />, {
      wrapper: createWrapper(),
    })

    // There are multiple "Close" buttons (dialog X + our footer Close button)
    // Use getAllByRole and click the last one (footer Close)
    const closeBtns = screen.getAllByRole('button', { name: /close/i })
    fireEvent.click(closeBtns[closeBtns.length - 1])

    expect(onCloseMock).toHaveBeenCalled()
  })

  // Round 15, Tier E item 2 — the sibling of WebhooksPage's replay, stated separately because a fix
  // applied only to the page would leave this one live, which is exactly how #1496 left BOTH of them
  // live. Same defect: `.mutate({ id })` with no options object at all.
  //
  // ⚠ THIS ONE HAS A THIRD KEY TO INVALIDATE. The operator is still standing in front of the DETAIL
  // query, whose processed/attempts/processing_error fields are precisely what a replay changes — so
  // without it the modal keeps showing the pre-replay state for as long as it is open.
  describe('Issue #1496 – modal replay outcome is reported to the operator', () => {
    type ReplayOptions = {
      onSuccess?: (data: unknown) => void
      onError?: (err: unknown) => void
    } | undefined

    const REPLAY_FAILURE_DETAIL = 'webhook event has already been processed; replay refused'

    function renderModal() {
      const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
      const invalidateQueries = mock(() => Promise.resolve())
      queryClient.invalidateQueries = invalidateQueries as unknown as typeof queryClient.invalidateQueries

      function Wrapper({ children }: { children: React.ReactNode }) {
        return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      }

      render(<WebhookDetailModal webhookId="wh-1" onClose={() => {}} />, { wrapper: Wrapper })
      return { invalidateQueries }
    }

    beforeEach(() => {
      mockToastError.mockClear()
      mockToastSuccess.mockClear()
      mockUseGetAdminPaddleWebhookDetail.mockReturnValue({ data: mockEvent, isLoading: false, error: null })
    })

    it('surfaces a rejected replay to the operator', () => {
      mockUseReplayAdminPaddleWebhook.mockReturnValue({
        mutate: mock((_vars: unknown, options: ReplayOptions) => {
          options?.onError?.({
            response: { status: 409, data: { detail: REPLAY_FAILURE_DETAIL } },
            message: 'Request failed with status code 409',
            isAxiosError: true,
          })
        }),
        isPending: false,
      })

      renderModal()
      fireEvent.click(screen.getByRole('button', { name: /^replay$/i }))

      expect(mockToastError).toHaveBeenCalledTimes(1)
      expect(mockToastError).toHaveBeenCalledWith(expect.stringContaining(REPLAY_FAILURE_DETAIL))
      expect(mockToastSuccess).not.toHaveBeenCalled()
    })

    // ⚠ SIBLING, MANDATORY — and the detail-key assertion is the one specific to this component.
    it('reports success and invalidates the detail, the list and the stats', () => {
      const replayMutate = mock((_vars: unknown, options: ReplayOptions) => {
        options?.onSuccess?.({})
      })
      mockUseReplayAdminPaddleWebhook.mockReturnValue({ mutate: replayMutate, isPending: false })

      const { invalidateQueries } = renderModal()
      fireEvent.click(screen.getByRole('button', { name: /^replay$/i }))

      expect(replayMutate).toHaveBeenCalledWith({ id: 'wh-1' }, expect.any(Object))
      expect(mockToastSuccess).toHaveBeenCalledTimes(1)
      expect(mockToastError).not.toHaveBeenCalled()
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getGetAdminPaddleWebhookDetailQueryKey('wh-1') })
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getListAdminPaddleWebhooksQueryKey() })
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: getGetAdminPaddleWebhookStatsQueryKey() })
    })

    it('names the event type, never the Paddle event id, in the toast', () => {
      mockUseReplayAdminPaddleWebhook.mockReturnValue({
        mutate: mock((_vars: unknown, options: ReplayOptions) => { options?.onSuccess?.({}) }),
        isPending: false,
      })

      renderModal()
      fireEvent.click(screen.getByRole('button', { name: /^replay$/i }))

      const message = mockToastSuccess.mock.calls[0]?.[0] as string
      expect(message).toContain('subscription.created')
      expect(message).not.toContain('evt_01abc')
    })
  })
})
