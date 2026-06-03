import { cn } from '@/lib/utils'

interface ResizeHandleProps {
  columnId: string
  currentWidth: number
  isCoarsePointer: boolean
  isResizing?: boolean
  minSize?: number
  maxSize?: number
  onKeyDown: (e: React.KeyboardEvent) => void
  onPointerDown: (e: React.PointerEvent) => void
}

export function ResizeHandle({
  currentWidth,
  isCoarsePointer,
  isResizing,
  minSize,
  maxSize,
  onKeyDown,
  onPointerDown,
}: ResizeHandleProps) {
  return (
    <div
      data-testid="resize-handle"
      role="separator"
      aria-orientation="vertical"
      aria-valuenow={Math.round(currentWidth)}
      aria-valuemin={minSize}
      aria-valuemax={maxSize}
      aria-label="Resize column"
      aria-hidden={isCoarsePointer ? 'true' : undefined}
      tabIndex={isCoarsePointer ? -1 : 0}
      onPointerDown={onPointerDown}
      onKeyDown={onKeyDown}
      className={cn(
        'absolute right-0 top-0 h-full w-1.5 cursor-col-resize select-none touch-none',
        // Persistent subtle hairline; brightens + thickens on hover/active/resizing
        'border-r border-border/30 transition-[border-color,border-width]',
        'hover:border-primary/60 hover:border-r-2',
        isResizing && 'border-primary/80 border-r-2',
        isCoarsePointer && 'pointer-events-none opacity-0'
      )}
    />
  )
}
