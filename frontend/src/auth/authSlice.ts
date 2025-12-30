import { createSlice, PayloadAction } from '@reduxjs/toolkit'

// Define our own User interface (not dependent on Auth0/Clerk types)
export interface User {
  sub: string
  email?: string
  email_verified?: boolean
  name?: string
  picture?: string
  nickname?: string
  // Custom claims from Clerk publicMetadata
  subscription_tier?: string
  primary_region?: string
  [key: string]: unknown
}

interface AuthState {
  user: User | null
  token: string | null
  isAuthenticated: boolean
}

const initialState: AuthState = {
  user: null,
  token: null,
  isAuthenticated: false,
}

const authSlice = createSlice({
  name: 'auth',
  initialState,
  reducers: {
    setUser: (state, action: PayloadAction<{ user: User; token?: string | null }>) => {
      state.user = action.payload.user
      state.token = action.payload.token || null
      state.isAuthenticated = true
    },
    clearUser: (state) => {
      state.user = null
      state.token = null
      state.isAuthenticated = false
    },
  },
})

export const { setUser, clearUser } = authSlice.actions
export default authSlice.reducer
