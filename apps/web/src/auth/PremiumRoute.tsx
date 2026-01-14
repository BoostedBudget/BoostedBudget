/**
 * PremiumRoute Component
 *
 * Route guard that checks if user has an active subscription (trial or paid).
 * Redirects to pricing page if subscription is inactive.
 *
 * Issue #679, #680: Account freezing when subscription expires
 */

import { Navigate, useLocation } from 'react-router-dom'
import { useProfile } from '@/features/profile/hooks/useProfile'

interface PremiumRouteProps {
  children: React.ReactNode
}

/**
 * Loading skeleton displayed while fetching user profile
 */
function LoadingSkeleton() {
  return (
    <div className="flex justify-center items-center min-h-screen bg-gray-50">
      <div className="w-full max-w-md p-6 space-y-4">
        {/* Skeleton header */}
        <div className="h-6 bg-gray-200 rounded animate-pulse w-1/3"></div>
        {/* Skeleton content blocks */}
        <div className="space-y-3">
          <div className="h-4 bg-gray-200 rounded animate-pulse"></div>
          <div className="h-4 bg-gray-200 rounded animate-pulse w-5/6"></div>
          <div className="h-4 bg-gray-200 rounded animate-pulse w-4/6"></div>
        </div>
        {/* Skeleton card */}
        <div className="h-32 bg-gray-200 rounded-lg animate-pulse mt-4"></div>
      </div>
    </div>
  )
}

export function PremiumRoute({ children }: PremiumRouteProps) {
  const { user, loading } = useProfile()
  const location = useLocation()

  if (loading) {
    return <LoadingSkeleton />
  }

  const hasActiveSubscription = user?.subscription_status === 'active' || user?.subscription_status === 'trial'

  if (!hasActiveSubscription) {
    return (
      <Navigate
        to="/pricing"
        state={{ from: location, frozen: true }}
        replace
      />
    )
  }

  return <>{children}</>
}
