/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_URL?: string
  readonly VITE_CLERK_PUBLISHABLE_KEY?: string
  readonly VITE_POSTHOG_KEY?: string
  readonly VITE_POSTHOG_HOST?: string
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
}

interface Window {
  ENV?: RuntimeEnv
}
