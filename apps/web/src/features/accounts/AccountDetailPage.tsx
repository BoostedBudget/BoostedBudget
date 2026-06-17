/**
 * AccountDetailPage
 *
 * Per-account detail page at /accounts/:accountId.
 * Fetches the full account list and finds the account by id (no single-account endpoint).
 *
 * - Shows AccountSummaryHeader for all account types
 * - Shows Holdings section + activity link for investment accounts only
 * - Fires analytics on mount and on activity link click
 *
 * Issue #1298: Investment account holdings detail
 */

import { useEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import { ArrowLeft, AlertCircle } from 'lucide-react'
import { useGetAccounts } from '@boostedbudget/api-client/endpoints/accounts/accounts'
import { AccountSummaryHeader } from './components/AccountSummaryHeader'
import { getAccountTypeLabel } from './utils'
import { HoldingsTable } from '../investments/components/HoldingsTable'
import { NetWorthSection } from '../net-worth/components/NetWorthSection'
import { EmptyState } from '@/components/ui/empty-state'
import { ErrorState } from '@/components/ui/error-state'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'

const BackLink = ({ to = '/accounts', label = 'Back to Accounts' }: { to?: string; label?: string }) => (
  <Link
    to={to}
    className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
  >
    <ArrowLeft className="h-4 w-4" />
    {label}
  </Link>
)

const PageContainer = ({ children }: { children: React.ReactNode }) => (
  <div className="max-w-[1200px] mx-auto p-6">{children}</div>
)

export function AccountDetailPage() {
  const { accountId } = useParams<{ accountId: string }>()
  const trackEvent = useTrackEvent()

  const { data, isLoading, isError, refetch } = useGetAccounts({ include_hidden: 'true' })
  const account = data?.accounts?.find((a) => a.id === accountId)

  useEffect(() => {
    if (!account) return
    trackEvent(ANALYTICS_EVENTS.ACCOUNT_DETAIL_VIEWED, {
      account_id: account.id,
      account_type: account.type,
    })
    // trackEvent is stable (useCallback with empty deps in useTrackEvent)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [account?.id])

  if (isLoading) {
    return (
      <PageContainer>
        <div className="animate-pulse space-y-4">
          <div className="h-4 w-32 rounded bg-muted" />
          <div className="h-8 w-64 rounded bg-muted" />
          <div className="h-4 w-full rounded bg-muted" />
          <div className="h-4 w-3/4 rounded bg-muted" />
          <div className="h-32 w-full rounded bg-muted" />
        </div>
      </PageContainer>
    )
  }

  if (isError) {
    return (
      <PageContainer>
        <ErrorState title="Failed to load account" onRetry={refetch} />
      </PageContainer>
    )
  }

  if (!account) {
    return (
      <PageContainer>
        <EmptyState
          icon={<AlertCircle />}
          title="Account not found"
          description="We couldn't find an account with this ID."
          action={<BackLink to="/accounts" />}
        />
      </PageContainer>
    )
  }

  const displayName = account.nickname ?? account.official_name ?? account.name
  const isInvestment = account.type === 'investment'

  return (
    <PageContainer>
      <div className="flex flex-col gap-6">
        {/* Back link */}
        <BackLink />

        {/* Account header */}
        <AccountSummaryHeader
          institutionName={account.institution_name ?? ''}
          mask={account.mask}
          displayName={displayName}
          typeLabel={getAccountTypeLabel(account.type, account.subtype, { surface: 'card' })}
          currentBalance={account.current_balance}
          availableBalance={account.available_balance}
          currencyCode={account.currency_code}
        />

        {/* Investment-only sections */}
        {isInvestment && (
          <>
            <NetWorthSection title="Holdings">
              <HoldingsTable accountId={account.id} />
            </NetWorthSection>

            <Link
              to={`/transactions?tab=investment-activity&account_id=${account.id}`}
              className="inline-flex items-center gap-1.5 text-sm text-primary hover:underline"
              onClick={() =>
                trackEvent(ANALYTICS_EVENTS.ACCOUNT_INVESTMENT_ACTIVITY_CLICKED, {
                  account_id: account.id,
                  account_type: account.type,
                })
              }
            >
              View investment activity →
            </Link>
          </>
        )}
      </div>
    </PageContainer>
  )
}
