import axios from 'axios'

type GetTokenFunction = () => Promise<string | null>

export const setupAuthInterceptor = (getToken: GetTokenFunction) => {
  // Add a request interceptor to add the Authorization header
  axios.interceptors.request.use(
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

// Create axios instance with base configuration
export const apiClient = axios.create({
  baseURL: import.meta.env.VITE_API_URL || '/api',
  headers: {
    'Content-Type': 'application/json',
  },
})
