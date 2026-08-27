/**
 * RuleAutomationHistoryPanel tests — Issue #1569 Task 9.4.
 *
 * `rule_execution_history` is the automation audit trail: one row each time a categorization rule
 * actually CHANGED a transaction. Deleting a rule is a hard `DELETE FROM categorization_rules`, and
 * `fk_rule_execution_history_rule_id` is ON DELETE SET NULL — so the row SURVIVES, carrying
 * `rule_name` and `definition_snapshot` frozen at execution time. `GET /api/v1/rule-executions`
 * exists to return exactly those rows, and `useGetUserRuleExecutions` had ZERO consumers in
 * `apps/web/src`: retained, and unreachable.
 *
 * The RED these tests encode: a user can see what a rule they have since deleted did to their
 * transactions, and why.
 */

import { describe, it, expect, beforeEach, afterAll } from 'bun:test'
import { render, screen, fireEvent, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { RuleExecution } from '@boostedbudget/api-client/models'
import {
  mockUseGetUserRuleExecutions,
  mockRefetchUserRuleExecutions,
} from '@/test/mocks/auto-categorization-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { RuleAutomationHistoryPanel } from './RuleAutomationHistoryPanel'

/** Rule still exists: rule_id is populated. */
const liveExecution: RuleExecution = {
  id: 'exec-live',
  rule_id: 'rule-live',
  transaction_id: 'txn-1',
  user_id: 'user-1',
  rule_name: 'Coffee to Dining',
  action_type: 'category',
  result: 'applied',
  executed_at: '2026-03-01T10:00:00Z',
  definition_snapshot: {
    name: 'Coffee to Dining',
    condition_logic: 'AND',
    conditions: [{ type: 'merchant_contains', value: 'starbucks' }],
  },
}

/**
 * Rule DELETED. `categorization_rules` has no `deleted_at` — the delete is hard — so the FK's
 * ON DELETE SET NULL is the only signal, and it is server-authoritative.
 */
const deletedRuleExecution: RuleExecution = {
  id: 'exec-deleted',
  rule_id: null,
  transaction_id: 'txn-2',
  user_id: 'user-1',
  rule_name: 'Old rainy-day rule',
  action_type: 'both',
  result: 'applied',
  executed_at: '2026-02-01T10:00:00Z',
  definition_snapshot: {
    name: 'Old rainy-day rule',
    condition_logic: 'OR',
    conditions: [{ type: 'amount_greater_than', value: 4812.09 }],
    captured_by: 'rule_application_service',
  },
}

/** An old row from before migration 000164 backfilled the snapshot columns. */
const unsnapshottedExecution: RuleExecution = {
  id: 'exec-nosnap',
  rule_id: null,
  transaction_id: null,
  user_id: 'user-1',
  action_type: 'tags',
  result: 'skipped_manual',
  executed_at: '2026-01-01T10:00:00Z',
}

function renderPanel() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <RuleAutomationHistoryPanel />
    </QueryClientProvider>
  )
}

function setExecutions(executions: RuleExecution[], overrides: Record<string, unknown> = {}) {
  mockUseGetUserRuleExecutions.mockReturnValue({
    data: { executions, total: executions.length, limit: 20, offset: 0 },
    isLoading: false,
    error: null,
    refetch: mockRefetchUserRuleExecutions,
    ...overrides,
  } as ReturnType<typeof mockUseGetUserRuleExecutions>)
}

function expand() {
  fireEvent.click(screen.getByRole('button', { name: /rule run history/i }))
}

function resetMock() {
  mockUseGetUserRuleExecutions.mockReturnValue({
    data: { executions: [], total: 0, limit: 20, offset: 0 },
    isLoading: false,
    error: null,
    refetch: mockRefetchUserRuleExecutions,
  } as ReturnType<typeof mockUseGetUserRuleExecutions>)
}

describe('RuleAutomationHistoryPanel', () => {
  // `mock.module` is process-global: leaving this hook returning THIS file's fixtures would change
  // what every later-loaded test file sees from the preloaded default.
  afterAll(resetMock)

  beforeEach(() => {
    mockTrackEvent.mockClear()
    mockRefetchUserRuleExecutions.mockClear()
    setExecutions([liveExecution, deletedRuleExecution, unsnapshottedExecution])
  })

  it('reaches the account-wide rule-execution endpoint', () => {
    renderPanel()
    expect(mockUseGetUserRuleExecutions).toHaveBeenCalled()
  })

  it('shows what a rule the user DELETED did (the whole point)', () => {
    renderPanel()
    expand()
    expect(screen.getByText('Old rainy-day rule')).toBeInTheDocument()
  })

  it('marks deleted-rule rows in TEXT, never by colour alone', () => {
    renderPanel()
    expand()
    // Two of the three fixtures carry rule_id === null.
    expect(screen.getAllByText(/^Deleted rule$/).length).toBe(2)
  })

  it('does not mark a row whose rule still exists', () => {
    setExecutions([liveExecution])
    renderPanel()
    expand()
    expect(screen.queryByText(/^Deleted rule$/)).not.toBeInTheDocument()
  })

  it('still names a row whose rule_name was never captured', () => {
    setExecutions([unsnapshottedExecution])
    renderPanel()
    expand()
    expect(screen.getByText(/unnamed rule/i)).toBeInTheDocument()
  })

  it('masks the retained rule name for session replay', () => {
    renderPanel()
    expand()
    expect(screen.getByText('Old rainy-day rule').closest('[data-ph-mask]')).not.toBeNull()
  })

  it('masks the condition VALUE — a merchant substring or an amount the user typed', () => {
    renderPanel()
    expand()
    expect(screen.getByText('starbucks').closest('[data-ph-mask]')).not.toBeNull()
    expect(screen.getByText('4812.09').closest('[data-ph-mask]')).not.toBeNull()
  })

  it('leaves the condition LABEL outside the mask so replay keeps the shape', () => {
    renderPanel()
    expand()
    expect(screen.getByText('Merchant contains').closest('[data-ph-mask]')).toBeNull()
  })

  it('leaves the "Deleted rule" marker OUTSIDE the mask so replay keeps the signal', () => {
    renderPanel()
    expand()
    expect(screen.getAllByText(/^Deleted rule$/)[0].closest('[data-ph-mask]')).toBeNull()
  })

  it('never dumps the raw definition_snapshot JSON into the DOM', () => {
    renderPanel()
    expand()
    const body = document.body.textContent ?? ''
    expect(body).not.toMatch(/captured_by/)
    expect(body).not.toMatch(/"conditions"/)
  })

  it('says so, rather than rendering a blank cell, when no snapshot was captured', () => {
    setExecutions([unsnapshottedExecution])
    renderPanel()
    expand()
    expect(screen.getByText(/not recorded/i)).toBeInTheDocument()
  })

  it('states the outcome and the change in words, not colour', () => {
    renderPanel()
    expand()
    expect(screen.getAllByText(/^Applied$/).length).toBe(2)
    expect(screen.getByText(/^Skipped \(manual\)$/)).toBeInTheDocument()
    expect(screen.getByText(/^Category and tags$/)).toBeInTheDocument()
  })

  it('is collapsed by default — retained history should not compete with live rules', () => {
    renderPanel()
    expect(screen.queryByText('Old rainy-day rule')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /rule run history/i })).toHaveAttribute(
      'aria-expanded',
      'false'
    )
  })

  it('wires aria-controls to the region it discloses', () => {
    renderPanel()
    const toggle = screen.getByRole('button', { name: /rule run history/i })
    const regionId = toggle.getAttribute('aria-controls')
    expect(regionId).toBeTruthy()
    expand()
    expect(document.getElementById(regionId as string)).not.toBeNull()
  })

  it('fires the view event on expand, with counts only and no identity', () => {
    renderPanel()
    expand()
    expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.RULE_EXECUTION_HISTORY_VIEWED, {
      has_deleted_rule_rows: true,
      deleted_row_count: 2,
    })
    const [, props] = mockTrackEvent.mock.calls[0] as unknown as [string, Record<string, unknown>]
    expect(JSON.stringify(props)).not.toMatch(/Old rainy-day rule|starbucks|4812|rule-live|exec-/)
  })

  it('does not fire the view event again when collapsing', () => {
    renderPanel()
    expand()
    expand()
    expect(mockTrackEvent).toHaveBeenCalledTimes(1)
  })

  it('names a loading state', () => {
    setExecutions([], { isLoading: true, data: undefined })
    renderPanel()
    expand()
    expect(screen.getByRole('status')).toBeInTheDocument()
  })

  it('names an error state that does not claim the history is gone', () => {
    setExecutions([], { error: new Error('boom'), data: undefined })
    renderPanel()
    expand()
    const alert = screen.getByRole('alert')
    expect(alert.textContent ?? '').toMatch(/still kept/i)
    expect(alert.textContent ?? '').not.toMatch(/lost|gone|deleted/i)
  })

  it('offers a retry from the error state', () => {
    setExecutions([], { error: new Error('boom'), data: undefined })
    renderPanel()
    expand()
    fireEvent.click(screen.getByRole('button', { name: /retry/i }))
    expect(mockRefetchUserRuleExecutions).toHaveBeenCalled()
  })

  it('renders no toggle at all when nothing has ever run', () => {
    setExecutions([])
    renderPanel()
    expect(screen.queryByRole('button', { name: /rule run history/i })).not.toBeInTheDocument()
  })

  it('names an empty state when a later page of a non-empty history has no rows', () => {
    mockUseGetUserRuleExecutions.mockReturnValue({
      data: { executions: [], total: 40, limit: 20, offset: 20 },
      isLoading: false,
      error: null,
      refetch: mockRefetchUserRuleExecutions,
    } as ReturnType<typeof mockUseGetUserRuleExecutions>)
    renderPanel()
    expand()
    expect(screen.getByText(/no runs on this page/i)).toBeInTheDocument()
  })

  it('renders each execution once', () => {
    renderPanel()
    expand()
    const region = document.getElementById(
      screen
        .getByRole('button', { name: /rule run history/i })
        .getAttribute('aria-controls') as string
    )
    expect(within(region as HTMLElement).getAllByText('Old rainy-day rule').length).toBe(1)
  })
})
