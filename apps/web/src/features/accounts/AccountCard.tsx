import { Pencil, EyeOff, GripVertical, Unplug } from 'lucide-react'
import { cn } from '@/lib/utils'
import { AccountIconDisplay } from './components/AccountIconDisplay'
import type { Account, AccountIcon } from './types'

interface AccountCardProps {
  account: Account
  onDelete?: (account: Account) => void
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

  const handleDelete = () => onDelete?.(account)
  const handleEdit = () => onEdit?.(account)

  return (
    <div
      className={cn(
        'bg-card rounded-lg shadow-sm p-6 transition-shadow hover:shadow-md',
        isDragging && 'shadow-xl scale-[1.02] opacity-90 cursor-grabbing',
        is_hidden && 'opacity-60',
        isDisconnected && 'opacity-80 border border-dashed border-warning'
      )}
      style={custom_color ? { borderLeftColor: custom_color, borderLeftWidth: '4px', borderLeftStyle: 'solid' } : undefined}
    >
      <div className="flex items-center gap-3 mb-4 pb-3 border-b border-border">
        {/* Drag handle */}
        {dragHandleProps && (
          <div className="cursor-grab p-1 rounded hover:bg-black/5 transition-colors" {...dragHandleProps}>
            <GripVertical className="h-5 w-5 text-muted-foreground" />
          </div>
        )}

        <div className="font-semibold text-sm text-muted-foreground uppercase tracking-wide flex-1">
          {institution_name || 'Unknown Bank'}
        </div>

        <div className="flex items-center gap-3">
          {/* Issue #679, #680: Disconnected badge when subscription expires */}
          {isDisconnected && (
            <span className="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium bg-warning/20 text-warning" title="Account disconnected - resubscribe to reconnect">
              <Unplug className="h-3 w-3" />
              <span>Disconnected</span>
            </span>
          )}
          {is_hidden && (
            <span className="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium bg-muted text-muted-foreground" title="Hidden account">
              <EyeOff className="h-3 w-3" />
              <span>Hidden</span>
            </span>
          )}
          {mask && (
            <div className="font-mono text-sm text-muted-foreground" data-ph-mask>
              ****{mask}
            </div>
          )}
        </div>
      </div>

      <div className="flex flex-col gap-2">
        <div className="flex items-center gap-2">
          {/* Icon or Emoji */}
          {custom_emoji ? (
            <span className="text-xl leading-none" aria-label="Custom emoji">
              {custom_emoji}
            </span>
          ) : custom_icon ? (
            <span
              className="flex items-center justify-center"
              style={custom_color ? { color: custom_color } : undefined}
            >
              <AccountIconDisplay icon={custom_icon as AccountIcon} className="h-5 w-5" />
            </span>
          ) : null}

          <h3
            className="text-xl font-semibold text-foreground m-0"
            style={custom_color ? { color: custom_color } : undefined}
          >
            {displayName}
          </h3>
        </div>

        <p className="text-sm text-muted-foreground m-0">{typeLabel}</p>

        <div className="mt-4 flex flex-col gap-3">
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Current Balance</span>
            <span className="text-lg font-semibold text-primary" data-ph-mask>{currentBalanceFormatted}</span>
          </div>
          {available_balance !== undefined && available_balance !== current_balance && (
            <div className="flex justify-between items-center">
              <span className="text-sm text-muted-foreground">Available Balance</span>
              <span className="text-lg font-semibold text-primary" data-ph-mask>{availableBalanceFormatted}</span>
            </div>
          )}
        </div>

        <div className="flex gap-3 mt-4 pt-4 border-t border-border">
          {onEdit && (
            <button
              className="inline-flex items-center gap-1.5 px-4 py-2 bg-muted text-foreground border border-border rounded-md text-sm font-medium cursor-pointer hover:bg-accent transition-colors focus:outline-none focus:ring-2 focus:ring-primary/50"
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
              className="px-4 py-2 bg-destructive/10 text-destructive border border-destructive/30 rounded-md text-sm font-medium cursor-pointer hover:bg-destructive/20 transition-colors focus:outline-none focus:ring-2 focus:ring-destructive/50"
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
