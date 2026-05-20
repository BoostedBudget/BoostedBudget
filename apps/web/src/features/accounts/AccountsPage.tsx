import { useCallback, useMemo, useState } from 'react'
import { useTrackEvent } from '../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../shared/analytics'
import { useQueryClient } from '@tanstack/react-query'
import { Eye, EyeOff, Landmark } from 'lucide-react'
import { toast } from 'sonner'
import { EmptyState } from '@/components/ui/empty-state'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { PlaidLinkButton } from './PlaidLinkButton'
import { AccountCard } from './AccountCard'
import { AccountTable, type SortColumn as AccountSortColumn, type SortDirection } from './components/AccountTable'
import type { FilterOption } from '@/shared/components/ColumnFilterDropdown'
import { UpgradeBanner } from './UpgradeBanner'
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
import { useSubscriptionTiers } from '../subscription/hooks/useSubscriptionTiers'
import { useTierLimits } from '../subscription/hooks/useTierLimits'
import { useAccountCustomization } from './hooks/useAccountCustomization'
import { useAccountFilters } from './hooks/useAccountFilters'
import type { Account, UpdateAccountRequest } from './types'
import { isReplaced } from './hooks/useActiveAccounts'

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

export function AccountsPage() {
  const queryClient = useQueryClient()

  const [showHidden, setShowHidden] = useState(false)
  const [view, setView] = useState<ListView>(getInitialView)
  const [selectedAccount, setSelectedAccount] = useState<Account | null>(null)
  const [isModalOpen, setIsModalOpen] = useState(false)
  const [accountToDelete, setAccountToDelete] = useState<Account | null>(null)

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

  const {
    data: accountsData,
    isLoading: accountsLoading,
    error: accountsError,
    refetch: refetchAccounts,
  } = useGetAccounts({ include_hidden: 'true' })

  const deleteAccountMutation = useDeleteAccount()

  const accounts = accountsData?.accounts ?? []
  const accountsQueryKey = getGetAccountsQueryKey({ include_hidden: 'true' })

  const { user, loading: userLoading, refetch: refetchProfile } = useProfile()
  const { tiers, loading: tiersLoading } = useSubscriptionTiers()

  const loading = accountsLoading || userLoading
  const error = accountsError ? 'Failed to load accounts. Please try again.' : null

  const {
    updateAccount,
    isUpdating,
  } = useAccountCustomization()

  function handleAccountsLinked() {
    refetchAccounts()
    refetchProfile()
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
      a => a.linked_item_id === account.linked_item_id && !isReplaced(a)
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
        <br />Balance history will be removed.
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
  }, [])

  const handleSort = useCallback((column: AccountSortColumn, direction: SortDirection) => {
    setSort({ columnId: column, direction })
  }, [setSort])

  const institutionOptions = useMemo<FilterOption[]>(() => {
    const accs = accountsData?.accounts ?? []
    const institutions = new Set<string>()
    accs.forEach((acc) => {
      if (acc.institution_name) {
        institutions.add(acc.institution_name)
      }
    })
    return Array.from(institutions)
      .sort()
      .map((inst) => ({
        value: inst,
        label: inst,
        masked: true,
      }))
  }, [accountsData?.accounts])

  const typeOptions = useMemo<FilterOption[]>(() => {
    const accs = accountsData?.accounts ?? []
    const types = new Set<string>()
    accs.forEach((acc) => {
      // Use subtype if available, otherwise type
      const typeValue = acc.subtype || acc.type
      if (typeValue) {
        types.add(typeValue)
      }
    })
    return Array.from(types)
      .sort()
      .map((type) => ({
        value: type,
        label: type.charAt(0).toUpperCase() + type.slice(1),
      }))
  }, [accountsData?.accounts])

  const handleSaveAccount = useCallback(async (account: Account, changes: Partial<UpdateAccountRequest>) => {
    const updatedAccount = await updateAccount(account, changes)
    if (updatedAccount) {
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
    }
  }, [updateAccount, queryClient, accountsQueryKey])

  const displayedAccounts = useMemo(() => {
    const accs = accountsData?.accounts ?? []
    let filtered = accs

    filtered = filtered.filter(acc => !isReplaced(acc))

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
  }, [accountsData?.accounts, showHidden, sortColumn, sortDirection, institutionFilter, typeFilter, balanceRange])

  const { hiddenCount, activeAccountsCount } = useMemo(() => {
    const accs = accountsData?.accounts ?? []
    const activeAccounts = accs.filter(acc => !isReplaced(acc))
    return {
      hiddenCount: activeAccounts.filter(acc => acc.is_hidden).length,
      activeAccountsCount: activeAccounts.length,
    }
  }, [accountsData?.accounts])

  const { isAtLimit, shouldShowUpgradeBanner, accountCountDisplay } = useTierLimits({
    user,
    tiers,
    currentAccountCount: activeAccountsCount,
  })

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
          <ListViewToggle
            view={view}
            onChange={setView}
            storageKey={STORAGE_KEY_LIST_VIEW}
          />
          <ReadOnlyGuard>
            <PlaidLinkButton
              onSuccess={handleAccountsLinked}
              disabled={isAtLimit}
            />
          </ReadOnlyGuard>
        </div>
      </div>

      {shouldShowUpgradeBanner && <UpgradeBanner />}

      {!(loading || tiersLoading) && accounts.length === 0 ? (
        <EmptyState
          icon={<Landmark />}
          title="Connect your first bank account"
          description="Link your bank to automatically track balances, transactions, and spending — all in one place."
          action={
            <ReadOnlyGuard>
              <PlaidLinkButton
                onSuccess={handleAccountsLinked}
                disabled={isAtLimit}
              />
            </ReadOnlyGuard>
          }
          className="mt-8"
        />
      ) : (
        <div>
          <p className="text-base text-muted-foreground mb-4">
            {accountCountDisplay}
            {hiddenCount > 0 && !showHidden && (
              <span className="text-muted-foreground italic"> ({hiddenCount} hidden)</span>
            )}
          </p>

          {view === 'table' ? (
            <AccountTable
              accounts={displayedAccounts}
              loading={loading || tiersLoading}
              onEdit={handleEditAccount}
              onDelete={(account) => setAccountToDelete(account)}
              sortable
              sortColumn={sortColumn}
              sortDirection={sortDirection}
              onSort={handleSort}
              institutionOptions={institutionOptions}
              institutionFilter={institutionFilter}
              onInstitutionFilterChange={setInstitutionFilter}
              typeOptions={typeOptions}
              typeFilter={typeFilter}
              onTypeFilterChange={setTypeFilter}
              balanceRange={balanceRange}
              onBalanceRangeChange={setBalanceRange}
            />
          ) : (loading || tiersLoading) && displayedAccounts.length === 0 ? (
            <div role="status">
              <span className="sr-only">Loading accounts…</span>
              <div className="grid gap-6 max-sm:grid-cols-1" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))' }} aria-hidden="true">
                {Array.from({ length: 4 }).map((_, i) => (
                  <div key={i} className="h-48 rounded-lg bg-skeleton motion-safe:animate-pulse motion-reduce:animate-none" />
                ))}
              </div>
            </div>
          ) : (
            <div className="grid gap-6 max-sm:grid-cols-1" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))' }}>
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
    </div>
  )
}
