import { useId } from 'react'
import { Pencil, EyeOff, GripVertical, Unplug, Archive, Trash2 } from 'lucide-react'
import { Link } from 'react-router'
import { cn } from '@/lib/utils'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { AccountSummaryHeader } from './components/AccountSummaryHeader'
import type { Account } from './types'
import { getAccountTypeLabel, getAccountClosedDate, getAccountLastKnownBalance } from './utils'
import { isInactive } from './hooks/useActiveAccounts'

interface AccountCardProps {
  account: Account
  // Issue #1569, Task 6: disconnect (stop syncing, retain history) is the primary action for a
  // LIVE account; purge (permanently destroy) is the destructive action, reachable only for a
  // CLOSED account — see the render logic below for why a closed account gets neither onEdit nor
  // onDisconnect (a Task 5 reviewer finding: closed rows previously kept working Edit/Delete
  // actions that harmlessly re-ran UpdateStatus(removed)).
  onDisconnect?: (account: Account) => void
  onPurge?: (account: Account) => void
  onEdit?: (account: Account) => void
  isDragging?: boolean
  dragHandleProps?: React.HTMLAttributes<HTMLDivElement>
}


export function AccountCard({ account, onDisconnect, onPurge, onEdit, isDragging, dragHandleProps }: AccountCardProps) {
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

  // Check if the linked item is disconnected — this can mean the subscription lapsed (#679/#680)
  // OR that orphan cleanup removed the Plaid item successfully; status alone can't tell which.
  const isDisconnected = linked_item_status === 'disconnected'

  // Issue #1569: a retained (non-active) account — `replaced`, `expired`, or `removed`. Distinct
  // from isDisconnected above: that's about the linked ITEM's subscription state; this is about the
  // ACCOUNT's own status.
  //
  // ⚠ #1569 T8.5: do NOT say "the user disconnected it". `removed` has THREE causes
  // (backend/internal/domain/account.go): user_disconnect, provider_absent (the bank stopped
  // reporting it) and link_removed (one of our own sweeps). accounts.removed_reason is NOT on the
  // wire — it appears nowhere in api/openapi.yaml — so this component CANNOT tell them apart, and
  // the badge copy is deliberately worded to be true of all three rather than inventing a
  // distinction the data cannot support. If removed_reason is ever exposed, revisit the copy here
  // and in AccountTable.tsx together.
  const isClosed = isInactive(account)
  // Issue #1591: resolved through the helper that owns the "both or neither" rule, so the card
  // never decides for itself whether a half-pair is renderable.
  const lastKnown = getAccountLastKnownBalance(account)

  // Display name: nickname > official_name > name
  const displayName = nickname || official_name || name
  const typeLabel = getAccountTypeLabel(type, subtype)
  const summaryLinkLabelId = useId()


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
          // PII: aria-label is recorded verbatim by session replay; data-ph-mask only covers
          // text content, not attributes. aria-labelledby stores only an opaque useId() value
          // in the attribute; the accessible name (account type and mask) is computed at
          // read time from the sr-only span below, so that PII never appears as attribute text.
          // `href` still carries account.id — an opaque, non-financial UUID — because routing
          // depends on it; changing that would be an out-of-scope routing change. The
          // aria-labelledby/id pair no longer carries it (matches IncomeSourceItem.tsx's
          // getRemoveLabelText pattern).
          aria-labelledby={summaryLinkLabelId}
        >
          <span id={summaryLinkLabelId} className="sr-only" data-ph-mask>
            {`View ${typeLabel} account${mask ? ` ending in ${mask}` : ''} details`}
          </span>
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
            isClosed={isClosed}
            closedOn={getAccountClosedDate(account)}
            lastKnownBalance={lastKnown?.amount}
            lastKnownBalanceAt={lastKnown?.at}
            institutionRowTrailingSlot={
              (isDisconnected || is_hidden || isClosed) ? (
                <>
                  {isClosed && (
                    <Badge variant="secondary" title="This account is no longer active — it may have been disconnected, replaced by a re-link, or stopped being reported by your bank. Its history is retained.">
                      <Archive className="h-3 w-3" />
                      <span>Closed</span>
                    </Badge>
                  )}
                  {isDisconnected && (
                    <Badge variant="warning" title="This connection is no longer active">
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
        {/* Issue #1569, Task 6: a closed account's actions are the ones that make sense for a
            closed account — purge only. Edit and Disconnect both act on a LIVE account, and this
            one no longer is one; re-running them would be a silent no-op at best. */}
        {isClosed ? (
          onPurge && (
            <Button
              variant="destructive"
              onClick={() => onPurge(account)}
              title="Permanently delete this account and its history"
            >
              <Trash2 className="h-4 w-4" />
              <span>Delete from history</span>
            </Button>
          )
        ) : (
          <>
            {onEdit && (
              <Button
                variant="secondary"
                onClick={() => onEdit(account)}
                title="Customize this account"
                aria-label="Edit account"
              >
                <Pencil className="h-4 w-4" />
                <span>Edit</span>
              </Button>
            )}
            {onDisconnect && (
              <Button
                variant="secondary"
                onClick={() => onDisconnect(account)}
                title="Stop syncing this account, retaining its history"
              >
                <Unplug className="h-4 w-4" />
                <span>Disconnect</span>
              </Button>
            )}
          </>
        )}
      </div>
    </Card>
  )
}
