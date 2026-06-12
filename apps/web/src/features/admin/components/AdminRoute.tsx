import { Navigate, useLocation } from 'react-router-dom'
import { useIsAdmin } from '../hooks/useIsAdmin'
import { useAdminMode } from '../hooks/useAdminMode'
import { Button } from '@/components/ui/button'

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
    <div data-testid="admin-error" className="flex justify-center items-center min-h-screen bg-background">
      <div className="bg-card rounded-lg shadow-lg p-8 max-w-md text-center">
        <div className="text-destructive text-5xl mb-4" aria-hidden="true">⚠️</div>
        <h2 className="text-xl font-semibold text-foreground mb-2">
          Unable to Check Admin Status
        </h2>
        <p className="text-muted-foreground mb-6">
          Please try refreshing the page. If the problem persists, try signing out and back in.
        </p>
        {error && (
          <pre
            data-testid="error-message"
            className="text-xs text-destructive mb-4 text-left bg-destructive/10 p-3 rounded overflow-auto max-h-32"
          >
            {error.message}
          </pre>
        )}
        <Button onClick={() => window.location.reload()}>
          Refresh Page
        </Button>
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
