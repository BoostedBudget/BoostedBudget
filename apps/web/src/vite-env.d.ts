/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_URL?: string
  readonly VITE_CLERK_PUBLISHABLE_KEY?: string
  readonly VITE_ENVIRONMENT?: string
  readonly VITE_SENTRY_DSN?: string
  readonly VITE_POSTHOG_KEY?: string
  readonly VITE_POSTHOG_HOST?: string
  readonly VITE_MCP_URL?: string
  readonly VITE_REVENUECAT_PUBLIC_API_KEY?: string
  readonly DEV: boolean
  readonly PROD: boolean
  readonly MODE: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

// Runtime configuration injected by docker-entrypoint.sh
interface RuntimeEnv {
  API_URL?: string
  CLERK_PUBLISHABLE_KEY?: string
  ENVIRONMENT?: string
  SENTRY_DSN?: string
  POSTHOG_KEY?: string
  POSTHOG_HOST?: string
  MCP_URL?: string
  REVENUECAT_PUBLIC_API_KEY?: string
}

interface Window {
  ENV?: RuntimeEnv
}
