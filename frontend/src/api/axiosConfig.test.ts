import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import axios from 'axios'
import MockAdapter from 'axios-mock-adapter'
import { setupAuthInterceptor } from './axiosConfig'

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
