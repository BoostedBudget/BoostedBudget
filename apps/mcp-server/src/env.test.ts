import { describe, expect, it } from 'bun:test'
import { parseEnv } from './env'

describe('parseEnv', () => {
  const validEnv = {
    CLERK_SECRET_KEY: 'sk_test_abc123',
    CLERK_PUBLISHABLE_KEY: 'pk_test_abc123',
    CLERK_JWKS_URL: 'https://clerk.example.com/.well-known/jwks.json',
    BOOSTEDBUDGET_API_URL: 'https://api.boostedbudget.com',
    BOOSTEDBUDGET_ORIGIN_SECRET: 'super-secret-origin-value',
    MCP_RESOURCE_URL: 'https://mcp.boostedbudget.com',
    ENVIRONMENT: 'production',
  }

  it('parses a valid environment without throwing', () => {
    const result = parseEnv(validEnv)
    expect(result.CLERK_SECRET_KEY).toBe('sk_test_abc123')
    expect(result.BOOSTEDBUDGET_API_URL).toBe('https://api.boostedbudget.com')
    expect(result.ENVIRONMENT).toBe('production')
  })

  it('accepts staging environment', () => {
    const result = parseEnv({ ...validEnv, ENVIRONMENT: 'staging' })
    expect(result.ENVIRONMENT).toBe('staging')
  })

  it('accepts development environment', () => {
    const result = parseEnv({ ...validEnv, ENVIRONMENT: 'development' })
    expect(result.ENVIRONMENT).toBe('development')
  })

  it('throws on missing CLERK_SECRET_KEY', () => {
    const { CLERK_SECRET_KEY: _, ...rest } = validEnv
    expect(() => parseEnv(rest)).toThrow()
  })

  it('throws on missing BOOSTEDBUDGET_ORIGIN_SECRET', () => {
    const { BOOSTEDBUDGET_ORIGIN_SECRET: _, ...rest } = validEnv
    expect(() => parseEnv(rest)).toThrow()
  })

  it('throws on missing BOOSTEDBUDGET_API_URL', () => {
    const { BOOSTEDBUDGET_API_URL: _, ...rest } = validEnv
    expect(() => parseEnv(rest)).toThrow()
  })

  it('throws on invalid ENVIRONMENT value', () => {
    expect(() => parseEnv({ ...validEnv, ENVIRONMENT: 'production_bad' })).toThrow()
  })

  it('throws on non-https API URL in production', () => {
    expect(() =>
      parseEnv({ ...validEnv, BOOSTEDBUDGET_API_URL: 'http://api.boostedbudget.com' }),
    ).toThrow()
  })

  it('allows http URL in development', () => {
    const result = parseEnv({
      ...validEnv,
      ENVIRONMENT: 'development',
      BOOSTEDBUDGET_API_URL: 'http://localhost:8080',
    })
    expect(result.BOOSTEDBUDGET_API_URL).toBe('http://localhost:8080')
  })
})
