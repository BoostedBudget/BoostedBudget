import { Navigate, useLocation } from 'react-router-dom'
import { useIsAdmin } from '../hooks/useIsAdmin'
import { useAdminMode } from '../hooks/useAdminMode'

interface AdminRouteProps {
  children: React.ReactNode
}

function LoadingSkeleton() {
  return (
    <div data-testid="admin-loading" className="flex justify-center items-center min-h-screen bg-muted">
      <div className="w-full max-w-md p-6 space-y-4">
        <div className="h-6 bg-skeleton rounded animate-pulse w-1/3"></div>
        <div className="space-y-3">
          <div className="h-4 bg-skeleton rounded animate-pulse"></div>
          <div className="h-4 bg-skeleton rounded animate-pulse w-5/6"></div>
          <div className="h-4 bg-skeleton rounded animate-pulse w-4/6"></div>
        </div>
        <div className="h-32 bg-skeleton rounded-lg animate-pulse mt-4"></div>
      </div>
    </div>
  )
}

function AdminError({ error }: { error: Error | null }) {
  if (error) {
    console.error('[AdminRoute] Admin check error:', error.message)
  }

  return (
    <div data-testid="admin-error" className="flex justify-center items-center min-h-screen bg-gray-50 dark:bg-gray-900">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-lg p-8 max-w-md text-center">
        <div className="text-red-600 text-5xl mb-4" aria-hidden="true">⚠️</div>
        <h2 className="text-xl font-semibold text-gray-900 dark:text-white mb-2">
          Unable to Check Admin Status
        </h2>
        <p className="text-gray-600 dark:text-gray-400 mb-6">
          Please try refreshing the page. If the problem persists, try signing out and back in.
        </p>
        {error && (
          <pre
            data-testid="error-message"
            className="text-xs text-red-500 mb-4 text-left bg-red-50 dark:bg-red-900/20 p-3 rounded overflow-auto max-h-32"
          >
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

export function AdminRoute({ children }: AdminRouteProps) {
  const { isAdmin, loading, error } = useIsAdmin()
  const { adminModeEnabled } = useAdminMode()
  const location = useLocation()

  if (loading) return <LoadingSkeleton />
  if (error) return <AdminError error={error} />
  if (!isAdmin || !adminModeEnabled) {
    return <Navigate to="/dashboard" state={{ from: location }} replace />
  }

  return <>{children}</>
}
