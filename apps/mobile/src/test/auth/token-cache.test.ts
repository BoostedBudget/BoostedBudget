import { beforeEach, describe, expect, it, jest } from '@jest/globals'
import AsyncStorage from '@react-native-async-storage/async-storage'
import * as SecureStore from 'expo-secure-store'
import {
  mobileTokenCache,
  resetTokenCacheProbe,
  tokenCachePersistedKeys,
} from '../../auth/token-cache'

/**
 * jest-expo's default preset runs `defaultPlatform: 'ios'`, so the native branch is what
 * executes here. The web branch is exercised by re-requiring the module with `Platform.OS`
 * mocked to `'web'` and `expo-secure-store` mocked to throw, proving a web import never
 * pulls the native secure-store module.
 */

describe('mobileTokenCache on native', () => {
  beforeEach(() => {
    jest.clearAllMocks()
    resetTokenCacheProbe()
  })

  it('is defined and delegates saveToken to SecureStore', async () => {
    expect(mobileTokenCache).toBeDefined()

    await mobileTokenCache!.saveToken('clerk-token-key', 'secret-token')

    expect(SecureStore.setItemAsync).toHaveBeenCalledWith(
      'clerk-token-key',
      'secret-token',
      expect.anything(),
    )
  })

  it('round-trips a token through SecureStore', async () => {
    await mobileTokenCache!.saveToken('clerk-token-key', 'secret-token')

    await expect(mobileTokenCache!.getToken('clerk-token-key')).resolves.toBe('secret-token')
  })

  it('never writes a token key to AsyncStorage', async () => {
    await mobileTokenCache!.saveToken('clerk-token-key', 'secret-token')

    // The probe is live: it recorded the key the cache was asked to persist...
    expect(tokenCachePersistedKeys()).toContain('clerk-token-key')
    // ...and AsyncStorage never saw it.
    expect(AsyncStorage.setItem).not.toHaveBeenCalled()
  })

  it('resolves null after a cleanup delete when the SecureStore read rejects', async () => {
    jest.mocked(SecureStore.getItemAsync).mockRejectedValueOnce(new Error('keychain locked'))

    await expect(mobileTokenCache!.getToken('clerk-token-key')).resolves.toBeNull()
    expect(SecureStore.deleteItemAsync).toHaveBeenCalledWith(
      'clerk-token-key',
      expect.anything(),
    )
  })
})

describe('mobileTokenCache on web', () => {
  it('is undefined and never imports expo-secure-store', () => {
    jest.resetModules()
    jest.doMock('react-native', () => ({ Platform: { OS: 'web' } }))
    jest.doMock('expo-secure-store', () => {
      throw new Error('expo-secure-store must not be imported on web')
    })

    const web = require('../../auth/token-cache') as typeof import('../../auth/token-cache')

    expect(web.mobileTokenCache).toBeUndefined()
  })
})