import { useCallback, useMemo, useState } from 'react'
import { useNavigate } from 'react-router'
import { useTrackEvent } from '../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../shared/analytics'
import { useQueryClient } from '@tanstack/react-query'
import { Eye, EyeOff, Landmark, Archive } from 'lucide-react'
import { getAccountTypeLabel, accountMatchesBalanceRange, compareAccountsByBalance } from './utils'
import { toast } from 'sonner'
import { mutationFailureMessage } from '@/features/admin/lib/mutationFailureMessage'
import { EmptyState } from '@/components/ui/empty-state'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { PlaidLinkButton } from './PlaidLinkButton'
import { AccountCard } from './AccountCard'
import { AccountTable, type SortColumn as AccountSortColumn, type SortDirection } from './components/AccountTable'
import type { FilterOption } from '@/shared/components/ColumnFilterDropdown'
import { ReadOnlyGuard } from '@/auth/ReadOnlyGuard'
import { AccountCustomizationModal } from './components/AccountCustomizationModal'
import { ListViewToggle } from '@/shared/components/ListViewToggle'
import type { ListView } from '@/shared/components/ListViewToggle'
import { STORAGE_KEY_LIST_VIEW } from './constants'
import {
  useGetAccounts,
  useDisconnectAccount,
  usePurgeAccount,
  getGetAccountsQueryKey,
} from '@boostedbudget/api-client/endpoints/accounts/accounts'
import { useProfile } from '../profile/hooks/useProfile'
import { useAccountCustomization } from './hooks/useAccountCustomization'
import { useAccountFilters } from './hooks/useAccountFilters'
import type { Account, ExchangePublicTokenResponse, UpdateAccountRequest } from './types'
import { isInactive } from './hooks/useActiveAccounts'
import { useHousehold, ShareNewAccountsDialog } from '@/features/household'

// Get initial view from localStorage
function getInitialView(): ListView {
  try {
    const stored = localStorage.getItem(STORAGE_KEY_LIST_VIEW)
    if (stored === 'cards' || stored === 'table') return stored
  } catch {
    /* ignore localStorage errors */
  }
  return 'cards' // Default to cards
}

// Reusable grid style for responsive account cards layout
const ACCOUNT_GRID_STYLE = { gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))' }

export function AccountsPage() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()

  const [showHidden, setShowHidden] = useState(false)
  // Issue #1569, Task 5D: retained (non-active) accounts have nowhere to be viewed. Mirrors the
  // "Show Hidden" toggle's idiom rather than a new filter-dropdown dimension — both are
  // "excluded from the default view, viewable on demand" concepts, and this page has no existing
  // pattern for a status-based filter dimension (useAccountFilters covers institution/type/
  // balance only).
  const [showClosed, setShowClosed] = useState(false)
  const [view, setView] = useState<ListView>(getInitialView)
  const [selectedAccount, setSelectedAccount] = useState<Account | null>(null)
  const [isModalOpen, setIsModalOpen] = useState(false)
  // Issue #1569, Task 6: the single "delete" dialog is split into two verbs — disconnect (stop
  // syncing, retain history) and purge (permanently destroy). Two separate pieces of state so
  // each dialog's open/closed state and copy are independent.
  const [accountToDisconnect, setAccountToDisconnect] = useState<Account | null>(null)
  const [accountToPurge, setAccountToPurge] = useState<Account | null>(null)
  // Issue #1370 Task 9: post-Plaid-link household share dialog. Dialog
  // visibility and its account list are tracked separately from
  // `isInHousehold` below — the dialog only OPENS when both "in a
  // household" and "at least one just-linked account was found" are true,
  // decided inside handleAccountsLinked.
  const [justLinkedAccounts, setJustLinkedAccounts] = useState<Account[]>([])
  const [isShareDialogOpen, setIsShareDialogOpen] = useState(false)

  const {
    sort,
    setSort,
    institutionFilter,
    typeFilter,
    balanceRange,
    setInstitutionFilter,
    setTypeFilter,
    setBalanceRange,
  } = useAccountFilters()

  const sortColumn = sort?.columnId as AccountSortColumn | undefined
  const sortDirection: SortDirection = sort?.direction ?? 'asc'

  const trackEvent = useTrackEvent()

  // status: 'all_statuses' fetches the full universe (active, replaced, expired, removed) in one
  // request so the "Closed accounts" toggle below can split it locally via isInactive() — see
  // useActiveAccounts.ts. This is distinct from the pre-existing status=all (active+replaced
  // only, silently drops expired/removed).
  const {
    data: accountsData,
    isLoading: accountsLoading,
    error: accountsError,
    refetch: refetchAccounts,
  } = useGetAccounts({ include_hidden: 'true', status: 'all_statuses' })

  const disconnectAccountMutation = useDisconnectAccount()
  const purgeAccountMutation = usePurgeAccount()

  const accounts = accountsData?.accounts ?? []
  const accountsQueryKey = getGetAccountsQueryKey({ include_hidden: 'true', status: 'all_statuses' })

  const { loading: userLoading, refetch: refetchProfile } = useProfile()
  const {
    isInHousehold,
    isLoading: householdLoading,
    error: householdError,
  } = useHousehold()

  // Issue #1569 whole-branch review, minor 1 residual. The purge dialog's household caveat was
  // unconditional, so a solo user — who has no shared totals for anything to drop out of — read a
  // paragraph about a scope they are not in. Gate it, but gate it in the SAFE direction: show it
  // unless we positively know the user is solo.
  //
  // ⚠ The asymmetry is deliberate. `isInHousehold` is false while `useGetMyHousehold` is still in
  // flight and false when it errors, so a plain `isInHousehold &&` would HIDE a true warning about
  // an irreversible action from a household member on a slow or failing request. Suppressing a
  // relevant caveat is the expensive mistake; showing an irrelevant one costs a sentence.
  const knownSolo = !householdLoading && !householdError && !isInHousehold

  const loading = accountsLoading || userLoading
  const error = accountsError ? 'Failed to load accounts. Please try again.' : null

  const {
    updateAccount,
    isUpdating,
  } = useAccountCustomization()

  // `ExchangePublicTokenResponse` never carries the new account ids
  // directly (see types.ts), so the just-linked accounts are resolved by
  // refetching accounts and filtering by `linked_item_id ===
  // response.linked_item_id` — NOT `response.item_id`. `item_id` is the
  // Plaid PROVIDER's item id (`LinkedItem.ItemID` on the backend); an
  // account's `linked_item_id` is the INTERNAL `linked_items.id` (set from
  // `linkedItem.ID` in account_service.go). These are different values, so
  // matching on `item_id` would never find the just-linked accounts in
  // production. Filtering by `linked_item_id` (rather than diffing "accounts
  // that are newly present") also avoids over-including an unrelated account
  // linked concurrently by another tab/session.
  // Wrapped in try/catch so this async function's returned Promise never
  // rejects: `PlaidLinkButton.tsx`'s `onSuccess?.(...)` call is
  // fire-and-forget (not awaited, no `.catch()`), and by the time it fires
  // the bank link itself has already succeeded — a failure refreshing the
  // accounts list / opening the share dialog afterward is a distinct,
  // lower-severity problem that must not surface as an unhandled promise
  // rejection, nor be misattributed as a "bank connection failed" error
  // (which is what PlaidLinkButton's own try/catch messaging is for).
  async function handleAccountsLinked(response: ExchangePublicTokenResponse) {
    try {
      const result = await refetchAccounts()
      refetchProfile()

      if (!isInHousehold) return

      const freshAccounts = result.data?.accounts ?? []
      const linkedAccounts = freshAccounts.filter(
        (account) => account.linked_item_id === response.linked_item_id
      )
      if (linkedAccounts.length > 0) {
        setJustLinkedAccounts(linkedAccounts)
        setIsShareDialogOpen(true)
      }
    } catch (err) {
      // The accounts list will catch up on the next natural refetch (e.g.
      // revisiting this page) — logging is sufficient here since the link
      // itself already succeeded.
      console.error('Failed to refresh accounts after linking:', err)
    }
  }

  async function handleDisconnectAccount(account: Account) {
    try {
      await disconnectAccountMutation.mutateAsync({ id: account.id })
      trackEvent(ANALYTICS_EVENTS.ACCOUNT_REMOVED)
      setAccountToDisconnect(null)
      await refetchAccounts()
    } catch (err) {
      console.error('Failed to disconnect account:', err)
      toast.error('Failed to disconnect account. Please try again.')
    }
  }

  async function handlePurgeAccount(account: Account) {
    try {
      await purgeAccountMutation.mutateAsync({ id: account.id })
      trackEvent(ANALYTICS_EVENTS.ACCOUNT_PURGED)
      setAccountToPurge(null)
      await refetchAccounts()
    } catch (err) {
      console.error('Failed to purge account:', err)
      // Issue #1569, Task 6: a savings goal referencing this account surfaces as a 409 whose
      // actionable text (repository.ErrPurgeBlockedBySavingsGoal's mapped message) lives at
      // `err.response.data.detail`, NOT `err.message` (axios's `err.message` for an HTTP error is
      // the useless "Request failed with status code 409"). mutationFailureMessage is the existing
      // shared extractor for this exact RFC 9457 shape (already used by the DLQ and Plaid-orphan
      // admin surfaces) — reused rather than reimplemented so this bug class can't recur here.
      toast.error(mutationFailureMessage(err))
    }
  }

  // DISCONNECT ONLY (Issue #1569 F5). This counts ACTIVE accounts under the item and asks whether
  // the one being acted on is the last of them — which is only meaningful when that account is
  // itself still active, i.e. on the disconnect path. It mirrors the backend's own trigger for
  // stopping the Plaid item: accountService.cleanupAfterAccountRemoval schedules item cleanup when
  // CountActiveByLinkedItemID drops to zero. Do NOT reuse it for the purge dialog: a purge target
  // is by definition inactive, so it is never in this count, and purging it cannot change the
  // active count at all.
  function isLastAccountForItem(account: Account): boolean {
    const allAccounts = accountsData?.accounts ?? []
    const activeAccountsForItem = allAccounts.filter(
      a => a.linked_item_id === account.linked_item_id && !isInactive(a)
    )
    return activeAccountsForItem.length === 1
  }

  function buildDisconnectDescription(account: Account): React.ReactNode {
    const displayName = account.nickname || account.official_name || account.name
    const maskPart = account.mask ? ` (...${account.mask})` : ''
    const isLast = isLastAccountForItem(account)
    return (
      <span>
        <strong data-ph-mask>{displayName}{maskPart}</strong> will stop syncing new transactions and balances.
        <br />Your transactions, balance history, and account details are all retained — you can still view this account under Closed accounts.
        {isLast && (
          <>
            <br />The bank connection to <strong data-ph-mask>{account.institution_name}</strong> will also be disconnected.
          </>
        )}
      </span>
    )
  }

  // Issue #1569, Task 6: purge is the ONE place a hard delete is correct, so the dialog must say
  // exactly what is destroyed and what survives — including the multi-member-chain case. DECISION:
  // a purge is allowed on a multi-member chain (a re-linked/replaced account) rather than refused,
  // because MatchOrCreate already keys transactions by the chain's user_account_id independently of
  // any single account row — a surviving chain member keeps that key, so the purged account's
  // transactions continue to appear under it automatically. The dialog states this rather than
  // leaving it implied.
  //
  // The savings-goal sentences are BOTH load-bearing after #1569 task 21 (C1), and they say
  // different things. A LIVE goal still blocks the purge, so that sentence is advice. A goal the
  // user has ALREADY deleted no longer blocks it — the purge destroys the tombstone and its
  // hand-logged contributions and says nothing, because a tombstoned goal is invisible in every
  // view. That is the only destruction here the user gets no second prompt for, which is exactly
  // why it belongs in the enumeration and not only in the advice.
  //
  // ⚠ THE HOUSEHOLD QUALIFICATION IS NOT HEDGING (#1569 whole-branch review, minor 1). "They
  // remain visible" was unqualified and false in household scope: every solo analytics read is
  // `LEFT JOIN accounts`, every household read is `JOIN accounts`. After a purge
  // `transactions.account_id` is NULL, so the rows stay in personal analytics and drop out of
  // household-scoped spending, income, cash-flow and savings-rate. That is semantically forced —
  // the share cannot be resolved without the account row — but it is a real consequence of
  // pressing this button, so it is stated rather than discovered.
  //
  // It is now scoped to `!knownSolo` (see that binding for why it is "not known solo" rather than
  // "in a household") — the sentence shipped unconditional, which put a household paragraph in
  // front of solo users. Everything else in this dialog stays unconditional, because everything
  // else in it is true of every user.
  //
  // ⚠ THE INVESTMENT SENTENCE IS UNCONDITIONAL, and that is a #1569 T8 correction, not sloppiness.
  // It used to be gated on `account.type === 'investment'`, while the thing it describes —
  // fk_inv_txn_account_id, ON DELETE CASCADE — does not consult type at all. `brokerage` is NOT
  // a value of `accounts.type` (that enum, domain/account.go:~237-243, has no `brokerage` — only
  // depository, credit, loan, investment, other). `brokerage` lives only on `user_accounts.
  // primary_type` (domain/user_account.go:~100-111; :84-88 there is the unrelated hex-colour
  // validator). The cascade this sentence describes is keyed off `accounts.type`, so the
  // "brokerage account" example below is really about a `type: 'investment'` account carrying a
  // `primary_type: 'brokerage'` subtype — still true, but not because `brokerage` is a value of
  // the enum this sentence used to gate on. A brokerage-subtyped account holding investment
  // transactions got no warning that the purge would destroy them. Rather than enumerate which
  // types can hold them — a list that goes stale the next time Plaid adds one — the sentence now
  // says "any investment transactions recorded against it", which is true of every account and
  // matches exactly what the cascade does.
  function buildPurgeDescription(account: Account): React.ReactNode {
    const displayName = account.nickname || account.official_name || account.name
    const maskPart = account.mask ? ` (...${account.mask})` : ''
    return (
      <span>
        You are about to permanently delete <strong data-ph-mask>{displayName}{maskPart}</strong> and all of its balance history, holdings, liabilities, and any savings goal you have already deleted that was tracking it (including the contributions you logged by hand against it).
        <br /><strong>This cannot be undone.</strong>
        <br /><strong>Any investment transactions recorded against it (buys, sells, dividends) will also be permanently deleted</strong> — unlike bank transactions, they have no independent existence apart from the account.
        <br />Your bank transactions will NOT be deleted — if another connection to this same account exists, they will continue to appear there; otherwise they stay in your own spending and reports with no linked account.{!knownSolo && (
          <> <strong>In a household, they stop counting towards shared totals</strong> — household views resolve every transaction through its account, and this one no longer has one.</>
        )}
        <br />If a savings goal still tracks this account, the purge will be refused until you delete or re-link that goal. <strong>Deleting the goal first means it is destroyed here too</strong> — along with every contribution you logged by hand against it.
      </span>
    )
  }

  const handleEditAccount = useCallback((account: Account) => {
    setSelectedAccount(account)
    setIsModalOpen(true)
  }, []) // No deps — setters are stable


  // Issue #1569: institution/type picker options are built from ACTIVE accounts only — a closed
  // account must not pad any picker, even while "Show closed accounts" is on.
  const institutionOptions = useMemo<FilterOption[]>(() => {
    const accs = (accountsData?.accounts ?? []).filter(acc => !isInactive(acc))
    const institutions = Array.from(
      new Set(accs.map((a) => a.institution_name).filter((x): x is string => x != null && x !== ''))
    )
    return institutions
      .sort()
      .map((inst) => ({
        value: inst,
        label: inst,
        masked: true,
      }))
  }, [accountsData?.accounts])

  const typeOptions = useMemo<FilterOption[]>(() => {
    const accs = (accountsData?.accounts ?? []).filter(acc => !isInactive(acc))
    // Map from typeValue (subtype || type) → { type, subtype } for label generation
    const seen = new Map<string, { type: string; subtype?: string }>()
    accs.forEach((acc) => {
      // Use subtype if available, otherwise type
      const typeValue = acc.subtype || acc.type
      if (typeValue && !seen.has(typeValue)) {
        seen.set(typeValue, { type: acc.type, subtype: acc.subtype ?? undefined })
      }
    })
    return Array.from(seen.entries())
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([typeValue, { type, subtype }]) => ({
        value: typeValue,
        label: getAccountTypeLabel(type, subtype, { surface: 'table' }),
      }))
  }, [accountsData?.accounts])

  const handleSaveAccount = useCallback(async (account: Account, changes: Partial<UpdateAccountRequest>) => {
    const updatedAccount = await updateAccount(account, changes)
    if (!updatedAccount) return

    // Optimistically update the query cache
    queryClient.setQueryData(accountsQueryKey, (oldData: { accounts: Account[] } | undefined) => {
      if (!oldData) return oldData
      return {
        ...oldData,
        accounts: oldData.accounts.map(acc =>
          acc.id === account.id ? updatedAccount : acc
        ),
      }
    })
  }, [updateAccount, queryClient, accountsQueryKey])

  const displayedAccounts = useMemo(() => {
    const accs = accountsData?.accounts ?? []
    let filtered = accs

    // Closed (non-active) accounts are excluded unless the "Show closed accounts" toggle is on
    // (Issue #1569, Task 5D) — never padding the default active list.
    if (!showClosed) {
      filtered = filtered.filter(acc => !isInactive(acc))
    }

    // Filter hidden accounts unless showHidden is true
    if (!showHidden) {
      filtered = filtered.filter(acc => !acc.is_hidden)
    }

    if (institutionFilter.length > 0) {
      filtered = filtered.filter(acc =>
        acc.institution_name && institutionFilter.includes(acc.institution_name)
      )
    }

    if (typeFilter.length > 0) {
      filtered = filtered.filter(acc => {
        const typeValue = acc.subtype || acc.type
        return typeValue && typeFilter.includes(typeValue)
      })
    }

    // Issue #1580: a closed account carries no current balance, so it matches no bounded range.
    // See accountMatchesBalanceRange — the previous `?? 0` here got BOTH directions wrong.
    if (balanceRange) {
      filtered = filtered.filter(acc => accountMatchesBalanceRange(acc, balanceRange))
    }

    if (sortColumn) {
      return [...filtered].sort((a, b) => {
        let comparison = 0

        switch (sortColumn) {
          case 'institution': {
            const instA = (a.institution_name || '').toLowerCase()
            const instB = (b.institution_name || '').toLowerCase()
            comparison = instA.localeCompare(instB)
            break
          }
          case 'name': {
            const nameA = (a.nickname || a.name || '').toLowerCase()
            const nameB = (b.nickname || b.name || '').toLowerCase()
            comparison = nameA.localeCompare(nameB)
            break
          }
          case 'type': {
            const typeA = (a.subtype || a.type || '').toLowerCase()
            const typeB = (b.subtype || b.type || '').toLowerCase()
            comparison = typeA.localeCompare(typeB)
            break
          }
          // Issue #1580: balance-less (closed) accounts form their own group, always last, in
          // BOTH directions — so this case returns outright rather than falling through to the
          // `sortDirection` negation below, which would flip the grouping too.
          case 'balance':
            return compareAccountsByBalance(a, b, sortDirection)
        }

        return sortDirection === 'asc' ? comparison : -comparison
      })
    }

    // Default sort: by display_order (nulls last), then created_at
    return [...filtered].sort((a, b) => {
      const orderA = a.display_order ?? Number.MAX_SAFE_INTEGER
      const orderB = b.display_order ?? Number.MAX_SAFE_INTEGER

      if (orderA !== orderB) {
        return orderA - orderB
      }

      // Fallback to created_at
      return new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
    })
  }, [accountsData?.accounts, showHidden, showClosed, sortColumn, sortDirection, institutionFilter, typeFilter, balanceRange])

  const { hiddenCount, activeAccountsCount, closedCount } = useMemo(() => {
    const accs = accountsData?.accounts ?? []
    const activeAccounts = accs.filter(acc => !isInactive(acc))
    return {
      hiddenCount: activeAccounts.filter(acc => acc.is_hidden).length,
      activeAccountsCount: activeAccounts.length,
      // Issue #1569, Task 5D: never counted toward activeAccountsCount — the "N accounts"
      // header must keep meaning "N active accounts", not include retained history.
      closedCount: accs.filter(isInactive).length,
    }
  }, [accountsData?.accounts])

  if (error) {
    return (
      <div className="p-8 max-w-[1200px] mx-auto">
        <div className="text-center py-12 px-4">
          <p className="text-destructive mb-4 text-lg">{error}</p>
          <button
            onClick={() => refetchAccounts()}
            className="px-6 py-3 text-base bg-primary text-primary-foreground border-none rounded cursor-pointer hover:bg-primary/90 transition-colors"
          >
            Retry
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="p-8 max-w-[1200px] mx-auto max-sm:p-4">
      <div className="flex justify-between items-center mb-8 max-sm:flex-col max-sm:items-start max-sm:gap-4">
        <h1 className="m-0 text-3xl text-foreground">My Accounts</h1>
        <div className="flex items-center gap-3 max-sm:w-full max-sm:flex-wrap">
          {hiddenCount > 0 && (
            <button
              className={`inline-flex items-center gap-1.5 px-4 py-2 text-sm font-medium rounded border cursor-pointer transition-colors focus:outline-none focus:ring-2 focus:ring-primary/50 ${showHidden ? 'bg-info/10 border-primary text-primary' : 'bg-muted text-foreground border-border hover:bg-accent'}`}
              onClick={() => setShowHidden(!showHidden)}
              aria-pressed={showHidden}
            >
              {showHidden ? (
                <>
                  <EyeOff className="h-4 w-4" />
                  <span>Hide Hidden ({hiddenCount})</span>
                </>
              ) : (
                <>
                  <Eye className="h-4 w-4" />
                  <span>Show Hidden ({hiddenCount})</span>
                </>
              )}
            </button>
          )}
          {closedCount > 0 && (
            <button
              className={`inline-flex items-center gap-1.5 px-4 py-2 text-sm font-medium rounded border cursor-pointer transition-colors focus:outline-none focus:ring-2 focus:ring-primary/50 ${showClosed ? 'bg-info/10 border-primary text-primary' : 'bg-muted text-foreground border-border hover:bg-accent'}`}
              onClick={() => setShowClosed(!showClosed)}
              aria-pressed={showClosed}
            >
              <Archive className="h-4 w-4" />
              <span>{showClosed ? 'Hide' : 'Show'} closed accounts ({closedCount})</span>
            </button>
          )}
          <ListViewToggle
            view={view}
            onChange={setView}
            storageKey={STORAGE_KEY_LIST_VIEW}
          />
          <ReadOnlyGuard>
            <PlaidLinkButton
              onSuccess={handleAccountsLinked}
            />
          </ReadOnlyGuard>
        </div>
      </div>

      {!loading && accounts.length === 0 ? (
        <EmptyState
          icon={<Landmark />}
          title="Connect your first bank account"
          description="Link your bank to automatically track balances, transactions, and spending — all in one place."
          action={
            <ReadOnlyGuard>
              <PlaidLinkButton
                onSuccess={handleAccountsLinked}
              />
            </ReadOnlyGuard>
          }
          className="mt-8"
        />
      ) : (
        <div>
          <p className="text-base text-muted-foreground mb-4">
            {activeAccountsCount} {activeAccountsCount === 1 ? 'account' : 'accounts'}
            {hiddenCount > 0 && !showHidden && (
              <span className="text-muted-foreground italic"> ({hiddenCount} hidden)</span>
            )}
          </p>

          {view === 'table' ? (
            <AccountTable
              accounts={displayedAccounts}
              loading={loading}
              onEdit={handleEditAccount}
              onDisconnect={(account) => setAccountToDisconnect(account)}
              onPurge={(account) => setAccountToPurge(account)}
              onRowClick={(account) => navigate(`/accounts/${account.id}`)}
              sort={sort}
              onSortChange={setSort}
              institutionOptions={institutionOptions}
              institutionFilter={institutionFilter}
              onInstitutionFilterChange={setInstitutionFilter}
              typeOptions={typeOptions}
              typeFilter={typeFilter}
              onTypeFilterChange={setTypeFilter}
              balanceRange={balanceRange}
              onBalanceRangeChange={setBalanceRange}
            />
          ) : loading && displayedAccounts.length === 0 ? (
            <div role="status">
              <span className="sr-only">Loading accounts…</span>
              <div className="grid gap-6 max-sm:grid-cols-1" style={ACCOUNT_GRID_STYLE} aria-hidden="true">
                {Array.from({ length: 4 }).map((_, i) => (
                  <div key={i} className="h-48 rounded-lg bg-skeleton motion-safe:animate-pulse motion-reduce:animate-none" />
                ))}
              </div>
            </div>
          ) : (
            <div className="grid gap-6 max-sm:grid-cols-1" style={ACCOUNT_GRID_STYLE}>
              {displayedAccounts.map((account) => (
                <AccountCard
                  key={account.id}
                  account={account}
                  onDisconnect={(acc) => setAccountToDisconnect(acc)}
                  onPurge={(acc) => setAccountToPurge(acc)}
                  onEdit={handleEditAccount}
                />
              ))}
            </div>
          )}
        </div>
      )}

      <AccountCustomizationModal
        account={selectedAccount}
        open={isModalOpen}
        onOpenChange={setIsModalOpen}
        onSave={handleSaveAccount}
        isSaving={isUpdating}
      />

      {/* Issue #1569, Task 6: disconnect is the primary, non-destructive action — no type-to-confirm,
          since nothing is destroyed and the account remains fully visible under Closed accounts. */}
      <ConfirmationDialog
        open={accountToDisconnect !== null}
        onOpenChange={(open) => { if (!open) setAccountToDisconnect(null) }}
        onConfirm={() => accountToDisconnect && handleDisconnectAccount(accountToDisconnect)}
        title="Disconnect account"
        description={accountToDisconnect ? buildDisconnectDescription(accountToDisconnect) : ''}
        variant="warning"
        confirmButtonText="Disconnect"
        loading={disconnectAccountMutation.isPending}
      />

      {/* Purge is the destructive secondary action — type-to-confirm, reusing the pattern above,
          with copy stating exactly what is destroyed and that it cannot be undone. */}
      <ConfirmationDialog
        open={accountToPurge !== null}
        onOpenChange={(open) => { if (!open) setAccountToPurge(null) }}
        onConfirm={() => accountToPurge && handlePurgeAccount(accountToPurge)}
        title="Permanently delete account"
        description={accountToPurge ? buildPurgeDescription(accountToPurge) : ''}
        confirmText={accountToPurge ? (accountToPurge.nickname || accountToPurge.official_name || accountToPurge.name) : ''}
        loading={purgeAccountMutation.isPending}
      />

      <ShareNewAccountsDialog
        open={isShareDialogOpen}
        onOpenChange={setIsShareDialogOpen}
        accounts={justLinkedAccounts}
      />
    </div>
  )
}
