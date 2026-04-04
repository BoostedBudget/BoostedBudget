import { useCallback, useMemo, useState } from 'react'
import { useTrackEvent } from '../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../shared/analytics'
import { useQueryClient } from '@tanstack/react-query'
import { Eye, EyeOff, Landmark } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
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
import './AccountsPage.css'

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
  // Query client for optimistic updates
  const queryClient = useQueryClient()

  // Issue #307: Show/hide hidden accounts toggle
  const [showHidden, setShowHidden] = useState(false)

  // Issue #584: View toggle state (cards/table)
  const [view, setView] = useState<ListView>(getInitialView)

  // Issue #307: Modal state
  const [selectedAccount, setSelectedAccount] = useState<Account | null>(null)
  const [isModalOpen, setIsModalOpen] = useState(false)

  // Issue #590: Sorting state
  const [sortColumn, setSortColumn] = useState<AccountSortColumn | undefined>(undefined)
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')

  // Issue #590: Inline filter state (URL-synced)
  const {
    institutionFilter,
    typeFilter,
    balanceRange,
    setInstitutionFilter,
    setTypeFilter,
    setBalanceRange,
  } = useAccountFilters()

  const trackEvent = useTrackEvent()

  // Use generated query hook for fetching accounts (always include hidden for toggle/counts)
  const {
    data: accountsData,
    isLoading: accountsLoading,
    error: accountsError,
    refetch: refetchAccounts,
  } = useGetAccounts({ include_hidden: 'true' })

  // Use generated mutation hook for deleting accounts
  const deleteAccountMutation = useDeleteAccount()

  // Derive accounts from query data (no local state sync needed)
  const accounts = accountsData?.accounts ?? []

  // Query key for optimistic updates
  const accountsQueryKey = getGetAccountsQueryKey({ include_hidden: 'true' })

  // Fetch user profile using the hook
  const { user, loading: userLoading, refetch: refetchProfile } = useProfile()

  // Fetch subscription tiers for limit enforcement
  const { tiers, loading: tiersLoading } = useSubscriptionTiers()

  // Combined loading state
  const loading = accountsLoading || userLoading

  // Error message
  const error = accountsError ? 'Failed to load accounts. Please try again.' : null

  // Issue #307: Account customization hook
  const {
    updateAccount,
    isUpdating,
  } = useAccountCustomization()

  // Handle successful account linking - reload accounts and profile
  function handleAccountsLinked() {
    refetchAccounts()
    refetchProfile()
  }

  // Handle account deletion
  async function handleDeleteAccount(accountId: string) {
    try {
      await deleteAccountMutation.mutateAsync({ id: accountId })
      trackEvent(ANALYTICS_EVENTS.ACCOUNT_REMOVED)
      // Refetch accounts after successful deletion
      await refetchAccounts()
    } catch (err) {
      console.error('Failed to delete account:', err)
      // Error will be shown from query error state
    }
  }

  // Issue #307: Handle edit button click
  const handleEditAccount = useCallback((account: Account) => {
    setSelectedAccount(account)
    setIsModalOpen(true)
  }, [])

  // Issue #590: Handle sort change
  const handleSort = useCallback((column: AccountSortColumn, direction: SortDirection) => {
    setSortColumn(column)
    setSortDirection(direction)
  }, [])

  // Issue #590: Create filter options from accounts data
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

  // Issue #590: Filter handlers from useAccountFilters hook (URL-synced)
  // setInstitutionFilter, setTypeFilter, setBalanceRange come from the hook

  // Issue #307: Handle save from modal
  // Issue #171: Now passes full account for user_account_id resolution
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

  // Issue #307: Filter and sort accounts
  // Use accountsData?.accounts directly in deps to avoid unstable reference
  const displayedAccounts = useMemo(() => {
    const accs = accountsData?.accounts ?? []
    let filtered = accs

    // Issue #171: Always filter out replaced accounts (they're soft-deleted after Plaid re-link)
    filtered = filtered.filter(acc => acc.status !== 'replaced')

    // Filter hidden accounts unless showHidden is true
    if (!showHidden) {
      filtered = filtered.filter(acc => !acc.is_hidden)
    }

    // Issue #590: Apply inline filters
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

    // Issue #590: If user has selected a sort column, use that
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

  // Account counts (excluding replaced accounts)
  const { hiddenCount, activeAccountsCount } = useMemo(() => {
    const accs = accountsData?.accounts ?? []
    const activeAccounts = accs.filter(acc => acc.status !== 'replaced')
    return {
      hiddenCount: activeAccounts.filter(acc => acc.is_hidden).length,
      activeAccountsCount: activeAccounts.length,
    }
  }, [accountsData?.accounts])

  // Calculate tier limits using hook
  // Issue #171: Use activeAccountsCount to exclude replaced accounts
  const { isAtLimit, shouldShowUpgradeBanner, accountCountDisplay } = useTierLimits({
    user,
    tiers,
    currentAccountCount: activeAccountsCount,
  })

  if (loading || tiersLoading) {
    return (
      <div className="accounts-page responsive">
        <div className="loading">Loading your accounts...</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="accounts-page responsive">
        <div className="error-state">
          <p className="error-message">{error}</p>
          <button onClick={() => refetchAccounts()} className="btn-retry">
            Retry
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="accounts-page responsive">
      <div className="accounts-header">
        <h1>My Accounts</h1>
        <div className="accounts-header__actions">
          {/* Issue #307: Show hidden toggle */}
          {hiddenCount > 0 && (
            <button
              className="accounts-header__toggle"
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
          {/* Issue #584: View toggle */}
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

      {accounts.length === 0 ? (
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
        <div className="accounts-list">
          <p className="accounts-count">
            {accountCountDisplay}
            {hiddenCount > 0 && !showHidden && (
              <span className="accounts-count__hidden"> ({hiddenCount} hidden)</span>
            )}
          </p>

          {/* Issue #584: Conditional rendering based on view */}
          {view === 'table' ? (
            <AccountTable
              accounts={displayedAccounts}
              onEdit={handleEditAccount}
              onDelete={(account) => handleDeleteAccount(account.id)}
              sortable
              sortColumn={sortColumn}
              sortDirection={sortDirection}
              onSort={handleSort}
              // Issue #590: Inline column filters (URL-synced via useAccountFilters)
              institutionOptions={institutionOptions}
              institutionFilter={institutionFilter}
              onInstitutionFilterChange={setInstitutionFilter}
              typeOptions={typeOptions}
              typeFilter={typeFilter}
              onTypeFilterChange={setTypeFilter}
              balanceRange={balanceRange}
              onBalanceRangeChange={setBalanceRange}
            />
          ) : (
            <div className="accounts-grid">
              {displayedAccounts.map((account) => (
                <AccountCard
                  key={account.id}
                  account={account}
                  onDelete={handleDeleteAccount}
                  onEdit={handleEditAccount}
                />
              ))}
            </div>
          )}
        </div>
      )}

      {/* Issue #307: Customization modal */}
      <AccountCustomizationModal
        account={selectedAccount}
        open={isModalOpen}
        onOpenChange={setIsModalOpen}
        onSave={handleSaveAccount}
        isSaving={isUpdating}
      />
    </div>
  )
}
