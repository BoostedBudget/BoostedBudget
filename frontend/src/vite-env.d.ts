/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_ADSENSE_CLIENT_ID?: string
  readonly VITE_AUTH0_DOMAIN?: string
  readonly VITE_AUTH0_CLIENT_ID?: string
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
  AUTH0_DOMAIN?: string
  AUTH0_CLIENT_ID?: string
  AUTH0_AUDIENCE?: string
  ENVIRONMENT?: string
}

interface Window {
  ENV?: RuntimeEnv
}
