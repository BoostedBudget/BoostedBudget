/**
 * AccountSummaryHeader Component
 *
 * Purely presentational header region extracted from AccountCard.
 * Renders: institution row, icon+name row, type label, and balance rows.
 *
 * Intentionally free of hooks, side effects, and action buttons.
 * Drag handles and status badges stay in AccountCard; they are injected via
 * the `institutionRowLeadingSlot` and `institutionRowTrailingSlot` render props
 * so the institution row remains a single horizontal flex row (preserving layout).
 *
 * Issue #1298: Extract AccountSummaryHeader from AccountCard
 */

import { AccountIconDisplay } from './AccountIconDisplay'
import { formatAccountBalance } from '../utils'
import { formatHistoryLabel } from '@/shared/dates/utils'
import type { AccountIcon } from '../types'

export interface AccountSummaryHeaderProps {
  institutionName: string
  mask?: string | null
  displayName: string
  typeLabel: string
  currentBalance?: number | null
  availableBalance?: number | null
  currencyCode?: string | null
  customEmoji?: string | null
  customIcon?: string | null
  customColor?: string | null
  /**
   * The account is retained but no longer active (Issue #1569): `removed`, `replaced` or
   * `expired`.
   *
   * A closed account carries NO current balance (user ruling, Issue #1580).
   * `accountBalanceProjection` (backend/internal/repository/account_liveness_sql.go) emits NULL —
   * not zero — for `current_balance` and `available_balance` at every account-shaped read, so
   * there is no figure to show and nothing to date.
   *
   * This prop replaces the former `asOfDate`, which switched the labels to "Last known
   * balance" and appended "as of <date>". That affordance was designed for a world where a
   * closed account still carried a frozen figure; once the backend stopped sending one it
   * rendered as "Last known balance: N/A … as of Jun 4, 2025" — a label and a date with nothing
   * between them. We now say plainly that a closed account has no balance, and that the history
   * is kept.
   */
  isClosed?: boolean
  /**
   * When the account stopped being live — `getAccountClosedDate(account)` (fix round 1 of Issue
   * #1580, item 1). T7 removed the "as of <date>" caption because the BALANCE it labelled is
   * never sent for a terminal-status account; the date in it, however, was real, and deleting the
   * caption left the closure date displayed nowhere in the app. Rendered only when `isClosed`.
   */
  closedOn?: string | null
  /**
   * The last balance recorded for this account before it closed, and the instant it was true
   * (Issue #1591) — `getAccountLastKnownBalance(account)` at the call sites.
   *
   * ⚠ This is NOT `currentBalance` arriving by another name. `currentBalance` is what the account
   * HOLDS, and for a closed account the backend sends nothing at all, permanently. These come from
   * `balance_snapshots`, which carries its own `snapshot_at` — and that instant is the entire
   * licence to show the number. #1569 T7 deleted a caption reading "Last known balance: N/A … as
   * of Jun 4, 2025" because there was no figure to date; an undated figure is the same defect
   * pointed the other way, and worse, because it reads exactly like a current balance.
   *
   * BOTH OR NEITHER. The row renders only when `isClosed` and both values are present. A closed
   * account with no snapshot on record renders no row at all — which is how "we never recorded
   * one" stays distinguishable from a recorded `0`, itself a fact about an account that was
   * emptied before it closed.
   *
   * ⚠ "BEFORE IT CLOSED" IS ENFORCED, NOT ASSUMED. Snapshot writes and status transitions are not
   * serialised — a sync holding a stale account list can write a snapshot dated after the user
   * disconnected — so the backend bounds the lateral by the terminal transition recorded in
   * `account_status_history` (Decision 4 on `lastKnownBalanceLateral`,
   * backend/internal/repository/account_liveness_sql.go). An account whose ONLY snapshot is
   * post-closure therefore arrives here as both-NULL and falls into the no-row case above; it does
   * not need, and must not get, an empty state of its own.
   */
  lastKnownBalance?: number | null
  /** See `lastKnownBalance` — the two are one fact and are rendered together or not at all. */
  lastKnownBalanceAt?: string | null
  /**
   * Optional content rendered at the start of the institution row (before institution name).
   * AccountCard uses this for the drag handle.
   */
  institutionRowLeadingSlot?: React.ReactNode
  /**
   * Optional content rendered at the end of the institution row (after mask).
   * AccountCard uses this for Hidden/Disconnected badges.
   */
  institutionRowTrailingSlot?: React.ReactNode
}

export function AccountSummaryHeader({
  institutionName,
  mask,
  displayName,
  typeLabel,
  currentBalance,
  availableBalance,
  currencyCode,
  customEmoji,
  customIcon,
  customColor,
  isClosed = false,
  closedOn,
  lastKnownBalance,
  lastKnownBalanceAt,
  institutionRowLeadingSlot,
  institutionRowTrailingSlot,
}: AccountSummaryHeaderProps) {
  // A closed account has no current balance, so nothing is rendered from `currentBalance` /
  // `availableBalance` even if a caller (a stale cache, a hand-built payload) supplies one —
  // restating a frozen figure as a current one is the defect, not the display of it.
  const currentBalanceFormatted = isClosed
    ? '—'
    : formatAccountBalance(currentBalance, currencyCode, { empty: 'N/A' })
  const availableBalanceFormatted = formatAccountBalance(availableBalance, currencyCode, { empty: 'N/A' })
  const showAvailableBalance =
    !isClosed && availableBalance !== undefined && availableBalance !== currentBalance
  // `== null` and not a truthiness check: a recorded 0.00 is a real balance on a real date, and
  // erasing it would render identically to "no snapshot on record" — a different, false statement.
  const showLastKnownBalance =
    isClosed && lastKnownBalance != null && lastKnownBalanceAt != null

  return (
    <>
      {/* Institution row: leading slot (drag handle) | institution name | mask | trailing slot (badges) */}
      <div className="flex items-center gap-3 mb-4 pb-3 border-b border-border">
        {institutionRowLeadingSlot}

        <div className="font-semibold text-sm text-muted-foreground uppercase tracking-wide flex-1">
          <span data-ph-mask>{institutionName || 'Unknown Bank'}</span>
        </div>

        <div className="flex items-center gap-3">
          {institutionRowTrailingSlot}
          {mask && (
            <div className="font-mono text-sm text-muted-foreground" data-ph-mask>
              ****{mask}
            </div>
          )}
        </div>
      </div>

      {/* Icon + name row */}
      <div className="flex flex-col gap-2">
        <div className="flex items-center gap-2">
          {customEmoji ? (
            <span className="text-xl leading-none" aria-label="Custom emoji">
              {customEmoji}
            </span>
          ) : customIcon ? (
            <span
              className="flex items-center justify-center"
              style={customColor ? { color: customColor } : undefined}
            >
              <AccountIconDisplay icon={customIcon as AccountIcon} className="h-5 w-5" />
            </span>
          ) : null}

          <h3
            className="text-xl font-semibold text-foreground m-0"
            style={customColor ? { color: customColor } : undefined}
          >
            <span data-ph-mask>{displayName}</span>
          </h3>
        </div>

        {/* Type label */}
        <p className="text-sm text-muted-foreground m-0">{typeLabel}</p>

        {/* Balance rows */}
        <div className="mt-4 flex flex-col gap-3">
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">
              {isClosed ? 'Balance' : 'Current Balance'}
            </span>
            <span className="text-lg font-semibold text-primary" data-ph-mask>
              {currentBalanceFormatted}
            </span>
          </div>
          {showAvailableBalance && (
            <div className="flex justify-between items-center">
              <span className="text-sm text-muted-foreground">Available Balance</span>
              <span className="text-lg font-semibold text-primary" data-ph-mask>
                {availableBalanceFormatted}
              </span>
            </div>
          )}
          {showLastKnownBalance && (
            <div className="flex justify-between items-start">
              <span className="text-sm text-muted-foreground">Last known balance</span>
              {/*
                The amount and its date live in ONE masked element on purpose. Visually they are a
                figure with a caption; to a screen reader they are one phrase — "$4,321.00 as of
                Feb 24, 2026" — so the historical qualifier cannot be read separately from the
                number it qualifies. `data-ph-mask` sits on the wrapper because rrweb records text
                verbatim and this is a real balance for a real account.
              */}
              <span className="text-right" data-ph-mask>
                <span className="block text-lg font-semibold text-foreground">
                  {formatAccountBalance(lastKnownBalance, currencyCode, { empty: 'N/A' })}
                </span>
                <span className="block text-xs font-normal text-muted-foreground">
                  as of {formatHistoryLabel(lastKnownBalanceAt!, 'MMM d, yyyy')}
                </span>
              </span>
            </div>
          )}
          {isClosed && (
            <p className="text-xs text-muted-foreground m-0">
              {closedOn && <>Closed on {formatHistoryLabel(closedOn, 'MMM d, yyyy')}. </>}
              Closed accounts do not carry a current balance. Your transaction and balance history
              is kept.
            </p>
          )}
        </div>
      </div>
    </>
  )
}
