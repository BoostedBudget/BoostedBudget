import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, waitFor } from '@testing-library/react'
import { Provider } from 'react-redux'
import { configureStore } from '@reduxjs/toolkit'
import { AuthWrapper } from './AuthWrapper'
import authReducer from './authSlice'
import type { RootState } from '../store/store'

// Mock Clerk
const mockUseAuth = vi.hoisted(() => vi.fn())
const mockUseUser = vi.hoisted(() => vi.fn())

vi.mock('@clerk/clerk-react', () => ({
  useAuth: mockUseAuth,
  useUser: mockUseUser,
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
    const mockClerkUser = {
      id: 'user_123',
      primaryEmailAddress: {
        emailAddress: 'test@example.com',
        verification: { status: 'verified' },
      },
      fullName: 'Test User',
      imageUrl: 'https://example.com/avatar.jpg',
      publicMetadata: {
        subscription_tier: 'free',
        primary_region: 'iad',
      },
    }

    const mockGetToken = vi.fn().mockResolvedValue('mock-token')

    mockUseAuth.mockReturnValue({
      isSignedIn: true,
      getToken: mockGetToken,
    })

    mockUseUser.mockReturnValue({
      user: mockClerkUser,
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
      expect(state.auth.user).toEqual({
        sub: 'user_123',
        email: 'test@example.com',
        email_verified: true,
        name: 'Test User',
        picture: 'https://example.com/avatar.jpg',
        subscription_tier: 'free',
        primary_region: 'iad',
      })
      expect(state.auth.token).toBe('mock-token')
      expect(state.auth.isAuthenticated).toBe(true)
    })
  })

  it('should clear user in Redux when not authenticated', async () => {
    mockUseAuth.mockReturnValue({
      isSignedIn: false,
      getToken: vi.fn(),
    })

    mockUseUser.mockReturnValue({
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
    const mockClerkUser = {
      id: 'user_123',
      primaryEmailAddress: {
        emailAddress: 'test@example.com',
        verification: { status: 'verified' },
      },
      fullName: null,
      imageUrl: null,
      publicMetadata: {},
    }

    const mockGetToken = vi.fn().mockRejectedValue(new Error('Token refresh failed'))

    mockUseAuth.mockReturnValue({
      isSignedIn: true,
      getToken: mockGetToken,
    })

    mockUseUser.mockReturnValue({
      user: mockClerkUser,
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
    mockUseAuth.mockReturnValue({
      isSignedIn: false,
      getToken: vi.fn(),
    })

    mockUseUser.mockReturnValue({
      user: undefined,
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
