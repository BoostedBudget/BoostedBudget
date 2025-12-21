import { useState, useEffect, useCallback, useMemo } from 'react'
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
import { fetchAccounts, deleteAccount } from './api/plaidApi'
import { fetchProfile } from '../profile/api/profileApi'
import { useSubscriptionTiers } from '../subscription/hooks/useSubscriptionTiers'
import { useTierLimits } from '../subscription/hooks/useTierLimits'
import { useAccountCustomization } from './hooks/useAccountCustomization'
import type { Account, UpdateAccountRequest } from './types'
import type { User } from '../profile/types'
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
  const [accounts, setAccounts] = useState<Account[]>([])
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Issue #307: Show/hide hidden accounts toggle
  const [showHidden, setShowHidden] = useState(false)

  // Issue #307: Modal state
  const [selectedAccount, setSelectedAccount] = useState<Account | null>(null)
  const [isModalOpen, setIsModalOpen] = useState(false)

  // Fetch subscription tiers for limit enforcement
  const { tiers, loading: tiersLoading } = useSubscriptionTiers()

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

  // Load accounts and user profile on mount
  useEffect(() => {
    loadAccountsAndProfile()
  }, [])

  async function loadAccountsAndProfile() {
    try {
      setLoading(true)
      setError(null)

      // Fetch both accounts and user profile in parallel
      // Always fetch all accounts including hidden (for toggle and counts)
      const [accountsResponse, userProfile] = await Promise.all([
        fetchAccounts(true), // include_hidden = true
        fetchProfile(),
      ])

      setAccounts(accountsResponse.accounts)
      setUser(userProfile)
    } catch (err) {
      console.error('Failed to load accounts:', err)
      setError('Failed to load accounts. Please try again.')
    } finally {
      setLoading(false)
    }
  }

  // Handle successful account linking - reload accounts and profile
  function handleAccountsLinked() {
    loadAccountsAndProfile()
  }

  // Handle account deletion
  async function handleDeleteAccount(accountId: string) {
    try {
      await deleteAccount(accountId)
      // Reload accounts after successful deletion
      await loadAccountsAndProfile()
    } catch (err) {
      console.error('Failed to delete account:', err)
      setError('Failed to delete account. Please try again.')
    }
  }

  // Issue #307: Handle edit button click
  const handleEditAccount = useCallback((account: Account) => {
    setSelectedAccount(account)
    setIsModalOpen(true)
  }, [])

  // Issue #307: Handle save from modal
  const handleSaveAccount = useCallback(async (accountId: string, changes: UpdateAccountRequest) => {
    const updatedAccount = await updateAccount(accountId, changes)
    if (updatedAccount) {
      // Update local state with the new account data
      setAccounts(prev => prev.map(acc =>
        acc.id === accountId ? updatedAccount : acc
      ))
    }
  }, [updateAccount])

  // Issue #307: Filter and sort accounts
  const displayedAccounts = useMemo(() => {
    let filtered = accounts

    // Filter hidden accounts unless showHidden is true
    if (!showHidden) {
      filtered = accounts.filter(acc => !acc.is_hidden)
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
  }, [accounts, showHidden])

  // Count of hidden accounts
  const hiddenCount = useMemo(() => {
    return accounts.filter(acc => acc.is_hidden).length
  }, [accounts])

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

        // Update local state optimistically
        setAccounts(prev => {
          const updated = [...prev]
          accountOrders.forEach(order => {
            const acc = updated.find(a => a.id === order.account_id)
            if (acc) {
              acc.display_order = order.display_order
            }
          })
          return updated
        })

        // Call API
        await reorderAccounts(accountOrders)
      }
    }
  }, [displayedAccounts, reorderAccounts])

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
          <button onClick={loadAccountsAndProfile} className="btn-retry">
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
