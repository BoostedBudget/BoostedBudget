import { describePublicEnvironment, parsePublicEnvironment } from '../config/public-env'

describe('public mobile environment', () => {
  it('accepts an HTTPS API URL and exposes only the parsed URL to bootstrap code', () => {
    expect(parsePublicEnvironment({ EXPO_PUBLIC_API_URL: 'https://api.boostedbudget.com/' })).toEqual({
      apiUrl: 'https://api.boostedbudget.com',
    })
  })

  it('rejects missing, credential-bearing, and non-HTTP URLs', () => {
    expect(parsePublicEnvironment({}).apiUrl).toBeUndefined()
    expect(parsePublicEnvironment({ EXPO_PUBLIC_API_URL: 'ftp://example.com' }).apiUrl).toBeUndefined()
    expect(parsePublicEnvironment({ EXPO_PUBLIC_API_URL: 'https://user:secret@example.com' }).apiUrl).toBeUndefined()
  })

  it('reports validity without including configured or rejected values', () => {
    const secretLikeValue = 'https://token-value@example.com'
    const description = describePublicEnvironment({ EXPO_PUBLIC_API_URL: secretLikeValue })

    expect(description).toEqual({ apiUrl: { present: true, valid: false } })
    expect(JSON.stringify(description)).not.toContain(secretLikeValue)
    expect(JSON.stringify(description)).not.toContain('token-value')
  })
})
