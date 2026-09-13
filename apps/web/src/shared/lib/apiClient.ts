import {
  configureApiClient,
  type GetTokenFunction,
} from '@boostedbudget/api-client/client'

interface WebRuntimeEnvironment {
  API_URL?: string
}

interface WebBuildEnvironment {
  VITE_API_URL?: string
  DEV?: boolean
}

interface ConfigureWebApiClientOptions {
  runtimeEnv?: WebRuntimeEnvironment
  buildEnv?: WebBuildEnvironment
  getAccessToken?: GetTokenFunction
  navigate?: (path: string) => void
}

/** Preserve the established web priority: runtime config, Vite config, proxy. */
export function getWebApiUrl(
  runtimeEnv: WebRuntimeEnvironment | undefined,
  buildEnv: WebBuildEnvironment,
): string {
  return runtimeEnv?.API_URL || buildEnv.VITE_API_URL || ''
}

/** Bind browser/Vite capabilities to the otherwise platform-neutral client. */
export function configureWebApiClient(options: ConfigureWebApiClientOptions = {}): void {
  const runtimeEnv = options.runtimeEnv ?? window.ENV
  const buildEnv = options.buildEnv ?? import.meta.env
  const navigate = options.navigate ?? ((path: string) => { window.location.href = path })

  configureApiClient({
    baseUrl: getWebApiUrl(runtimeEnv, buildEnv),
    getAccessToken: options.getAccessToken,
    // This app has no /sign-in route. Authentication is a Clerk modal hosted by
    // the landing page, matching ClerkProvider and ProtectedRoute behavior.
    onUnauthorized: () => navigate('/'),
    debug: buildEnv.DEV,
    debugLog: buildEnv.DEV
      ? (message, details) => console.error(message, details)
      : undefined,
  })
}
