import { useMemo } from 'react'

export type Environment = 'development' | 'qa' | 'staging' | 'production'

export interface UseEnvironmentResult {
  environment: Environment
  isDevelopment: boolean
  isQA: boolean
  isStaging: boolean
  isProduction: boolean
}

function detectEnvironment(hostname: string): Environment {
  if (hostname === 'localhost' || hostname === '127.0.0.1' || hostname.endsWith('.local')) {
    return 'development'
  }
  if (hostname.startsWith('qa.')) return 'qa'
  if (hostname.startsWith('staging.')) return 'staging'
  return 'production'
}

export function useEnvironment(): UseEnvironmentResult {
  const environment = useMemo(() => {
    return detectEnvironment(window.location.hostname)
  }, [])

  return {
    environment,
    isDevelopment: environment === 'development',
    isQA: environment === 'qa',
    isStaging: environment === 'staging',
    isProduction: environment === 'production',
  }
}
