import { useEffect } from 'react'
import { useAuth0 } from '@auth0/auth0-react'
import { useAppDispatch } from '../store/hooks'
import { setUser, clearUser } from './authSlice'

interface AuthWrapperProps {
  children: React.ReactNode
}

export const AuthWrapper: React.FC<AuthWrapperProps> = ({ children }) => {
  const { isAuthenticated, user, getAccessTokenSilently } = useAuth0()
  const dispatch = useAppDispatch()

  useEffect(() => {
    const syncAuthState = async () => {
      if (isAuthenticated && user) {
        try {
          const token = await getAccessTokenSilently()
          dispatch(setUser({ user, token }))
        } catch (error) {
          console.error('Failed to get access token:', error)
        }
      } else {
        dispatch(clearUser())
      }
    }

    syncAuthState()
  }, [isAuthenticated, user, getAccessTokenSilently, dispatch])

  return <>{children}</>
}
