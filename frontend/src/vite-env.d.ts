/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_URL?: string
  readonly VITE_ADSENSE_CLIENT_ID?: string
  readonly VITE_CLERK_PUBLISHABLE_KEY?: string
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
}

interface Window {
  ENV?: RuntimeEnv
}
