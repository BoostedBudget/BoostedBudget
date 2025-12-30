import { useEffect } from 'react'
import { useAuth, useUser } from '@clerk/clerk-react'
import { useAppDispatch } from '../store/hooks'
import { setUser, clearUser } from './authSlice'

interface AuthWrapperProps {
  children: React.ReactNode
}

export const AuthWrapper: React.FC<AuthWrapperProps> = ({ children }) => {
  const { isSignedIn, getToken } = useAuth()
  const { user } = useUser()
  const dispatch = useAppDispatch()

  useEffect(() => {
    const syncAuthState = async () => {
      if (isSignedIn && user) {
        try {
          const token = await getToken()
          // Map Clerk user to our auth state format
          const mappedUser = {
            sub: user.id,
            email: user.primaryEmailAddress?.emailAddress,
            email_verified: user.primaryEmailAddress?.verification?.status === 'verified',
            name: user.fullName || undefined,
            picture: user.imageUrl,
            // Custom claims from publicMetadata
            subscription_tier: (user.publicMetadata?.subscription_tier as string) || 'free',
            primary_region: (user.publicMetadata?.primary_region as string) || 'iad',
          }
          dispatch(setUser({ user: mappedUser, token: token || undefined }))
        } catch (error) {
          console.error('Failed to get access token:', error)
        }
      } else {
        dispatch(clearUser())
      }
    }

    syncAuthState()
  }, [isSignedIn, user, getToken, dispatch])

  return <>{children}</>
}
