export interface MobilePublicEnvironment {
  EXPO_PUBLIC_API_URL?: string
}

export interface ParsedPublicEnvironment {
  apiUrl?: string
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

export function parsePublicEnvironment(environment: MobilePublicEnvironment): ParsedPublicEnvironment {
  return { apiUrl: parseApiUrl(environment.EXPO_PUBLIC_API_URL) }
}

export function describePublicEnvironment(environment: MobilePublicEnvironment) {
  return {
    apiUrl: {
      present: Boolean(environment.EXPO_PUBLIC_API_URL),
      valid: Boolean(parseApiUrl(environment.EXPO_PUBLIC_API_URL)),
    },
  }
}
