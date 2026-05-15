import { GripVertical } from 'lucide-react'
import { cn } from '@/lib/utils'

interface DragHandleProps {
  isDragging: boolean
  onKeyDown: (e: React.KeyboardEvent) => void
}

export function DragHandle({ isDragging, onKeyDown }: DragHandleProps) {
  return (
    <button
      type="button"
      data-testid="drag-handle"
      aria-label="Drag to reorder column"
      aria-pressed={isDragging}
      onKeyDown={onKeyDown}
      className={cn(
        'inline-flex items-center justify-center rounded p-0.5',
        'cursor-grab active:cursor-grabbing',
        'text-muted-foreground hover:text-foreground',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
        'transition-colors',
        isDragging && 'text-primary ring-2 ring-primary'
      )}
    >
      <GripVertical className="h-3 w-3" aria-hidden="true" />
    </button>
  )
}
