import { afterEach, beforeAll, describe, expect, it } from 'bun:test'
import { execFileSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { fixturePsqlEnvironment, initializeRevenueCatFixture, registerRevenueCatFixture, fixtureHeartbeatSQL, verifyRevenueCatFixtureCompletion } from '../../src/test/support/revenuecat-e2e-fixture'

function query(sql: string): string {
  return execFileSync('psql', ['-X', '-v', 'ON_ERROR_STOP=1', '-Atq'], {
    input: sql, env: fixturePsqlEnvironment(), encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'],
  }).trim()
}
const ids: string[] = []
function user(): string {
  const id = randomUUID()
  ids.push(id)
  query(`INSERT INTO users (id, clerk_user_id, email, primary_region, subscription_tier, subscription_status)
    VALUES ('${id}', 'fixture_${id}', '${id}@fixture.invalid', 'iad', 'premium', 'active');`)
  return id
}

describe('RevenueCat fixture against disposable PostgreSQL', () => {
  beforeAll(() => initializeRevenueCatFixture())
  afterEach(() => {
    for (const id of ids.splice(0)) query(`DELETE FROM users WHERE id = '${id}';`)
  })
  it('initializes an unobserved projection even after a failed provider read encrypted its empty billing', () => {
    const id = user()
    query(`INSERT INTO revenuecat_customers(user_id, billing, access_error) VALUES ('${id}', 'encrypted-empty-observation', 'provider_unavailable');`)
    registerRevenueCatFixture(id)
    expect(query(`SELECT grants->0->>'key' FROM revenuecat_customers WHERE user_id='${id}';`)).toBe('comp_premium')
    registerRevenueCatFixture(id)
    query(fixtureHeartbeatSQL())
    expect(query(`SELECT access_updated_at > clock_timestamp() - interval '5 seconds' FROM revenuecat_customers WHERE user_id='${id}';`)).toBe('t')
  })
  it('refuses an existing confirmed projection and either erasure fence', () => {
    for (const column of ['access_updated_at', 'erasure_started_at', 'erasure_confirmed_at']) {
      const id = user()
      query(`INSERT INTO revenuecat_customers(user_id, ${column}) VALUES ('${id}', clock_timestamp());`)
      expect(() => registerRevenueCatFixture(id)).toThrow()
      expect(query(`SELECT grants::text FROM revenuecat_customers WHERE user_id='${id}';`)).toBe('[]')
    }
  })
  it('never repairs a changed grant or error during freshness maintenance', () => {
    const id = user()
    registerRevenueCatFixture(id)
    query(`UPDATE revenuecat_customers SET access_error='fixture-error' WHERE user_id='${id}';`)
    expect(() => query(fixtureHeartbeatSQL())).toThrow()
    expect(() => verifyRevenueCatFixtureCompletion('12345', () => {})).toThrow()
    expect(query(`SELECT access_error FROM revenuecat_customers WHERE user_id='${id}';`)).toBe('fixture-error')
    query(`UPDATE revenuecat_customers SET access_error='',grants='[]' WHERE user_id='${id}';`)
    expect(() => query(fixtureHeartbeatSQL())).toThrow()
    expect(query(`SELECT grants::text FROM revenuecat_customers WHERE user_id='${id}';`)).toBe('[]')
  })
})
