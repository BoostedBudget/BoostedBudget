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

  it.each([
    'https://api.example.com/v1',
    'https://api.example.com?token=must-not-leak',
    'https://api.example.com#fragment',
    'http://api.example.com',
  ])('rejects API values that are not safe origins: %s', (value) => {
    const description = describePublicEnvironment({ EXPO_PUBLIC_API_URL: value })

    expect(parsePublicEnvironment({ EXPO_PUBLIC_API_URL: value }).apiUrl).toBeUndefined()
    expect(description).toEqual({ apiUrl: { present: true, valid: false } })
    expect(JSON.stringify(description)).not.toContain(value)
    expect(JSON.stringify(description)).not.toContain('must-not-leak')
  })

  it.each(['http://localhost:8080', 'http://127.0.0.1:8080'])('allows local HTTP development origin %s', (value) => {
    expect(parsePublicEnvironment({ EXPO_PUBLIC_API_URL: value })).toEqual({ apiUrl: value })
  })

  it('reports validity without including configured or rejected values', () => {
    const secretLikeValue = 'https://token-value@example.com'
    const description = describePublicEnvironment({ EXPO_PUBLIC_API_URL: secretLikeValue })

    expect(description).toEqual({ apiUrl: { present: true, valid: false } })
    expect(JSON.stringify(description)).not.toContain(secretLikeValue)
    expect(JSON.stringify(description)).not.toContain('token-value')
  })
})
