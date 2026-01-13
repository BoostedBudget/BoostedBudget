import { configureStore } from '@reduxjs/toolkit'

// Note: Auth state is now managed by Clerk hooks, not Redux
// This store is kept for other app state (add reducers here as needed)
export const store = configureStore({
  reducer: {},
})

export type RootState = ReturnType<typeof store.getState>
export type AppDispatch = typeof store.dispatch
