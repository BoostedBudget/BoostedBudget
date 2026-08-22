import { useCallback, useMemo, useState } from 'react'
import { useNavigate } from 'react-router'
import { useTrackEvent } from '../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../shared/analytics'
import { useQueryClient } from '@tanstack/react-query'
import { Eye, EyeOff, Landmark, Archive } from 'lucide-react'
import { getAccountTypeLabel } from './utils'
import { toast } from 'sonner'
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
  useDeleteAccount,
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
  const [accountToDelete, setAccountToDelete] = useState<Account | null>(null)
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

  const deleteAccountMutation = useDeleteAccount()

  const accounts = accountsData?.accounts ?? []
  const accountsQueryKey = getGetAccountsQueryKey({ include_hidden: 'true', status: 'all_statuses' })

  const { loading: userLoading, refetch: refetchProfile } = useProfile()
  const { isInHousehold } = useHousehold()

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

  async function handleDeleteAccount(account: Account) {
    try {
      await deleteAccountMutation.mutateAsync({ id: account.id })
      trackEvent(ANALYTICS_EVENTS.ACCOUNT_REMOVED)
      setAccountToDelete(null)
      await refetchAccounts()
    } catch (err) {
      console.error('Failed to delete account:', err)
      toast.error('Failed to delete account. Please try again.')
    }
  }

  function isLastAccountForItem(account: Account): boolean {
    const allAccounts = accountsData?.accounts ?? []
    const activeAccountsForItem = allAccounts.filter(
      a => a.linked_item_id === account.linked_item_id && !isInactive(a)
    )
    return activeAccountsForItem.length === 1
  }

  function buildDeleteDescription(account: Account): React.ReactNode {
    const displayName = account.nickname || account.official_name || account.name
    const maskPart = account.mask ? ` (...${account.mask})` : ''
    const isLast = isLastAccountForItem(account)
    return (
      <span>
        You are about to permanently delete <strong data-ph-mask>{displayName}{maskPart}</strong>.
        <br />Your transactions will be preserved.
        {/* Issue #1569: DeleteAccount now soft-removes — the row, its balance_snapshots,
            holdings, and liabilities all survive. "Balance history will be removed" was false
            the moment that shipped. */}
        <br />Your balance history will be retained — you can still view this account under Closed accounts.
        {isLast && (
          <>
            <br />The bank connection to <strong data-ph-mask>{account.institution_name}</strong> will also be removed.
          </>
        )}
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

    if (balanceRange) {
      filtered = filtered.filter(acc => {
        const balance = acc.current_balance ?? 0
        if (balanceRange.min !== undefined && balance < balanceRange.min) return false
        if (balanceRange.max !== undefined && balance > balanceRange.max) return false
        return true
      })
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
          case 'balance': {
            const balA = a.current_balance ?? 0
            const balB = b.current_balance ?? 0
            comparison = balA - balB
            break
          }
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
              onDelete={(account) => setAccountToDelete(account)}
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
                  onDelete={(acc) => setAccountToDelete(acc)}
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

      <ConfirmationDialog
        open={accountToDelete !== null}
        onOpenChange={(open) => { if (!open) setAccountToDelete(null) }}
        onConfirm={() => accountToDelete && handleDeleteAccount(accountToDelete)}
        title="Delete Account"
        description={accountToDelete ? buildDeleteDescription(accountToDelete) : ''}
        confirmText={accountToDelete ? (accountToDelete.nickname || accountToDelete.official_name || accountToDelete.name) : ''}
        loading={deleteAccountMutation.isPending}
      />

      <ShareNewAccountsDialog
        open={isShareDialogOpen}
        onOpenChange={setIsShareDialogOpen}
        accounts={justLinkedAccounts}
      />
    </div>
  )
}
