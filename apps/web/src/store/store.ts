import { configureStore, createSlice } from '@reduxjs/toolkit'

// Placeholder reducer to satisfy Redux requirements
// Add actual reducers here as needed
const placeholderSlice = createSlice({
  name: '_placeholder',
  initialState: {},
  reducers: {},
})

// Note: Auth state is now managed by Clerk hooks, not Redux
// This store is kept for other app state (add reducers here as needed)
export const store = configureStore({
  reducer: {
    _placeholder: placeholderSlice.reducer,
  },
})

export type RootState = ReturnType<typeof store.getState>
export type AppDispatch = typeof store.dispatch
