import { describe, expect, it } from 'bun:test'
import { readFileSync } from 'node:fs'
import { assertFixtureProcessAlive, fixtureDatabase, fixturePsqlEnvironment, fixtureRegistrationSQL, fixtureHeartbeatSQL, verifyRevenueCatFixtureCompletion } from './support/revenuecat-e2e-fixture'

const configuration = { E2E_REVENUECAT_FIXTURE_MODE: 'simulated', DATABASE_URL: 'postgres://test:test@127.0.0.1:15200/test?sslmode=disable' }
describe('RevenueCat E2E fixture safety', () => {
  it('checks final database invariants even when the fixture process is still alive', () => {
    const calls: string[] = []
    verifyRevenueCatFixtureCompletion('12345', () => { calls.push('alive') }, () => { calls.push('state') })
    expect(calls).toEqual(['alive', 'state'])
    expect(() => verifyRevenueCatFixtureCompletion('12345', () => {}, () => { throw new Error('changed after last heartbeat') })).toThrow('changed after last heartbeat')
  })
  it('finishes destructive fixture regressions before starting the managed heartbeat', () => {
    const config = readFileSync(new URL('../../playwright.config.ts', import.meta.url), 'utf8')
    expect(config).toContain("command: 'bun run test:revenuecat-fixture && bun src/test/support/revenuecat-e2e-fixture.ts'")
    expect(config).not.toContain('revenuecat-fixture.setup.ts')
  })
  it('fails teardown for a missing, invalid, or terminated fixture process', () => {
    const calls: number[] = []
    const probe = (pid: number) => { calls.push(pid) }
    for (const value of [undefined, '', '-1', '0', '1.5', 'not-a-pid']) {
      expect(() => assertFixtureProcessAlive(value, probe)).toThrow()
    }
    expect(calls).toEqual([])
    assertFixtureProcessAlive('12345', probe)
    expect(calls).toEqual([12345])
    expect(() => assertFixtureProcessAlive('12345', () => { throw new Error('process ended') })).toThrow()
  })
  it('uses explicit libpq fields and removes inherited service selection', () => {
    const connection = fixturePsqlEnvironment({...configuration, PGSERVICE: 'unrelated-service'})
    expect(connection.PGHOST).toBe('127.0.0.1')
    expect(connection.PGPORT).toBe('15200')
    expect(connection.PGDATABASE).toBe('test')
    expect(connection).not.toHaveProperty('PGSERVICE')
  })
  it('requires explicit simulation against only the local disposable test database', () => {
    expect(fixtureDatabase(configuration)).toBe(configuration.DATABASE_URL)
    for (const overrides of [
      { E2E_REVENUECAT_FIXTURE_MODE: '' },
      { DATABASE_URL: 'postgres://test:test@example.com/test' },
      { DATABASE_URL: 'postgres://test:test@localhost/boostedbudget' },
      { DATABASE_URL: 'postgres://test:test@localhost/freshcheck' },
      { DATABASE_URL: 'postgres://devuser:devpass@localhost/test' },
      { DATABASE_URL: 'postgres://test:test@localhost/test?options=-csearch_path=public' },
      { REVENUECAT_SECRET_API_KEY: 'configured' },
    ]) expect(() => fixtureDatabase({...configuration, ...overrides})).toThrow()
  })
  it('registers only an exact validated UUID without replacing existing confirmed grants', () => {
    expect(() => fixtureRegistrationSQL("x'; DROP TABLE users;--")).toThrow()
    const sql = fixtureRegistrationSQL('11111111-1111-4111-8111-111111111111')
    expect(sql).toContain("'11111111-1111-4111-8111-111111111111'::uuid")
    expect(sql).toContain('access_updated_at IS NULL')
    expect(sql).toContain("grants = '[]'::jsonb")
    expect(sql).toContain('erasure_started_at IS NULL')
    expect(sql).toContain('erasure_confirmed_at IS NULL')
  })
  it('advances current observation time only for registered, unchanged, unfenced fixtures', () => {
    const sql = fixtureHeartbeatSQL()
    expect(sql).toContain('clock_timestamp()')
    expect(sql).toContain('c.grants = f.expected_grants')
    expect(sql).toContain('c.erasure_started_at IS NULL')
    expect(sql).toContain('c.erasure_confirmed_at IS NULL')
    expect(sql).toContain('NOT u.is_deleted')
    expect(sql).not.toMatch(/SET\s+grants\s*=/i)
    expect(sql).not.toMatch(/SET\s+access_error\s*=/i)
    expect(sql).not.toContain('revenuecat_operations')
    // Registration may commit during a heartbeat; both counts need one MVCC snapshot.
    expect(sql).toContain('WITH refreshed_rows AS (UPDATE')
    expect(sql).toContain('(SELECT count(*) FROM refreshed_rows) INTO expected, refreshed')
  })
})
