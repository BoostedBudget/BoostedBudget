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
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) {
      return undefined
    }
    return url.toString().replace(/\/$/, '')
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
