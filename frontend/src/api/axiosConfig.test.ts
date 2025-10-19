import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import axios from 'axios'
import MockAdapter from 'axios-mock-adapter'
import { setupAuthInterceptor, apiClient, getApiUrl } from './axiosConfig'

describe('Axios Auth Interceptor', () => {
  let mock: MockAdapter

  beforeEach(() => {
    // Clear all interceptors before each test
    axios.interceptors.request.clear()
    mock = new MockAdapter(axios)
  })

  afterEach(() => {
    mock.restore()
    // Clear interceptors after each test
    axios.interceptors.request.clear()
  })

  it('should add Authorization header when token is provided', async () => {
    const mockGetToken = vi.fn().mockResolvedValue('test-token-123')
    setupAuthInterceptor(mockGetToken)

    mock.onGet('/api/test').reply((config) => {
      expect(config.headers?.Authorization).toBe('Bearer test-token-123')
      return [200, { success: true }]
    })

    await axios.get('/api/test')
    expect(mockGetToken).toHaveBeenCalled()
  })

  it('should not add Authorization header when token is null', async () => {
    const mockGetToken = vi.fn().mockResolvedValue(null)
    setupAuthInterceptor(mockGetToken)

    mock.onGet('/api/test').reply((config) => {
      expect(config.headers?.Authorization).toBeUndefined()
      return [200, { success: true }]
    })

    await axios.get('/api/test')
    expect(mockGetToken).toHaveBeenCalled()
  })

  it('should handle token retrieval errors gracefully', async () => {
    const mockGetToken = vi.fn().mockRejectedValue(new Error('Token error'))
    setupAuthInterceptor(mockGetToken)

    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    mock.onGet('/api/test').reply((config) => {
      expect(config.headers?.Authorization).toBeUndefined()
      return [200, { success: true }]
    })

    await axios.get('/api/test')

    expect(consoleSpy).toHaveBeenCalledWith(
      'Error getting access token:',
      expect.any(Error)
    )

    consoleSpy.mockRestore()
  })

  it('should call getToken for each request', async () => {
    const mockGetToken = vi.fn().mockResolvedValue('token-abc')
    setupAuthInterceptor(mockGetToken)

    mock.onGet('/api/test1').reply(200, {})
    mock.onGet('/api/test2').reply(200, {})

    await axios.get('/api/test1')
    await axios.get('/api/test2')

    expect(mockGetToken).toHaveBeenCalledTimes(2)
  })

  it('should not interfere with existing headers', async () => {
    const mockGetToken = vi.fn().mockResolvedValue('my-token')
    setupAuthInterceptor(mockGetToken)

    mock.onGet('/api/test').reply((config) => {
      expect(config.headers?.Authorization).toBe('Bearer my-token')
      expect(config.headers?.['X-Custom-Header']).toBe('custom-value')
      return [200, {}]
    })

    await axios.get('/api/test', {
      headers: {
        'X-Custom-Header': 'custom-value',
      },
    })
  })
})

describe('API URL Configuration (getApiUrl function)', () => {
  let originalWindowENV: typeof window.ENV
  let originalViteApiUrl: string | undefined

  beforeEach(() => {
    // Save original values
    originalWindowENV = window.ENV
    originalViteApiUrl = import.meta.env.VITE_API_URL
  })

  afterEach(() => {
    // Restore original values
    window.ENV = originalWindowENV
    if (originalViteApiUrl !== undefined) {
      import.meta.env.VITE_API_URL = originalViteApiUrl
    } else {
      delete import.meta.env.VITE_API_URL
    }
  })

  it('should use window.ENV.API_URL when available (runtime config)', () => {
    // Set runtime config
    window.ENV = {
      API_URL: 'https://api.boostedbudget.com',
    }

    const apiUrl = getApiUrl()

    expect(apiUrl).toBe('https://api.boostedbudget.com')
  })

  it('should fall back to VITE_API_URL when window.ENV not set', () => {
    // Clear runtime config
    window.ENV = undefined

    // Set build-time config
    import.meta.env.VITE_API_URL = 'http://localhost:8080'

    const apiUrl = getApiUrl()

    expect(apiUrl).toBe('http://localhost:8080')
  })

  it('should default to /api when no config found', () => {
    // Clear all config
    window.ENV = undefined
    delete import.meta.env.VITE_API_URL

    const apiUrl = getApiUrl()

    expect(apiUrl).toBe('/api')
  })

  it('should prefer window.ENV.API_URL over VITE_API_URL (runtime takes precedence)', () => {
    // Set both runtime and build-time config
    window.ENV = {
      API_URL: 'https://api.boostedbudget.com',
    }
    import.meta.env.VITE_API_URL = 'http://localhost:8080'

    const apiUrl = getApiUrl()

    // Runtime config should win
    expect(apiUrl).toBe('https://api.boostedbudget.com')
  })

  it('should handle empty string in window.ENV.API_URL', () => {
    // Set empty runtime config
    window.ENV = {
      API_URL: '',
    }
    import.meta.env.VITE_API_URL = 'http://localhost:8080'

    const apiUrl = getApiUrl()

    // Should fall back to VITE_API_URL when window.ENV.API_URL is empty
    expect(apiUrl).toBe('http://localhost:8080')
  })

  it('should handle window.ENV being defined but API_URL being undefined', () => {
    // Set window.ENV but without API_URL
    window.ENV = {}
    import.meta.env.VITE_API_URL = 'http://localhost:8080'

    const apiUrl = getApiUrl()

    // Should fall back to VITE_API_URL
    expect(apiUrl).toBe('http://localhost:8080')
  })
})

describe('apiClient instance', () => {
  it('should be created with a baseURL', () => {
    expect(apiClient.defaults.baseURL).toBeDefined()
    expect(typeof apiClient.defaults.baseURL).toBe('string')
  })

  it('should have Content-Type header set to application/json', () => {
    expect(apiClient.defaults.headers['Content-Type']).toBe('application/json')
  })
})
