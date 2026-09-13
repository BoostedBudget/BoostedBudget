import { afterEach, describe, expect, test } from 'bun:test'
import { AXIOS_INSTANCE, configureApiClient, customInstance } from '@boostedbudget/api-client/client'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { configureWebApiClient, getWebApiUrl } from './apiClient'

const originalAdapter = AXIOS_INSTANCE.defaults.adapter

afterEach(() => {
  configureApiClient()
  AXIOS_INSTANCE.defaults.adapter = originalAdapter
})

describe('web API client adapter', () => {
  test('prefers runtime API configuration over Vite build configuration', () => {
    expect(getWebApiUrl(
      { API_URL: 'https://runtime.example.com/' },
      { VITE_API_URL: 'https://build.example.com' },
    )).toBe('https://runtime.example.com/')
  })

  test('uses Vite configuration and then the empty proxy fallback', () => {
    expect(getWebApiUrl(undefined, { VITE_API_URL: 'https://build.example.com' }))
      .toBe('https://build.example.com')
    expect(getWebApiUrl(undefined, {})).toBe('')
  })

  test('wires authenticated 401 responses to the landing-page auth flow', async () => {
    const navigations: string[] = []
    AXIOS_INSTANCE.defaults.adapter = async (config) => {
      const requestConfig = {
        ...config,
        headers: AxiosHeaders.from(config.headers),
      } as InternalAxiosRequestConfig
      throw new AxiosError('Unauthorized', undefined, requestConfig, undefined, {
        data: {},
        status: 401,
        statusText: 'Unauthorized',
        headers: {},
        config: requestConfig,
      })
    }
    configureWebApiClient({
      runtimeEnv: { API_URL: 'https://runtime.example.com' },
      buildEnv: { DEV: false },
      getAccessToken: async () => 'clerk-token',
      navigate: (path) => navigations.push(path),
    })

    await expect(customInstance({ url: '/api/v1/accounts' })).rejects.toBeInstanceOf(AxiosError)

    expect(navigations).toEqual(['/'])
  })
})
