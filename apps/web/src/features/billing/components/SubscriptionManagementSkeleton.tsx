export function SubscriptionManagementSkeleton() {
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label="Loading subscription details"
      className="max-w-[800px] mx-auto p-8 space-y-8"
    >
      {/* Page title skeleton */}
      <div className="h-8 bg-muted rounded animate-pulse w-1/3" />

      {/* CurrentSubscriptionDisplay skeleton */}
      <div
        data-testid="skeleton-subscription-card"
        className="bg-card rounded-lg p-6 shadow-sm space-y-4"
      >
        {/* Card header: icon + title */}
        <div className="flex items-center gap-3">
          <div className="h-6 w-6 bg-muted rounded animate-pulse" />
          <div className="h-5 bg-muted rounded animate-pulse w-48" />
        </div>
        {/* Tier name row + badge */}
        <div className="flex items-center justify-between">
          <div className="h-6 bg-muted rounded animate-pulse w-32" />
          <div className="h-5 bg-muted rounded-full animate-pulse w-20" />
        </div>
        {/* Status badge */}
        <div className="h-5 bg-muted rounded-full animate-pulse w-16" />
        {/* Price */}
        <div className="h-8 bg-muted rounded animate-pulse w-36" />
        {/* Features list */}
        <div className="space-y-2">
          {[1, 2, 3, 4].map((i) => (
            <div
              key={i}
              className="h-4 bg-muted rounded animate-pulse w-3/4"
            />
          ))}
        </div>
      </div>

      {/* BillingDetails skeleton */}
      <div
        data-testid="skeleton-billing-card"
        className="bg-card rounded-lg p-6 shadow-sm space-y-4"
      >
        {/* Card header */}
        <div className="flex items-center gap-3">
          <div className="h-6 w-6 bg-muted rounded animate-pulse" />
          <div className="h-5 bg-muted rounded animate-pulse w-36" />
        </div>
        {/* Info rows */}
        {[1, 2, 3].map((i) => (
          <div key={i} className="flex justify-between py-2">
            <div className="h-4 bg-muted rounded animate-pulse w-28" />
            <div className="h-4 bg-muted rounded animate-pulse w-24" />
          </div>
        ))}
      </div>
    </div>
  )
}
