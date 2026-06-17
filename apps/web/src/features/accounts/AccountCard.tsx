import { Pencil, EyeOff, GripVertical, Unplug } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { AccountIconDisplay } from './components/AccountIconDisplay'
import type { Account, AccountIcon } from './types'
import { getAccountTypeLabel, formatAccountBalance } from './utils'

interface AccountCardProps {
  account: Account
  onDelete?: (account: Account) => void
  onEdit?: (account: Account) => void
  isDragging?: boolean
  dragHandleProps?: React.HTMLAttributes<HTMLDivElement>
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
  const currentBalanceFormatted = formatAccountBalance(current_balance, currency_code)
  const availableBalanceFormatted = formatAccountBalance(available_balance, currency_code)

  const handleDelete = () => onDelete?.(account)
  const handleEdit = () => onEdit?.(account)

  return (
    <Card
      className={cn(
        'shadow-sm p-6 transition-shadow hover:shadow-md',
        isDragging && 'shadow-xl scale-[1.02] opacity-90 cursor-grabbing',
        is_hidden && 'opacity-60',
        isDisconnected && 'opacity-80 border-dashed border-warning'
      )}
      style={custom_color ? { borderLeftColor: custom_color, borderLeftWidth: '4px', borderLeftStyle: 'solid' } : undefined}
    >
      <div className="flex items-center gap-3 mb-4 pb-3 border-b border-border">
        {dragHandleProps && (
          <div className="cursor-grab p-1 rounded hover:bg-black/5 transition-colors" {...dragHandleProps}>
            <GripVertical className="h-5 w-5 text-muted-foreground" />
          </div>
        )}

        <div className="font-semibold text-sm text-muted-foreground uppercase tracking-wide flex-1">
          <span data-ph-mask>{institution_name || 'Unknown Bank'}</span>
        </div>

        <div className="flex items-center gap-3">
          {isDisconnected && (
            <Badge variant="warning" title="Account disconnected - resubscribe to reconnect">
              <Unplug className="h-3 w-3" />
              <span>Disconnected</span>
            </Badge>
          )}
          {is_hidden && (
            <Badge variant="secondary" title="Hidden account">
              <EyeOff className="h-3 w-3" />
              <span>Hidden</span>
            </Badge>
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
            <span data-ph-mask>{displayName}</span>
          </h3>
        </div>

        <p className="text-sm text-muted-foreground m-0">{typeLabel}</p>

        <div className="mt-4 flex flex-col gap-3">
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Current Balance</span>
            <span className="text-lg font-semibold text-primary" data-ph-mask>
              {currentBalanceFormatted}
            </span>
          </div>
          {available_balance !== undefined && available_balance !== current_balance && (
            <div className="flex justify-between items-center">
              <span className="text-sm text-muted-foreground">Available Balance</span>
              <span className="text-lg font-semibold text-primary" data-ph-mask>
                {availableBalanceFormatted}
              </span>
            </div>
          )}
        </div>

        <div className="flex gap-3 mt-4 pt-4 border-t border-border">
          {onEdit && (
            <Button
              variant="secondary"
              onClick={handleEdit}
              title="Customize this account"
              aria-label="Edit account"
            >
              <Pencil className="h-4 w-4" />
              <span>Edit</span>
            </Button>
          )}
          {onDelete && (
            <Button
              variant="destructive"
              onClick={handleDelete}
              title="Delete this account"
            >
              Delete Account
            </Button>
          )}
        </div>
      </div>
    </Card>
  )
}
