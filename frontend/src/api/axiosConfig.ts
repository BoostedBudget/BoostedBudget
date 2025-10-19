import axios from 'axios'

type GetTokenFunction = () => Promise<string | null>

/**
 * Get the API URL from runtime or build-time configuration
 * Priority: window.ENV (runtime) > import.meta.env (build-time) > '/api' (fallback)
 * Exported for testing purposes
 */
export function getApiUrl(): string {
  // Runtime config (production) - injected by docker-entrypoint.sh
  if (window.ENV?.API_URL) {
    return window.ENV.API_URL
  }

  // Build-time config (development)
  if (import.meta.env.VITE_API_URL) {
    return import.meta.env.VITE_API_URL
  }

  // Fallback for local dev with Vite proxy
  return '/api'
}

// Create axios instance with base configuration
export const apiClient = axios.create({
  baseURL: getApiUrl(),
  headers: {
    'Content-Type': 'application/json',
  },
})

export const setupAuthInterceptor = (getToken: GetTokenFunction) => {
  // Add a request interceptor to apiClient (not global axios)
  apiClient.interceptors.request.use(
    async (config) => {
      try {
        const token = await getToken()
        if (token) {
          config.headers.Authorization = `Bearer ${token}`
        }
      } catch (error) {
        console.error('Error getting access token:', error)
      }
      return config
    },
    (error) => {
      return Promise.reject(error)
    }
  )
}
