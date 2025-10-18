import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, waitFor } from '@testing-library/react'
import { Provider } from 'react-redux'
import { configureStore } from '@reduxjs/toolkit'
import { AuthWrapper } from './AuthWrapper'
import authReducer from './authSlice'
import type { RootState } from '../store/store'

// Mock Auth0
const mockUseAuth0 = vi.hoisted(() => vi.fn())

vi.mock('@auth0/auth0-react', () => ({
  useAuth0: mockUseAuth0,
}))

describe('AuthWrapper', () => {
  let store: ReturnType<typeof configureStore>

  beforeEach(() => {
    vi.clearAllMocks()
    store = configureStore({
      reducer: {
        auth: authReducer,
      },
    })
  })

  it('should set user in Redux when authenticated', async () => {
    const mockUser = {
      sub: 'auth0|123',
      email: 'test@example.com',
      name: 'Test User',
    }

    const mockGetAccessTokenSilently = vi.fn().mockResolvedValue('mock-token')

    mockUseAuth0.mockReturnValue({
      isAuthenticated: true,
      isLoading: false,
      user: mockUser,
      getAccessTokenSilently: mockGetAccessTokenSilently,
    })

    render(
      <Provider store={store}>
        <AuthWrapper>
          <div>Child Component</div>
        </AuthWrapper>
      </Provider>
    )

    await waitFor(() => {
      const state = store.getState() as RootState
      expect(state.auth.user).toEqual(mockUser)
      expect(state.auth.token).toBe('mock-token')
      expect(state.auth.isAuthenticated).toBe(true)
    })
  })

  it('should clear user in Redux when not authenticated', async () => {
    mockUseAuth0.mockReturnValue({
      isAuthenticated: false,
      isLoading: false,
      user: undefined,
    })

    render(
      <Provider store={store}>
        <AuthWrapper>
          <div>Child Component</div>
        </AuthWrapper>
      </Provider>
    )

    await waitFor(() => {
      const state = store.getState() as RootState
      expect(state.auth.user).toBeNull()
      expect(state.auth.token).toBeNull()
      expect(state.auth.isAuthenticated).toBe(false)
    })
  })

  it('should handle token refresh errors gracefully', async () => {
    const mockUser = {
      sub: 'auth0|123',
      email: 'test@example.com',
    }

    const mockGetAccessTokenSilently = vi.fn().mockRejectedValue(new Error('Token refresh failed'))

    mockUseAuth0.mockReturnValue({
      isAuthenticated: true,
      isLoading: false,
      user: mockUser,
      getAccessTokenSilently: mockGetAccessTokenSilently,
    })

    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    render(
      <Provider store={store}>
        <AuthWrapper>
          <div>Child Component</div>
        </AuthWrapper>
      </Provider>
    )

    await waitFor(() => {
      expect(consoleSpy).toHaveBeenCalledWith(
        'Failed to get access token:',
        expect.any(Error)
      )
    })

    consoleSpy.mockRestore()
  })

  it('should render children', () => {
    mockUseAuth0.mockReturnValue({
      isAuthenticated: false,
      isLoading: false,
    })

    const { getByText } = render(
      <Provider store={store}>
        <AuthWrapper>
          <div>Child Component</div>
        </AuthWrapper>
      </Provider>
    )

    expect(getByText('Child Component')).toBeInTheDocument()
  })
})
