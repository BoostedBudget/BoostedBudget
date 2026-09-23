export interface MobilePublicEnvironment {
  EXPO_PUBLIC_API_URL?: string
  EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY?: string
}

export interface ParsedPublicEnvironment {
  apiUrl?: string
  clerkPublishableKey?: string
}

function parseApiUrl(value: string | undefined): string | undefined {
  if (!value) return undefined

  try {
    const url = new URL(value)
    const localHttp = url.protocol === 'http:' && ['localhost', '127.0.0.1'].includes(url.hostname)
    const secure = url.protocol === 'https:' || localHttp
    const originOnly = url.pathname === '/' && !url.search && !url.hash
    if (!secure || !originOnly || url.username || url.password) {
      return undefined
    }
    return url.origin
  } catch {
    return undefined
  }
}

// A Clerk publishable key is public by design, but only the `pk_test_`/`pk_live_` forms are
// publishable. Anything else (a secret key, a padded value, an unrelated string) is rejected so
// it can never reach the client bundle or a description. The raw value is never logged.
function parseClerkPublishableKey(value: string | undefined): string | undefined {
  if (!value) return undefined
  if (value.trim() !== value) return undefined
  if (!value.startsWith('pk_test_') && !value.startsWith('pk_live_')) return undefined
  return value
}

export function parsePublicEnvironment(environment: MobilePublicEnvironment): ParsedPublicEnvironment {
  return {
    apiUrl: parseApiUrl(environment.EXPO_PUBLIC_API_URL),
    clerkPublishableKey: parseClerkPublishableKey(environment.EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY),
  }
}

export function describePublicEnvironment(environment: MobilePublicEnvironment) {
  return {
    apiUrl: {
      present: Boolean(environment.EXPO_PUBLIC_API_URL),
      valid: Boolean(parseApiUrl(environment.EXPO_PUBLIC_API_URL)),
    },
    clerkPublishableKey: {
      present: Boolean(environment.EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY),
      valid: Boolean(parseClerkPublishableKey(environment.EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY)),
    },
  }
}
