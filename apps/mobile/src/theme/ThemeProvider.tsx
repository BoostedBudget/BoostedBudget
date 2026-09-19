import AsyncStorage from '@react-native-async-storage/async-storage'
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { useColorScheme } from 'react-native'
import {
  readThemePreference,
  resolveTheme,
  themeStorageKey,
  type ThemePreference,
} from './preference'

export interface ThemeContextValue {
  preference: ThemePreference
  resolvedTheme: 'light' | 'dark'
  setPreference: (preference: ThemePreference) => void
  hydrated: boolean
}

const ThemeContext = createContext<ThemeContextValue | undefined>(undefined)

export interface ThemeProviderProps {
  children: ReactNode
  userId?: string
}

export function ThemeProvider({ children, userId }: ThemeProviderProps) {
  const systemScheme = useColorScheme()
  const [preference, setPreferenceState] = useState<ThemePreference>('system')
  const [hydrated, setHydrated] = useState(false)
  const storageKey = themeStorageKey(userId)

  useEffect(() => {
    let active = true

    readThemePreference(AsyncStorage, userId).then((stored) => {
      if (!active) return
      setPreferenceState(stored)
      setHydrated(true)
    })

    return () => {
      active = false
    }
  }, [userId])

  const setPreference = useCallback(
    (next: ThemePreference) => {
      setPreferenceState(next)
      AsyncStorage.setItem(storageKey, next).catch(() => undefined)
    },
    [storageKey],
  )

  const resolvedTheme = resolveTheme(preference, systemScheme)
  const value = useMemo(
    () => ({ preference, resolvedTheme, setPreference, hydrated }),
    [preference, resolvedTheme, setPreference, hydrated],
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme(): ThemeContextValue {
  const context = useContext(ThemeContext)
  if (!context) {
    throw new Error('useTheme must be used within a ThemeProvider')
  }
  return context
}