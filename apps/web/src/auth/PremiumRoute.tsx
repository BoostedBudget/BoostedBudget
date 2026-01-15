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
    <div data-testid="premium-loading" className="flex justify-center items-center min-h-screen bg-gray-50">
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

interface ProfileErrorProps {
  error: Error | null
}

/**
 * Error state displayed when profile API fails
 */
function ProfileError({ error }: ProfileErrorProps) {
  // Log error for debugging - will appear in Playwright console output
  console.error('[PremiumRoute] Profile API error:', error?.message, error)

  return (
    <div data-testid="profile-error" className="flex justify-center items-center min-h-screen bg-gray-50 dark:bg-gray-900">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-lg p-8 max-w-md text-center">
        <div className="text-red-600 text-5xl mb-4" aria-hidden="true">⚠️</div>
        <h2 className="text-xl font-semibold text-gray-900 dark:text-white mb-2">
          Unable to Load Profile
        </h2>
        <p className="text-gray-600 dark:text-gray-400 mb-6">
          Please try refreshing the page. If the problem persists, try signing out and back in.
        </p>
        {/* Issue #698: Always show error message for E2E/QA debugging */}
        {error && (
          <pre data-testid="error-message" className="text-xs text-red-500 mb-4 text-left bg-red-50 dark:bg-red-900/20 p-3 rounded overflow-auto max-h-32">
            {error.message}
          </pre>
        )}
        <button
          onClick={() => window.location.reload()}
          className="px-6 py-3 bg-blue-600 text-white font-medium rounded-lg hover:bg-blue-700 transition-colors"
        >
          Refresh Page
        </button>
      </div>
    </div>
  )
}

export function PremiumRoute({ children }: PremiumRouteProps) {
  const { user, loading, error } = useProfile()
  const location = useLocation()

  if (loading) {
    return <LoadingSkeleton />
  }

  // Handle API error - show error state instead of redirecting to pricing
  // This prevents false redirects when the profile API temporarily fails
  if (error) {
    return <ProfileError error={error} />
  }

  // Safety guard: if user is null without error, keep showing loading
  // This shouldn't happen normally but prevents incorrect redirect
  if (!user) {
    return <LoadingSkeleton />
  }

  const hasActiveSubscription = user.subscription_status === 'active' || user.subscription_status === 'trial'

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
