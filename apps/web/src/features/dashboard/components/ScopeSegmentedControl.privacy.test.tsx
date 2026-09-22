/**
 * ScopeSegmentedControl — household disclosure PRIVACY GUARD
 * Issue #1706: household net-worth "doubled" report — candidate root cause D3
 * (no share-gate disclosure) and the owner's constraint: "we shouldn't expose
 * what accounts are NOT shared with the other members of a household".
 *
 * This guard is written BEFORE the caller-scoped share-coverage caption exists
 * (the caption is added later, inside ScopeSegmentedControl.tsx). It pins the
 * rule the caption must obey:
 *
 *   The household dashboard view may state only the CALLER'S OWN share state.
 *   It must never enumerate, count, name, or imply ANOTHER member's unshared
 *   accounts, and must never attribute any part of the household total to a
 *   member.
 *
 * Fixture: a two-member household where the CALLER shares some accounts and
 * ANOTHER member shares some and keeps others unshared.
 *
 * ── DATA SOURCES (why this guard is non-vacuous) ────────────────────────────
 * Scanning a rendered tree for a leak is worthless if the tree rendered nothing.
 * So this guard also pins the SET OF DATA SOURCES the household view consumes to
 * the caller-scoped allowlist, and asserts the set is non-empty today. The scan
 * reads named imports from `@/features/household`, `@/features/accounts` and
 * `@/shared/hooks/useTrackEvent`; every identifier it finds must be in the
 * caller-scoped allowlist below.
 *
 *   CALLER-SCOPED (allowed):
 *     - useHousehold()          → only the caller's own membership signal (`isInHousehold`)
 *     - useHouseholdVisibility()→ wraps `GET /api/v1/households/me/account-shares`
 *                                 (`listMyAccountShares`), "the caller's own accounts"
 *     - useActiveAccounts()     → the caller's own account list (future denominator)
 *
 *   HOUSEHOLD-WIDE (forbidden — must never feed the caption):
 *     - useHousehold().seatsUsed / seatsTotal  (from useGetHouseholdEntitlement)
 *     - useHouseholdMembers() / useListHouseholdMembers()
 *
 * The source scan fails if the host component is widened to any household-wide
 * source; the runtime spy fails if a household-wide hook is actually invoked.
 * That is what makes the "nothing rendered contains a leak" assertions honest.
 *
 * The over-sharing stub used to prove this guard can fail lives in TEST CODE
 * ONLY and is recorded in .omo/evidence/task-4-1706-privacy-guard.txt; it is
 * NOT part of this committed file and appears nowhere in apps/web/src.
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { readFileSync } from 'node:fs'
import type { ReactNode } from 'react'

import '@/test/mocks/registry'
import { mockUseDashboardContext, mockSetScope } from '@/test/mocks/dashboard-context'
import {
  mockUseGetMyHousehold,
  mockUseGetHouseholdEntitlement,
  mockRefetchMyHousehold,
  mockRefetchHouseholdEntitlement,
  mockUseListHouseholdMembers,
  mockRefetchHouseholdMembers,
  mockUseListMyAccountShares,
  mockRefetchMyAccountShares,
} from '@/test/mocks/household-api'
import { mockTrackEvent } from '@/test/mocks/track-event'

import { ScopeSegmentedControl } from './ScopeSegmentedControl'
import type { DashboardScope } from '../types'

/* ────────────────────────────────────────────────────────────────────────────
 * The household fixture
 * ──────────────────────────────────────────────────────────────────────────── */

const CALLER = {
  userId: 'user-caller',
  displayName: 'Casey Caller',
  // The caller shares 2 of their 3 accounts and keeps 1 unshared.
  sharedAccountIds: ['acct-caller-shared-1', 'acct-caller-shared-2'],
  unsharedAccountIds: ['acct-caller-unshared-1'],
} as const

const OTHER_MEMBER = {
  userId: 'user-other-member',
  displayName: 'Jordan Othermember',
  // The other member shares 1 account and keeps 2 unshared.
  sharedAccountIds: ['acct-other-shared-1'],
  unsharedAccountIds: ['acct-other-unshared-1', 'acct-other-unshared-2'],
} as const

/** The only share data the caller is allowed to see: their OWN rows. */
const CALLER_SCOPED_SHARES = [
  ...CALLER.sharedAccountIds.map((id) => ({ user_account_id: id, visibility: 'shared', revoked_at: undefined })),
  ...CALLER.unsharedAccountIds.map((id) => ({ user_account_id: id, visibility: 'hidden', revoked_at: undefined })),
]

/* ────────────────────────────────────────────────────────────────────────────
 * The pinned data-source registry
 * ──────────────────────────────────────────────────────────────────────────── */

const PRODUCTION_SOURCE = readFileSync(new URL('./ScopeSegmentedControl.tsx', import.meta.url), 'utf8')

/** Household-feature hooks the view may import: all caller-scoped. */
const CALLER_SCOPED_HOUSEHOLD_SOURCES = [
  'useHousehold', // membership signal (`isInHousehold`) only
  'useHouseholdVisibility', // the caller's own account shares
  'useActiveAccounts', // the caller's own account list
  'useTrackEvent', // analytics only, no account data
] as const

/** Household-wide sources that must NEVER feed the household view. */
const HOUSEHOLD_WIDE_SOURCES = [
  'useHouseholdMembers',
  'useListHouseholdMembers',
  'seatsUsed',
  'seatsTotal',
  'useGetHouseholdEntitlement',
] as const

/**
 * Module specifiers whose named imports are pinned to the caller-scoped allowlist. The household
 * feature is the primary surface; the accounts hook and the analytics hook are included because the
 * header lists them as caller-scoped, and an allowlist that never scans them cannot enforce that.
 */
const SCANNED_IMPORT_SOURCES = [
  '@/features/household',
  '@/features/accounts',
  '@/shared/hooks/useTrackEvent',
] as const

/** Named identifiers imported from any scanned module. */
function importedScannedIdentifiers(source: string): string[] {
  const identifiers: string[] = []
  for (const moduleSource of SCANNED_IMPORT_SOURCES) {
    const escaped = moduleSource.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const importRe = new RegExp(
      `import\\s*\\{([^}]*)\\}\\s*from\\s*['"]${escaped}[^'"]*['"]`,
      'g',
    )
    for (const match of source.matchAll(importRe)) {
      for (const raw of match[1].split(',')) {
        const identifier = raw.trim().split(/\s+as\s+/)[0].trim()
        if (identifier) identifiers.push(identifier)
      }
    }
  }
  return identifiers
}

/* ────────────────────────────────────────────────────────────────────────────
 * The leak scanner — its failure messages name the exact leak
 * ──────────────────────────────────────────────────────────────────────────── */

function expectNoOtherMemberUnsharedIdentifiers(view: HTMLElement) {
  const text = view.textContent ?? ''
  for (const id of OTHER_MEMBER.unsharedAccountIds) {
    expect(
      text,
      `PRIVACY LEAK: the household dashboard rendered another member's unshared account identifier "${id}"`,
    ).not.toContain(id)
  }
}

function expectNoOtherMemberUnsharedCount(view: HTMLElement) {
  const text = view.textContent ?? ''
  expect(
    text,
    `PRIVACY LEAK: the household dashboard surfaced "unshared" — another member keeps ${OTHER_MEMBER.unsharedAccountIds.length} accounts unshared`,
  ).not.toMatch(/\bunshared\b/i)
}

function expectNoPerMemberAttributionOfHouseholdTotal(view: HTMLElement) {
  const text = view.textContent ?? ''
  expect(
    text,
    `PRIVACY LEAK: the household dashboard named another member "${OTHER_MEMBER.displayName}" (per-member attribution of the household total)`,
  ).not.toContain(OTHER_MEMBER.displayName)
  expect(
    text,
    `PRIVACY LEAK: the household dashboard referenced "member" (implied per-member attribution of the household total)`,
  ).not.toMatch(/\bmember\b/i)
}

/* ────────────────────────────────────────────────────────────────────────────
 * Render helpers
 * ──────────────────────────────────────────────────────────────────────────── */

function createWrapper() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

function baseContextValue(scope: DashboardScope) {
  return {
    filters: { startDate: '2024-01-01', endDate: '2024-01-31' },
    setFilters: mock(() => {}),
    timeframe: 'this_month' as const,
    setTimeframe: mock(() => {}),
    isEditing: false,
    setIsEditing: mock(() => {}),
    hasAccounts: true,
    accountsLoading: false,
    scope,
    setScope: mockSetScope,
  }
}

describe('ScopeSegmentedControl household disclosure privacy guard (Issue #1706)', () => {
  beforeEach(() => {
    mockUseDashboardContext.mockClear()
    mockSetScope.mockClear()
    mockUseGetMyHousehold.mockClear()
    mockUseGetHouseholdEntitlement.mockClear()
    mockRefetchMyHousehold.mockClear()
    mockRefetchHouseholdEntitlement.mockClear()
    mockUseListHouseholdMembers.mockClear()
    mockRefetchHouseholdMembers.mockClear()
    mockUseListMyAccountShares.mockClear()
    mockRefetchMyAccountShares.mockClear()
    mockTrackEvent.mockClear()

    // Household scope: the caller is looking at the household view.
    mockUseDashboardContext.mockReturnValue(baseContextValue('household'))

    // Caller is in a household (membership signal only — caller-scoped).
    mockUseGetMyHousehold.mockReturnValue({
      data: { household: { id: 'hh-1', name: 'The Household' } },
      isLoading: false,
      error: null,
      refetch: mockRefetchMyHousehold,
    } as never)

    // HOUSEHOLD-WIDE source #1, armed with leaky sentinels: seat counts and the
    // roster that names the other member. A caption fed by these would leak.
    mockUseGetHouseholdEntitlement.mockReturnValue({
      data: { seats_active: 2, seats_pending: 0, users_included: 3, is_owner: true, source: 'household', tier: 'household_3' },
      isLoading: false,
      error: null,
      refetch: mockRefetchHouseholdEntitlement,
    } as never)
    mockUseListHouseholdMembers.mockReturnValue({
      data: [
        { user_id: CALLER.userId, display_name: CALLER.displayName, joined_at: '2024-01-01T00:00:00Z', role: 'owner', status: 'active' },
        {
          user_id: OTHER_MEMBER.userId,
          display_name: OTHER_MEMBER.displayName,
          joined_at: '2024-01-02T00:00:00Z',
          role: 'member',
          status: 'active',
        },
      ],
      isLoading: false,
      error: null,
      refetch: mockRefetchHouseholdMembers,
    } as never)

    // CALLER-SCOPED source: the caller's own shares only.
    mockUseListMyAccountShares.mockReturnValue({
      data: CALLER_SCOPED_SHARES,
      isLoading: false,
      error: null,
      refetch: mockRefetchMyAccountShares,
    } as never)
  })

  function renderHouseholdView() {
    return render(<ScopeSegmentedControl />, { wrapper: createWrapper() })
  }

  describe('data sources (non-vacuous: the set of consumed sources is pinned)', () => {
    it('imports only caller-scoped data sources (household, accounts, analytics)', () => {
      const imported = importedScannedIdentifiers(PRODUCTION_SOURCE)

      // Non-vacuous anchor: the set is NON-EMPTY today — the host already reads
      // the caller's own membership signal. A future caption must not widen it.
      expect(
        imported.length,
        'non-vacuous anchor: ScopeSegmentedControl must import at least one household source (today: useHousehold for isInHousehold)',
      ).toBeGreaterThan(0)

      const householdWideImports = imported.filter((id) =>
        (HOUSEHOLD_WIDE_SOURCES as readonly string[]).includes(id),
      )
      expect(
        householdWideImports,
        `PRIVACY LEAK (data path): ScopeSegmentedControl imports household-wide source(s): ${householdWideImports.join(', ')}`,
      ).toEqual([])

      const nonCallerScopedImports = imported.filter(
        (id) => !(CALLER_SCOPED_HOUSEHOLD_SOURCES as readonly string[]).includes(id),
      )
      expect(
        nonCallerScopedImports,
        `PRIVACY LEAK (data path): ScopeSegmentedControl imports household source(s) that are not caller-scoped: ${nonCallerScopedImports.join(', ')}`,
      ).toEqual([])
    })

    it('reads the caller-owned membership signal and no household-wide data field', () => {
      // Non-vacuous anchor: the household data path IS present (membership), not absent.
      expect(
        PRODUCTION_SOURCE,
        'non-vacuous anchor: ScopeSegmentedControl must read the caller-owned membership signal "isInHousehold"',
      ).toContain('isInHousehold')

      for (const token of HOUSEHOLD_WIDE_SOURCES) {
        expect(
          PRODUCTION_SOURCE,
          `PRIVACY LEAK (data path): ScopeSegmentedControl references household-wide data token "${token}"`,
        ).not.toContain(token)
      }
    })

    it('never invokes the household-wide member-enumeration hook at render time', () => {
      renderHouseholdView()

      expect(
        mockUseListHouseholdMembers,
        'PRIVACY LEAK (runtime): the household view invoked useListHouseholdMembers() (household-wide roster)',
      ).not.toHaveBeenCalled()
    })
  })

  describe('rendered output', () => {
    it('renders the household control — so the leak scan below is not an empty-tree pass', () => {
      renderHouseholdView()

      expect(screen.getByTestId('scope-segmented-control')).toBeInTheDocument()
      expect(screen.getByText('Household')).toBeInTheDocument()
    })

    it("never renders another member's unshared account identifiers", () => {
      const { container } = renderHouseholdView()
      expectNoOtherMemberUnsharedIdentifiers(container)
    })

    it("never renders another member's unshared count", () => {
      const { container } = renderHouseholdView()
      expectNoOtherMemberUnsharedCount(container)
    })

    it('never attributes any part of the household total to a member', () => {
      const { container } = renderHouseholdView()
      expectNoPerMemberAttributionOfHouseholdTotal(container)
    })

    it('the full scanner passes against the real household view', () => {
      const { container } = renderHouseholdView()
      expectNoOtherMemberUnsharedIdentifiers(container)
      expectNoOtherMemberUnsharedCount(container)
      expectNoPerMemberAttributionOfHouseholdTotal(container)
    })
  })
})