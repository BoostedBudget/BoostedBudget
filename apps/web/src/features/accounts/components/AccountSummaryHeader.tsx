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
   * When set, the account is retained but no longer active (Issue #1569): its balance was
   * frozen at this instant and must never read as a live figure. Switches the "Current
   * Balance"/"Available Balance" labels to "Last known balance"/"Last known available balance"
   * and appends "as of <date>".
   */
  asOfDate?: string | null
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
  asOfDate,
  institutionRowLeadingSlot,
  institutionRowTrailingSlot,
}: AccountSummaryHeaderProps) {
  const currentBalanceFormatted = formatAccountBalance(currentBalance, currencyCode, { empty: 'N/A' })
  const availableBalanceFormatted = formatAccountBalance(availableBalance, currencyCode, { empty: 'N/A' })
  const asOfLabel = asOfDate ? formatHistoryLabel(asOfDate, 'MMM d, yyyy') : null

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
              {asOfLabel ? 'Last known balance' : 'Current Balance'}
            </span>
            <span className="text-lg font-semibold text-primary" data-ph-mask>
              {currentBalanceFormatted}
            </span>
          </div>
          {availableBalance !== undefined && availableBalance !== currentBalance && (
            <div className="flex justify-between items-center">
              <span className="text-sm text-muted-foreground">
                {asOfLabel ? 'Last known available balance' : 'Available Balance'}
              </span>
              <span className="text-lg font-semibold text-primary" data-ph-mask>
                {availableBalanceFormatted}
              </span>
            </div>
          )}
          {asOfLabel && (
            <p className="text-xs text-muted-foreground text-right m-0">as of {asOfLabel}</p>
          )}
        </div>
      </div>
    </>
  )
}
