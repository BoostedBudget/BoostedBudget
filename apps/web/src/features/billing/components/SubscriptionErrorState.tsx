import { RefreshCw } from 'lucide-react'
import { Button } from '@/components/ui/button'

interface SubscriptionErrorStateProps {
  message: string
  onRetry: () => void
}

export function SubscriptionErrorState({ message, onRetry }: SubscriptionErrorStateProps) {
  return (
    <div className="max-w-[800px] mx-auto p-8">
      <div
        role="alert"
        className="flex flex-col items-center justify-center text-center rounded-xl border border-destructive/30 bg-destructive/10 px-6 py-12"
      >
        <RefreshCw className="h-12 w-12 text-destructive mb-3" aria-hidden="true" />
        <h2 className="text-base font-medium text-foreground">
          Unable to load subscription
        </h2>
        <p className="mt-1 text-sm text-muted-foreground max-w-sm">{message}</p>
        <Button onClick={onRetry} variant="outline" className="mt-4">
          <RefreshCw className="h-4 w-4 mr-2" aria-hidden="true" />
          Try Again
        </Button>
      </div>
    </div>
  )
}
