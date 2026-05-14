import { cn } from '../../lib/utils'

export function PageSkeleton({ className }: { className?: string }) {
  return (
    <div
      className={cn('flex items-center justify-center min-h-[60vh]', className)}
      aria-busy="true"
      aria-label="Loading page"
    >
      <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600 motion-reduce:animate-none" />
    </div>
  )
}
