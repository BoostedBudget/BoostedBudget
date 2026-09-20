export type ThemePreference = 'light' | 'dark' | 'system'

export const THEME_STORAGE_KEY = 'bb-theme'

export function themeStorageKey(userId?: string): string {
  return userId === undefined ? THEME_STORAGE_KEY : `${THEME_STORAGE_KEY}:${userId}`
}

export function parseThemePreference(value: unknown): ThemePreference {
  return value === 'light' || value === 'dark' || value === 'system' ? value : 'system'
}

export function resolveTheme(
  preference: ThemePreference,
  systemScheme: string | null | undefined,
): 'light' | 'dark' {
  if (preference === 'system') {
    return systemScheme === 'dark' ? 'dark' : 'light'
  }
  return preference
}

export interface ThemeStorage {
  getItem(key: string): Promise<string | null>
}

export async function readThemePreference(
  storage: ThemeStorage,
  userId?: string,
): Promise<ThemePreference> {
  try {
    return parseThemePreference(await storage.getItem(themeStorageKey(userId)))
  } catch {
    return 'system'
  }
}