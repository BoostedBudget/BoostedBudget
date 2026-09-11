import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { ActivityTypeChip } from '@/shared/components/ActivityTypeChip'
import { formatTransactionAmount } from '@/shared/transactions/amount'
import { formatPortfolioAmount } from '@/features/investments/utils/portfolioCurrency'
import type { Account } from '@/features/accounts/types'
import type { InvestmentActivity } from '../types'
import { formatDate } from '@/shared/transactions/dates'

interface InvestmentDetailDrawerProps {
  transaction: InvestmentActivity | null
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Optional lookup map (account_id → Account) to resolve nickname/name. Falls back to raw id. */
  accountsMap?: Record<string, Account>
}


/**
 * Labeled detail row — a simple label/value pair for the drawer body.
 */
function DetailRow({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <div className="flex items-start justify-between gap-4 py-2 border-b border-border last:border-b-0">
      <span className="text-sm text-muted-foreground min-w-[100px]">{label}</span>
      <span className="text-sm text-foreground text-right">{children}</span>
    </div>
  )
}

/**
 * InvestmentDetailDrawer — read-only right-side Sheet for investment activity detail.
 *
 * Renders null when transaction is null. No edit controls — strictly display only.
 * All PII fields carry data-ph-mask for PostHog session replay masking.
 */
export function InvestmentDetailDrawer({
  transaction,
  open,
  onOpenChange,
  accountsMap,
}: InvestmentDetailDrawerProps) {
  if (!transaction) return null

  const {
    security_name,
    ticker_symbol,
    activity_type,
    investment_quantity,
    investment_price,
    investment_fees,
    currency_code,
    amount,
    account_id,
    date,
  } = transaction

  // Resolution order (T1 — loading-race fix):
  // 1. accountsMap (accounts already loaded — best source)
  // 2. transaction.account (embedded by API — available on cold-load before accountsMap resolves)
  // 3. "Unknown account" placeholder — never show the raw UUID
  const acc = accountsMap?.[account_id]
  const accountDisplay =
    acc?.nickname ??
    acc?.name ??
    transaction.account?.nickname ??
    transaction.account?.name ??
    'Unknown account'

  // Guard against undefined/null to avoid rendering "undefined" (investment fields are optional in API)
  const securityDisplay = security_name ?? '—'
  const tickerDisplay = ticker_symbol ?? '—'
  const quantityDisplay = investment_quantity != null ? `${investment_quantity} shares` : '—'
  // #1631 CX1: price and fees render in the currency the ROW states, not in dollars.
  // formatCurrencyFull hard-coded the dollar, so a GBP trade read "$150.00" — the UI asserting a
  // unit the backend never claimed. `currency_code` is passed straight through so every one of
  // portfolioCurrency's states survives: a named code renders in that currency, absent/null
  // renders as dollars (the pre-000202 legacy base, deliberately unchanged), and a code that
  // fails the three-letter test renders UNLABELLED rather than guessing. `is_mixed_currency` is
  // omitted rather than passed as false: that flag is about an aggregate refusing to name a
  // currency, and a single transaction is in exactly one currency.
  const currencyFields = { currency_code }
  const priceDisplay =
    investment_price != null ? formatPortfolioAmount(investment_price, currencyFields) : '—'
  // #1631: fees is 0.00 on all 1,192 production rows — this institution never populates it. A
  // "Fees: —" row would read as "we confirmed no fee was charged" when the truth is "we don't
  // know", so the row is hidden entirely rather than shown with a placeholder. Naming a currency
  // does not change that: a 0.00 charge is still not worth a line.
  const hasFees = investment_fees != null && investment_fees !== 0
  const feesDisplay = hasFees ? formatPortfolioAmount(investment_fees, currencyFields) : null

  const amountFormatted = formatTransactionAmount(amount)

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right">
        <SheetHeader>
          <SheetTitle>Investment Activity</SheetTitle>
          <SheetDescription data-ph-mask="" data-testid="security-name-header">
            {securityDisplay}
          </SheetDescription>
        </SheetHeader>

        <div className="flex flex-col px-4 pb-4 overflow-y-auto">
          <DetailRow label="Security">
            <span data-ph-mask="" data-testid="security-name-value">
              {securityDisplay}
            </span>
            {' '}
            <span data-ph-mask="" data-testid="ticker-symbol-value" className="text-muted-foreground">
              ({tickerDisplay})
            </span>
          </DetailRow>

          <DetailRow label="Type">
            <ActivityTypeChip activityType={activity_type} size="default" />
          </DetailRow>

          <DetailRow label="Quantity">
            <span data-ph-mask="" data-testid="quantity-value">
              {quantityDisplay}
            </span>
          </DetailRow>

          <DetailRow label="Price/share">
            <span data-ph-mask="" data-testid="price-value">
              {priceDisplay}
            </span>
          </DetailRow>

          {hasFees && (
            <DetailRow label="Fees">
              <span data-ph-mask="" data-testid="fees-value">
                {feesDisplay}
              </span>
            </DetailRow>
          )}

          <DetailRow label="Amount">
            <span
              data-ph-mask=""
              data-testid="amount-value"
              className={amountFormatted.signClass}
            >
              {amountFormatted.text}
            </span>
          </DetailRow>

          <DetailRow label="Account">
            <span data-ph-mask="" data-testid="account-value">
              {accountDisplay}
            </span>
          </DetailRow>

          <DetailRow label="Date">
            <span data-ph-mask="" data-testid="date-value">{formatDate(date)}</span>
          </DetailRow>
        </div>
      </SheetContent>
    </Sheet>
  )
}
