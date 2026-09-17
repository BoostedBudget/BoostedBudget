import { describe, expect, it } from 'bun:test'
import { privacySettingsQueryKey } from './privacyQueryKey'

describe('privacySettingsQueryKey', () => {
  it('isolates privacy cache entries by Clerk identity', () => {
    expect(privacySettingsQueryKey('user-a')).toEqual(['/api/v1/user/privacy', 'user-a'])
    expect(privacySettingsQueryKey('user-b')).toEqual(['/api/v1/user/privacy', 'user-b'])
    expect(privacySettingsQueryKey('user-a')).not.toEqual(privacySettingsQueryKey('user-b'))
  })
})
