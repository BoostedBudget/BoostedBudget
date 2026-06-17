import { Pencil, EyeOff, GripVertical, Unplug } from 'lucide-react'
import { Link } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { AccountSummaryHeader } from './components/AccountSummaryHeader'
import type { Account } from './types'
import { getAccountTypeLabel } from './utils'

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
      {/* Drag handle is a sibling of the Link — keeping it outside avoids nested interactive controls */}
      <div className="flex items-start gap-2">
        {dragHandleProps && (
          <div className="cursor-grab p-1 rounded hover:bg-black/5 transition-colors mt-1 shrink-0" {...dragHandleProps}>
            <GripVertical className="h-5 w-5 text-muted-foreground" />
          </div>
        )}
        <Link
          to={`/accounts/${account.id}`}
          className="flex-1 block hover:opacity-90 transition-opacity focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-lg"
          aria-label={`View ${displayName} account details`}
        >
          <AccountSummaryHeader
            institutionName={institution_name ?? ''}
            mask={mask}
            displayName={displayName}
            typeLabel={typeLabel}
            currentBalance={current_balance}
            availableBalance={available_balance}
            currencyCode={currency_code}
            customEmoji={custom_emoji}
            customIcon={custom_icon}
            customColor={custom_color}
            institutionRowTrailingSlot={
              (isDisconnected || is_hidden) ? (
                <>
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
                </>
              ) : undefined
            }
          />
        </Link>
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
    </Card>
  )
}
