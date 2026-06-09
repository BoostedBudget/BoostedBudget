// Issue #1267: Shared error-state primitive for boosts tabs and other fetch-error surfaces.
import { AlertCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface ErrorStateProps {
  title: string
  description?: string
  onRetry?: () => void
  className?: string
}

function ErrorState({ title, description, onRetry, className }: ErrorStateProps) {
  return (
    <div
      data-slot="error-state"
      role="alert"
      className={cn(
        'flex flex-col items-center justify-center text-center rounded-xl border border-dashed border-border bg-muted/50 px-6 py-12',
        className
      )}
    >
      <div className="mb-3 [&>svg]:h-12 [&>svg]:w-12" aria-hidden="true">
        <AlertCircle className="text-destructive" />
      </div>
      <h3 className="text-base font-medium text-foreground">{title}</h3>
      {description && (
        <p className="mt-1 text-sm text-muted-foreground max-w-sm">{description}</p>
      )}
      {onRetry && (
        <Button
          variant="outline"
          size="sm"
          onClick={onRetry}
          aria-label="Retry"
          className="mt-4"
        >
          Retry
        </Button>
      )}
    </div>
  )
}

export { ErrorState }
