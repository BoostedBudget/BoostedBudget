import { useState, useCallback } from 'react'
import { ADMIN_MODE_STORAGE_KEY } from '../constants'

export interface UseAdminModeResult {
  adminModeEnabled: boolean
  toggleAdminMode: () => void
  setAdminMode: (enabled: boolean) => void
}

export function useAdminMode(): UseAdminModeResult {
  const [enabled, setEnabled] = useState(() => {
    try {
      return localStorage.getItem(ADMIN_MODE_STORAGE_KEY) === 'true'
    } catch {
      return false
    }
  })

  const setAdminMode = useCallback((value: boolean) => {
    setEnabled(value)
    try {
      localStorage.setItem(ADMIN_MODE_STORAGE_KEY, String(value))
    } catch {
      // Ignore localStorage errors (e.g., private browsing mode)
    }
  }, [])

  const toggleAdminMode = useCallback(() => {
    setAdminMode(!enabled)
  }, [enabled, setAdminMode])

  return { adminModeEnabled: enabled, toggleAdminMode, setAdminMode }
}
