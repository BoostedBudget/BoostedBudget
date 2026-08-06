/**
 * PlaidOrphanedItemsPage — admin surface for `plaid_orphaned_items` (Issue #1497).
 *
 * The screen exists to keep TWO operator stories apart: `auto_retrying` (do nothing, work is in
 * flight) and `manual_intervention_required` (go into Plaid's dashboard by hand, or the Item may
 * keep billing). Most of the assertions below exist to pin that distinction, the fact that the
 * state is taken from the server rather than recomputed in the browser, and the exact wording of
 * the retention deadline.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, within, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router'
import { mockUseListAdminPlaidOrphanedItems } from '@/test/mocks/admin-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { PlaidOrphanedItemsPage } from './PlaidOrphanedItemsPage'

const manualRow = {
  id: 'orphan-manual',
  plaid_item_id: 'item-manual-AAA',
  admin_state: 'manual_intervention_required',
  institution_id: 'ins_100',
  institution_name: 'First Manual Bank',
  linked_item_status_at_erasure: 'active',
  erasure_error_code: 'ITEM_NOT_FOUND',
  last_retry_error_code: 'ORPHAN_TOKEN_UNDECRYPTABLE',
  retry_attempt_count: 7,
  orphaned_at: '2026-06-01T10:00:00Z',
  token_expires_at: '2026-07-01T10:00:00Z',
  token_purged_at: '2026-07-01T10:30:00Z',
  token_retained: false,
}

const autoRow = {
  id: 'orphan-auto',
  plaid_item_id: 'item-auto-BBB',
  admin_state: 'auto_retrying',
  institution_id: 'ins_200',
  institution_name: 'Second Auto Bank',
  linked_item_status_at_erasure: 'replaced',
  erasure_error_code: '',
  last_retry_error_code: 'ITEM_LOGIN_REQUIRED',
  retry_attempt_count: 2,
  orphaned_at: '2026-07-20T10:00:00Z',
  last_retry_at: '2026-07-30T10:00:00Z',
  token_expires_at: '2026-08-19T10:00:00Z',
  token_retained: true,
}

// A row the SERVER decayed into manual intervention even though its token is live and its last
// retry is recent — a browser that recomputed the state from the timestamps would call this one
// "auto-retrying" and hide real work. The screen must render what the server said.
const decayedRow = {
  id: 'orphan-decayed',
  plaid_item_id: 'item-decayed-CCC',
  admin_state: 'manual_intervention_required',
  institution_id: 'ins_300',
  institution_name: 'Third Decayed Bank',
  linked_item_status_at_erasure: 'active',
  erasure_error_code: 'INTERNAL_SERVER_ERROR',
  last_retry_error_code: '',
  retry_attempt_count: 1,
  orphaned_at: '2026-07-31T10:00:00Z',
  last_retry_at: '2026-07-31T10:00:00Z',
  token_expires_at: '2026-08-30T10:00:00Z',
  token_retained: true,
}

const resolvedRow = {
  id: 'orphan-resolved',
  plaid_item_id: 'item-resolved-DDD',
  admin_state: 'resolved',
  institution_id: 'ins_400',
  institution_name: 'Fourth Resolved Bank',
  linked_item_status_at_erasure: 'active',
  erasure_error_code: 'RATE_LIMIT',
  retry_attempt_count: 3,
  orphaned_at: '2026-05-01T10:00:00Z',
  token_expires_at: '2026-05-31T10:00:00Z',
  token_purged_at: '2026-05-31T11:00:00Z',
  token_retained: false,
  resolved_at: '2026-05-10T10:00:00Z',
}

const listResponse = {
  items: [manualRow, autoRow, decayedRow, resolvedRow],
  total: 4,
  manual_intervention_required: 2,
  auto_retrying: 1,
  resolved: 1,
  limit: 50,
  offset: 0,
  as_of: '2026-08-04T12:00:00Z',
}

function loaded(body: Record<string, unknown> = listResponse) {
  mockUseListAdminPlaidOrphanedItems.mockReturnValue({
    data: body,
    isLoading: false,
    error: null,
    refetch: () => Promise.resolve(),
  } as never)
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <PlaidOrphanedItemsPage />
      </MemoryRouter>
    </QueryClientProvider>
  )
}

function row(id: string): HTMLElement {
  const el = document.querySelector(`[data-row-id="${id}"]`)
  if (!el) throw new Error(`row ${id} not rendered`)
  return el as HTMLElement
}

describe('PlaidOrphanedItemsPage', () => {
  beforeEach(() => {
    mockUseListAdminPlaidOrphanedItems.mockClear()
    mockTrackEvent.mockClear()
    mockUseListAdminPlaidOrphanedItems.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: () => Promise.resolve(),
    } as never)
  })

  describe('request states', () => {
    it('renders a loading status while the listing is in flight', () => {
      renderPage()
      expect(screen.getByRole('status')).toHaveTextContent(/loading orphaned plaid items/i)
    })

    it('surfaces a failed listing instead of rendering an empty table', () => {
      mockUseListAdminPlaidOrphanedItems.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: new Error('boom'),
        refetch: () => Promise.resolve(),
      } as never)
      renderPage()
      expect(screen.getByRole('alert')).toHaveTextContent(/failed to load orphaned plaid items/i)
      expect(document.querySelector('table')).toBeNull()
    })

    it('renders an empty state when nothing is orphaned', () => {
      loaded({ ...listResponse, items: [], total: 0, manual_intervention_required: 0, auto_retrying: 0, resolved: 1 })
      renderPage()
      expect(screen.getByText(/no orphaned plaid items/i)).toBeInTheDocument()
    })

    it('tolerates a null items array from the API', () => {
      loaded({ ...listResponse, items: null, total: 0 })
      renderPage()
      expect(screen.getByText(/no orphaned plaid items/i)).toBeInTheDocument()
    })
  })

  describe('the two operator states', () => {
    it('labels each row with the action the server said it needs', () => {
      loaded()
      renderPage()
      expect(within(row('orphan-manual')).getByText('Manual intervention required')).toBeInTheDocument()
      expect(within(row('orphan-auto')).getByText('Auto-retrying')).toBeInTheDocument()
      expect(within(row('orphan-resolved')).getByText('Resolved')).toBeInTheDocument()
    })

    it('renders the server-decayed state rather than recomputing it from the timestamps', () => {
      loaded()
      renderPage()
      // Live token, retried minutes ago by its own timestamps — still manual, because the server said so.
      expect(within(row('orphan-decayed')).getByText('Manual intervention required')).toBeInTheDocument()
      expect(within(row('orphan-decayed')).queryByText('Auto-retrying')).toBeNull()
    })

    it('distinguishes manual intervention by more than colour', () => {
      loaded()
      renderPage()
      const badge = within(row('orphan-manual')).getByText('Manual intervention required')
      // An icon plus distinct wording — a badge that differed only by hue would fail WCAG 1.4.1.
      expect(badge.querySelector('svg')).not.toBeNull()
      expect(within(row('orphan-auto')).getByText('Auto-retrying').querySelector('svg')).not.toBeNull()
    })

    it('spells out the manual remedy so an operator knows where to go', () => {
      loaded()
      renderPage()
      expect(screen.getByText(/remove the item in plaid/i)).toBeInTheDocument()
    })

    it('does not claim an unrecognised state is auto-retrying', () => {
      loaded({ ...listResponse, items: [{ ...autoRow, admin_state: 'something_new' }] })
      renderPage()
      const cells = within(row('orphan-auto'))
      expect(cells.getByText('something_new')).toBeInTheDocument()
      expect(cells.queryByText('Auto-retrying')).toBeNull()
      expect(cells.queryByText('Manual intervention required')).toBeNull()
    })
  })

  describe('row contents', () => {
    it('shows the Plaid Item id, institution, error codes and attempt count', () => {
      loaded()
      renderPage()
      const manual = within(row('orphan-manual'))
      expect(manual.getByText('item-manual-AAA')).toBeInTheDocument()
      expect(manual.getByText('First Manual Bank')).toBeInTheDocument()
      expect(manual.getByText('ITEM_NOT_FOUND')).toBeInTheDocument()
      expect(manual.getByText('ORPHAN_TOKEN_UNDECRYPTABLE')).toBeInTheDocument()
      expect(manual.getByText('7')).toBeInTheDocument()
    })

    it('masks the Plaid Item id and institution for session replay', () => {
      loaded()
      renderPage()
      const cell = within(row('orphan-manual')).getByText('item-manual-AAA').closest('td')
      expect(cell).not.toBeNull()
      expect(cell).toHaveAttribute('data-ph-mask')
      const institutionCell = within(row('orphan-manual')).getByText('First Manual Bank').closest('td')
      expect(institutionCell).toHaveAttribute('data-ph-mask')
    })

    it('renders when the row was orphaned', () => {
      loaded()
      renderPage()
      expect(
        within(row('orphan-manual')).getByText(new Date(manualRow.orphaned_at).toLocaleString())
      ).toBeInTheDocument()
    })
  })

  describe('the retention deadline', () => {
    it('shows the deadline as the instant the credential stops being usable', () => {
      loaded()
      renderPage()
      expect(screen.getByRole('columnheader', { name: /unusable from/i })).toBeInTheDocument()
      expect(
        within(row('orphan-auto')).getByText(new Date(autoRow.token_expires_at).toLocaleString())
      ).toBeInTheDocument()
    })

    it('never restates the deadline as a deletion time or a countdown', () => {
      loaded()
      renderPage()
      const text = document.body.textContent ?? ''
      expect(text).not.toMatch(/deleted at/i)
      expect(text).not.toMatch(/expires in/i)
      expect(text).not.toMatch(/purged at\b/i)
    })

    it('reports whether a credential is still stored, and separately that one was destroyed', () => {
      loaded()
      renderPage()
      expect(within(row('orphan-auto')).getByText('Retained')).toBeInTheDocument()
      expect(within(row('orphan-manual')).getByText(/^Purged/)).toBeInTheDocument()
    })

    it('distinguishes a record that never held a credential from one that was purged', () => {
      loaded({ ...listResponse, items: [{ ...manualRow, token_purged_at: undefined, token_retained: false }] })
      renderPage()
      expect(within(row('orphan-manual')).getByText('None held')).toBeInTheDocument()
    })
  })

  describe('summary counts', () => {
    it('shows the server-computed counts and says they describe this page', () => {
      loaded()
      renderPage()
      const manualCard = screen.getByTestId('orphan-count-manual')
      expect(manualCard).toHaveTextContent('2')
      expect(screen.getByTestId('orphan-count-auto')).toHaveTextContent('1')
      expect(screen.getByTestId('orphan-count-resolved')).toHaveTextContent('1')
      expect(screen.getByText(/on this page/i)).toBeInTheDocument()
    })

    it('reports the instant the states were computed for', () => {
      loaded()
      renderPage()
      expect(screen.getByText(new RegExp(`as of ${new Date(listResponse.as_of).toLocaleString()}`, 'i'))).toBeInTheDocument()
    })
  })

  describe('paging and read-only posture', () => {
    it('requests the first page by default', () => {
      loaded()
      renderPage()
      expect(mockUseListAdminPlaidOrphanedItems.mock.calls[0]?.[0]).toEqual({ limit: 50, offset: 0 })
    })

    it('advances the offset when the operator pages forward', () => {
      loaded({ ...listResponse, total: 120 })
      renderPage()
      fireEvent.click(screen.getByRole('button', { name: /next page/i }))
      const lastCall = mockUseListAdminPlaidOrphanedItems.mock.calls.at(-1)?.[0]
      expect(lastCall).toEqual({ limit: 50, offset: 50 })
    })

    it('offers no per-row action — the sweep is automated and an expired credential cannot be retried', () => {
      loaded()
      renderPage()
      expect(screen.queryByRole('button', { name: /retry/i })).toBeNull()
      expect(screen.queryByRole('button', { name: /re-?attempt/i })).toBeNull()
      expect(screen.queryByRole('button', { name: /delete/i })).toBeNull()
    })

    it('tracks the page view', () => {
      loaded()
      renderPage()
      expect(mockTrackEvent).toHaveBeenCalledWith('admin_page_viewed', { page: 'plaid-orphaned-items' })
    })
  })
})
