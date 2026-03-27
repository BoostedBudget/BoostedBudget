import { Pencil, EyeOff, GripVertical, Unplug } from 'lucide-react'
import { cn } from '@/lib/utils'
import { AccountIconDisplay } from './components/AccountIconDisplay'
import type { Account, AccountIcon } from './types'
import './AccountCard.css'

interface AccountCardProps {
  account: Account
  onDelete?: (accountId: string) => void
  onEdit?: (account: Account) => void
  isDragging?: boolean
  dragHandleProps?: React.HTMLAttributes<HTMLDivElement>
}

/**
 * Format currency amount with proper symbol and decimal places
 */
function formatCurrency(amount: number | undefined, currencyCode: string): string {
  if (amount === undefined) return 'N/A'

  // Use Intl.NumberFormat for proper currency formatting
  try {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: currencyCode,
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(amount)
  } catch {
    // Fallback if currency code is invalid
    return `${currencyCode} ${amount.toFixed(2)}`
  }
}

/**
 * Get human-readable account type label
 */
function getAccountTypeLabel(type: string, subtype?: string): string {
  const typeMap: Record<string, string> = {
    depository: 'Bank Account',
    credit: 'Credit Card',
    loan: 'Loan',
    investment: 'Investment',
    other: 'Other',
  }

  const baseLabel = typeMap[type] || type

  // Add subtype if available (e.g., "Bank Account - Checking")
  if (subtype) {
    const subtypeCapitalized = subtype.charAt(0).toUpperCase() + subtype.slice(1)
    return `${baseLabel} - ${subtypeCapitalized}`
  }

  return baseLabel
}

export function AccountCard({ account, onDelete, onEdit, isDragging, dragHandleProps }: AccountCardProps) {
  const {
    id,
    name,
    official_name,
    type,
    subtype,
    mask,
    current_balance,
    available_balance,
    currency_code,
    institution_name,
    // Customization fields (Issue #307)
    nickname,
    custom_color,
    custom_icon,
    custom_emoji,
    is_hidden,
    // Issue #679, #680: Linked item status for disconnected accounts
    linked_item_status,
  } = account

  // Check if the linked item is disconnected (subscription expired)
  const isDisconnected = linked_item_status === 'disconnected'

  // Display name: nickname > official_name > name
  const displayName = nickname || official_name || name
  const typeLabel = getAccountTypeLabel(type, subtype)
  const currentBalanceFormatted = formatCurrency(current_balance, currency_code)
  const availableBalanceFormatted = formatCurrency(available_balance, currency_code)

  function handleDelete() {
    if (!onDelete) return

    const accountName = nickname || official_name || name
    const confirmed = window.confirm(
      `Are you sure you want to delete "${accountName}"?\n\nThis action cannot be undone. Other accounts from ${institution_name} will remain connected.`
    )

    if (confirmed) {
      onDelete(id)
    }
  }

  function handleEdit() {
    if (onEdit) {
      onEdit(account)
    }
  }

  return (
    <div
      className={cn(
        'account-card',
        isDragging && 'account-card--dragging',
        is_hidden && 'account-card--hidden',
        isDisconnected && 'account-card--disconnected'
      )}
      style={custom_color ? { borderLeftColor: custom_color, borderLeftWidth: '4px', borderLeftStyle: 'solid' } : undefined}
    >
      <div className="account-card__header">
        {/* Drag handle */}
        {dragHandleProps && (
          <div className="account-card__drag-handle" {...dragHandleProps}>
            <GripVertical className="h-5 w-5 text-muted-foreground" />
          </div>
        )}

        <div className="account-card__institution">
          {institution_name || 'Unknown Bank'}
        </div>

        <div className="account-card__header-right">
          {/* Issue #679, #680: Disconnected badge when subscription expires */}
          {isDisconnected && (
            <span className="account-card__badge account-card__badge--disconnected" title="Account disconnected - resubscribe to reconnect">
              <Unplug className="h-3 w-3" />
              <span>Disconnected</span>
            </span>
          )}
          {is_hidden && (
            <span className="account-card__badge account-card__badge--hidden" title="Hidden account">
              <EyeOff className="h-3 w-3" />
              <span>Hidden</span>
            </span>
          )}
          {mask && (
            <div className="account-card__mask" data-ph-mask>
              ****{mask}
            </div>
          )}
        </div>
      </div>

      <div className="account-card__body">
        <div className="account-card__name-row">
          {/* Icon or Emoji */}
          {custom_emoji ? (
            <span className="account-card__emoji" aria-label="Custom emoji">
              {custom_emoji}
            </span>
          ) : custom_icon ? (
            <span
              className="account-card__icon"
              style={custom_color ? { color: custom_color } : undefined}
            >
              <AccountIconDisplay icon={custom_icon as AccountIcon} className="h-5 w-5" />
            </span>
          ) : null}

          <h3
            className="account-card__name"
            style={custom_color ? { color: custom_color } : undefined}
          >
            {displayName}
          </h3>
        </div>

        <p className="account-card__type">{typeLabel}</p>

        <div className="account-card__balances">
          <div className="balance-item">
            <span className="balance-label">Current Balance</span>
            <span className="balance-amount" data-ph-mask>{currentBalanceFormatted}</span>
          </div>
          {available_balance !== undefined && available_balance !== current_balance && (
            <div className="balance-item">
              <span className="balance-label">Available Balance</span>
              <span className="balance-amount" data-ph-mask>{availableBalanceFormatted}</span>
            </div>
          )}
        </div>

        <div className="account-card__actions">
          {onEdit && (
            <button
              className="account-card__edit-btn"
              onClick={handleEdit}
              title="Customize this account"
              aria-label="Edit account"
            >
              <Pencil className="h-4 w-4" />
              <span>Edit</span>
            </button>
          )}
          {onDelete && (
            <button
              className="account-card__delete-btn"
              onClick={handleDelete}
              title="Delete this account"
            >
              Delete Account
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
