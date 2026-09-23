import { Platform } from 'react-native'

/**
 * The subset of Clerk's `TokenCache` the mobile app relies on. Declared locally so this
 * module never imports `@clerk/expo` at module scope.
 */
export interface MobileTokenCache {
  getToken(key: string): Promise<string | null>
  saveToken(key: string, token: string): Promise<void>
  clearToken?(key: string): void | Promise<void>
}

const isNativePlatform = Platform.OS !== 'web'

const persistedKeys: string[] = []

/**
 * Test-visible probe. Every key the mobile token cache has been asked to persist. A test
 * asserts none of these keys appear in AsyncStorage, proving the cache never routes a
 * token through plaintext storage.
 */
export function tokenCachePersistedKeys(): readonly string[] {
  return persistedKeys
}

/** Test-visible probe reset. */
export function resetTokenCacheProbe(): void {
  persistedKeys.length = 0
}

function wrapWithProbe(clerkCache: MobileTokenCache): MobileTokenCache {
  return {
    getToken: (key) => clerkCache.getToken(key),
    saveToken: async (key, token) => {
      persistedKeys.push(key)
      await clerkCache.saveToken(key, token)
    },
    clearToken: (key) => clerkCache.clearToken?.(key),
  }
}

/**
 * The encrypted, device-local token cache handed to `ClerkProvider`.
 *
 * On native it delegates to Clerk's `expo-secure-store`-backed cache; on web it is
 * `undefined` (Clerk keeps the session in memory). The `require` sits inside the native
 * branch so a web bundle never pulls `expo-secure-store` — a static import would execute
 * it during the static web export.
 */
export const mobileTokenCache: MobileTokenCache | undefined = isNativePlatform
  ? wrapWithProbe(
      (require('@clerk/expo/token-cache') as { tokenCache: MobileTokenCache }).tokenCache,
    )
  : undefined