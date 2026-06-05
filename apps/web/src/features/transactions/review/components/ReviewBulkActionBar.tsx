import { CheckCircle2, Circle, Tag, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { ReadOnlyGuard } from '@/auth'

interface ReviewBulkActionBarProps {
  selectedCount: number
  onMarkReviewed: () => void
  onMarkUnreviewed: () => void
  onBulkRecategorize?: () => void
  onClear: () => void
  isLoading?: boolean
}

export function ReviewBulkActionBar({
  selectedCount,
  onMarkReviewed,
  onMarkUnreviewed,
  onBulkRecategorize,
  onClear,
  isLoading = false,
}: ReviewBulkActionBarProps) {
  if (selectedCount === 0) return null

  return (
    <div
      className="sticky bottom-4 mx-auto flex w-fit items-center gap-2 rounded-xl border border-border bg-card px-4 py-2.5 shadow-lg"
      role="toolbar"
      aria-label="Bulk actions"
    >
      <span className="mr-2 text-sm font-medium text-foreground">
        {selectedCount} selected
      </span>

      <ReadOnlyGuard message="Resubscribe to review transactions">
        <>
          <Button
            variant="ghost"
            size="sm"
            onClick={onMarkReviewed}
            disabled={isLoading}
            aria-label="Mark selected as reviewed"
          >
            <CheckCircle2 className="text-success" />
            Mark reviewed
          </Button>

          <Button
            variant="ghost"
            size="sm"
            onClick={onMarkUnreviewed}
            disabled={isLoading}
            aria-label="Mark selected as unreviewed"
          >
            <Circle className="text-warning" />
            Mark unreviewed
          </Button>

          {onBulkRecategorize && (
            <Button
              variant="ghost"
              size="sm"
              onClick={onBulkRecategorize}
              disabled={isLoading}
              aria-label="Recategorize selected"
            >
              <Tag />
              Recategorize
            </Button>
          )}
        </>
      </ReadOnlyGuard>

      <Button
        variant="ghost"
        size="icon-sm"
        onClick={onClear}
        aria-label="Clear selection"
      >
        <X />
      </Button>
    </div>
  )
}
