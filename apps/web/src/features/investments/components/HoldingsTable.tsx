import { useId, useState, useMemo, useEffect } from 'react'
import type { ReactElement } from 'react'
import {
  useGetHoldings,
  useGetAccountHoldingsHistory,
} from '@boostedbudget/api-client/endpoints/investments/investments'
import { useAllAccounts, isInactive } from '@/features/accounts/hooks'
import { getAccountClosedDate } from '@/features/accounts/utils'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef, SortState, FilterState } from '@/shared/components/DataTable'
import type { FilterOption } from '@/shared/components/ColumnFilterDropdown'
import { formatPercent } from '@/shared/charts/formatters'
import { formatPortfolioAmount, isCurrencyUnnameable } from '../utils/portfolioCurrency'
import { MixedCurrencyBadge } from './MixedCurrencyBadge'
import { formatHistoryLabel } from '@/shared/dates/utils'
import { Badge } from '@/components/ui/badge'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import type { HoldingView } from '@boostedbudget/api-client/models/holdingView'
import type { HoldingSnapshotView } from '@boostedbudget/api-client/models/holdingSnapshotView'
import { securityTypeLabel } from '@/features/transactions/investmentSubcategory'

/**
 * The fields the column definitions below actually read.
 *
 * Both HoldingView (what the account holds NOW) and HoldingSnapshotView (what it held on a past
 * UTC day) satisfy this, which is what lets ONE set of column definitions serve both tables
 * instead of a second near-copy. Consolidation is a hard constraint here, not a preference: the
 * investments route's gzip budget is 7 kB and it last measured 6.03 kB.
 *
 * ⚠ THE SHAPE STOPS SHORT OF day_change ON PURPOSE. Day change is quantity x (price - today's
 * prior close), and pairing a position from months ago with today's close states a historical
 * movement as a current one. HoldingSnapshotView has no such field at all (the backend type
 * refuses it structurally — see backend/internal/domain/holding_snapshot_view.go), so the two
 * day-change columns live in their own array that only the live table ever concatenates.
 */
type HoldingRowShape = {
  security_id: string
  security_name?: string
  ticker_symbol?: string
  security_type?: string
  quantity: number
  institution_price?: number
  institution_value?: number
  cost_basis?: number
  gain_loss?: number
  gain_loss_pct?: number
  // #1631 CX1: the currency this position's money columns are denominated in. Present on BOTH
  // HoldingView and HoldingSnapshotView, which is what lets the shared shape carry it -- and it
  // has to be per row, because one account can hold positions in several currencies.
  currency_code?: string
}

// #1631 CX1: formatted in the currency the ROW states, not the dollar. A single investment
// account can hold positions in several currencies, so there is no portfolio-level verdict that
// applies to an individual position. fractionDigits 0 preserves exactly what formatCurrency
// rendered; this change moves the SYMBOL and nothing else.
const formatOrDefault = (
  value: number | null | undefined,
  row: { currency_code?: string },
): string => (value != null ? formatPortfolioAmount(value, row, 0) : '--')

const formatPercentOrDefault = (value: number | null | undefined): string =>
  value != null ? formatPercent(value) : '--'

// All column IDs including account (used in portfolio/unscoped mode)
const PORTFOLIO_HOLDINGS_COLUMN_IDS = [
  'name',
  'ticker',
  'type',
  'quantity',
  'price',
  'market_value',
  'cost_basis',
  'gain_loss',
  'gain_loss_pct',
  'day_change',
  'day_change_pct',
  'account',
] as const

// Column IDs for scoped (single-account) mode — excludes 'account'
const ACCOUNT_HOLDINGS_COLUMN_IDS = [
  'name',
  'ticker',
  'type',
  'quantity',
  'price',
  'market_value',
  'cost_basis',
  'gain_loss',
  'gain_loss_pct',
  'day_change',
  'day_change_pct',
] as const

// name and ticker reveal portfolio composition and must be masked per posthog-pii-masking.md
// The historical table drops the two day-change columns and the account column: it is always
// scoped to one account, and day change does not exist for a past day.
const HISTORICAL_HOLDINGS_COLUMN_IDS = [
  'name',
  'ticker',
  'type',
  'quantity',
  'price',
  'market_value',
  'cost_basis',
  'gain_loss',
  'gain_loss_pct',
] as const

const MASKED_COLUMN_IDS = [
  'name',
  'ticker',
  'quantity',
  'price',
  'market_value',
  'cost_basis',
  'gain_loss',
  'gain_loss_pct',
  'day_change',
  'day_change_pct',
  'account',
]

/**
 * The nine columns BOTH tables render. Definitions, formatting, masking and sort accessors are
 * shared, so the historical view of a closed brokerage is visually and behaviourally the same
 * table the user already knows — and there is exactly one place to change a column.
 */
const VALUE_COLUMNS: ColumnDef<HoldingRowShape>[] = [
  {
    id: 'name',
    header: 'Security',
    key: 'security_name',
    // `??` alone misses production rows where security_name is '' rather than null/undefined
    // (#1631) — a blank string is still truthy-checked here, not merely non-nullish.
    render: (r) => <span>{r.security_name && r.security_name.length > 0 ? r.security_name : r.security_id}</span>,
  },
  {
    id: 'ticker',
    header: 'Ticker',
    key: 'ticker_symbol',
  },
  {
    id: 'type',
    header: 'Type',
    // Route through the shared title-caser + acronym map (#1631) so this column renders the same
    // label the allocation donut immediately above it on the same page already shows for the
    // same securities.type value ("etf" -> "ETF", "mutual fund" -> "Mutual Fund"). A null/blank
    // type renders "Unknown" rather than a blank cell, matching HoldingView.SecurityType's doc
    // comment.
    render: (r) => <span>{securityTypeLabel(r.security_type, 'Unknown')}</span>,
  },
  {
    id: 'quantity',
    header: 'Qty',
    key: 'quantity',
    numeric: true,
    align: 'right',
  },
  {
    id: 'price',
    header: 'Price',
    key: 'institution_price',
    render: (r) => formatOrDefault(r.institution_price, r),
    numeric: true,
    align: 'right',
  },
  {
    id: 'market_value',
    header: 'Market Value',
    sortable: true,
    sortAccessor: (r) => r.institution_value ?? 0,
    render: (r) => formatOrDefault(r.institution_value, r),
    numeric: true,
    align: 'right',
  },
  {
    id: 'cost_basis',
    header: 'Cost Basis',
    render: (r) => formatOrDefault(r.cost_basis, r),
    numeric: true,
    align: 'right',
  },
  {
    id: 'gain_loss',
    // #1631 CX1: no dollar sign in the header. The cells beneath it are formatted per row, so a
    // GBP position would sit under a header asserting dollars. The "(%)" sibling column is what
    // this one is being distinguished from, and the absence of a suffix does that on its own.
    header: 'Gain/Loss',
    sortable: true,
    sortAccessor: (r) => r.gain_loss ?? 0,
    render: (r) => formatOrDefault(r.gain_loss, r),
    numeric: true,
    align: 'right',
  },
  {
    id: 'gain_loss_pct',
    header: 'Gain/Loss (%)',
    sortable: true,
    sortAccessor: (r) => r.gain_loss_pct ?? 0,
    render: (r) => formatPercentOrDefault(r.gain_loss_pct),
    numeric: true,
    align: 'right',
  },
]

// #1631 I1: the backend distinguishes a REFUSED day change (day_change_unavailable_reason) from a
// STALE one (day_change_is_stale) -- opposite meanings, so this renders both rather than
// collapsing either into a blank cell. Stale is MARKED, not hidden: the word "Stale" is text
// content, so it is legible without depending on colour (WCAG 2.1 AA "never colour alone").
// Follows the shape MixedCurrencyBadge established rather than inventing a new affordance.
function renderDayChange(r: HoldingView): ReactElement {
  if (r.day_change_unavailable_reason === 'currency_mismatch') {
    return <span className="text-muted-foreground text-sm">Priced in a different currency</span>
  }
  const value = formatOrDefault(r.day_change, r)
  if (!r.day_change_is_stale) return <span>{value}</span>
  return (
    <span className="inline-flex items-center gap-1.5">
      {value}
      <Badge variant="warning" size="sm">
        Stale
      </Badge>
    </span>
  )
}

/**
 * Live-table-only. Day change is measured against today's prior close and has no meaning on a
 * past day's position, so it is never concatenated into the historical column set.
 */
const DAY_CHANGE_COLUMNS: ColumnDef<HoldingView>[] = [
  {
    id: 'day_change',
    header: 'Day Change',
    sortable: true,
    sortAccessor: (r) => r.day_change ?? 0,
    render: renderDayChange,
    numeric: true,
    align: 'right',
  },
  {
    id: 'day_change_pct',
    header: 'Day Change (%)',
    sortable: true,
    sortAccessor: (r) => r.day_change_pct ?? 0,
    render: (r) => formatPercentOrDefault(r.day_change_pct),
    numeric: true,
    align: 'right',
  },
]

const NO_FILTERS: FilterState = {}
const noopFiltersChange = () => {}

/**
 * Sort accessors shared by the live and historical tables. Kept in one place so a sortable column
 * cannot become a no-op in one table and work in the other.
 */
const SHARED_SORT_ACCESSORS: Record<string, (r: HoldingRowShape) => number> = {
  market_value: (r) => r.institution_value ?? 0,
  gain_loss: (r) => r.gain_loss ?? 0,
  gain_loss_pct: (r) => r.gain_loss_pct ?? 0,
}

function sortRows<T extends HoldingRowShape>(
  rows: T[],
  sortState: SortState,
  accessors: Record<string, (r: HoldingRowShape) => number>,
): T[] {
  if (!sortState) return rows
  const accessor = accessors[sortState.columnId]
  if (!accessor) return rows
  return [...rows].sort((a, b) => {
    const cmp = accessor(a) < accessor(b) ? -1 : accessor(a) > accessor(b) ? 1 : 0
    return sortState.direction === 'asc' ? cmp : -cmp
  })
}

interface HoldingsTableProps {
  accountId?: string
}

export function HoldingsTable({ accountId }: HoldingsTableProps) {
  const isScoped = !!accountId

  const [sortState, setSortState] = useState<SortState>(null)
  const [filters, setFilters] = useState<FilterState>({})

  const { data: holdingsData, isLoading, isError } = useGetHoldings(
    isScoped ? { account_id: accountId } : undefined,
  )
  // useAllAccounts, not useActiveAccounts — and NOT for the reason TransactionsPage switched
  // (#1569 T3). No holding row can reference a terminal account: the backend's current-state
  // universe hides them at the query (see account_liveness_sql.go, currentStateAccountLeg), so
  // widening this map adds no name to resolve. It is widened because the component needs the
  // STATUS of the account it is scoped to. AccountsPage lists closed accounts and links to them,
  // so this tab is reachable for one, and without the status it can only say "nothing synced
  // yet" — which is the opposite of true.
  // ⚠ isError IS CONSUMED, not discarded (#1569 R9 B2). scopedAccountIsDisconnected returns false
  // when `accounts` is undefined, and a FAILED accounts read is indistinguishable from a loaded-
  // empty one at that predicate — so a closed account whose accounts read failed fell through to
  // the live empty state and asserted "No holdings synced yet for this account." That is a false
  // claim about a closed account, manufactured by a failure.
  //
  // ⚠ include_hidden IS NOT OPTIONAL HERE (#1569 T9-FIX items 5 and 6), and it is the SAME
  // question AccountDetailPage's own useGetAccounts asks. Two things follow, and both were
  // broken:
  //   1. useAllAccounts sends status=all_statuses and nothing else, and the backend defaults
  //      include_hidden to FALSE. A HIDDEN closed investment account was therefore absent from
  //      the response, so scopedAccountIsDisconnected read false and the retained history this
  //      surface exists for never mounted at all.
  //   2. Identical params mean an identical react-query key, so this read and the parent's are
  //      ONE cache entry rather than two that can diverge. AccountDetailPage mounts the scoped
  //      form only after its own read succeeded, so the child now cannot be data-less while the
  //      parent knows the account — which is the unreachability the error arm below works
  //      around. That arm stays: this component is also mounted unscoped, and belt-and-braces on
  //      a false claim about someone's closed account is the cheap side of the trade.
  const { accounts, isError: accountsIsError } = useAllAccounts({ include_hidden: 'true' })

  // Build id→name map for resolving account UUIDs to human-readable names.
  const accountNameById = useMemo(() => {
    const map = new Map<string, string>(
      accounts?.map((a) => [a.id, a.nickname ?? a.name]) ?? []
    )
    return map
  }, [accounts])

  // One lookup, three facts. Three separate useMemos each re-ran the same find over the same
  // list; collapsing them is also how this change pays for its own bytes on the tightest route
  // budget in the repo.
  const scopedAccount = useMemo(
    () => (isScoped && accounts ? accounts.find((a) => a.id === accountId) : undefined),
    [isScoped, accounts, accountId],
  )

  // Scoped to an account that is no longer connected. Deliberately derived from the account,
  // never from "zero rows": a live account with nothing in it is a different fact and keeps the
  // ordinary empty state.
  const scopedAccountIsDisconnected = scopedAccount != null && isInactive(scopedAccount)

  // Consumed from the helper that owns the "which timestamp means closed" rule (#1591) rather
  // than re-derived here; undefined for an account that is not closed.
  const scopedAccountClosedOn = scopedAccount ? getAccountClosedDate(scopedAccount) : undefined

  // A fixed discriminator for the view event — never free text, never an amount. Same treatment
  // as ClosedAccountBalanceHistory's accountType on the sibling surface.
  const scopedAccountType = scopedAccount?.type

  const activeColumnIds = isScoped ? ACCOUNT_HOLDINGS_COLUMN_IDS : PORTFOLIO_HOLDINGS_COLUMN_IDS
  const tablePrefsKey = isScoped ? 'account-holdings' : 'portfolio-holdings'

  const {
    sizing,
    onSizingChange,
    columnOrder,
    onColumnOrderChange,
    columnVisibility,
    onColumnVisibilityChange,
  } = useTablePreferences(tablePrefsKey, [...activeColumnIds])

  const rawHoldings = holdingsData?.holdings

  // DataTable uses manualFiltering=true — apply account filter client-side (unscoped only).
  // In scoped mode the server already filters by account_id; do not apply a client-side filter
  // (a stale filter would hide all rows with no visible control to clear it).
  const filteredHoldings = useMemo(() => {
    const all = rawHoldings ?? []
    if (isScoped) return all
    const accountFilter = filters['account']
    if (!accountFilter || !Array.isArray(accountFilter) || accountFilter.length === 0) return all
    return all.filter((h) => (accountFilter as string[]).includes(h.account_id))
  }, [rawHoldings, filters, isScoped])

  // DataTable uses manualSorting=true — apply client-side sort before rendering.
  const sortedHoldings = useMemo(
    () =>
      sortRows(filteredHoldings, sortState, {
        ...SHARED_SORT_ACCESSORS,
        day_change: (r) => (r as HoldingView).day_change ?? 0,
        day_change_pct: (r) => (r as HoldingView).day_change_pct ?? 0,
      }),
    [filteredHoldings, sortState],
  )

  // accountOptions derived from all holdings (unfiltered) so the dropdown always shows
  // every account even while a filter is active. Only used in unscoped mode.
  const accountOptions: FilterOption[] = !isScoped
    ? Array.from(new Set((rawHoldings ?? []).map((h) => h.account_id))).map((id) => ({
        label: accountNameById.get(id) ?? id,
        value: id,
        masked: true,
      }))
    : []

  const baseColumns: ColumnDef<HoldingView>[] = [...VALUE_COLUMNS, ...DAY_CHANGE_COLUMNS]
  const accountColumn: ColumnDef<HoldingView> = {
    id: 'account',
    header: 'Account',
    render: (r) => <span>{accountNameById.get(r.account_id) ?? r.account_id}</span>,
    hideable: true,
    filter:
      accountOptions.length > 0
        ? { type: 'select', options: accountOptions, multiSelect: true, searchable: true }
        : undefined,
  }

  const columns: ColumnDef<HoldingView>[] = isScoped
    ? baseColumns
    : [...baseColumns, accountColumn]

  if (isLoading) {
    return (
      <div role="status" aria-label="Loading holdings" className="flex justify-center py-8">
        <div className="animate-spin rounded-full size-6 border-b-2 border-primary" />
      </div>
    )
  }

  // One node, mounted from TWO branches below (the live read's error arm and its empty arm). The
  // props are identical in both and this route's gzip budget is the tightest in the repo, so the
  // element is built once rather than written out twice.
  const retainedHistory = accountId ? (
    <ClosedAccountHoldingsHistory
      accountId={accountId}
      accountType={scopedAccountType}
      closedOn={scopedAccountClosedOn}
    />
  ) : null

  // ⚠ THE LIVE READ'S ERROR IS NOT ALLOWED TO PREEMPT THE RETAINED HISTORY (#1569 R9 B1).
  // Same shape as SavingsPage's live-goals error arm: the page's own failure keeps its message,
  // and the retained surface is ADDITIVE beneath it rather than replaced by it. On a closed
  // account the live read is *expected* to return nothing, so "refresh the page" pointed the user
  // at a page that can never produce live holdings while the history we deliberately kept sat
  // hidden behind it — which is also what the comment below (the notice replaces the EMPTY state,
  // never the rows) already said should not happen.
  if (isError) {
    // ⚠ SELF-REVIEW OF THE FIX ABOVE. The first draft keyed the copy on scopedAccountIsDisconnected
    // alone, which left one axis of the same defect open: that predicate is false when the ACCOUNTS
    // read failed too, so a closed account with BOTH reads failing was still told to refresh. It is
    // ⚠ WHY IT WAS REACHABLE HAS CHANGED, and the old reason is now false: this read and
    // AccountDetailPage's used DIFFERENT query keys (include_hidden), so they were separate
    // cache entries that could diverge. They are one entry now (see the hook call above), which
    // is what closes item 6. What remains reachable is the unscoped mount and a first load that
    // fails outright, and stating a failure we can stand behind still beats an instruction we
    // cannot.
    // When we cannot tell what the account is, we state the failure and stop short of an
    // instruction we cannot stand behind. No new string: the closed-account sentence is reused.
    const cannotPromiseRefresh = scopedAccountIsDisconnected || (isScoped && accountsIsError)
    return (
      <div className="flex flex-col gap-3">
        <div role="alert" className="text-sm text-destructive p-4">
          {cannotPromiseRefresh
            ? 'We could not load current holdings for this account.'
            : 'Unable to load holdings. Please refresh the page.'}
        </div>
        {/* scopedAccountIsDisconnected already implies a scoped accountId, so retainedHistory is
            never null here. */}
        {scopedAccountIsDisconnected && retainedHistory}
      </div>
    )
  }

  // The disconnected notice REPLACES the empty state, never the rows. If a position is somehow
  // present for a terminal account, showing it beats claiming there is nothing.
  if (scopedAccountIsDisconnected && sortedHoldings.length === 0 && accountId) {
    return retainedHistory
  }

  return (
    <DataTable
      data={sortedHoldings}
      columns={columns}
      rowKey={(r) => r.id}
      sort={sortState}
      onSortChange={setSortState}
      filters={filters}
      onFiltersChange={setFilters}
      maskedColumnIds={MASKED_COLUMN_IDS}
      emptyState={
        isScoped && accountsIsError ? (
          <p role="alert" className="text-sm text-destructive p-4">
            We could not load this account&apos;s details. Nothing was deleted — try again in a
            moment.
          </p>
        ) : (
          <p className="text-sm text-muted-foreground p-4">
            {isScoped ? 'No holdings synced yet for this account.' : 'No holdings found.'}
          </p>
        )
      }
      columnSizing={sizing}
      onColumnSizingChange={onSizingChange}
      columnOrder={columnOrder}
      onColumnOrderChange={onColumnOrderChange}
      columnVisibility={columnVisibility}
      onColumnVisibilityChange={onColumnVisibilityChange}
    />
  )
}

/**
 * ClosedAccountHoldingsHistory — Issue #1569.
 *
 * WHAT IT REPLACED, AND WHY. This slot used to hold a notice that said positions are not shown for
 * a disconnected account, and that only the transactions were still reachable. That notice was
 * honest at the time — holding_snapshots was written and never read, so there was genuinely
 * nothing to show. Privacy policy §8.2a promised more than the notice delivered: it told the user a
 * removed account's holdings history "is retained ... so you can still view it under 'Closed
 * accounts'". Rather than narrow the policy, the reader was built. This component is where the
 * user finally sits in front of it.
 *
 * ⚠ TWO UNIVERSES, AND THIS ONE IS THE HISTORICAL ONE. Everything here is labelled with the UTC day
 * it was true on, and nothing here may be read as what the account holds now. The type carries no
 * day-change field (the backend refuses to emit one), the current-state holdings read is untouched,
 * and no figure from this component reaches balanceMatchesRange / compareBalanceValues.
 *
 * REUSE. The nine columns, their formatters, their masking and their sort accessors are the same
 * VALUE_COLUMNS the live table renders — so a closed account's positions look and behave like the
 * table the user already knows, and the investments route's tight gzip budget pays for the query
 * hook and this shell rather than for a second table.
 */
function ClosedAccountHoldingsHistory({
  accountId,
  accountType,
  closedOn,
}: {
  accountId: string
  /** A fixed discriminator on the analytics event — never free text, never an amount. */
  accountType?: string
  closedOn?: string
}) {
  const trackEvent = useTrackEvent()
  const selectId = useId()
  const [asOf, setAsOf] = useState<string | undefined>(undefined)
  const [sortState, setSortState] = useState<SortState>(null)

  const { data, isLoading, isError } = useGetAccountHoldingsHistory({
    account_id: accountId,
    ...(asOf ? { as_of: asOf } : {}),
  })

  const {
    sizing,
    onSizingChange,
    columnOrder,
    onColumnOrderChange,
    columnVisibility,
    onColumnVisibilityChange,
  } = useTablePreferences('account-holdings-history', [...HISTORICAL_HOLDINGS_COLUMN_IDS])

  const positions = useMemo<HoldingSnapshotView[]>(() => data?.positions ?? [], [data])
  const sortedPositions = useMemo(
    () => sortRows(positions, sortState, SHARED_SORT_ACCESSORS),
    [positions, sortState],
  )

  const availableDates = data?.available_dates ?? []
  const shownDate = data?.as_of_date

  // THE VIEW MEASUREMENT (#1569 T7.8): did anyone actually go and look at the money we kept?
  // Fired once the read resolves, whether or not anything was retained — "the user opened it and
  // we had nothing" is the more interesting half of that question, not a case to drop. The
  // date-changed event below cannot answer it: the picker only renders above one available day.
  //
  // ⚠ COUNTS ONLY. No amount, no security name, no date — event properties are not covered by
  // session-replay masking, and a security name is portfolio composition.
  const resolved = !isLoading && !isError
  useEffect(() => {
    if (!resolved) return
    trackEvent(ANALYTICS_EVENTS.ACCOUNT_HOLDINGS_HISTORY_VIEWED, {
      account_type: accountType,
      retained_days: availableDates.length,
      positions_shown: positions.length,
    })
    // trackEvent is stable (useCallback with empty deps in useTrackEvent); the effect is keyed on
    // the account and the resolution, not on the arrays, so re-sorting does not re-fire it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resolved, accountId])

  if (isLoading) {
    return (
      <div role="status" aria-label="Loading retained holdings" className="flex justify-center py-8">
        <div className="animate-spin rounded-full size-6 border-b-2 border-primary" />
      </div>
    )
  }

  if (isError) {
    return (
      <div role="alert" className="text-sm text-destructive p-4">
        We could not load this account&apos;s retained holdings. They are still kept — try again in
        a moment.
      </div>
    )
  }

  // No history at all. This is a real case rather than an error. #1569 R6B/C3: the cause is not
  // established — the account may simply have held nothing, or day-by-day capture may not have
  // covered it for other reasons — so the copy below stays neutral about WHY and only states the
  // fact, while still being explicit that nothing was destroyed.
  if (availableDates.length === 0) {
    return (
      <div
        role="note"
        className="rounded-lg border border-border bg-muted/40 p-4 text-sm text-muted-foreground"
      >
        <p className="font-medium text-foreground m-0">
          No day-by-day positions were recorded for this account.
        </p>
        <p className="mt-1">
          We don&apos;t have retained holdings history for this account. Nothing was deleted — its
          transactions are retained and still appear in your activity.
        </p>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <Badge variant="secondary" size="sm">
          Closed account
        </Badge>
        <p className="text-sm text-muted-foreground m-0">
          {/* Never colour alone: the word "Closed" carries the state, the badge only tints it. */}
          These are the positions we last recorded, not what the account holds today
          {closedOn ? ` — it closed on ${formatHistoryLabel(closedOn, 'MMM d, yyyy')}` : ''}.
        </p>
      </div>

      {availableDates.length > 1 ? (
        <div className="flex items-center gap-2">
          <label htmlFor={selectId} className="text-sm text-muted-foreground">
            Positions as of
          </label>
          <select
            id={selectId}
            className="rounded-md border border-border bg-background px-2 py-1 text-sm text-foreground"
            value={shownDate ?? availableDates[0]}
            onChange={(e) => {
              setAsOf(e.target.value)
              trackEvent(ANALYTICS_EVENTS.ACCOUNT_HOLDINGS_HISTORY_DATE_CHANGED, {
                account_id: accountId,
              })
            }}
          >
            {availableDates.map((d) => (
              <option key={d} value={d}>
                {formatHistoryLabel(d, 'MMM d, yyyy')}
              </option>
            ))}
          </select>
        </div>
      ) : (
        shownDate && (
          <p className="text-sm text-muted-foreground m-0">
            Positions as of {formatHistoryLabel(shownDate, 'MMM d, yyyy')}.
          </p>
        )
      )}

      <DataTable
        data={sortedPositions}
        columns={VALUE_COLUMNS}
        rowKey={(r) => r.security_id}
        sort={sortState}
        onSortChange={setSortState}
        // No column here is filterable — the table is already scoped to one account and one day,
        // and the day is chosen by the select above rather than by a column filter.
        filters={NO_FILTERS}
        onFiltersChange={noopFiltersChange}
        maskedColumnIds={MASKED_COLUMN_IDS}
        emptyState={
          /*
            #1569 T7.8 — the fully liquidated day. Reaching this state means the day IS in
            available_dates (it is retained) and every one of its rows is marked absent: the
            account was observed holding nothing. The old copy ("No positions were recorded for
            this day") denied a day we recorded, which is the same false claim about retained data
            this whole surface exists to stop making. A day we kept nothing for never gets here —
            the server 404s it — so this can say what is actually true.

            ⚠ It says WHAT, not WHY, per the same R6B/C2 rule as the no-history copy above: the
            rows record that the positions stopped being held, not whether they were sold,
            transferred out, or dropped by the provider. Naming a cause would invent one.
          */
          <p className="text-sm text-muted-foreground p-4">
            This account held no positions on this day — we recorded the day, and it was empty.
          </p>
        }
        columnSizing={sizing}
        onColumnSizingChange={onSizingChange}
        columnOrder={columnOrder}
        onColumnOrderChange={onColumnOrderChange}
        columnVisibility={columnVisibility}
        onColumnVisibilityChange={onColumnVisibilityChange}
      />

      {data?.total_value != null && (
        <p className="flex items-baseline justify-between border-t border-border pt-2 text-sm m-0">
          <span className="text-muted-foreground">Total recorded value</span>
          <span className="flex items-baseline gap-2">
            <span className="font-semibold tabular-nums text-foreground" data-ph-mask>
              {/* #1631 CX1: the snapshot states its own currency verdict, and refuses to name one
                  when the day's positions spanned several. */}
              {formatPortfolioAmount(data.total_value, data)}
            </span>
            {isCurrencyUnnameable(data) && <MixedCurrencyBadge />}
          </span>
        </p>
      )}

      <p className="text-xs leading-snug text-muted-foreground m-0">
        Removing a bank connection keeps this account&apos;s history rather than erasing it. These
        figures are excluded from your active balances and reports.
      </p>
    </div>
  )
}
