import { cn } from '@/lib/utils'

interface ResizeHandleProps {
  columnId: string
  currentWidth: number
  isCoarsePointer: boolean
  onKeyDown: (e: React.KeyboardEvent) => void
  onPointerDown: (e: React.PointerEvent) => void
}

export function ResizeHandle({
  currentWidth,
  isCoarsePointer,
  onKeyDown,
  onPointerDown,
}: ResizeHandleProps) {
  return (
    <div
      data-testid="resize-handle"
      role="separator"
      aria-orientation="vertical"
      aria-valuenow={Math.round(currentWidth)}
      aria-label="Resize column"
      aria-hidden={isCoarsePointer ? 'true' : undefined}
      tabIndex={0}
      onPointerDown={onPointerDown}
      onKeyDown={onKeyDown}
      className={cn(
        'absolute right-0 top-0 h-full w-1 cursor-col-resize select-none touch-none',
        'opacity-0 hover:opacity-100 focus:opacity-100',
        'bg-border transition-opacity',
        isCoarsePointer && 'pointer-events-none'
      )}
    />
  )
}
