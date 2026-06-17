/**
 * AccountSummaryHeader Component
 *
 * Purely presentational header region extracted from AccountCard.
 * Renders: institution row, icon+name row, type label, and balance rows.
 *
 * Intentionally free of hooks, side effects, drag handles, badges, and action buttons.
 * Those concerns stay in AccountCard.
 *
 * Issue #1298: Investment account holdings detail
 */

import { AccountIconDisplay } from './AccountIconDisplay'
import { formatAccountBalance } from '../utils'
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
}: AccountSummaryHeaderProps) {
  const currentBalanceFormatted = formatAccountBalance(currentBalance, currencyCode, { empty: 'N/A' })
  const availableBalanceFormatted = formatAccountBalance(availableBalance, currencyCode, { empty: 'N/A' })

  return (
    <>
      {/* Institution row */}
      <div className="flex items-center justify-between mb-4 pb-3 border-b border-border">
        <div className="font-semibold text-sm text-muted-foreground uppercase tracking-wide flex-1">
          <span data-ph-mask>{institutionName || 'Unknown Bank'}</span>
        </div>
        {mask && (
          <div className="font-mono text-sm text-muted-foreground" data-ph-mask>
            ****{mask}
          </div>
        )}
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
            <span className="text-sm text-muted-foreground">Current Balance</span>
            <span className="text-lg font-semibold text-primary" data-ph-mask>
              {currentBalanceFormatted}
            </span>
          </div>
          {availableBalance !== undefined && availableBalance !== null && availableBalance !== currentBalance && (
            <div className="flex justify-between items-center">
              <span className="text-sm text-muted-foreground">Available Balance</span>
              <span className="text-lg font-semibold text-primary" data-ph-mask>
                {availableBalanceFormatted}
              </span>
            </div>
          )}
        </div>
      </div>
    </>
  )
}
