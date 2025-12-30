import { Navigate, useLocation } from 'react-router-dom'
import { useAuth } from '@clerk/react-router'

interface ProtectedRouteProps {
  children: React.ReactNode
}

export function ProtectedRoute({ children }: ProtectedRouteProps) {
  const { isSignedIn, isLoaded } = useAuth()
  const location = useLocation()

  if (!isLoaded) {
    return (
      <div className="flex justify-center items-center min-h-screen">
        <p>Loading...</p>
      </div>
    )
  }

  if (!isSignedIn) {
    // Redirect to home, preserving intended destination
    return <Navigate to="/" state={{ from: location }} replace />
  }

  return <>{children}</>
}
