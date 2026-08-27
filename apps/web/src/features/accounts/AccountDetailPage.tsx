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
import { useParams, Link } from 'react-router'
import { ArrowLeft, AlertCircle } from 'lucide-react'
import { useGetAccounts } from '@boostedbudget/api-client/endpoints/accounts/accounts'
import { AccountSummaryHeader } from './components/AccountSummaryHeader'
import { getAccountTypeLabel, getAccountClosedDate, getAccountLastKnownBalance } from './utils'
import { isInactive } from './hooks/useActiveAccounts'
import { ClosedAccountBalanceHistory } from './components/ClosedAccountBalanceHistory'
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

  // status: 'all_statuses' — NOT the server's `active` default. Issue #1569 task 21 (F4): the
  // Closed accounts view renders an AccountCard per terminal-status account, and AccountCard links
  // unconditionally to this page. Without this parameter the account is simply absent from the
  // response and every one of those cards led to "Account not found." Retention the user cannot
  // open is retention nobody can act on, which is the failure this whole issue is about.
  //
  // It must match AccountsPage's own query key ({ include_hidden: 'true', status: 'all_statuses' })
  // so the two share one cache entry rather than issuing two requests for overlapping universes.
  const { data, isLoading, isError, refetch } = useGetAccounts({
    include_hidden: 'true',
    status: 'all_statuses',
  })
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
  // Issue #1591: the pair is resolved once, by the helper that owns the "both or neither" rule, so
  // the page never decides on its own whether a half-pair is renderable.
  const lastKnown = getAccountLastKnownBalance(account)
  // Issue #1569 T7.8 — this gate also decides whether a CLOSED account can reach its retained
  // holdings history, so it was re-derived rather than inherited. It stays type-based, and here is
  // why, so the next reader does not have to re-derive it either:
  //
  //   - `accounts.type` is CHECK-constrained to five values — depository, credit, loan,
  //     investment, other (migration 000007, `valid_account_type`, still the only definition).
  //     `brokerage` is NOT one of them: that value belongs to `user_accounts.primary_type`, a
  //     different column on a different table, which is what the purge-copy comment on
  //     AccountsPage is about. So "some other type holds positions" is a narrower claim than it
  //     looks.
  //   - The column is write-once. It is set from Plaid at insert (account_service.go) and no
  //     UPDATE in account_repository.go touches it, so a captured account cannot drift out of the
  //     gate after its history was written.
  //   - `holding_snapshots` rows come only from holdingRepository.ReplaceForAccount, whose only
  //     caller is the /investments/holdings/get sync — and Plaid returns holdings only for
  //     investment accounts. None of the other four types can carry a retained position.
  //
  // The alternative — gate on the retained data existing — was rejected on cost, not on taste: it
  // would mount the history query on EVERY account detail page (a depository account included) to
  // discover a row that provably is not there, and the investments route has 0.13 kB of its 7 kB
  // gzip budget left. If accounts.type ever gains a value that can hold positions, this gate must
  // be revisited — TestAccountTypeUniverseMatchesTheHoldingsGate (backend) fails when it does.
  const isInvestment = account.type === 'investment'
  const isClosed = isInactive(account)

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
          customEmoji={account.custom_emoji}
          customIcon={account.custom_icon}
          customColor={account.custom_color}
          isClosed={isClosed}
          closedOn={getAccountClosedDate(account)}
          lastKnownBalance={lastKnown?.amount}
          lastKnownBalanceAt={lastKnown?.at}
        />

        {/*
          Retained balance history — Issue #1569, and EVERY account type, not just investments.
          Privacy policy §8.2a promises a removed account's balance history is retained "so you can
          still view it", and until now the only per-account balance on this page was the single
          last-known figure in the header above (#1591). The day-by-day rows existed and were
          reachable from nothing.

          Rendered only for a CLOSED account on purpose: a live account's balances are answered by
          the dashboard's aggregated series and by its own current balance, and putting a
          historical table beside a live figure is exactly the two-universes confusion the rest of
          #1569 removes. The section also mounts the query, so a live account pays nothing.
        */}
        {isClosed && (
          <NetWorthSection title="Balance history">
            <ClosedAccountBalanceHistory
              accountId={account.id}
              accountType={account.type}
              closedOn={getAccountClosedDate(account)}
            />
          </NetWorthSection>
        )}

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
