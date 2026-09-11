/**
 * HealthDashboardPage Component Tests
 * Issue #747: Updated to include Paddle Status section
 */

import { render, screen, waitFor } from '@testing-library/react'
import { describe, it, expect, beforeEach } from 'bun:test'
import { HealthDashboardPage } from './HealthDashboardPage'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { mockUseGetSystemHealth } from '@/test/mocks/admin-api'

const createTestQueryClient = () =>
  new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

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
  paddle_status: {
    active_subscriptions: 150,
    webhook_success_rate_24h: 98.5,
    failed_webhooks_24h: 2,
    total_webhooks24h: 134,
    last_reconciliation_at: '2026-03-22T10:00:00Z',
    reconciliation_discrepancies: 0,
  },
  // Issue #1458: the 3 DB-derived Plaid-cleanup/manual-tier-override backlog gauges.
  // Issue #1458 follow-up G16: `available: true` here represents a SUCCESSFUL query that
  // legitimately found zero backlog — a verified zero, not an unknown. See the "unavailable" test
  // below for the failed-query case this fixture is deliberately NOT exercising.
  job_backlog: {
    plaid_cleanup_outstanding_users: 0,
    plaid_cleanup_oldest_outstanding_seconds: 0,
    manual_tier_override_oldest_uncleared_expired_seconds: 0,
    available: true,
  },
  // Issue #1497 item 5: the orphaned-Plaid-Item state counts. `available: true` with all-zero
  // counts is a VERIFIED empty table, not an unknown — the failed-read case is exercised
  // separately below.
  plaid_orphaned_items: {
    auto_retrying: 0,
    manual_intervention_required: 0,
    resolved: 0,
    operator_attested: 0,
    max_unresolved_retry_attempt_count: 0,
    available: true,
  },
  // Issue #1504: the post-gate external-effect residue. `available: true` with a zero count is a
  // VERIFIED "this has never happened", the reading this gauge exists to be able to make — the
  // failed-read and absent-section cases are exercised separately below.
  deletion_residue: {
    records: 0,
    newest_record_seconds: 0,
    available: true,
  },
  // Issue #1514 requirement #5: rejected (unsigned/invalid-signature) webhook attempts.
  // `available: true` with zero counts is a VERIFIED quiet 24 hours, not an unknown — the
  // failed-read and absent-section cases are exercised separately below.
  rejected_webhook_attempts_24h: {
    total_24h: 0,
    by_provider: {},
    most_recent_at: null,
    available: true,
  },
  // #1631 M8b: forked-security-identity groups (a Plaid corporate action reissuing
  // plaid_security_id). `available: true` with a zero count is a VERIFIED "no forks detected",
  // not an unknown — the failed-read and absent-section cases are exercised separately below.
  forked_security_identities: {
    group_count: 0,
    available: true,
  },
}

const renderWithQueryClient = (component: React.ReactElement) => {
  const queryClient = createTestQueryClient()
  return render(<QueryClientProvider client={queryClient}>{component}</QueryClientProvider>)
}

describe('HealthDashboardPage', () => {
  beforeEach(() => {
    mockUseGetSystemHealth.mockClear()
  })

  it('should render page title', () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: mockHealthData,
      isLoading: false,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)
    expect(screen.getByText(/System Health/i)).toBeInTheDocument()
  })

  it('should display service status cards', async () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: mockHealthData,
      isLoading: false,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)

    await waitFor(() => {
      expect(screen.getByText(/Database/i)).toBeInTheDocument()
      expect(screen.getByText(/Clerk/i)).toBeInTheDocument()
      expect(screen.getByText(/sandbox/i)).toBeInTheDocument()
    })
  })

  it('should show loading state', () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)
    expect(screen.getByText(/Loading/i)).toBeInTheDocument()
  })

  it('should show error state', () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('Failed to fetch'),
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)
    expect(screen.getByText(/Error/i)).toBeInTheDocument()
  })

  it('should render queue cards', async () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: mockHealthData,
      isLoading: false,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)

    await waitFor(() => {
      expect(screen.getByText(/default/i)).toBeInTheDocument()
      expect(screen.getByText(/critical/i)).toBeInTheDocument()
    })
  })

  it('should show dead letter alert when count > 0', async () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: mockHealthData,
      isLoading: false,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)

    await waitFor(() => {
      expect(screen.getByText(/dead letter/i)).toBeInTheDocument()
    })
  })

  it('should render Paddle Status section', async () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: mockHealthData,
      isLoading: false,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)

    await waitFor(() => {
      expect(screen.getByText(/Paddle Status/i)).toBeInTheDocument()
      expect(screen.getByText('Active Subscriptions')).toBeInTheDocument()
      expect(screen.getByText('150')).toBeInTheDocument()
    })
  })

  it('should display webhook success rate', async () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: mockHealthData,
      isLoading: false,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)

    await waitFor(() => {
      expect(screen.getByText('Webhook Success (24h)')).toBeInTheDocument()
      expect(screen.getByText('98.5%')).toBeInTheDocument()
    })
  })

  it('should show warning status when webhook success rate is below 90%', async () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: {
        ...mockHealthData,
        paddle_status: {
          ...mockHealthData.paddle_status,
          webhook_success_rate_24h: 85.0,
          failed_webhooks_24h: 15,
        },
      },
      isLoading: false,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)

    await waitFor(() => {
      expect(screen.getByText('85.0%')).toBeInTheDocument()
    })
  })

  it('should show "Never" when last_reconciliation_at is null', async () => {
    mockUseGetSystemHealth.mockReturnValue({
      data: {
        ...mockHealthData,
        paddle_status: {
          ...mockHealthData.paddle_status,
          last_reconciliation_at: null,
        },
      },
      isLoading: false,
      error: null,
      refetch: () => {},
    })

    renderWithQueryClient(<HealthDashboardPage />)

    await waitFor(() => {
      expect(screen.getByText('Never')).toBeInTheDocument()
    })
  })

  // Issue #1458: the 3 DB-derived Plaid-cleanup/manual-tier-override backlog gauges, surfaced via
  // the same SummaryCard grid idiom as the Paddle Status section above (there is no Dead Letter
  // Queue SummaryCard to copy — DLQ renders as a conditional alert banner instead).
  describe('Job Backlog section (Issue #1458)', () => {
    // Reads a SummaryCard's value by its title, scoped via StatCard's own `data-slot="stat-value"`
    // — this file's other numeric fixtures (active_syncs, job queue counts, etc.) can coincide
    // with these gauges' values, so an unscoped `screen.getByText('<number>')` is ambiguous.
    function backlogCardValue(title: string): string {
      const label = screen.getByText(title)
      const card = label.closest('[data-slot="card"]')
      if (!card) throw new Error(`Could not find card for "${title}"`)
      return card.querySelector('[data-slot="stat-value"]')?.textContent ?? ''
    }

    it('should render the Job Backlog section with all three gauges', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          job_backlog: {
            plaid_cleanup_outstanding_users: 42,
            plaid_cleanup_oldest_outstanding_seconds: 7500,
            manual_tier_override_oldest_uncleared_expired_seconds: 5400,
            available: true,
          },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(/Job Backlog/i)).toBeInTheDocument()
        expect(screen.getByText('Plaid Cleanup Outstanding Users')).toBeInTheDocument()
        expect(screen.getByText('Plaid Cleanup Oldest Outstanding')).toBeInTheDocument()
        expect(screen.getByText('Manual Override Oldest Uncleared Expired')).toBeInTheDocument()
      })

      expect(backlogCardValue('Plaid Cleanup Outstanding Users')).toBe('42')
      expect(backlogCardValue('Plaid Cleanup Oldest Outstanding')).toBe('2h 5m')
      expect(backlogCardValue('Manual Override Oldest Uncleared Expired')).toBe('1h 30m')
    })

    it('should render zero-state values without crashing when there is no backlog', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: mockHealthData, // job_backlog all zeros, available: true (a verified zero)
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText('Plaid Cleanup Outstanding Users')).toBeInTheDocument()
      })

      expect(backlogCardValue('Plaid Cleanup Outstanding Users')).toBe('0')
      expect(backlogCardValue('Plaid Cleanup Oldest Outstanding')).toBe('0s')
      expect(backlogCardValue('Manual Override Oldest Uncleared Expired')).toBe('0s')
    })

    // Issue #1458 follow-up G16: before this fix, a FAILED backlog query and a genuinely EMPTY
    // backlog were both represented by the identical zero-valued fields, so this section rendered
    // a confident green "0" in both cases — indistinguishable, and wrong exactly when the operator
    // most needed to know the gauge could not currently be trusted.
    it('should render an "Unavailable" warning state, not a green zero, when the backlog query failed', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          job_backlog: {
            plaid_cleanup_outstanding_users: 0,
            plaid_cleanup_oldest_outstanding_seconds: 0,
            manual_tier_override_oldest_uncleared_expired_seconds: 0,
            available: false,
          },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText('Plaid Cleanup Outstanding Users')).toBeInTheDocument()
      })

      // Every card in the section must read "Unavailable" (not the numeric "0" a real empty
      // backlog would show, and not a green success accent).
      expect(backlogCardValue('Plaid Cleanup Outstanding Users')).toBe('Unavailable')
      expect(backlogCardValue('Plaid Cleanup Oldest Outstanding')).toBe('Unavailable')
      expect(backlogCardValue('Manual Override Oldest Uncleared Expired')).toBe('Unavailable')

      // Confirms the cards are NOT rendered with the 'success' (green) accent StatCard maps
      // status='success' to — the whole point being fixed here is that "unavailable" must never
      // read as a healthy/confident state.
      const outstandingUsersCard = screen.getByText('Plaid Cleanup Outstanding Users').closest('[data-slot="card"]')
      expect(outstandingUsersCard).not.toBeNull()
      expect(outstandingUsersCard?.className ?? '').not.toMatch(/success/i)
    })

    // Same "unknown != zero" signal, but for a client with an OLDER cached response predating
    // this field's addition — `available` is simply absent (undefined), not explicitly `false`.
    // Must degrade the same fail-closed way, not accidentally read as available.
    it('treats a missing `available` field (older cached response) the same as unavailable', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          job_backlog: {
            plaid_cleanup_outstanding_users: 0,
            plaid_cleanup_oldest_outstanding_seconds: 0,
            manual_tier_override_oldest_uncleared_expired_seconds: 0,
          },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText('Plaid Cleanup Outstanding Users')).toBeInTheDocument()
      })

      expect(backlogCardValue('Plaid Cleanup Outstanding Users')).toBe('Unavailable')
    })

    // Defect R3-7: a rolling deploy against an older API response, or a partial mock, can omit
    // `job_backlog` ENTIRELY (not just its nested `available` field, covered above). An unguarded
    // `data.job_backlog.available` throws a TypeError on that shape and blanks the WHOLE
    // dashboard — the opposite of the fail-closed "degrade to Unavailable" intent this section
    // exists for. Must degrade exactly like the missing-`available` case above, not crash.
    it('degrades to "Unavailable" (not a crash) when `job_backlog` itself is entirely absent', async () => {
      const healthDataWithoutJobBacklog: Record<string, unknown> = { ...mockHealthData }
      delete healthDataWithoutJobBacklog.job_backlog

      mockUseGetSystemHealth.mockReturnValue({
        data: healthDataWithoutJobBacklog,
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(/System Health/i)).toBeInTheDocument()
      })

      await waitFor(() => {
        expect(screen.getByText('Plaid Cleanup Outstanding Users')).toBeInTheDocument()
      })

      expect(backlogCardValue('Plaid Cleanup Outstanding Users')).toBe('Unavailable')
      expect(backlogCardValue('Plaid Cleanup Oldest Outstanding')).toBe('Unavailable')
      expect(backlogCardValue('Manual Override Oldest Uncleared Expired')).toBe('Unavailable')
    })
  })

  // ─── Issue #1497 item 5: the Orphaned Plaid Items section ──────────────────────────────────────
  //
  // WHAT THIS SECTION IS FOR. Each `manual_intervention_required` row is a Plaid Item that survived
  // a GDPR erasure, that nothing automated will ever finish removing, and that MAY STILL BE BILLING.
  // Every assertion below names a NUMBER from a seeded fixture, and the two operator states are
  // seeded to DIFFERENT numbers, so a UI that showed one "unresolved" total for both cannot pass —
  // "auto-retrying" means do nothing, "needs manual removal" means go act at Plaid.
  describe('Orphaned Plaid Items section (Issue #1497 item 5)', () => {
    function orphanCardValue(title: string): string {
      const label = screen.getByText(title)
      const card = label.closest('[data-slot="card"]')
      if (!card) throw new Error(`Could not find card for "${title}"`)
      return card.querySelector('[data-slot="stat-value"]')?.textContent ?? ''
    }

    it('renders each admin state as its own number, not one unresolved total', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          plaid_orphaned_items: {
            auto_retrying: 2,
            manual_intervention_required: 5,
            operator_attested: 0,
            resolved: 9,
            max_unresolved_retry_attempt_count: 23,
            available: true,
          },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(/Orphaned Plaid Items/i)).toBeInTheDocument()
      })

      // 2, 5 and 7 (the unresolved total) are all distinct, so a conflated implementation fails.
      expect(orphanCardValue('Needs Manual Removal At Plaid')).toBe('5')
      expect(orphanCardValue('Auto-Retrying')).toBe('2')
      expect(orphanCardValue('Worst Retry Attempt Count')).toBe('23')
      expect(orphanCardValue('Needs Manual Removal At Plaid')).not.toBe('7')
      expect(orphanCardValue('Auto-Retrying')).not.toBe('7')
    })

    // THE SIBLING: an outstanding manual-intervention count must NOT read as healthy, and a genuine
    // zero must. Without both directions, a card hardcoded to one accent would pass.
    it('accents an outstanding manual-intervention count as a warning and a verified zero as success', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          plaid_orphaned_items: {
            auto_retrying: 0,
            manual_intervention_required: 3,
            operator_attested: 0,
            resolved: 0,
            max_unresolved_retry_attempt_count: 4,
            available: true,
          },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      const { unmount } = renderWithQueryClient(<HealthDashboardPage />)
      await waitFor(() => {
        expect(screen.getByText('Needs Manual Removal At Plaid')).toBeInTheDocument()
      })
      const warnCard = screen.getByText('Needs Manual Removal At Plaid').closest('[data-slot="card"]')
      expect(warnCard?.className ?? '').toMatch(/warning/i)
      unmount()

      mockUseGetSystemHealth.mockReturnValue({
        data: mockHealthData, // verified zero
        isLoading: false,
        error: null,
        refetch: () => {},
      })
      renderWithQueryClient(<HealthDashboardPage />)
      await waitFor(() => {
        expect(screen.getByText('Needs Manual Removal At Plaid')).toBeInTheDocument()
      })
      expect(orphanCardValue('Needs Manual Removal At Plaid')).toBe('0')
      const okCard = screen.getByText('Needs Manual Removal At Plaid').closest('[data-slot="card"]')
      expect(okCard?.className ?? '').not.toMatch(/warning/i)
    })

    // ⚠ THE FAIL-CLOSED PATH. A green "0" produced by a FAILED read would tell an operator that no
    // erased user's Plaid Item is still billing — a claim nothing checked. Must read "Unavailable".
    it('renders "Unavailable", not a green zero, when the derivation failed', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          plaid_orphaned_items: {
            auto_retrying: 0,
            manual_intervention_required: 0,
            operator_attested: 0,
            resolved: 0,
            max_unresolved_retry_attempt_count: 0,
            available: false,
          },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText('Needs Manual Removal At Plaid')).toBeInTheDocument()
      })

      expect(orphanCardValue('Needs Manual Removal At Plaid')).toBe('Unavailable')
      expect(orphanCardValue('Auto-Retrying')).toBe('Unavailable')
      expect(orphanCardValue('Worst Retry Attempt Count')).toBe('Unavailable')

      const card = screen.getByText('Needs Manual Removal At Plaid').closest('[data-slot="card"]')
      expect(card?.className ?? '').not.toMatch(/success/i)
    })

    // ── Issue #1497 item 2: the operator-attested count reaches this dashboard ────────────────────
    //
    // ⚠ WHY IT MUST BE HERE AT ALL. The four state counts come from one hand-written switch in the
    // backend and are rendered by a hand-written list of cards here, so a state added to the domain
    // and not added to both simply disappears — an operator reading this page would see the four
    // numbers no longer account for the table and have no way to tell.
    it('shows the operator-attested count as its own number, never folded into a resolved total', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          plaid_orphaned_items: {
            auto_retrying: 1,
            manual_intervention_required: 2,
            resolved: 9,
            operator_attested: 4,
            max_unresolved_retry_attempt_count: 6,
            available: true,
          },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)
      await waitFor(() => {
        expect(screen.getByText(/Operator-Attested/i)).toBeInTheDocument()
      })

      // 4 is distinct from every other number in the fixture, so no other card's value can satisfy
      // this and a card summing attested into resolved (13) cannot either.
      expect(orphanCardValue('Operator-Attested (Unverified)')).toBe('4')
      expect(orphanCardValue('Needs Manual Removal At Plaid')).toBe('2')

      // ⚠ The label must say UNVERIFIED. This number is what colleagues said, not what Plaid
      // confirmed, and a card titled merely "Attested" would read as a second kind of success.
      expect(screen.getByText(/Operator-Attested \(Unverified\)/i)).toBeInTheDocument()
    })

    it('renders the attested count as Unavailable rather than a confident zero on a failed read', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          plaid_orphaned_items: {
            auto_retrying: 0,
            manual_intervention_required: 0,
            resolved: 0,
            operator_attested: 0,
            max_unresolved_retry_attempt_count: 0,
            available: false,
          },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)
      await waitFor(() => {
        expect(screen.getByText(/Operator-Attested/i)).toBeInTheDocument()
      })
      expect(orphanCardValue('Operator-Attested (Unverified)')).toBe('Unavailable')
    })

    // A rolling deploy, or an older cached response, can omit `plaid_orphaned_items` ENTIRELY. An
    // unguarded read throws and blanks the WHOLE dashboard — the opposite of fail-closed. Same
    // defect class as R3-7 for job_backlog above.
    it('degrades to "Unavailable" (not a crash) when `plaid_orphaned_items` is entirely absent', async () => {
      const withoutOrphans: Record<string, unknown> = { ...mockHealthData }
      delete withoutOrphans.plaid_orphaned_items

      mockUseGetSystemHealth.mockReturnValue({
        data: withoutOrphans,
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(/System Health/i)).toBeInTheDocument()
      })
      await waitFor(() => {
        expect(screen.getByText('Needs Manual Removal At Plaid')).toBeInTheDocument()
      })

      expect(orphanCardValue('Needs Manual Removal At Plaid')).toBe('Unavailable')
    })
  })

  // Issue #1504. A residue record means a GDPR deletion was revoked AFTER the user's Paddle
  // subscription-cancel had already landed: they keep their account and all their data, and have no
  // subscription. Before this section, that row was read by nothing anywhere in the product.
  describe('Deletion Residue section (Issue #1504)', () => {
    function residueCardValue(title: string): string {
      const label = screen.getByText(title)
      const card = label.closest('[data-slot="card"]')
      if (!card) throw new Error(`Could not find card for "${title}"`)
      return card.querySelector('[data-slot="stat-value"]')?.textContent ?? ''
    }

    const RECORDS_CARD = 'Subscriptions Cancelled By A Revoked Deletion'
    const RECENT_CARD = 'Most Recent Occurrence'

    // THE RED TEST: a residue record is visible to an operator.
    it('surfaces a seeded residue count and the age of the most recent record', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          deletion_residue: {
            records: 3,
            newest_record_seconds: 7500,
            available: true,
          },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(/Deletion Residue/i)).toBeInTheDocument()
      })

      // 3, not the fixture's 0 and not the 7500 beside it — a card wired to the wrong field fails.
      expect(residueCardValue(RECORDS_CARD)).toBe('3')
      expect(residueCardValue(RECENT_CARD)).toBe('2h 5m ago')
    })

    // THE MANDATORY SIBLING. Without it the count card could render a constant and the test above
    // would still pass. This is the ordinary-successful-deletion case: no residue at all.
    it('reports a verified zero — and "None", never "0s" — when no deletion left residue', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: mockHealthData, // records: 0, newest_record_seconds: 0, available: true
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(RECORDS_CARD)).toBeInTheDocument()
      })

      expect(residueCardValue(RECORDS_CARD)).toBe('0')
      // ⚠ THE WHOLE POINT OF THIS ASSERTION. The backend sends 0 seconds both for "never happened"
      // and for "happened this instant", so a card that formatted the number unconditionally would
      // render '0s ago' here — telling an operator this just happened when it never has.
      expect(residueCardValue(RECENT_CARD)).toBe('None')
      expect(residueCardValue(RECENT_CARD)).not.toBe('0s ago')
    })

    it('renders "Unavailable" rather than a confident zero when the residue query failed', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          deletion_residue: {
            records: 0,
            newest_record_seconds: 0,
            available: false,
          },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(RECORDS_CARD)).toBeInTheDocument()
      })

      expect(residueCardValue(RECORDS_CARD)).toBe('Unavailable')
      expect(residueCardValue(RECENT_CARD)).toBe('Unavailable')
    })

    // A rolling deploy or an older cached response can omit `deletion_residue` entirely; an unguarded
    // read throws and blanks the WHOLE dashboard. Same defect class as R3-7 for job_backlog.
    it('degrades to "Unavailable" (not a crash) when `deletion_residue` is entirely absent', async () => {
      const withoutResidue: Record<string, unknown> = { ...mockHealthData }
      delete withoutResidue.deletion_residue

      mockUseGetSystemHealth.mockReturnValue({
        data: withoutResidue,
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(/System Health/i)).toBeInTheDocument()
      })
      await waitFor(() => {
        expect(screen.getByText(RECORDS_CARD)).toBeInTheDocument()
      })

      expect(residueCardValue(RECORDS_CARD)).toBe('Unavailable')
      expect(residueCardValue(RECENT_CARD)).toBe('Unavailable')
    })
  })

  // Issue #1514, requirement #5: the operator-visibility signal restored after Phase 1 stopped
  // persisting rejected (unsigned/invalid-signature) webhooks anywhere /admin/webhooks can read.
  describe('Rejected Webhook Attempts section (Issue #1514 requirement #5)', () => {
    function rejectedCardValue(title: string): string {
      const label = screen.getByText(title)
      const card = label.closest('[data-slot="card"]')
      if (!card) throw new Error(`Could not find card for "${title}"`)
      return card.querySelector('[data-slot="stat-value"]')?.textContent ?? ''
    }

    const TOTAL_CARD = 'Rejected Attempts (24h)'
    const RECENT_CARD = 'Most Recent Attempt'

    // THE RED TEST: a forged-webhook flood is visible to an operator, broken down by provider.
    it('surfaces a seeded flood total, per-provider breakdown, and the most recent attempt', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          rejected_webhook_attempts_24h: {
            total_24h: 45,
            by_provider: { paddle: 40, plaid: 5 },
            most_recent_at: '2026-08-12T11:50:00Z',
            available: true,
          },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(/Rejected Webhook Attempts/i)).toBeInTheDocument()
      })

      // 45, not 40 or 5 alone — the total must be the sum, and each provider its own card.
      expect(rejectedCardValue(TOTAL_CARD)).toBe('45')
      expect(rejectedCardValue('Paddle Attempts (24h)')).toBe('40')
      expect(rejectedCardValue('Plaid Attempts (24h)')).toBe('5')
      expect(rejectedCardValue(RECENT_CARD)).not.toBe('None')
    })

    // THE MANDATORY SIBLING: without it, the total card could render a hardcoded constant and the
    // test above would still pass. A genuinely quiet 24 hours must read as a verified zero.
    it('reports a verified zero and "None" for the most recent attempt when nothing was rejected', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: mockHealthData, // rejected_webhook_attempts_24h: total 0, empty map, available true
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(TOTAL_CARD)).toBeInTheDocument()
      })

      expect(rejectedCardValue(TOTAL_CARD)).toBe('0')
      expect(rejectedCardValue(RECENT_CARD)).toBe('None')
      // No per-provider cards render when the map is empty.
      expect(screen.queryByText(/Paddle Attempts \(24h\)/i)).not.toBeInTheDocument()
    })

    // ⚠ THE FAIL-CLOSED PATH — the exact operational hazard requirement #5 exists to close. A
    // green "0" produced by a failed query would tell an operator "no attacks", during a flood.
    it('renders "Unavailable", not a green zero, when the query failed', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          rejected_webhook_attempts_24h: {
            total_24h: 0,
            by_provider: {},
            most_recent_at: null,
            available: false,
          },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(TOTAL_CARD)).toBeInTheDocument()
      })

      expect(rejectedCardValue(TOTAL_CARD)).toBe('Unavailable')
      expect(rejectedCardValue(RECENT_CARD)).toBe('Unavailable')

      const card = screen.getByText(TOTAL_CARD).closest('[data-slot="card"]')
      expect(card?.className ?? '').not.toMatch(/success/i)
    })

    // A rolling deploy or an older cached response can omit `rejected_webhook_attempts_24h`
    // entirely; an unguarded read must not throw and blank the whole dashboard.
    it('degrades to "Unavailable" (not a crash) when `rejected_webhook_attempts_24h` is entirely absent', async () => {
      const withoutRejected: Record<string, unknown> = { ...mockHealthData }
      delete withoutRejected.rejected_webhook_attempts_24h

      mockUseGetSystemHealth.mockReturnValue({
        data: withoutRejected,
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(/System Health/i)).toBeInTheDocument()
      })
      await waitFor(() => {
        expect(screen.getByText(TOTAL_CARD)).toBeInTheDocument()
      })

      expect(rejectedCardValue(TOTAL_CARD)).toBe('Unavailable')
      expect(rejectedCardValue(RECENT_CARD)).toBe('Unavailable')
    })

    // Pins the distinction the brief calls out as the most likely future regression: this section
    // must never move in lockstep with paddle_status.failed_webhooks_24h.
    it('is independent of the Paddle Status "Failed Webhooks (24h)" card', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          paddle_status: {
            ...mockHealthData.paddle_status,
            failed_webhooks_24h: 2,
          },
          rejected_webhook_attempts_24h: {
            total_24h: 99,
            by_provider: { paddle: 99 },
            most_recent_at: '2026-08-12T11:50:00Z',
            available: true,
          },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      })

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(TOTAL_CARD)).toBeInTheDocument()
      })

      expect(rejectedCardValue(TOTAL_CARD)).toBe('99')
      // The Paddle Status section's own card keeps its own, unrelated number.
      const failedWebhooksCard = screen.getByText('Failed Webhooks (24h)').closest('[data-slot="card"]')
      expect(failedWebhooksCard?.querySelector('[data-slot="stat-value"]')?.textContent).toBe('2')
    })
  })

  // #1631 M8b: forked-security-identity detection. Per-group member detail is not yet in the
  // generated API client (backend-only as of this change), so only the group count is asserted
  // here — see the component's own comment on `forkedSecurityIdentitiesAvailable`.
  describe('Forked Security Identities section', () => {
    const FORK_CARD = 'Fork Groups Detected'

    const forkCardValue = (title: string) => {
      const label = screen.getByText(title)
      const card = label.closest('[data-slot="card"]')
      if (!card) throw new Error(`Could not find card for "${title}"`)
      return card.querySelector('[data-slot="stat-value"]')?.textContent ?? ''
    }

    // THE RED TEST: a seeded fork group reaches an operator, where before this task nothing
    // anywhere counted them.
    it('surfaces a seeded fork-group count', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          forked_security_identities: { group_count: 2, available: true },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      } as never)

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(/Forked Security Identities/i)).toBeInTheDocument()
      })

      expect(forkCardValue(FORK_CARD)).toBe('2')
    })

    // THE MANDATORY SIBLING: without it, the seeded test above could pass against a hardcoded
    // constant. No forks detected must read as a verified zero.
    it('reports a verified zero when no forks are detected', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: mockHealthData, // forked_security_identities: group_count 0, available true
        isLoading: false,
        error: null,
        refetch: () => {},
      } as never)

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(FORK_CARD)).toBeInTheDocument()
      })

      expect(forkCardValue(FORK_CARD)).toBe('0')
    })

    // ⚠ Unknown is not zero — a failed query must never render as a confident "0 forks".
    it('renders "Unavailable", not a green zero, when the query failed', async () => {
      mockUseGetSystemHealth.mockReturnValue({
        data: {
          ...mockHealthData,
          forked_security_identities: { group_count: 0, available: false },
        },
        isLoading: false,
        error: null,
        refetch: () => {},
      } as never)

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(FORK_CARD)).toBeInTheDocument()
      })

      expect(forkCardValue(FORK_CARD)).toBe('Unavailable')
      const card = screen.getByText(FORK_CARD).closest('[data-slot="card"]')
      expect(card?.className ?? '').not.toMatch(/success/i)
    })

    // A rolling deploy or an older cached response can omit `forked_security_identities` entirely;
    // an unguarded read must not throw and blank the whole dashboard.
    it('degrades to "Unavailable" (not a crash) when `forked_security_identities` is entirely absent', async () => {
      const withoutForks: Record<string, unknown> = { ...mockHealthData }
      delete withoutForks.forked_security_identities

      mockUseGetSystemHealth.mockReturnValue({
        data: withoutForks,
        isLoading: false,
        error: null,
        refetch: () => {},
      } as never)

      renderWithQueryClient(<HealthDashboardPage />)

      await waitFor(() => {
        expect(screen.getByText(/System Health/i)).toBeInTheDocument()
      })
      await waitFor(() => {
        expect(screen.getByText(FORK_CARD)).toBeInTheDocument()
      })

      expect(forkCardValue(FORK_CARD)).toBe('Unavailable')
    })
  })

})
