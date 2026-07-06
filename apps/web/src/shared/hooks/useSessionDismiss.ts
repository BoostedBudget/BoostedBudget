import { useState, useCallback } from 'react'

const SESSION_KEY_PREFIX = 'bb_sub_banner_dismissed'

const getSessionStorageFlag = (key: string): boolean => {
  try {
    return typeof window !== 'undefined' && sessionStorage.getItem(key) === 'true'
  } catch {
    return false
  }
}

const setSessionStorageFlag = (key: string): void => {
  try {
    sessionStorage.setItem(key, 'true')
  } catch {
    // Ignore storage errors (incognito mode, quota exceeded)
  }
}

export function useSessionDismiss(
  userId: string,
  status: string
): { isDismissed: boolean; dismiss: () => void } {
  const key = `${SESSION_KEY_PREFIX}_${userId}_${status}`
  const [isDismissed, setIsDismissed] = useState(() => getSessionStorageFlag(key))

  const dismiss = useCallback(() => {
    setSessionStorageFlag(key)
    setIsDismissed(true)
  }, [key])

  return { isDismissed, dismiss }
}
