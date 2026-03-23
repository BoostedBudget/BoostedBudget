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

    expect(mutateMock).toHaveBeenCalledWith({ id: 'wh-1' })
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
})
