import { useState, useCallback, useMemo } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core'
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { Eye, EyeOff } from 'lucide-react'
import { PlaidLinkButton } from './PlaidLinkButton'
import { AccountCard } from './AccountCard'
import { UpgradeBanner } from './UpgradeBanner'
import { SyncStatsCard } from './SyncStatsCard'
import { AccountCustomizationModal } from './components/AccountCustomizationModal'
import {
  useGetAccounts,
  useDeleteAccount,
  getGetAccountsQueryKey,
} from '@/api/generated/endpoints/accounts/accounts'
import { useProfile } from '../profile/hooks/useProfile'
import { useSubscriptionTiers } from '../subscription/hooks/useSubscriptionTiers'
import { useTierLimits } from '../subscription/hooks/useTierLimits'
import { useAccountCustomization } from './hooks/useAccountCustomization'
import type { Account, UpdateAccountRequest } from './types'
import './AccountsPage.css'

// Sortable wrapper for AccountCard
interface SortableAccountCardProps {
  account: Account
  onDelete: (accountId: string) => void
  onEdit: (account: Account) => void
}

function SortableAccountCard({ account, onDelete, onEdit }: SortableAccountCardProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: account.id })

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  }

  return (
    <div ref={setNodeRef} style={style} {...attributes}>
      <AccountCard
        account={account}
        onDelete={onDelete}
        onEdit={onEdit}
        isDragging={isDragging}
        dragHandleProps={listeners}
      />
    </div>
  )
}

export function AccountsPage() {
  // Query client for optimistic updates
  const queryClient = useQueryClient()

  // Issue #307: Show/hide hidden accounts toggle
  const [showHidden, setShowHidden] = useState(false)

  // Issue #307: Modal state
  const [selectedAccount, setSelectedAccount] = useState<Account | null>(null)
  const [isModalOpen, setIsModalOpen] = useState(false)

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
    reorderAccounts,
    isReordering,
  } = useAccountCustomization()

  // DnD sensors
  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: {
        distance: 8, // 8px of movement to start drag
      },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    })
  )

  // Handle successful account linking - reload accounts and profile
  function handleAccountsLinked() {
    refetchAccounts()
    refetchProfile()
  }

  // Handle account deletion
  async function handleDeleteAccount(accountId: string) {
    try {
      await deleteAccountMutation.mutateAsync({ id: accountId })
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

  // Issue #307: Handle save from modal
  const handleSaveAccount = useCallback(async (accountId: string, changes: Partial<UpdateAccountRequest>) => {
    const updatedAccount = await updateAccount(accountId, changes)
    if (updatedAccount) {
      // Optimistically update the query cache
      queryClient.setQueryData(accountsQueryKey, (oldData: { accounts: Account[] } | undefined) => {
        if (!oldData) return oldData
        return {
          ...oldData,
          accounts: oldData.accounts.map(acc =>
            acc.id === accountId ? updatedAccount : acc
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

    // Filter hidden accounts unless showHidden is true
    if (!showHidden) {
      filtered = accs.filter(acc => !acc.is_hidden)
    }

    // Sort by display_order (nulls last), then created_at
    return [...filtered].sort((a, b) => {
      const orderA = a.display_order ?? Number.MAX_SAFE_INTEGER
      const orderB = b.display_order ?? Number.MAX_SAFE_INTEGER

      if (orderA !== orderB) {
        return orderA - orderB
      }

      // Fallback to created_at
      return new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
    })
  }, [accountsData?.accounts, showHidden])

  // Count of hidden accounts
  const hiddenCount = useMemo(() => {
    const accs = accountsData?.accounts ?? []
    return accs.filter(acc => acc.is_hidden).length
  }, [accountsData?.accounts])

  // Issue #307: Handle drag end for reordering
  const handleDragEnd = useCallback(async (event: DragEndEvent) => {
    const { active, over } = event

    if (over && active.id !== over.id) {
      // Get current order
      const oldIndex = displayedAccounts.findIndex(acc => acc.id === active.id)
      const newIndex = displayedAccounts.findIndex(acc => acc.id === over.id)

      if (oldIndex !== -1 && newIndex !== -1) {
        // Optimistically update UI
        const newOrder = arrayMove(displayedAccounts, oldIndex, newIndex)

        // Build account orders for API
        const accountOrders = newOrder.map((acc, index) => ({
          account_id: acc.id,
          display_order: index,
        }))

        // Optimistically update query cache
        queryClient.setQueryData(accountsQueryKey, (oldData: { accounts: Account[] } | undefined) => {
          if (!oldData) return oldData
          const updated = oldData.accounts.map(acc => {
            const order = accountOrders.find(o => o.account_id === acc.id)
            if (order) {
              return { ...acc, display_order: order.display_order }
            }
            return acc
          })
          return { ...oldData, accounts: updated }
        })

        // Call API
        await reorderAccounts(accountOrders)
      }
    }
  }, [displayedAccounts, reorderAccounts, queryClient, accountsQueryKey])

  // Calculate tier limits using hook
  const { isAtLimit, shouldShowUpgradeBanner, accountCountDisplay } = useTierLimits({
    user,
    tiers,
    currentAccountCount: accounts.length,
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
          <PlaidLinkButton
            onSuccess={handleAccountsLinked}
            disabled={isAtLimit}
          />
        </div>
      </div>

      {shouldShowUpgradeBanner && <UpgradeBanner />}

      {/* Issue #242: Monthly sync limits - show sync statistics */}
      <div className="sync-stats-section">
        <SyncStatsCard />
      </div>

      {displayedAccounts.length === 0 ? (
        <div className="empty-state">
          <p className="empty-state__message">
            {accounts.length === 0
              ? 'No accounts connected yet. Click "Connect Bank Account" above to get started.'
              : 'All accounts are hidden. Click "Show Hidden" to view them.'}
          </p>
        </div>
      ) : (
        <div className="accounts-list">
          <p className="accounts-count">
            {accountCountDisplay}
            {hiddenCount > 0 && !showHidden && (
              <span className="accounts-count__hidden"> ({hiddenCount} hidden)</span>
            )}
          </p>

          {/* Issue #307: Drag and drop context */}
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragEnd={handleDragEnd}
          >
            <SortableContext
              items={displayedAccounts.map(acc => acc.id)}
              strategy={verticalListSortingStrategy}
            >
              <div className={`accounts-grid ${isReordering ? 'accounts-grid--reordering' : ''}`}>
                {displayedAccounts.map((account) => (
                  <SortableAccountCard
                    key={account.id}
                    account={account}
                    onDelete={handleDeleteAccount}
                    onEdit={handleEditAccount}
                  />
                ))}
              </div>
            </SortableContext>
          </DndContext>
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
