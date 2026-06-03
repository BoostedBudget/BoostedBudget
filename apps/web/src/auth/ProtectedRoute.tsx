import { Navigate, useLocation } from 'react-router-dom'
import { useAuth } from '@clerk/react-router'
import { useLoginEnabled } from '@/shared/hooks'

interface ProtectedRouteProps {
  children: React.ReactNode
}

export function ProtectedRoute({ children }: ProtectedRouteProps) {
  const { isSignedIn, isLoaded } = useAuth()
  const { enabled: loginEnabled, loading: flagLoading } = useLoginEnabled()
  const location = useLocation()

  // Show skeleton while either Clerk or feature flag is loading
  if (!isLoaded || flagLoading) {
    return (
      <div className="flex justify-center items-center min-h-screen bg-muted">
        <div className="w-full max-w-md p-6 space-y-4">
          {/* Skeleton header */}
          <div className="h-6 bg-skeleton rounded animate-pulse w-1/3"></div>
          {/* Skeleton content blocks */}
          <div className="space-y-3">
            <div className="h-4 bg-skeleton rounded animate-pulse"></div>
            <div className="h-4 bg-skeleton rounded animate-pulse w-5/6"></div>
            <div className="h-4 bg-skeleton rounded animate-pulse w-4/6"></div>
          </div>
          {/* Skeleton card */}
          <div className="h-32 bg-skeleton rounded-lg animate-pulse mt-4"></div>
        </div>
      </div>
    )
  }

  // Hard lockdown — block ALL users (even authenticated) when flag is OFF
  if (!loginEnabled) {
    return <Navigate to="/" replace />
  }

  if (!isSignedIn) {
    // Redirect to home, preserving intended destination
    return <Navigate to="/" state={{ from: location }} replace />
  }

  return <>{children}</>
}
